package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, msg, errType string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"message": msg,
			"type":    errType,
		},
	})
}

func (g *Gateway) resolveAPKPath() string {
	candidates := make([]string, 0, 10)
	if p := strings.TrimSpace(g.cfg.APKFile); p != "" {
		candidates = append(candidates, p)
	}
	if p := strings.TrimSpace(g.pool.GetMetaValue("apk_file_path")); p != "" {
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		candidates = append(candidates,
			"karouter.apk",
			filepath.Join("..", "karouter.apk"),
			filepath.Join("mobile", "app", "build", "outputs", "apk", "release", "app-release.apk"),
			filepath.Join("mobile", "app", "build", "outputs", "apk", "debug", "app-debug.apk"),
		)
		if exePath, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exePath)
			candidates = append(candidates,
				filepath.Join(exeDir, "karouter.apk"),
				filepath.Join(exeDir, "..", "karouter.apk"),
			)
		}
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() && info.Size() > 0 {
			return c
		}
	}
	return ""
}

func (g *Gateway) apkMeta() map[string]any {
	p := g.resolveAPKPath()
	if p == "" {
		return map[string]any{
			"available":  false,
			"filename":   "karouter.apk",
			"size_bytes": int64(0),
		}
	}
	info, err := os.Stat(p)
	if err != nil || info.IsDir() || info.Size() <= 0 {
		return map[string]any{
			"available":  false,
			"filename":   "karouter.apk",
			"size_bytes": int64(0),
		}
	}
	return map[string]any{
		"available":  true,
		"filename":   "karouter.apk",
		"size_bytes": info.Size(),
	}
}

func (g *Gateway) handleDownloadAPK(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	p := g.resolveAPKPath()
	if p == "" {
		writeJSONError(w, http.StatusNotFound, "APK belum tersedia di server", "not_found")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "APK belum tersedia di server", "not_found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() <= 0 {
		writeJSONError(w, http.StatusNotFound, "APK belum tersedia di server", "not_found")
		return
	}
	w.Header().Set("Content-Type", "application/vnd.android.package-archive")
	w.Header().Set("Content-Disposition", `attachment; filename="karouter.apk"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "karouter.apk", info.ModTime(), f)
}

func (g *Gateway) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if g.cfg.UIFile != "" {
		if raw, err := os.ReadFile(g.cfg.UIFile); err == nil {
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodGet {
				_, _ = w.Write(raw)
			}
			return
		}
	}
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(g.uiHTMLGz) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = w.Write(g.uiHTMLGz)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(g.uiHTML)
	}
}

func (g *Gateway) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"name":       "karouter",
		"version":    gatewayVersion,
		"uptime_sec": time.Now().Unix() - g.startedAt,
		"storage":    "sqlite-wal",
		"pool":       g.pool.HealthSummary(),
	})
}

func (g *Gateway) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	s := g.pool.Settings()
	avgLat, avgRoute := g.pool.AvgStats()
	stats := RuntimeStats{
		TotalRequests:  g.reqTotal.Load(),
		TotalSuccess:   g.reqOK.Load(),
		TotalErrors:    g.reqErr.Load(),
		TotalRetries:   g.reqRetry.Load(),
		TotalFallbacks: g.reqFall.Load(),
		TotalTokens:    g.tokTotal.Load(),
		AvgLatencyMs:   avgLat,
		AvgRouteUs:     avgRoute,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":       "karouter",
		"version":    gatewayVersion,
		"uptime_sec": time.Now().Unix() - g.startedAt,
		"started_at": g.startedAt,
		"storage":    "sqlite-wal",
		"db_path":    g.cfg.DBPath,
		"stats":      stats,
		"pool":       g.pool.HealthSummary(),
		"config": map[string]any{
			"rotation_mode":            s.RotationMode,
			"model_routing_policy":     s.ModelRoutingPolicy,
			"key_selection":            s.KeySelection,
			"cross_fallback":           s.CrossFallback,
			"max_retries_per_provider": s.MaxRetriesPerProvider,
			"cooldown_base_sec":        s.CooldownBaseSec,
			"default_max_tokens":       s.DefaultMaxTokens,
			"request_timeout_sec":      s.RequestTimeoutSec,
			"host":                     g.cfg.Host,
			"port":                     g.cfg.Port,
			"db_path":                  g.cfg.DBPath,
		},
		"backup":      g.pool.BackupSnapshotConfig(),
		"apk":         g.apkMeta(),
		"providers":   g.pool.ProviderViews(),
		"keys":        g.pool.KeyViews(),
		"client_keys": g.pool.ClientKeyViews(),
		"logs":        g.pool.RecentLogs(),
	})
}

func (g *Gateway) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut && r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	cur := g.pool.Settings()
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cur); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid config json", "invalid_request_error")
		return
	}
	updated, err := g.pool.UpdateSettings(cur)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error(), "api_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "config": updated})
}

func (g *Gateway) handleProvidersRoot(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"providers": g.pool.ProviderViews()})
	case http.MethodPost:
		var pr Provider
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&pr); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		created, err := g.pool.CreateProvider(pr)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": created})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (g *Gateway) handleProviderByID(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/providers/")
	if id == "" {
		writeJSONError(w, http.StatusNotFound, "not found", "invalid_request_error")
		return
	}
	switch r.Method {
	case http.MethodPut, http.MethodPatch:
		cur, ok := g.pool.GetProvider(id)
		if !ok {
			writeJSONError(w, http.StatusNotFound, "provider not found", "invalid_request_error")
			return
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&cur); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		updated, _, err := g.pool.UpdateProvider(id, cur)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": updated})
	case http.MethodDelete:
		found, err := g.pool.DeleteProvider(id)
		if !found {
			writeJSONError(w, http.StatusNotFound, "provider not found", "invalid_request_error")
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

func (g *Gateway) handleBackupConfig(w http.ResponseWriter, r *http.Request) {
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]any{"backup": g.pool.BackupSnapshotConfig()})
	case http.MethodPut, http.MethodPost:
		var body struct {
			NeonURL      *string `json:"neon_url"`
			AutoBackup   *bool   `json:"auto_backup"`
			IntervalDays *int    `json:"interval_days"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
			return
		}
		updated, err := g.pool.UpdateBackupConfig(body.NeonURL, body.AutoBackup, body.IntervalDays)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error(), "invalid_request_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "backup": updated})
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
	}
}

