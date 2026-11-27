package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"
)

func openAIModelsEndpoint(baseURL string) string {
	b := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(b, "/chat/completions") {
		b = strings.TrimSuffix(b, "/chat/completions")
	} else if strings.HasSuffix(b, "/completions") {
		b = strings.TrimSuffix(b, "/completions")
	} else if strings.HasSuffix(b, "/responses") {
		b = strings.TrimSuffix(b, "/responses")
	}
	return b + "/models"
}

func (g *Gateway) pickKeyForModelProbe(providerID string) string {
	if sel := g.pool.SelectKey(providerID, map[string]bool{}); sel != nil && sel.Key != "" {
		return sel.Key
	}
	for _, kv := range g.pool.KeyViews() {
		if kv.Provider == providerID && kv.Enabled {
			if snap, ok := g.pool.GetKeySnapshot(kv.ID); ok && snap.Key != "" {
				return snap.Key
			}
		}
	}
	return ""
}

func (g *Gateway) fetchUpstreamModels(ctx context.Context, prov ProviderView) ([]discoveredUpstreamModel, int, int64, string) {
	rawKey := g.pickKeyForModelProbe(prov.ID)
	if rawKey == "" {
		return nil, 0, 0, "tidak ada API key aktif"
	}
	tCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	start := time.Now()
	base := strings.TrimRight(strings.TrimSpace(prov.BaseURL), "/")
	endpoint := openAIModelsEndpoint(base)
	if prov.APIType == apiTypeGemini {
		endpoint = base + "/models?pageSize=100"
	}
	httpReq, err := http.NewRequestWithContext(tCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, time.Since(start).Milliseconds(), sanitizeError(err.Error(), rawKey)
	}
	finalURL := applyUpstreamAuth(httpReq, endpoint, prov.AuthMode, rawKey)
	if finalURL != endpoint {
		if u, uErr := neturl.Parse(finalURL); uErr == nil {
			httpReq.URL = u
		}
	}
	resp, err := g.httpClient.Do(httpReq)
	lat := time.Since(start).Milliseconds()
	if err != nil {
		return nil, 0, lat, sanitizeError(err.Error(), rawKey)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, lat, sanitizeError(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(raw)), rawKey)
	}
	var out []discoveredUpstreamModel
	if prov.APIType == apiTypeGemini {
		var gem struct {
			Models []struct {
				Name                       string   `json:"name"`
				BaseModelID                string   `json:"baseModelId"`
				DisplayName                string   `json:"displayName"`
				Description                string   `json:"description"`
				InputTokenLimit            int      `json:"inputTokenLimit"`
				OutputTokenLimit           int      `json:"outputTokenLimit"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
		}
		if err := json.Unmarshal(raw, &gem); err != nil {
			return nil, resp.StatusCode, lat, "format JSON Gemini /models tidak valid"
		}
		for _, m := range gem.Models {
			id := strings.TrimSpace(strings.TrimPrefix(m.Name, "models/"))
			if id == "" {
				id = strings.TrimSpace(m.BaseModelID)
			}
			if id == "" {
				continue
			}
			disp := strings.TrimSpace(m.DisplayName)
			if disp == "" {
				disp = id
			}
			out = append(out, discoveredUpstreamModel{
				ID:               id,
				DisplayName:      disp,
				Description:      strings.TrimSpace(m.Description),
				OwnedBy:          prov.Name,
				InputTokenLimit:  m.InputTokenLimit,
				OutputTokenLimit: m.OutputTokenLimit,
				ContextLength:    m.InputTokenLimit,
				Methods:          m.SupportedGenerationMethods,
			})
		}
		return out, resp.StatusCode, lat, ""
	}
	var openai struct {
		Data []struct {
			ID               string `json:"id"`
			OwnedBy          string `json:"owned_by"`
			ContextLength    int    `json:"context_length"`
			ContextWindow    int    `json:"context_window"`
			MaxModelLen      int    `json:"max_model_len"`
			InputTokenLimit  int    `json:"input_token_limit"`
			OutputTokenLimit int    `json:"output_token_limit"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &openai); err != nil {
		return nil, resp.StatusCode, lat, "format JSON OpenAI /models tidak valid"
	}
	for _, m := range openai.Data {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		ctxLen := m.ContextLength
		if ctxLen <= 0 {
			ctxLen = m.ContextWindow
		}
		if ctxLen <= 0 {
			ctxLen = m.MaxModelLen
		}
		owner := strings.TrimSpace(m.OwnedBy)
		if owner == "" {
			owner = prov.Name
		}
		out = append(out, discoveredUpstreamModel{
			ID:               id,
			DisplayName:      id,
			OwnedBy:          owner,
			InputTokenLimit:  m.InputTokenLimit,
			OutputTokenLimit: m.OutputTokenLimit,
			ContextLength:    ctxLen,
			Methods:          []string{"chat.completions"},
		})
	}
	return out, resp.StatusCode, lat, ""
}

func (g *Gateway) buildModelsCatalog(ctx context.Context, scopeFilter, providerFilter string, probe bool) ([]map[string]any, []map[string]any) {
	allProtocols := []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1beta/models"}
	sc := strings.ToLower(strings.TrimSpace(scopeFilter))
	pf := strings.ToLower(strings.TrimSpace(providerFilter))
	if sc == "" {
		sc = scopeGlobal
	}
	isCat := strings.HasPrefix(sc, scopeCategoryPrefix)
	catFilter := strings.TrimPrefix(sc, scopeCategoryPrefix)

	var matching []ProviderView
	for _, pv := range g.pool.ProviderViews() {
		if pf != "" && pf != "all" && pv.ID != pf {
			continue
		}
		if sc != "all" && sc != scopeGlobal {
			if isCat && normalizeCategory(pv.Category) != catFilter {
				continue
			}
			if !isCat && pv.ID != sc {
				continue
			}
		}
		matching = append(matching, pv)
	}

	type probeResult struct {
		models  []discoveredUpstreamModel
		status  int
		latency int64
		errMsg  string
		ok      bool
	}
	probeRes := make([]probeResult, len(matching))
	if probe && len(matching) > 0 {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 5)
		for i, pv := range matching {
			if !pv.Enabled {
				probeRes[i] = probeResult{errMsg: "provider nonaktif"}
				continue
			}
			wg.Add(1)
			go func(idx int, prov ProviderView) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				mods, st, lat, errMsg := g.fetchUpstreamModels(ctx, prov)
				probeRes[idx] = probeResult{
					models:  mods,
					status:  st,
					latency: lat,
					errMsg:  errMsg,
					ok:      errMsg == "" && st == http.StatusOK,
				}
			}(i, pv)
		}
		wg.Wait()
	}

	provSummaries := make([]map[string]any, 0, len(matching))
	modelEntries := make([]map[string]any, 0, len(matching)*4)
	seenComposite := map[string]bool{}

	if pf == "" || pf == "all" {
		if sc == "all" || sc == scopeGlobal || isCat {
			modelEntries = append(modelEntries,
				map[string]any{
					"id":            "automodel",
					"routed_id":     "automodel",
					"display_name":  "Auto Model (Rotasi Otomatis)",
					"description":   "Rotasi otomatis ke semua provider & model siap pakai sesuai pengaturan.",
					"owned_by":      "karouter",
					"provider_id":   "karouter",
					"provider_name": "KaRouter",
					"provider_icon": "globe",
					"category":      "router",
					"api_type":      "gateway",
					"is_default":    true,
					"source":        "virtual",
					"status":        "ready",
					"methods":       []string{"chat.completions", "responses", "messages", "generateContent"},
					"protocols":     allProtocols,
				},
			)
		}
	}

	for i, pv := range matching {
		pr := probeRes[i]
		provStatus := "ready"
		if !pv.Enabled {
			provStatus = "disabled"
		} else if pv.EligibleKeys == 0 {
			provStatus = "no_key"
		}
		provSummaries = append(provSummaries, map[string]any{
			"id":               pv.ID,
			"name":             pv.Name,
			"icon":             pv.Icon,
			"category":         pv.Category,
			"api_type":         pv.APIType,
			"base_url":         pv.BaseURL,
			"default_model":    pv.DefaultModel,
			"enabled":          pv.Enabled,
			"eligible_keys":    pv.EligibleKeys,
			"total_keys":       pv.TotalKeys,
			"probed":           probe,
			"probe_ok":         pr.ok,
			"probe_status":     pr.status,
			"probe_latency_ms": pr.latency,
			"probe_count":      len(pr.models),
			"probe_error":      pr.errMsg,
		})

		appendItem := func(id, display, desc, owner, source string, inTok, outTok, ctxLen int, methods []string) {
			key := pv.ID + "\x00" + strings.ToLower(id)
			if id == "" || seenComposite[key] {
				return
			}
			seenComposite[key] = true
			if display == "" {
				display = id
			}
			if owner == "" {
				owner = pv.Name
			}
			if len(methods) == 0 {
				if pv.APIType == apiTypeGemini {
					methods = []string{"generateContent", "streamGenerateContent"}
				} else {
					methods = []string{"chat.completions"}
				}
			}
			modelEntries = append(modelEntries, map[string]any{
				"id":                 id,
				"routed_id":          pv.ID + "/" + id,
				"display_name":       display,
				"description":        desc,
				"owned_by":           owner,
				"provider_id":        pv.ID,
				"provider_name":      pv.Name,
				"provider_icon":      pv.Icon,
				"category":           pv.Category,
				"api_type":           pv.APIType,
				"is_default":         strings.EqualFold(id, pv.DefaultModel),
				"source":             source,
				"status":             provStatus,
				"input_token_limit":  inTok,
				"output_token_limit": outTok,
				"context_length":     ctxLen,
				"methods":            methods,
				"protocols":          allProtocols,
			})
		}

		if pv.DefaultModel != "" {
			appendItem(pv.DefaultModel, pv.DefaultModel, "Model default terkonfigurasi untuk provider "+pv.Name, pv.Name, "configured", 0, 0, 0, nil)
		}
		for _, rm := range pv.RotationModels {
			appendItem(rm, rm, "Model rotasi aktif untuk provider "+pv.Name, pv.Name, "rotation", 0, 0, 0, nil)
		}
		for _, m := range pr.models {
			appendItem(m.ID, m.DisplayName, m.Description, m.OwnedBy, "upstream", m.InputTokenLimit, m.OutputTokenLimit, m.ContextLength, m.Methods)
		}
	}
	return provSummaries, modelEntries
}

