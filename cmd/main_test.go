package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func newTestGateway(t *testing.T, gemKeys, agnKeys []string, gemHandler, agnHandler http.HandlerFunc) (*Gateway, string, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "gateway.db")
	gemFile := filepath.Join(dir, "keys.pool")
	agnFile := filepath.Join(dir, "agnes_keys.pool")

	if len(gemKeys) > 0 {
		if err := os.WriteFile(gemFile, []byte(strings.Join(gemKeys, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if len(agnKeys) > 0 {
		if err := os.WriteFile(agnFile, []byte(strings.Join(agnKeys, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var gemSrv, agnSrv *httptest.Server
	gemURL := "http://127.0.0.1:0"
	agnURL := "http://127.0.0.1:0"
	if gemHandler != nil {
		gemSrv = httptest.NewServer(gemHandler)
		gemURL = gemSrv.URL
	}
	if agnHandler != nil {
		agnSrv = httptest.NewServer(agnHandler)
		agnURL = agnSrv.URL
	}

	cfg := Config{
		Host:             "127.0.0.1",
		Port:             "0",
		DBPath:           dbPath,
		LegacyGatewayKey: "test-gw-secret",
		NeonIntervalDays: 7,
		GeminiModel:      "gemini-2.5-flash",
		GeminiBaseURL:    gemURL,
		AgnesModel:       "agnes-2.5-flash",
		AgnesBaseURL:     agnURL,
		LegacyGemKeys:    gemFile,
		LegacyAgnKeys:    agnFile,
		LegacyStateFile:  filepath.Join(dir, "router_state.json"),
		LegacyPoolState:  filepath.Join(dir, "pool_state.json"),
	}
	gw, err := newGateway(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		gw.pool.FlushPending()
		_ = gw.pool.Close()
		if gemSrv != nil {
			gemSrv.Close()
		}
		if agnSrv != nil {
			agnSrv.Close()
		}
	}
	return gw, dbPath, cleanup
}

func loginCookie(t *testing.T, h http.Handler, password string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"`+password+`"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	t.Fatalf("missing session cookie")
	return nil
}

func TestDynamicCustomProviderRotationAndRawKeyTest(t *testing.T) {
	var customHits atomic.Int32
	customSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "custom_secret_xyz123" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid x-api-key"}}`))
			return
		}
		customHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"custom ok"},"finish_reason":"stop"}],"usage":{"total_tokens":7}}`))
	}))
	defer customSrv.Close()

	gw, _, cleanup := newTestGateway(
		t,
		[]string{"AIzaSyTestGemini001"},
		[]string{"ag_test_001"},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"gem ok"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":6}}`))
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"agn-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"agn ok"},"finish_reason":"stop"}],"usage":{"total_tokens":6}}`))
		},
	)
	defer cleanup()

	h := gw.handler()
	cookie := loginCookie(t, h, defaultAdminPass)

	createProvBody := `{
		"name": "My Groq Node",
		"api_type": "openai",
		"auth_mode": "x-api-key",
		"base_url": "` + customSrv.URL + `",
		"default_model": "llama-3.3-70b",
		"icon": "groq",
		"enabled": true,
		"weight": 50,
		"priority": 3
	}`
	reqProv := httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(createProvBody))
	reqProv.AddCookie(cookie)
	recProv := httptest.NewRecorder()
	h.ServeHTTP(recProv, reqProv)
	if recProv.Code != http.StatusOK {
		t.Fatalf("create provider failed: %d %s", recProv.Code, recProv.Body.String())
	}
	var provOut struct {
		Provider Provider `json:"provider"`
	}
	_ = json.Unmarshal(recProv.Body.Bytes(), &provOut)
	if provOut.Provider.ID != "my-groq-node" {
		t.Fatalf("unexpected slug id: %q", provOut.Provider.ID)
	}

	rawTestReq := httptest.NewRequest(http.MethodPost, "/api/keys/test-raw", strings.NewReader(`{"provider_id":"my-groq-node","api_key":"custom_secret_xyz123"}`))
	rawTestReq.AddCookie(cookie)
	rawTestRec := httptest.NewRecorder()
	h.ServeHTTP(rawTestRec, rawTestReq)
	if rawTestRec.Code != http.StatusOK || !strings.Contains(rawTestRec.Body.String(), `"ok":true`) {
		t.Fatalf("raw key test failed: %d %s", rawTestRec.Code, rawTestRec.Body.String())
	}

	addKeyReq := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"provider":"my-groq-node","raw_text":"custom_secret_xyz123"}`))
	addKeyReq.AddCookie(cookie)
	addKeyRec := httptest.NewRecorder()
	h.ServeHTTP(addKeyRec, addKeyReq)
	if addKeyRec.Code != http.StatusOK {
		t.Fatalf("add key failed: %d %s", addKeyRec.Code, addKeyRec.Body.String())
	}

	ckReq := httptest.NewRequest(http.MethodPost, "/api/client-keys", strings.NewReader(`{"name":"Groq App","scope":"my-groq-node"}`))
	ckReq.AddCookie(cookie)
	ckRec := httptest.NewRecorder()
	h.ServeHTTP(ckRec, ckReq)
	var ckOut struct {
		Secret string `json:"secret"`
	}
	_ = json.Unmarshal(ckRec.Body.Bytes(), &ckOut)

	for i := 0; i < 2; i++ {
		chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}]}`))
		chatReq.Header.Set("Authorization", "Bearer "+ckOut.Secret)
		chatRec := httptest.NewRecorder()
		h.ServeHTTP(chatRec, chatReq)
		if chatRec.Code != http.StatusOK || chatRec.Header().Get("X-Gateway-Provider") != "my-groq-node" {
			t.Fatalf("expected my-groq-node, got %q (%d)", chatRec.Header().Get("X-Gateway-Provider"), chatRec.Code)
		}
	}

	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		chatReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}]}`))
		chatReq.Header.Set("Authorization", "Bearer test-gw-secret")
		chatRec := httptest.NewRecorder()
		h.ServeHTTP(chatRec, chatReq)
		if chatRec.Code != http.StatusOK {
			t.Fatalf("global req %d failed: %d %s", i, chatRec.Code, chatRec.Body.String())
		}
		seen[chatRec.Header().Get("X-Gateway-Provider")]++
	}
	if seen["gemini"] == 0 || seen["agnes"] == 0 || seen["my-groq-node"] == 0 {
		t.Fatalf("expected all 3 providers to receive traffic, got %v", seen)
	}
	if customHits.Load() < 5 {
		t.Fatalf("expected >= 5 custom hits, got %d", customHits.Load())
	}
}

func TestBackupSnapshotExportRestoreAndNeonURLMasking(t *testing.T) {
	gw, _, cleanup := newTestGateway(t, []string{"AIzaSyBackupGem001"}, []string{"ag_backup_001"}, nil, nil)
	defer cleanup()

	h := gw.handler()
	cookie := loginCookie(t, h, defaultAdminPass)

	upReq := httptest.NewRequest(http.MethodPut, "/api/backup/config", strings.NewReader(`{"neon_url":"postgresql://myuser:supersecretpass@ep-test.aws.neon.tech/neondb?sslmode=require","auto_backup":true,"interval_days":7}`))
	upReq.AddCookie(cookie)
	upRec := httptest.NewRecorder()
	h.ServeHTTP(upRec, upReq)
	if upRec.Code != http.StatusOK {
		t.Fatalf("update backup cfg failed: %d %s", upRec.Code, upRec.Body.String())
	}
	if strings.Contains(upRec.Body.String(), "supersecretpass") {
		t.Fatalf("neon password leaked in /api/backup/config response")
	}
	if !strings.Contains(upRec.Body.String(), "myuser:••••@ep-test.aws.neon.tech") {
		t.Fatalf("expected masked neon URL in response, got %s", upRec.Body.String())
	}

	snap, rawURL := gw.pool.exportSnapshot()
	if rawURL == "" || len(snap.Providers) < 2 || len(snap.ProviderKeys) != 2 {
		t.Fatalf("unexpected snapshot: providers=%d keys=%d", len(snap.Providers), len(snap.ProviderKeys))
	}

	_, _ = gw.pool.DeleteKey(snap.ProviderKeys[0].ID)
	if len(gw.pool.KeyViews()) != 1 {
		t.Fatalf("expected 1 key after delete")
	}
	if err := gw.pool.applyBackupSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	if len(gw.pool.KeyViews()) != 2 {
		t.Fatalf("expected 2 keys restored from snapshot, got %d", len(gw.pool.KeyViews()))
	}
}

func TestAdminLoginPasswordChangeAndNoSecretLeak(t *testing.T) {
	gw, _, cleanup := newTestGateway(t, []string{"AIzaSySecretKeyNeverLeak1234"}, nil, nil, nil)
	defer cleanup()

	h := gw.handler()

	unauth := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	recUnauth := httptest.NewRecorder()
	h.ServeHTTP(recUnauth, unauth)
	if recUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session, got %d", recUnauth.Code)
	}

	cookie := loginCookie(t, h, "280219")

	statusReq := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	statusReq.AddCookie(cookie)
	recStatus := httptest.NewRecorder()
	h.ServeHTTP(recStatus, statusReq)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("expected 200 status with cookie, got %d", recStatus.Code)
	}
	if strings.Contains(recStatus.Body.String(), "AIzaSySecretKeyNeverLeak1234") {
		t.Fatalf("raw API key leaked in /api/status response")
	}
	if strings.Contains(recStatus.Body.String(), "280219") {
		t.Fatalf("default password leaked in /api/status response")
	}

	addBody := bytes.NewBufferString(`{"provider":"agnes","raw_text":"ag_secret_11111111\nag_secret_22222222","label_prefix":"Agnes"}`)
	addReq := httptest.NewRequest(http.MethodPost, "/api/keys", addBody)
	addReq.AddCookie(cookie)
	recAdd := httptest.NewRecorder()
	h.ServeHTTP(recAdd, addReq)
	if recAdd.Code != http.StatusOK {
		t.Fatalf("add keys failed: %d %s", recAdd.Code, recAdd.Body.String())
	}

	uiReq := httptest.NewRequest(http.MethodGet, "/", nil)
	recUI := httptest.NewRecorder()
	h.ServeHTTP(recUI, uiReq)
	bodyBytes, _ := io.ReadAll(recUI.Body)
	if strings.Contains(string(bodyBytes), "280219") {
		t.Fatalf("default password must NEVER appear in embedded UI HTML")
	}
}

func TestFiveConsecutiveFailuresMarkDeadAndDetailedLogs(t *testing.T) {
	gw, _, cleanup := newTestGateway(t, nil, []string{"ag_fail_key_001"}, nil, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = w.Write([]byte(`{"error":{"message":"Your credit balance is too low to access the API"}}`))
	})
	defer cleanup()

	keys := gw.pool.KeyViews()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	keyID := keys[0].ID

	for strike := 1; strike <= 4; strike++ {
		deadNow, code, msg := gw.pool.MarkFail(keyID, http.StatusPaymentRequired, 0, `{"error":{"message":"Your credit balance is too low"}}`)
		if deadNow {
			t.Fatalf("key must NOT die before 5th strike (strike %d)", strike)
		}
		if code != "credit_exhausted" || !strings.Contains(msg, "Kredit") {
			t.Fatalf("expected credit_exhausted msg, got code=%q msg=%q", code, msg)
		}
		kv := gw.pool.KeyViews()[0]
		if kv.Status == "dead" || kv.ConsecFails != strike {
			t.Fatalf("strike %d: expected cooldown consec=%d, got status=%s consec=%d", strike, strike, kv.Status, kv.ConsecFails)
		}
	}

	reqFail := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}]}`))
	reqFail.Header.Set("Authorization", "Bearer test-gw-secret")
	recFail := httptest.NewRecorder()
	_, _, _ = gw.pool.PatchKey(keyID, nil, nil, "")
	gw.pool.mu.Lock()
	gw.pool.byID[keyID].Stat.CooldownUntil = 0
	gw.pool.mu.Unlock()
	gw.handler().ServeHTTP(recFail, reqFail)
	if recFail.Code == http.StatusOK {
		t.Fatalf("expected failure on 5th strike")
	}

	kv5 := gw.pool.KeyViews()[0]
	if kv5.ConsecFails != 5 || kv5.Status != "dead" || kv5.Health != "dead" {
		t.Fatalf("expected dead after 5 failures, got status=%s health=%s consec=%d", kv5.Status, kv5.Health, kv5.ConsecFails)
	}
	if kv5.LastErrorCode != "credit_exhausted" {
		t.Fatalf("expected credit_exhausted code, got %q", kv5.LastErrorCode)
	}

	logs := gw.pool.RecentLogs()
	if len(logs) == 0 || logs[0].Status != 402 || logs[0].ErrorCode != "credit_exhausted" || !strings.Contains(logs[0].Error, "Kredit") {
		t.Fatalf("expected detailed 402 credit_exhausted log, got %+v", logs)
	}

	revived, found, err := gw.pool.PatchKey(keyID, nil, nil, "revive")
	if err != nil || !found || revived.Status != "healthy" || revived.ConsecFails != 0 {
		t.Fatalf("revive failed: %+v err=%v", revived, err)
	}
}

