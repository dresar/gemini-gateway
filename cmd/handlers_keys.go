package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func (g *Gateway) testKeyAgainstProvider(ctx context.Context, prov Provider, rawKey, model string) map[string]any {
	s := g.pool.Settings()
	tCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	start := time.Now()
	explicitModel := strings.TrimSpace(model) != ""
	if strings.TrimSpace(model) == "" {
		model = prov.DefaultModel
	}
	testReq := &OpenAIRequest{
		Messages:  []OpenAIMessage{{Role: "user", Content: "Reply with only: OK"}},
		MaxTokens: 256,
	}
	var resp *OpenAIResponse
	var err error
	testedModel := model
	if prov.APIType == apiTypeGemini {
		gemReq := g.buildGeminiReq(testReq, s)
		resp, err = g.callGeminiOnce(tCtx, &prov, rawKey, testedModel, gemReq)
		if err != nil && !explicitModel && isModelUnavailable404(err) {
			for _, fbModel := range geminiFallbackModels(testedModel) {
				fbResp, fbErr := g.callGeminiOnce(tCtx, &prov, rawKey, fbModel, gemReq)
				if fbErr == nil {
					resp = fbResp
					err = nil
					testedModel = fbModel
					break
				}
				if !isModelUnavailable404(fbErr) {
					err = fbErr
					testedModel = fbModel
					break
				}
			}
		}
	} else {
		resp, err = g.callOpenAIOnce(tCtx, &prov, rawKey, testedModel, testReq, s)
	}
	lat := time.Since(start).Milliseconds()
	if err != nil {
		status := 502
		var ue *upstreamError
		if errors.As(err, &ue) && ue.Status > 0 {
			status = ue.Status
		}
		rawErr := sanitizeError(err.Error(), rawKey)
		errCode, label, detail := classifyUpstreamError(status, rawErr)
		if detail == "" {
			detail = label
		}
		return map[string]any{
			"ok":         false,
			"provider":   prov.ID,
			"model":      testedModel,
			"status":     status,
			"latency_ms": lat,
			"error_code": errCode,
			"raw_error":  rawErr,
			"error":      sanitizeError(fmt.Sprintf("%s (%d): %s", label, status, detail), rawKey),
		}
	}
	reply := ""
	if len(resp.Choices) > 0 {
		reply = extractTextContent(resp.Choices[0].Message.Content)
	}
	return map[string]any{
		"ok":         true,
		"provider":   prov.ID,
		"model":      testedModel,
		"status":     200,
		"latency_ms": lat,
		"tokens":     resp.Usage.TotalTokens,
		"reply":      strings.TrimSpace(reply),
	}
}

func (g *Gateway) testSingleKey(ctx context.Context, key KeyEntry) map[string]any {
	prov, ok := g.pool.GetProvider(key.Provider)
	if !ok {
		return map[string]any{"ok": false, "id": key.ID, "status": 404, "error": "provider missing"}
	}
	res := g.testKeyAgainstProvider(ctx, prov, key.Key, "")
	res["id"] = key.ID
	if okBool, _ := res["ok"].(bool); okBool {
		lat, _ := res["latency_ms"].(int64)
		tok, _ := res["tokens"].(int)
		g.pool.Mark200(key.ID, lat, 1, tok)
	} else {
		st, _ := res["status"].(int)
		rawErr, _ := res["raw_error"].(string)
		if rawErr == "" {
			rawErr, _ = res["error"].(string)
		}
		g.handleUpstreamFailure(&key, &upstreamError{Status: st, Message: rawErr})
	}
	return res
}

func (g *Gateway) handleKeysRoot(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"keys": g.pool.KeyViews()})
	case http.MethodPost:
		var body struct {
			Provider    string         `json:"provider"`
			Keys        []string       `json:"keys"`
			RawText     string         `json:"raw_text"`
			LabelPrefix string         `json:"label_prefix"`
			Items       []KeyItemInput `json:"items"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		items := parseKeyItemLines(body.RawText, body.Keys, body.Items)
		if len(items) == 0 {
			writeJSONError(w, http.StatusBadRequest, "keys list is empty", "invalid_request_error")
			return
		}
		res, err := g.pool.AddKeyItemsValidated(body.Provider, items, body.LabelPrefix)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                  true,
			"added":               res.Added,
			"duplicates":          res.Duplicates,
			"pool_duplicates":     res.PoolDuplicates,
			"batch_duplicates":    res.BatchDuplicates,
			"invalid":             res.Invalid,
			"cross_duplicates":    res.CrossDuplicates,
			"total_provider_keys": res.Total,
			"invalid_items":       res.InvalidItems,
			"warnings":            res.Warnings,
		})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (g *Gateway) handleKeyTestRaw(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var body struct {
		ProviderID string `json:"provider_id"`
		APIKey     string `json:"api_key"`
		Model      string `json:"model"`
		BaseURL    string `json:"base_url"`
		APIType    string `json:"api_type"`
		AuthMode   string `json:"auth_mode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	rawKey := strings.TrimSpace(body.APIKey)
	if rawKey == "" {
		writeJSONError(w, http.StatusBadRequest, "api_key wajib diisi", "invalid_request_error")
		return
	}
	var prov Provider
	if body.ProviderID != "" {
		found, ok := g.pool.GetProvider(body.ProviderID)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "provider tidak ditemukan", "invalid_request_error")
			return
		}
		prov = found
	} else {
		prov = Provider{
			ID:           "custom",
			Name:         "Custom",
			APIType:      body.APIType,
			AuthMode:     body.AuthMode,
			BaseURL:      body.BaseURL,
			DefaultModel: body.Model,
			Enabled:      true,
			Weight:       50,
			Priority:     1,
		}
		if err := normalizeProvider(&prov); err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
	}
	if body.BaseURL != "" {
		prov.BaseURL = strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	}
	if body.APIType != "" {
		prov.APIType = body.APIType
	}
	if body.AuthMode != "" {
		prov.AuthMode = body.AuthMode
	}
	res := g.testKeyAgainstProvider(r.Context(), prov, rawKey, body.Model)
	writeJSON(w, http.StatusOK, res)
}

