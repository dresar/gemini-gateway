package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyValidationDuplicatesAndAuditDedup(t *testing.T) {
	gw, _, cleanup := newTestGateway(
		t,
		[]string{"AIzaSyValidGemKey00000000000000000001"},
		[]string{"ag_valid_test_001"},
		nil,
		nil,
	)
	defer cleanup()

	h := gw.handler()
	cookie := loginCookie(t, h, defaultAdminPass)

	payload := `{
		"provider":"gemini",
		"raw_text":"AIzaSyValidGemKey00000000000000000001\nBearer AIzaSyValidGemKey00000000000000000001\nAIzaSyFreshGemKey00000000000000000002\nAIzaSyFreshGemKey00000000000000000002\nsk-wrong-provider-key-123456\nYOUR_API_KEY\nhttps://example.com/v1\nshort1\n{\"key\":\"broken\"}\nAIzaSyShortOne01\nAIzaSyBad Space 01"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/keys", strings.NewReader(payload))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"added":2`, `"pool_duplicates":2`, `"batch_duplicates":1`, `"invalid":6`} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %s in validation response, got: %s", want, body)
		}
	}

	reqPre := httptest.NewRequest(http.MethodPost, "/api/keys/audit", strings.NewReader(`{"action":"validate","provider":"gemini","raw_text":"AIzaSyValidGemKey00000000000000000001\nAIzaSyBrandNewKey00000000000000000003\nsk-not-gemini-000123"}`))
	reqPre.AddCookie(cookie)
	recPre := httptest.NewRecorder()
	h.ServeHTTP(recPre, reqPre)
	if recPre.Code != http.StatusOK || !strings.Contains(recPre.Body.String(), `"valid":1`) || !strings.Contains(recPre.Body.String(), `"pool_duplicates":1`) || !strings.Contains(recPre.Body.String(), `"invalid":1`) {
		t.Fatalf("preflight validation mismatch: %d %s", recPre.Code, recPre.Body.String())
	}

	_, err := gw.pool.db.Exec(`INSERT INTO provider_keys(id,provider,api_key,masked,label,enabled,health,cooldown_until,last_used_unix,ok_count,fail_429,fail_auth,fail_other,consec_fails,total_tokens,last_latency_ms,last_error_code,last_error,created_at) VALUES('dup_row_worse','gemini','Bearer AIzaSyFreshGemKey00000000000000000002','Bearer...0002','dupWorse',1,'dead',0,0,0,0,5,0,5,0,900,'invalid_key','dead',100)`)
	if err != nil {
		t.Fatalf("seed duplicate sqlite row failed: %v", err)
	}
	_, err = gw.pool.db.Exec(`INSERT INTO provider_keys(id,provider,api_key,masked,label,enabled,health,cooldown_until,last_used_unix,ok_count,fail_429,fail_auth,fail_other,consec_fails,total_tokens,last_latency_ms,last_error_code,last_error,created_at) VALUES('bad_row_1','gemini','YOUR_API_KEY','YOUR...KEY','badLbl',1,'healthy',0,0,0,0,0,0,0,0,0,'','',100)`)
	if err != nil {
		t.Fatalf("seed invalid sqlite row failed: %v", err)
	}

	reloaded, err := NewPool(gw.pool.cfg)
	if err != nil {
		t.Fatalf("reload pool failed: %v", err)
	}
	gw.pool.Close()
	gw.pool = reloaded

	audit := gw.pool.AuditKeys("gemini")
	if audit.ExactDuplicates != 1 || audit.InvalidCount != 1 {
		t.Fatalf("expected 1 exact duplicate and 1 invalid entry, got %+v", audit)
	}

	reqClean := httptest.NewRequest(http.MethodPost, "/api/keys/bulk", strings.NewReader(`{"action":"auto_clean","provider":"gemini"}`))
	reqClean.AddCookie(cookie)
	recClean := httptest.NewRecorder()
	h.ServeHTTP(recClean, reqClean)
	if recClean.Code != http.StatusOK || !strings.Contains(recClean.Body.String(), `"affected":2`) {
		t.Fatalf("auto_clean failed: %d %s", recClean.Code, recClean.Body.String())
	}

	afterAudit := gw.pool.AuditKeys("gemini")
	if afterAudit.ExactDuplicates != 0 || afterAudit.InvalidCount != 0 {
		t.Fatalf("expected 0 duplicates and 0 invalid after auto_clean, got %+v", afterAudit)
	}
	if _, stillHasDead := gw.pool.GetKeySnapshot("dup_row_worse"); stillHasDead {
		t.Fatalf("expected dead duplicate copy dup_row_worse to be deleted, keeping healthy copy")
	}
	if len(gw.pool.KeyViews()) != 4 {
		t.Fatalf("expected 4 total clean keys remaining, got %d", len(gw.pool.KeyViews()))
	}
}

func TestZeroEnvSQLitePersistenceAndAPKMeta(t *testing.T) {
	dir := t.TempDir()
	wd, _ := os.Getwd()
	_ = os.Chdir(dir)
	defer func() { _ = os.Chdir(wd) }()

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("# production empty env\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	apkFile := filepath.Join(dir, "custom.apk")
	if err := os.WriteFile(apkFile, []byte("PK-SQLITE-META"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := loadConfig()
	if cfg.Host != "0.0.0.0" || cfg.Port != "8031" {
		t.Fatalf("expected production zero-env default 0.0.0.0:8031, got %s:%s", cfg.Host, cfg.Port)
	}
	gw, err := newGateway(cfg)
	if err != nil {
		t.Fatalf("newGateway zero env failed: %v", err)
	}
	defer gw.pool.Close()

	if err := gw.pool.SetMetaValue("apk_file_path", apkFile); err != nil {
		t.Fatalf("SetMetaValue failed: %v", err)
	}
	if got := gw.pool.GetMetaValue("apk_file_path"); got != apkFile {
		t.Fatalf("expected apk_file_path=%q, got %q", apkFile, got)
	}
	meta := gw.apkMeta()
	if avail, _ := meta["available"].(bool); !avail {
		t.Fatalf("expected apk available from sqlite meta_config, got %+v", meta)
	}
}