func (g *Gateway) handleBackupTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var body struct {
		NeonURL string `json:"neon_url"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	if err := g.TestNeonConnection(r.Context(), body.NeonURL); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (g *Gateway) handleBackupRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	meta, err := g.BackupToNeon(r.Context(), "manual")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), "api_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "snapshot": meta, "backup": g.pool.BackupSnapshotConfig()})
}

func (g *Gateway) handleBackupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	list, err := g.ListNeonBackups(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), "api_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "backups": list})
}

func (g *Gateway) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var body struct {
		BackupID int64 `json:"backup_id"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)
	meta, err := g.RestoreFromNeon(r.Context(), body.BackupID)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error(), "api_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "restored": meta})
}

func (g *Gateway) handleChatTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	if !g.adminAuthorized(r) {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var body struct {
		Target       string `json:"target"`
		Model        string `json:"model"`
		SystemPrompt string `json:"system_prompt"`
		Prompt       string `json:"prompt"`
		MaxTokens    int    `json:"max_tokens"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		prompt = "Jawab singkat dalam 1 kalimat: halo, apakah koneksi aktif?"
	}
	maxTok := body.MaxTokens
	if maxTok <= 0 {
		maxTok = 256
	}
	var msgs []OpenAIMessage
	if strings.TrimSpace(body.SystemPrompt) != "" {
		msgs = append(msgs, OpenAIMessage{Role: "system", Content: strings.TrimSpace(body.SystemPrompt)})
	}
	msgs = append(msgs, OpenAIMessage{Role: "user", Content: prompt})

	forced := strings.TrimSpace(body.Target)
	if forced == "auto" {
		forced = ""
	}
	model := strings.TrimSpace(body.Model)
	if model == "" {
		model = "auto"
	}
	req := &OpenAIRequest{
		Model:     model,
		Messages:  msgs,
		MaxTokens: maxTok,
	}
	testClient := ClientKey{Name: "playground", Scope: scopeGlobal, Enabled: true}
	resp, trace, err := g.executeRoutedChat(r.Context(), nil, req, testClient, forced)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":       false,
			"attempts": trace.Attempts,
			"error":    err.Error(),
		})
		return
	}
	reply := ""
	if len(resp.Choices) > 0 {
		reply = extractTextContent(resp.Choices[0].Message.Content)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"provider":      trace.Provider,
		"provider_name": trace.ProviderName,
		"key_index":     trace.KeyIndex,
		"key_masked":    trace.KeyMasked,
		"model":         trace.Model,
		"route_us":      trace.RouteUs,
		"latency_ms":    trace.LatencyMs,
		"tokens":        trace.Tokens,
		"attempts":      trace.Attempts,
		"fallback":      trace.Fallback,
		"reply":         reply,
	})
}