func (g *Gateway) handleAdminModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	q := r.URL.Query()
	scope := strings.TrimSpace(q.Get("scope"))
	if scope == "" {
		scope = "all"
	}
	provider := strings.TrimSpace(q.Get("provider"))
	probe := q.Get("probe") == "1" || strings.EqualFold(q.Get("probe"), "true")
	providers, models := g.buildModelsCatalog(r.Context(), scope, provider, probe)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":           true,
		"scope":        scope,
		"probed":       probe,
		"generated_at": time.Now().Unix(),
		"providers":    providers,
		"models":       models,
	})
}

func (g *Gateway) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	client, ok := g.pool.AuthenticateClientToken(extractClientToken(r))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	now := time.Now().Unix()
	probe := r.URL.Query().Get("probe") == "1" || strings.EqualFold(r.URL.Query().Get("probe"), "true")
	_, items := g.buildModelsCatalog(r.Context(), client.Scope, "", probe)
	var models []map[string]any
	seen := map[string]bool{}
	for _, item := range items {
		if st, _ := item["status"].(string); st == "disabled" {
			continue
		}
		id, _ := item["id"].(string)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		owner, _ := item["owned_by"].(string)
		models = append(models, map[string]any{
			"id":          id,
			"routed_id":   item["routed_id"],
			"object":      "model",
			"created":     now,
			"owned_by":    owner,
			"provider":    item["provider_id"],
			"category":    item["category"],
			"is_default":  item["is_default"],
			"source":      item["source"],
			"description": item["description"],
			"protocols":   item["protocols"],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": models})
}