func TestCategoryScopedKeysAndUniversalAgentProtocols(t *testing.T) {
	var gemHits, codingHits atomic.Int32
	codingSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		codingHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cod-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"coding agent ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`))
	}))
	defer codingSrv.Close()

	gw, _, cleanup := newTestGateway(
		t,
		[]string{"AIzaSyCatGem001"},
		nil,
		func(w http.ResponseWriter, r *http.Request) {
			gemHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"gem ok"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":5}}`))
		},
		nil,
	)
	defer cleanup()

	h := gw.handler()
	cookie := loginCookie(t, h, defaultAdminPass)

	createProv := `{"name":"Qwen Coding","category":"coding","api_type":"openai","auth_mode":"bearer","base_url":"` + codingSrv.URL + `","default_model":"qwen-coder","icon":"qwen","enabled":true,"weight":50,"priority":1}`
	recProv := httptest.NewRecorder()
	reqProv := httptest.NewRequest(http.MethodPost, "/api/providers", strings.NewReader(createProv))
	reqProv.AddCookie(cookie)
	h.ServeHTTP(recProv, reqProv)
	if recProv.Code != http.StatusOK {
		t.Fatalf("create coding provider failed: %d %s", recProv.Code, recProv.Body.String())
	}

	recKey := httptest.NewRecorder()
	reqKey := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(`{"provider":"qwen-coding","raw_text":"qw_secret_001"}`))
	reqKey.AddCookie(cookie)
	h.ServeHTTP(recKey, reqKey)

	recCK := httptest.NewRecorder()
	reqCK := httptest.NewRequest(http.MethodPost, "/api/client-keys", strings.NewReader(`{"name":"Coding CLI","scope":"cat:coding"}`))
	reqCK.AddCookie(cookie)
	h.ServeHTTP(recCK, reqCK)
	var ckOut struct {
		Key    ClientKey `json:"key"`
		Secret string    `json:"secret"`
	}
	_ = json.Unmarshal(recCK.Body.Bytes(), &ckOut)
	if ckOut.Key.Scope != "cat:coding" || !strings.HasPrefix(ckOut.Secret, "kr_cat_") {
		t.Fatalf("expected cat:coding key, got %+v secret=%q", ckOut.Key, ckOut.Secret)
	}

	claudeReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"auto","max_tokens":128,"system":"be concise","messages":[{"role":"user","content":"ping"}]}`))
	claudeReq.Header.Set("x-api-key", ckOut.Secret)
	claudeRec := httptest.NewRecorder()
	h.ServeHTTP(claudeRec, claudeReq)
	if claudeRec.Code != http.StatusOK || !strings.Contains(claudeRec.Body.String(), `"coding agent ok"`) {
		t.Fatalf("claude /v1/messages failed: %d %s", claudeRec.Code, claudeRec.Body.String())
	}

	codexReq := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"auto","input":"ping","stream":true}`))
	codexReq.Header.Set("Authorization", "Bearer "+ckOut.Secret)
	codexRec := httptest.NewRecorder()
	h.ServeHTTP(codexRec, codexReq)
	if codexRec.Code != http.StatusOK || !strings.Contains(codexRec.Body.String(), "response.completed") {
		t.Fatalf("codex /v1/responses stream failed: %d %s", codexRec.Code, codexRec.Body.String())
	}

	gemReq := httptest.NewRequest(http.MethodPost, "/v1beta/models/auto:generateContent?key="+ckOut.Secret, strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"ping"}]}]}`))
	gemRec := httptest.NewRecorder()
	h.ServeHTTP(gemRec, gemReq)
	if gemRec.Code != http.StatusOK || !strings.Contains(gemRec.Body.String(), `"coding agent ok"`) {
		t.Fatalf("gemini native endpoint failed: %d %s", gemRec.Code, gemRec.Body.String())
	}

	if gemHits.Load() != 0 {
		t.Fatalf("cat:coding client key must not route to gratis gemini provider, got hits=%d", gemHits.Load())
	}
	if codingHits.Load() != 3 {
		t.Fatalf("expected 3 hits routed to coding provider, got %d", codingHits.Load())
	}
}

func TestSequentialKeyNamingSmartImportAndErrorKeyBottomPriority(t *testing.T) {
	gw, _, cleanup := newTestGateway(t, []string{"AIzaSyCleanKeyOne01", "AIzaSyCleanKeyTwo02"}, nil, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":4}}`))
	}, nil)
	defer cleanup()

	h := gw.handler()
	cookie := loginCookie(t, h, defaultAdminPass)

	importPayload := `{"provider":"gemini","raw_text":"[\"AIzaSyJsonImported03\", \"AIzaSyJsonImported04\"]\nGEMINI_API_KEY=\"AIzaSyEnvImported05\"\nAIzaSyCsvImported06, customA"}`
	reqImp := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(importPayload))
	reqImp.AddCookie(cookie)
	recImp := httptest.NewRecorder()
	h.ServeHTTP(recImp, reqImp)
	if recImp.Code != http.StatusOK || !strings.Contains(recImp.Body.String(), `"added":4`) {
		t.Fatalf("smart import failed: %d %s", recImp.Code, recImp.Body.String())
	}

	views := gw.pool.KeyViews()
	labels := map[string]string{}
	var firstKeyID, secondKeyID string
	for _, kv := range views {
		labels[kv.Masked] = kv.Label
		if kv.Label == "gemini01" {
			firstKeyID = kv.ID
		}
		if kv.Label == "gemini02" {
			secondKeyID = kv.ID
		}
	}
	wantSeq := map[string]bool{"gemini01": false, "gemini02": false, "gemini03": false, "gemini04": false, "gemini05": false}
	for _, kv := range views {
		if _, ok := wantSeq[kv.Label]; ok {
			wantSeq[kv.Label] = true
		}
	}
	for lbl, found := range wantSeq {
		if !found {
			t.Fatalf("expected deterministic sequential label %q in %+v", lbl, views)
		}
	}

	_, _, _ = gw.pool.MarkFail(firstKeyID, 429, 0, "rate limit")
	gw.pool.mu.Lock()
	gw.pool.byID[firstKeyID].Stat.CooldownUntil = 0
	gw.pool.mu.Unlock()

	for _, mode := range []string{"smart", "random", "round_robin", "lru_jitter"} {
		_, err := gw.pool.UpdateSettings(RouterSettings{
			RotationMode:          "weighted",
			ModelRoutingPolicy:    "auto_or_override",
			KeySelection:          mode,
			CrossFallback:         true,
			MaxRetriesPerProvider: 3,
			CooldownBaseSec:       30,
			DefaultMaxTokens:      1024,
			RequestTimeoutSec:     30,
		})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 6; i++ {
			picked := gw.pool.SelectKey("gemini", map[string]bool{})
			if picked == nil {
				t.Fatalf("mode=%s picked nil", mode)
			}
			if picked.ID == firstKeyID {
				t.Fatalf("mode=%s picked errored key %s while clean tier0 keys exist", mode, picked.Stat.Label)
			}
		}
	}

	sortedViews := gw.pool.KeyViews()
	lastIdx := len(sortedViews) - 1
	if sortedViews[lastIdx].ID != firstKeyID || sortedViews[0].ID == firstKeyID {
		t.Fatalf("expected errored key %s sorted at the bottom, got first=%s last=%s", secondKeyID, sortedViews[0].ID, sortedViews[lastIdx].ID)
	}
}