func (g *Gateway) handleKeysBulk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var body struct {
		Action   string `json:"action"`
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	if body.Action == "test_all" {
		views := g.pool.KeyViews()
		var targets []KeyEntry
		for _, kv := range views {
			if body.Provider != "" && body.Provider != "all" && kv.Provider != body.Provider {
				continue
			}
			if !kv.Enabled {
				continue
			}
			if snap, ok := g.pool.GetKeySnapshot(kv.ID); ok {
				targets = append(targets, snap)
			}
		}
		var okCount, failCount atomic.Int64
		sem := make(chan struct{}, 5)
		var wg sync.WaitGroup
		for _, snap := range targets {
			wg.Add(1)
			go func(k KeyEntry) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				res := g.testSingleKey(r.Context(), k)
				if b, _ := res["ok"].(bool); b {
					okCount.Add(1)
				} else {
					failCount.Add(1)
				}
			}(snap)
		}
		wg.Wait()
		g.pool.FlushPending()
		passed := int(okCount.Load())
		failed := int(failCount.Load())
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       true,
			"tested":   passed + failed,
			"passed":   passed,
			"failed":   failed,
			"affected": passed + failed,
		})
		return
	}
	affected, err := g.pool.BulkAction(body.Action, body.Provider)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
		return
	}
	out := map[string]any{"ok": true, "affected": affected}
	if body.Action == "dedupe_all" || body.Action == "purge_invalid" || body.Action == "auto_clean" {
		out["audit"] = g.pool.AuditKeys(body.Provider)
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *Gateway) handleKeyByID(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/keys/")
	if rest == "" {
		writeJSONError(w, http.StatusNotFound, "not found", "invalid_request_error")
		return
	}
	if strings.HasSuffix(rest, "/test") {
		if r.Method != http.MethodPost {
			writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
			return
		}
		id := strings.TrimSuffix(rest, "/test")
		snap, ok := g.pool.GetKeySnapshot(id)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "key not found", "invalid_request_error")
			return
		}
		res := g.testSingleKey(r.Context(), snap)
		g.pool.FlushPending()
		writeJSON(w, http.StatusOK, res)
		return
	}
	id := rest
	switch r.Method {
	case http.MethodPatch:
		var body struct {
			Enabled *bool   `json:"enabled"`
			Label   *string `json:"label"`
			Action  string  `json:"action"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		kv, found, err := g.pool.PatchKey(id, body.Enabled, body.Label, body.Action)
		if !found {
			writeJSONError(w, http.StatusNotFound, "key not found", "invalid_request_error")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error(), "api_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": kv})
	case http.MethodDelete:
		found, err := g.pool.DeleteKey(id)
		if !found {
			writeJSONError(w, http.StatusNotFound, "key not found", "invalid_request_error")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error(), "api_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": true, "id": id})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (g *Gateway) handleClientKeysRoot(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"client_keys": g.pool.ClientKeyViews()})
	case http.MethodPost:
		var body struct {
			Name      string `json:"name"`
			Scope     string `json:"scope"`
			CustomKey string `json:"custom_key"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		ck, secret, err := g.pool.CreateClientKey(body.Name, body.Scope, body.CustomKey)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":     true,
			"key":    ck,
			"secret": secret,
		})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (g *Gateway) handleClientKeyByID(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/client-keys/")
	if id == "" {
		writeJSONError(w, http.StatusNotFound, "not found", "invalid_request_error")
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var body struct {
			Enabled *bool   `json:"enabled"`
			Name    *string `json:"name"`
			Scope   *string `json:"scope"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		ck, found, err := g.pool.PatchClientKey(id, body.Enabled, body.Name, body.Scope)
		if !found {
			writeJSONError(w, http.StatusNotFound, "client key not found", "invalid_request_error")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": ck})
	case http.MethodDelete:
		found, err := g.pool.DeleteClientKey(id)
		if !found {
			writeJSONError(w, http.StatusNotFound, "client key not found", "invalid_request_error")
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, err.Error(), "api_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "deleted": true, "id": id})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}
