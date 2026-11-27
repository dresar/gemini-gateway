package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestIsChatModelProbeCandidate(t *testing.T) {
	cases := []struct {
		apiType string
		id      string
		methods []string
		want    bool
	}{
		{apiTypeGemini, "gemini-3.6-flash", []string{"generateContent"}, true},
		{apiTypeGemini, "gemini-3-flash-preview", []string{"generateContent"}, true},
		{apiTypeGemini, "gemini-embedding-001", []string{"embedContent"}, false},
		{apiTypeGemini, "gemini-3.1-flash-tts-preview", []string{"generateContent"}, false},
		{apiTypeGemini, "gemini-3-pro-image", []string{"generateContent"}, false},
		{apiTypeOpenAI, "agnes-3.0-flash", nil, true},
		{apiTypeOpenAI, "agnes-2.5-flash", nil, true},
		{apiTypeOpenAI, "agnes-image-2.1-flash", nil, false},
		{apiTypeOpenAI, "agnes-video-2.5", nil, false},
		{apiTypeOpenAI, "agnes-sys1-1.0", nil, false},
	}
	for _, tc := range cases {
		if got := isChatModelProbeCandidate(tc.apiType, tc.id, tc.methods); got != tc.want {
			t.Fatalf("isChatModelProbeCandidate(%q, %q) = %v, want %v", tc.apiType, tc.id, got, tc.want)
		}
	}
}

func TestProviderModelRotationPersistenceAndRoundRobin(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "gw.db")
	cfg := Config{
		DBPath:        dbPath,
		GeminiModel:   "gemini-3-flash-preview",
		GeminiBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		AgnesModel:    "agnes-3.0-flash",
		AgnesBaseURL:  "https://apihub.agnes-ai.com/v1",
	}
	pool, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	gem, ok := pool.GetProvider("gemini")
	if !ok {
		t.Fatal("missing default gemini provider")
	}
	gem.ModelRotationEnabled = true
	gem.RotationModels = []string{"m-a", "m-b", "m-c", "m-a"}
	updated, found, err := pool.UpdateProvider("gemini", gem)
	if err != nil || !found {
		t.Fatalf("UpdateProvider failed: found=%v err=%v", found, err)
	}
	if len(updated.RotationModels) != 3 {
		t.Fatalf("expected deduplicated 3 models, got %v", updated.RotationModels)
	}
	_ = pool.Close()

	pool2, err := NewPool(cfg)
	if err != nil {
		t.Fatalf("reload NewPool: %v", err)
	}
	defer pool2.Close()

	reloaded, ok := pool2.GetProvider("gemini")
	if !ok || !reloaded.ModelRotationEnabled || len(reloaded.RotationModels) != 3 {
		t.Fatalf("reloaded provider mismatch: %+v", reloaded)
	}

	got := []string{
		pool2.SelectProviderModel("gemini", nil),
		pool2.SelectProviderModel("gemini", nil),
		pool2.SelectProviderModel("gemini", nil),
		pool2.SelectProviderModel("gemini", nil),
	}
	want := []string{"m-a", "m-b", "m-c", "m-a"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round-robin step %d: got %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}

	pool2.MarkModelCooldown("gemini", "m-b", 120)
	if next := pool2.SelectProviderModel("gemini", nil); next == "m-b" {
		t.Fatalf("expected m-b skipped while cooling down, got %q", next)
	}
	pool2.MarkModelOK("gemini", "m-b")
	if next := pool2.SelectProviderModel("gemini", map[string]bool{"m-a": true, "m-c": true}); next != "m-b" {
		t.Fatalf("expected m-b after MarkModelOK, got %q", next)
	}
}

func TestExecuteRoutedChatModelRotation(t *testing.T) {
	var mu sync.Mutex
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		m, _ := req["model"].(string)
		mu.Lock()
		requested = append(requested, m)
		mu.Unlock()
		if m == "m-bad" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","object":"chat.completion","model":"` + m + `","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"total_tokens":5}}`))
	}))
	defer srv.Close()

	dbPath := filepath.Join(t.TempDir(), "rot.db")
	gw, err := newGateway(Config{
		DBPath:        dbPath,
		GeminiModel:   "gemini-3-flash-preview",
		GeminiBaseURL: srv.URL,
		AgnesModel:    "agnes-3.0-flash",
		AgnesBaseURL:  srv.URL,
	})
	if err != nil {
		t.Fatalf("newGateway: %v", err)
	}
	defer gw.pool.Close()

	gem, _ := gw.pool.GetProvider("gemini")
	gem.Enabled = false
	_, _, _ = gw.pool.UpdateProvider("gemini", gem)

	agn, _ := gw.pool.GetProvider("agnes")
	agn.BaseURL = srv.URL
	agn.ModelRotationEnabled = true
	agn.RotationModels = []string{"m-bad", "m-ok-1", "m-ok-2"}
	_, _, _ = gw.pool.UpdateProvider("agnes", agn)

	_, err = gw.pool.AddKeyItemsValidated("agnes", []KeyItemInput{{Key: "sk-test-agnes-key-123456"}}, "agn")
	if err != nil {
		t.Fatalf("AddKeyItemsValidated: %v", err)
	}

	client := ClientKey{Name: "test", Scope: scopeGlobal, Enabled: true}
	resp, trace, err := gw.executeRoutedChat(context.Background(), nil, &OpenAIRequest{
		Model:    "auto",
		Messages: []OpenAIMessage{{Role: "user", Content: "hi"}},
	}, client, "")
	if err != nil || resp == nil {
		t.Fatalf("turn 1 failed: %v", err)
	}
	if trace.Model != "m-ok-1" && trace.Model != "m-ok-2" {
		t.Fatalf("expected failover to m-ok-1 or m-ok-2, got %q", trace.Model)
	}

	_, trace2, err := gw.executeRoutedChat(context.Background(), nil, &OpenAIRequest{
		Model:    "auto",
		Messages: []OpenAIMessage{{Role: "user", Content: "hi2"}},
	}, client, "")
	if err != nil {
		t.Fatalf("turn 2 failed: %v", err)
	}
	if (trace2.Model != "m-ok-1" && trace2.Model != "m-ok-2") || trace2.Model == trace.Model {
		t.Fatalf("expected turn 2 rotated to other healthy model (turn1=%q turn2=%q calls=%v)", trace.Model, trace2.Model, strings.Join(requested, ","))
	}
}