func TestJWT24HourExpiryBearerAuthAndAPKDownload(t *testing.T) {
	gw, _, cleanup := newTestGateway(t, []string{"AIzaSyJwtTestKey01"}, nil, nil, nil)
	defer cleanup()

	h := gw.handler()

	reqLogin := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"`+defaultAdminPass+`"}`))
	recLogin := httptest.NewRecorder()
	h.ServeHTTP(recLogin, reqLogin)
	if recLogin.Code != http.StatusOK {
		t.Fatalf("login code=%d body=%s", recLogin.Code, recLogin.Body.String())
	}

	var loginBody struct {
		OK            bool   `json:"ok"`
		Authenticated bool   `json:"authenticated"`
		Token         string `json:"token"`
		ExpiresAt     int64  `json:"expires_at"`
		ExpiresIn     int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(recLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("decode login json: %v", err)
	}
	if !loginBody.OK || !loginBody.Authenticated || loginBody.ExpiresIn != 86400 {
		t.Fatalf("unexpected login response: %+v", loginBody)
	}
	parts := strings.Split(loginBody.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3-segment HS256 JWT, got %q", loginBody.Token)
	}
	if exp, ok := gw.pool.ParseSessionJWT(loginBody.Token); !ok || exp != loginBody.ExpiresAt {
		t.Fatalf("ParseSessionJWT failed: ok=%v exp=%d want=%d", ok, exp, loginBody.ExpiresAt)
	}

	reqStatus := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	reqStatus.Header.Set("Authorization", "Bearer "+loginBody.Token)
	recStatus := httptest.NewRecorder()
	h.ServeHTTP(recStatus, reqStatus)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("bearer /api/status code=%d body=%s", recStatus.Code, recStatus.Body.String())
	}
	if !strings.Contains(recStatus.Body.String(), `"apk":{"available":`) {
		t.Fatalf("expected apk metadata in status response, got %s", recStatus.Body.String())
	}

	reqSes := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	reqSes.Header.Set("Authorization", "Bearer "+loginBody.Token)
	recSes := httptest.NewRecorder()
	h.ServeHTTP(recSes, reqSes)
	if recSes.Code != http.StatusOK || !strings.Contains(recSes.Body.String(), `"authenticated":true`) {
		t.Fatalf("bearer session failed: %d %s", recSes.Code, recSes.Body.String())
	}

	reqTampered := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	reqTampered.Header.Set("Authorization", "Bearer "+loginBody.Token+"x")
	recTampered := httptest.NewRecorder()
	h.ServeHTTP(recTampered, reqTampered)
	if recTampered.Code != http.StatusUnauthorized {
		t.Fatalf("tampered jwt expected 401, got %d", recTampered.Code)
	}

	expiredTok, _ := gw.pool.issueSessionJWTAt(100)
	reqExpired := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	reqExpired.Header.Set("Authorization", "Bearer "+expiredTok)
	recExpired := httptest.NewRecorder()
	h.ServeHTTP(recExpired, reqExpired)
	if recExpired.Code != http.StatusUnauthorized {
		t.Fatalf("expired jwt expected 401, got %d", recExpired.Code)
	}

	reqExpiredSes := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	reqExpiredSes.Header.Set("Authorization", "Bearer "+expiredTok)
	recExpiredSes := httptest.NewRecorder()
	h.ServeHTTP(recExpiredSes, reqExpiredSes)
	if recExpiredSes.Code != http.StatusOK || !strings.Contains(recExpiredSes.Body.String(), `"authenticated":false`) {
		t.Fatalf("expired session expected authenticated:false, got %d %s", recExpiredSes.Code, recExpiredSes.Body.String())
	}

	gw.cfg.APKFile = filepath.Join(t.TempDir(), "absent.apk")
	reqMissingAPK := httptest.NewRequest(http.MethodGet, "/api/app/apk", nil)
	reqMissingAPK.Header.Set("Authorization", "Bearer "+loginBody.Token)
	recMissingAPK := httptest.NewRecorder()
	h.ServeHTTP(recMissingAPK, reqMissingAPK)
	if recMissingAPK.Code != http.StatusNotFound {
		t.Fatalf("missing apk expected 404, got %d body=%s", recMissingAPK.Code, recMissingAPK.Body.String())
	}

	dummyAPK := filepath.Join(t.TempDir(), "karouter-test.apk")
	if err := os.WriteFile(dummyAPK, []byte("PK-TEST-APK-BYTES"), 0o600); err != nil {
		t.Fatal(err)
	}
	gw.cfg.APKFile = dummyAPK

	reqAPK := httptest.NewRequest(http.MethodGet, "/api/app/apk", nil)
	reqAPK.Header.Set("Authorization", "Bearer "+loginBody.Token)
	recAPK := httptest.NewRecorder()
	h.ServeHTTP(recAPK, reqAPK)
	if recAPK.Code != http.StatusOK {
		t.Fatalf("apk download expected 200, got %d body=%s", recAPK.Code, recAPK.Body.String())
	}
	if ct := recAPK.Header().Get("Content-Type"); ct != "application/vnd.android.package-archive" {
		t.Fatalf("unexpected apk content-type: %q", ct)
	}
	if recAPK.Body.String() != "PK-TEST-APK-BYTES" {
		t.Fatalf("unexpected apk body: %q", recAPK.Body.String())
	}
}

func TestModelsCatalogAndLiveProbe(t *testing.T) {
	var sawGemModel, sawAgnModel string
	gemSrv := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"models/gemini-2.5-pro","displayName":"Gemini 2.5 Pro","description":"Pro reasoning model","inputTokenLimit":1048576,"outputTokenLimit":65536,"supportedGenerationMethods":["generateContent","streamGenerateContent"]}]}`))
			return
		}
		parts := strings.Split(r.URL.Path, "/models/")
		if len(parts) == 2 {
			sawGemModel = strings.Split(parts[1], ":")[0]
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"gem-ok"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":5}}`))
	}
	agnSrv := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"agnes-2.5-pro","owned_by":"agnes-ai","context_length":131072}]}`))
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if m, ok := req["model"].(string); ok {
			sawAgnModel = m
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","model":"agnes-2.5-pro","choices":[{"index":0,"message":{"role":"assistant","content":"agn-ok"},"finish_reason":"stop"}],"usage":{"total_tokens":5}}`))
	}

	gw, _, cleanup := newTestGateway(t, []string{"AIzaSyModelProbe01"}, []string{"agn-model-probe-01"}, gemSrv, agnSrv)
	defer cleanup()

	h := gw.handler()
	cookie := loginCookie(t, h, defaultAdminPass)

	reqProbe := httptest.NewRequest(http.MethodGet, "/api/models?probe=1", nil)
	reqProbe.AddCookie(cookie)
	recProbe := httptest.NewRecorder()
	h.ServeHTTP(recProbe, reqProbe)
	if recProbe.Code != http.StatusOK {
		t.Fatalf("/api/models?probe=1 status=%d body=%s", recProbe.Code, recProbe.Body.String())
	}
	body := recProbe.Body.String()
	for _, want := range []string{"gemini-2.5-pro", "Gemini 2.5 Pro", "1048576", "agnes-2.5-pro", "131072", "gemini/gemini-2.5-pro", "agnes/agnes-2.5-pro"} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in /api/models?probe=1 output: %s", want, body)
		}
	}

	reqV1 := httptest.NewRequest(http.MethodGet, "/v1/models?probe=1", nil)
	reqV1.Header.Set("Authorization", "Bearer test-gw-secret")
	recV1 := httptest.NewRecorder()
	h.ServeHTTP(recV1, reqV1)
	if recV1.Code != http.StatusOK || !strings.Contains(recV1.Body.String(), "gemini-2.5-pro") {
		t.Fatalf("/v1/models?probe=1 failed: %d %s", recV1.Code, recV1.Body.String())
	}

	reqChatGem := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gemini/gemini-2.5-pro","messages":[{"role":"user","content":"hi"}]}`))
	reqChatGem.Header.Set("Authorization", "Bearer test-gw-secret")
	recChatGem := httptest.NewRecorder()
	h.ServeHTTP(recChatGem, reqChatGem)
	if recChatGem.Code != http.StatusOK || sawGemModel != "gemini-2.5-pro" {
		t.Fatalf("prefixed gemini model routing failed: code=%d saw=%q body=%s", recChatGem.Code, sawGemModel, recChatGem.Body.String())
	}

	reqChatAgn := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"agnes/agnes-2.5-pro","messages":[{"role":"user","content":"hi"}]}`))
	reqChatAgn.Header.Set("Authorization", "Bearer test-gw-secret")
	recChatAgn := httptest.NewRecorder()
	h.ServeHTTP(recChatAgn, reqChatAgn)
	if recChatAgn.Code != http.StatusOK || sawAgnModel != "agnes-2.5-pro" {
		t.Fatalf("prefixed agnes model routing failed: code=%d saw=%q body=%s", recChatAgn.Code, sawAgnModel, recChatAgn.Body.String())
	}
}

func TestGemini404AutoFallbackForeignAliasesAndRoundRobin(t *testing.T) {
	var seenGemModels []string
	var maxTokSeen float64
	gemSrv := func(w http.ResponseWriter, r *http.Request) {
		var bodyMap map[string]any
		_ = json.NewDecoder(r.Body).Decode(&bodyMap)
		if gc, ok := bodyMap["generationConfig"].(map[string]any); ok {
			if mt, ok := gc["maxOutputTokens"].(float64); ok {
				maxTokSeen = mt
			}
		}
		parts := strings.Split(r.URL.Path, "/models/")
		m := ""
		if len(parts) == 2 {
			m = strings.Split(parts[1], ":")[0]
		}
		seenGemModels = append(seenGemModels, m)
		if m == "gemini-2.5-flash" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":404,"message":"This model models/gemini-2.5-flash is no longer available to new users"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"fallback-ok"}]},"finishReason":"STOP"}],"usageMetadata":{"totalTokenCount":4}}`))
	}

	gw, _, cleanup := newTestGateway(t, []string{"AIzaSyGemFallback01", "AIzaSyGemFallback02", "AIzaSyGemFallback03"}, nil, gemSrv, nil)
	defer cleanup()

	_, _ = gw.pool.UpdateSettings(RouterSettings{
		RotationMode:          "weighted",
		ModelRoutingPolicy:    "auto_or_override",
		KeySelection:          "round_robin",
		CrossFallback:         true,
		MaxRetriesPerProvider: 3,
		CooldownBaseSec:       30,
		DefaultMaxTokens:      1024,
		RequestTimeoutSec:     30,
	})

	h := gw.handler()
	cookie := loginCookie(t, h, defaultAdminPass)

	reqBulk := httptest.NewRequest(http.MethodPost, "/api/keys/bulk", strings.NewReader(`{"action":"test_all","provider":"gemini"}`))
	reqBulk.AddCookie(cookie)
	recBulk := httptest.NewRecorder()
	h.ServeHTTP(recBulk, reqBulk)
	if recBulk.Code != http.StatusOK || !strings.Contains(recBulk.Body.String(), `"passed":3`) {
		t.Fatalf("expected all 3 gemini keys to pass via 404 fallback, got %d %s", recBulk.Code, recBulk.Body.String())
	}
	if maxTokSeen < 256 {
		t.Fatalf("expected health check maxOutputTokens >= 256, got %.0f", maxTokSeen)
	}

	seenGemModels = nil
	reqClaude := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-20250514","max_tokens":128,"messages":[{"role":"user","content":"hi"}]}`))
	reqClaude.Header.Set("x-api-key", "test-gw-secret")
	recClaude := httptest.NewRecorder()
	h.ServeHTTP(recClaude, reqClaude)
	if recClaude.Code != http.StatusOK || !strings.Contains(recClaude.Body.String(), "fallback-ok") {
		t.Fatalf("claude alias + gemini 404 fallback failed: %d %s", recClaude.Code, recClaude.Body.String())
	}
	if len(seenGemModels) < 2 || seenGemModels[0] != "gemini-2.5-flash" || seenGemModels[1] != "gemini-3-flash-preview" {
		t.Fatalf("expected 404 fallback chain, got %v", seenGemModels)
	}

	picked := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		k := gw.pool.SelectKey("gemini", map[string]bool{})
		if k == nil {
			t.Fatal("nil key from round_robin")
		}
		picked = append(picked, k.Stat.Label)
	}
	if picked[0] == picked[1] || picked[1] == picked[2] || picked[0] == picked[2] || picked[3] != picked[0] {
		t.Fatalf("expected deterministic 3-key round_robin cycle, got %v", picked)
	}

	views := gw.pool.KeyViews()
	k0 := views[0].ID
	_, _, msg := gw.pool.MarkFail(k0, 404, 0, "status 404: This model is no longer available")
	if strings.Count(msg, "Model / endpoint tidak ada (404):") != 1 {
		t.Fatalf("expected single clean error prefix, got %q", msg)
	}
}
