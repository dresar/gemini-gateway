package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed ui/index.html

var embeddedUIFS embed.FS

var errNoEligibleKey = errors.New("no eligible key")

const (
	scopeGlobal          = "global"
	scopeCategoryPrefix  = "cat:"
	apiTypeOpenAI        = "openai"
	apiTypeGemini        = "gemini"
	authModeBearer       = "bearer"
	authModeXAPIKey      = "x-api-key"
	authModeQueryKey     = "query_key"
	maxLogEntries        = 150
	maxKeyConsecFails    = 5
	gatewayVersion       = "4.1.0"
	defaultAdminPass     = "280219"
	sessionCookieName    = "kr_session"
	sessionTTLSeconds    = 24 * 3600
	passwordHashRounds   = 12000
	writeBatchBufferSize = 2048
)

func (g *Gateway) selftest() {
	req := &OpenAIRequest{
		Model:     "auto",
		Messages:  []OpenAIMessage{{Role: "user", Content: "jawab: OK"}},
		MaxTokens: 16,
	}
	testClient := ClientKey{Name: "selftest", Scope: scopeGlobal, Enabled: true}
	resp, trace, err := g.executeRoutedChat(context.Background(), nil, req, testClient, "")
	if err != nil {
		fmt.Printf("HTTP Status: 500\nError: %v\n", err)
		os.Exit(1)
	}
	ans := ""
	if len(resp.Choices) > 0 {
		ans = extractTextContent(resp.Choices[0].Message.Content)
	}
	fmt.Printf("HTTP Status: 200\nProvider: %s\nRouteUs: %d\nJawaban: %s\n", trace.Provider, trace.RouteUs, strings.TrimSpace(ans))
	os.Exit(0)
}

func newGateway(cfg Config) (*Gateway, error) {
	pool, err := NewPool(cfg)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		MaxIdleConns:        128,
		MaxIdleConnsPerHost: 64,
		IdleConnTimeout:     90 * time.Second,
	}
	uiRaw, err := embeddedUIFS.ReadFile("ui/index.html")
	if err != nil {
		uiRaw = []byte("<!doctype html><html><body><h1>UI not bundled</h1></body></html>")
	}
	var gzBuf bytes.Buffer
	if gz, err := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression); err == nil {
		_, _ = gz.Write(uiRaw)
		_ = gz.Close()
	}
	return &Gateway{
		cfg:          cfg,
		pool:         pool,
		startedAt:    time.Now().Unix(),
		httpClient:   &http.Client{Transport: tr},
		streamClient: &http.Client{Transport: tr},
		uiHTML:       uiRaw,
		uiHTMLGz:     gzBuf.Bytes(),
		loginFails:   make(map[string]*authFailureState),
	}, nil
}

func (g *Gateway) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", g.handleHealth)
	mux.HandleFunc("/v1/models", g.handleModels)
	mux.HandleFunc("/v1/chat/completions", g.handleChat)
	mux.HandleFunc("/v1/responses", g.handleResponses)
	mux.HandleFunc("/v1/messages/count_tokens", g.handleAnthropicCountTokens)
	mux.HandleFunc("/v1/messages", g.handleAnthropicMessages)
	mux.HandleFunc("/v1beta/models/", g.handleGeminiNative)
	mux.HandleFunc("/api/auth/session", g.handleAuthSession)
	mux.HandleFunc("/api/auth/login", g.handleAuthLogin)
	mux.HandleFunc("/api/auth/logout", g.handleAuthLogout)
	mux.HandleFunc("/api/auth/password", g.handleAuthPassword)
	mux.HandleFunc("/api/status", g.handleStatus)
	mux.HandleFunc("/api/config", g.handleConfig)
	mux.HandleFunc("/api/providers", g.handleProvidersRoot)
	mux.HandleFunc("/api/providers/", g.handleProviderByID)
	mux.HandleFunc("/api/keys", g.handleKeysRoot)
	mux.HandleFunc("/api/keys/bulk", g.handleKeysBulk)
	mux.HandleFunc("/api/keys/audit", g.handleKeysAudit)
	mux.HandleFunc("/api/keys/test-raw", g.handleKeyTestRaw)
	mux.HandleFunc("/api/keys/", g.handleKeyByID)
	mux.HandleFunc("/api/client-keys", g.handleClientKeysRoot)
	mux.HandleFunc("/api/client-keys/", g.handleClientKeyByID)
	mux.HandleFunc("/api/backup/config", g.handleBackupConfig)
	mux.HandleFunc("/api/backup/test", g.handleBackupTest)
	mux.HandleFunc("/api/backup/run", g.handleBackupRun)
	mux.HandleFunc("/api/backup/list", g.handleBackupList)
	mux.HandleFunc("/api/backup/restore", g.handleBackupRestore)
	mux.HandleFunc("/api/chat/test", g.handleChatTest)
	mux.HandleFunc("/api/models", g.handleAdminModels)
	mux.HandleFunc("/api/app/apk", g.handleDownloadAPK)
	mux.HandleFunc("/karouter.apk", g.handleDownloadAPK)
	mux.HandleFunc("/", g.handleUI)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[gw] panic: %v", rec)
				writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("panic: %v", rec), "api_error")
			}
		}()
		if r.Header.Get("Origin") != "" {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, x-api-key, x-goog-api-key, anthropic-version")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func main() {
	if runCLIMode(os.Args[1:]) {
		return
	}
	cfg := loadConfig()
	gw, err := newGateway(cfg)
	if err != nil {
		log.Fatalf("init db error: %v", err)
	}
	defer gw.pool.Close()

	_, _, selftestFlag := parseArgs()
	isSelftest := selftestFlag || os.Getenv("GATEWAY_TEST") == "1"
	if isSelftest {
		gw.selftest()
		return
	}

	rootCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	go gw.pool.StartDBWriter(rootCtx)
	go gw.StartBackupScheduler(rootCtx)

	addr := cfg.Host + ":" + cfg.Port
	summary := gw.pool.HealthSummary()
	log.Printf("[karouter] starting v%s on http://%s | db=%s | pool=%v", gatewayVersion, addr, cfg.DBPath, summary)
	server := &http.Server{
		Addr:              addr,
		Handler:           gw.handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		<-rootCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		gw.pool.FlushPending()
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}
}
