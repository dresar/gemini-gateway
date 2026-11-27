package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeUsageAutoDetect(t *testing.T) {
	cases := []struct {
		name      string
		openai    *OpenAIUsage
		gemini    *[5]int
		wantIn    int64
		wantOut   int64
		wantRead  int64
		wantWrite int64
		wantReas  int64
		wantTotal int64
	}{
		{
			name: "openai_details",
			openai: &OpenAIUsage{
				PromptTokens:     1200,
				CompletionTokens: 350,
				TotalTokens:      1550,
				PromptTokensDetails: &struct {
					CachedTokens int `json:"cached_tokens"`
				}{CachedTokens: 800},
				CompletionTokensDetails: &struct {
					ReasoningTokens int `json:"reasoning_tokens"`
				}{ReasoningTokens: 120},
			},
			wantIn:    1200,
			wantOut:   350,
			wantRead:  800,
			wantWrite: 0,
			wantReas:  120,
			wantTotal: 1550,
		},
		{
			name: "anthropic_style_fields",
			openai: &OpenAIUsage{
				InputTokens:              500,
				OutputTokens:             200,
				CacheReadInputTokens:     300,
				CacheCreationInputTokens: 100,
			},
			wantIn:    500,
			wantOut:   200,
			wantRead:  300,
			wantWrite: 100,
			wantReas:  0,
			wantTotal: 1100,
		},
		{
			name:      "gemini_metadata",
			gemini:    &[5]int{2000, 400, 1500, 90, 2490},
			wantIn:    2000,
			wantOut:   400,
			wantRead:  1500,
			wantWrite: 0,
			wantReas:  90,
			wantTotal: 2490,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got TokenUsage
			if tc.openai != nil {
				got = normalizeOpenAIUsage(*tc.openai)
			} else if tc.gemini != nil {
				got = normalizeGeminiUsage(tc.gemini[0], tc.gemini[1], tc.gemini[2], tc.gemini[3], tc.gemini[4])
			}
			if got.InputTokens != tc.wantIn || got.OutputTokens != tc.wantOut ||
				got.CacheReadTokens != tc.wantRead || got.CacheWriteTokens != tc.wantWrite ||
				got.ReasoningTokens != tc.wantReas || got.TotalTokens != tc.wantTotal {
				t.Fatalf("got %+v, want in=%d out=%d read=%d write=%d reas=%d total=%d",
					got, tc.wantIn, tc.wantOut, tc.wantRead, tc.wantWrite, tc.wantReas, tc.wantTotal)
			}
		})
	}
}

func TestUsageSQLitePersistenceAndRanges(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "usage.db")
	cfg := Config{DBPath: dbPath, AdminPasswordEnv: "secret123"}
	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}

	pool.RecordUsage("gemini", "gemini-3-flash-preview", TokenUsage{
		InputTokens:     1000,
		OutputTokens:    250,
		CacheReadTokens: 400,
		ReasoningTokens: 50,
		TotalTokens:     1300,
	})
	pool.FlushPending()

	today := time.Now().Format("2006-01-02")
	day50 := time.Now().AddDate(0, 0, -50).Format("2006-01-02")
	day120 := time.Now().AddDate(0, 0, -120).Format("2006-01-02")
	day400 := time.Now().AddDate(0, 0, -400).Format("2006-01-02")

	for _, r := range []struct {
		day   string
		model string
		inTok int64
	}{
		{day50, "gemini-3.5-flash", 2000},
		{day120, "agnes-3.0-flash", 3000},
		{day400, "old-model", 9999},
	} {
		_, err := pool.db.Exec(
			`INSERT INTO token_usage_daily(day,provider,model,requests,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,reasoning_tokens,total_tokens) VALUES(?,?,?,2,?,500,100,0,0,?)`,
			r.day, "gemini", r.model, r.inTok, r.inTok+500,
		)
		if err != nil {
			t.Fatalf("insert %s: %v", r.day, err)
		}
	}
	_ = pool.Close()

	pool2, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("reopen NewPool: %v", err)
	}
	defer pool2.Close()

	r1 := pool2.UsageReport(1)
	if r1.Totals.TotalTokens != 1300 || r1.Totals.CacheReadTokens != 400 || r1.Totals.ReasoningTokens != 50 {
		t.Fatalf("1d totals mismatch: %+v (today=%s)", r1.Totals, today)
	}

	r60 := pool2.UsageReport(60)
	if r60.Totals.TotalTokens != 1300+2500 {
		t.Fatalf("60d totals mismatch: got %d want %d", r60.Totals.TotalTokens, 3800)
	}

	r365 := pool2.UsageReport(365)
	if r365.Totals.TotalTokens != 1300+2500+3500 {
		t.Fatalf("365d totals mismatch: got %d want %d (should exclude 400d old row)", r365.Totals.TotalTokens, 7300)
	}

	gw := &Gateway{cfg: cfg, pool: pool2, startedAt: time.Now().Unix()}
	req := httptest.NewRequest(http.MethodGet, "/api/usage?days=60", nil)
	tok := pool2.IssueSessionToken()
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: tok})
	rec := httptest.NewRecorder()
	gw.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/usage status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		OK     bool        `json:"ok"`
		Days   int         `json:"days"`
		Totals UsageTotals `json:"totals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode /api/usage: %v", err)
	}
	if !payload.OK || payload.Days != 60 || payload.Totals.TotalTokens != 3800 {
		t.Fatalf("unexpected /api/usage payload: %+v", payload)
	}
}
