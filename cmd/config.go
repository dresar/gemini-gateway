package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func parseArgs() (listenFlag, dbFlag string, selftestFlag bool) {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--selftest":
			selftestFlag = true
		case "--listen":
			if i+1 < len(args) {
				listenFlag = args[i+1]
				i++
			}
		case "--db":
			if i+1 < len(args) {
				dbFlag = args[i+1]
				i++
			}
		default:
			if strings.HasPrefix(args[i], "--listen=") {
				listenFlag = strings.TrimPrefix(args[i], "--listen=")
			} else if strings.HasPrefix(args[i], "--db=") {
				dbFlag = strings.TrimPrefix(args[i], "--db=")
			}
		}
	}
	return
}

func loadEnvMap() (map[string]string, string) {
	env := make(map[string]string)
	candidates := []string{
		strings.TrimSpace(os.Getenv("GATEWAY_ENV_FILE")),
		".env",
		"/opt/data/gemini-gateway/.env",
	}
	usedPath := ""
	for _, p := range candidates {
		if p == "" || !fileExists(p) {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		usedPath = p
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if len(line) == 0 || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
				env[k] = v
			}
		}
		break
	}
	for _, key := range []string{
		"HOST", "LISTEN_ADDR", "PORT", "GATEWAY_DB", "GATEWAY_API_KEY", "ADMIN_PASSWORD",
		"DATABASE_URL", "NEON_DATABASE_URL", "NEON_BACKUP_INTERVAL_DAYS",
		"GEMINI_MODEL", "GEMINI_BASE_URL", "AGNES_MODEL", "AGNES_BASE_URL",
		"GEMINI_API_KEYS_FILE", "AGNES_API_KEYS_FILE",
		"STATE_FILE", "ROUTER_STATE_FILE", "UI_HTML_FILE", "APK_FILE_PATH",
	} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			env[key] = v
		}
	}
	return env, usedPath
}

func loadConfig() Config {
	env, envPath := loadEnvMap()
	listenFlag, dbFlag, _ := parseArgs()

	host := env["HOST"]
	if host == "" {
		host = env["LISTEN_ADDR"]
	}
	if host == "" {
		host = "0.0.0.0"
	}
	port := env["PORT"]
	if port == "" {
		port = "8031"
	}
	if listenFlag != "" {
		if h, p, err := net.SplitHostPort(listenFlag); err == nil {
			if h != "" {
				host = h
			}
			if p != "" {
				port = p
			}
		} else if strings.HasPrefix(listenFlag, ":") {
			port = strings.TrimPrefix(listenFlag, ":")
		}
	}

	neonURL := env["NEON_DATABASE_URL"]
	if neonURL == "" {
		neonURL = env["DATABASE_URL"]
	}
	intervalDays := 7
	if v, err := strconv.Atoi(env["NEON_BACKUP_INTERVAL_DAYS"]); err == nil && v >= 1 && v <= 90 {
		intervalDays = v
	}

	gemModel := env["GEMINI_MODEL"]
	if gemModel == "" {
		gemModel = "gemini-2.5-flash"
	}
	gemURL := env["GEMINI_BASE_URL"]
	if gemURL == "" {
		gemURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	agnModel := env["AGNES_MODEL"]
	if agnModel == "" {
		agnModel = "agnes-2.5-flash"
	}
	agnURL := env["AGNES_BASE_URL"]
	if agnURL == "" {
		agnURL = "https://apihub.agnes-ai.com/v1"
	}

	dataDir := "."
	if dbFlag != "" {
		dataDir = filepath.Dir(dbFlag)
	} else if env["GATEWAY_DB"] != "" {
		dataDir = filepath.Dir(env["GATEWAY_DB"])
	} else if env["GEMINI_API_KEYS_FILE"] != "" {
		dataDir = filepath.Dir(env["GEMINI_API_KEYS_FILE"])
	} else if envPath != "" {
		dataDir = filepath.Dir(envPath)
	} else if dirExists("/opt/data/gemini-gateway") {
		dataDir = "/opt/data/gemini-gateway"
	}

	dbPath := dbFlag
	if dbPath == "" {
		dbPath = env["GATEWAY_DB"]
	}
	if dbPath == "" {
		dbPath = filepath.Join(dataDir, "gateway.db")
	}

	gemKeys := env["GEMINI_API_KEYS_FILE"]
	if gemKeys == "" {
		gemKeys = filepath.Join(dataDir, "keys.pool")
	}
	agnKeys := env["AGNES_API_KEYS_FILE"]
	if agnKeys == "" {
		agnKeys = filepath.Join(dataDir, "agnes_keys.pool")
	}
	stateFile := env["ROUTER_STATE_FILE"]
	if stateFile == "" {
		stateFile = env["STATE_FILE"]
	}
	if stateFile == "" {
		stateFile = filepath.Join(dataDir, "router_state.json")
	}

	return Config{
		Host:             host,
		Port:             port,
		DBPath:           dbPath,
		LegacyGatewayKey: env["GATEWAY_API_KEY"],
		AdminPasswordEnv: env["ADMIN_PASSWORD"],
		NeonDBURLEnv:     neonURL,
		NeonIntervalDays: intervalDays,
		GeminiModel:      gemModel,
		GeminiBaseURL:    strings.TrimRight(gemURL, "/"),
		AgnesModel:       agnModel,
		AgnesBaseURL:     strings.TrimRight(agnURL, "/"),
		LegacyGemKeys:    gemKeys,
		LegacyAgnKeys:    agnKeys,
		LegacyStateFile:  stateFile,
		LegacyPoolState:  filepath.Join(dataDir, "pool_state.json"),
		UIFile:           env["UI_HTML_FILE"],
		APKFile:          env["APK_FILE_PATH"],
	}
}

func defaultSettings() RouterSettings {
	return RouterSettings{
		RotationMode:          "weighted",
		ModelRoutingPolicy:    "auto_or_override",
		ModelRotationMode:     "round_robin",
		KeySelection:          "smart",
		CrossFallback:         true,
		MaxRetriesPerProvider: 5,
		CooldownBaseSec:       60,
		DefaultMaxTokens:      8192,
		RequestTimeoutSec:     60,
	}
}

func normalizeSettings(s *RouterSettings) {
	switch s.RotationMode {
	case "weighted", "priority", "model_prefix":
	default:
		s.RotationMode = "weighted"
	}
	switch s.ModelRoutingPolicy {
	case "auto_or_override", "always_rotate":
	default:
		s.ModelRoutingPolicy = "auto_or_override"
	}
	switch s.ModelRotationMode {
	case "off", "round_robin", "random", "smart":
	default:
		s.ModelRotationMode = "round_robin"
	}
	switch s.KeySelection {
	case "smart", "random", "lru_jitter", "round_robin":
	default:
		s.KeySelection = "smart"
	}
	if s.MaxRetriesPerProvider < 1 {
		s.MaxRetriesPerProvider = 1
	}
	if s.MaxRetriesPerProvider > 20 {
		s.MaxRetriesPerProvider = 20
	}
	if s.CooldownBaseSec < 5 {
		s.CooldownBaseSec = 5
	}
	if s.CooldownBaseSec > 600 {
		s.CooldownBaseSec = 600
	}
	if s.DefaultMaxTokens < 128 {
		s.DefaultMaxTokens = 128
	}
	if s.DefaultMaxTokens > 65536 {
		s.DefaultMaxTokens = 65536
	}
	if s.RequestTimeoutSec < 10 {
		s.RequestTimeoutSec = 10
	}
	if s.RequestTimeoutSec > 300 {
		s.RequestTimeoutSec = 300
	}
}

func normalizeCategory(raw string) string {
	s := slugifyProviderID(raw)
	if strings.TrimSpace(raw) == "" || strings.HasPrefix(s, "prov-") || s == "global" {
		return "gratis"
	}
	return s
}

func slugifyProviderID(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "prov-" + randomHex(3)
	}
	if out == scopeGlobal {
		out = "custom-" + randomHex(2)
	}
	return out
}

func normalizeProvider(p *Provider) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errors.New("nama provider wajib diisi")
	}
	p.Category = normalizeCategory(p.Category)
	if strings.TrimSpace(p.ID) == "" {
		p.ID = slugifyProviderID(p.Name)
	} else {
		p.ID = slugifyProviderID(p.ID)
	}
	switch strings.ToLower(strings.TrimSpace(p.APIType)) {
	case apiTypeGemini:
		p.APIType = apiTypeGemini
	default:
		p.APIType = apiTypeOpenAI
	}
	switch strings.ToLower(strings.TrimSpace(p.AuthMode)) {
	case authModeQueryKey:
		p.AuthMode = authModeQueryKey
	case authModeXAPIKey:
		p.AuthMode = authModeXAPIKey
	case authModeBearer:
		p.AuthMode = authModeBearer
	default:
		if p.APIType == apiTypeGemini {
			p.AuthMode = authModeQueryKey
		} else {
			p.AuthMode = authModeBearer
		}
	}
	p.BaseURL = strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if p.BaseURL == "" {
		return errors.New("base_url wajib diisi")
	}
	p.DefaultModel = strings.TrimSpace(p.DefaultModel)
	if p.DefaultModel == "" {
		return errors.New("default_model wajib diisi")
	}
	p.Icon = strings.TrimSpace(p.Icon)
	if p.Icon == "" {
		p.Icon = "custom"
	}
	if p.Weight < 0 {
		p.Weight = 0
	}
	if p.Weight > 100 {
		p.Weight = 100
	}
	if p.Weight == 0 {
		p.Weight = 50
	}
	if p.Priority < 1 {
		p.Priority = 1
	}
	if p.Priority > 999 {
		p.Priority = 999
	}
	p.RotationModels = normalizeRotationModelList(p.RotationModels)
	return nil
}

func normalizeRotationModelList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		m := strings.TrimSpace(raw)
		if m == "" {
			continue
		}
		key := strings.ToLower(m)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	return out
}

func maskPostgresURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := neturl.Parse(raw)
	if err != nil {
		return maskKey(raw)
	}
	if u.User != nil {
		user := u.User.Username()
		if _, hasPass := u.User.Password(); hasPass {
			return fmt.Sprintf("%s://%s:••••@%s%s", u.Scheme, user, u.Host, u.Path)
		}
	}
	return fmt.Sprintf("%s://%s%s", u.Scheme, u.Host, u.Path)
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func hashAdminPassword(password, saltHex string) string {
	salt, _ := hex.DecodeString(saltHex)
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(password))
	cur := mac.Sum(nil)
	for i := 1; i < passwordHashRounds; i++ {
		mac.Reset()
		_, _ = mac.Write(cur)
		cur = mac.Sum(nil)
	}
	return hex.EncodeToString(cur)
}

func makeKeyID(provider, rawKey string) string {
	h := sha256.Sum256([]byte(provider + ":" + rawKey))
	return hex.EncodeToString(h[:6])
}

func maskKey(k string) string {
	n := len(k)
	if n <= 8 {
		if n <= 2 {
			return "••••"
		}
		return k[:1] + "••••" + k[n-1:]
	}
	if n <= 14 {
		return k[:3] + "••••" + k[n-3:]
	}
	return k[:6] + "••••" + k[n-4:]
}

func sanitizeError(msg string, secrets ...string) string {
	out := strings.TrimSpace(msg)
	for _, s := range secrets {
		if len(s) >= 6 {
			out = strings.ReplaceAll(out, s, "[REDACTED]")
		}
	}
	out = strings.Join(strings.Fields(out), " ")
	if len(out) > 480 {
		out = out[:477] + "..."
	}
	return out
}

func extractJSONErrorMessage(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if idx := strings.Index(trimmed, "{"); idx >= 0 {
		sub := trimmed[idx:]
		var parsed struct {
			Error *struct {
				Message string `json:"message"`
				Type    string `json:"type"`
				Status  string `json:"status"`
			} `json:"error"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}
		if json.Unmarshal([]byte(sub), &parsed) == nil {
			if parsed.Error != nil && strings.TrimSpace(parsed.Error.Message) != "" {
				return strings.TrimSpace(parsed.Error.Message)
			}
			if strings.TrimSpace(parsed.Message) != "" {
				return strings.TrimSpace(parsed.Message)
			}
			if strings.TrimSpace(parsed.Detail) != "" {
				return strings.TrimSpace(parsed.Detail)
			}
		}
	}
	return trimmed
}

func classifyUpstreamError(status int, rawErr string) (code, label, detail string) {
	detail = sanitizeError(extractJSONErrorMessage(rawErr))
	if detail == "" {
		detail = sanitizeError(rawErr)
	}
	lower := strings.ToLower(rawErr + " " + detail)

	switch {
	case status == 402 ||
		strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "insufficient balance") ||
		strings.Contains(lower, "balance is too low") ||
		strings.Contains(lower, "credit balance") ||
		strings.Contains(lower, "out of credits") ||
		strings.Contains(lower, "no credit") ||
		strings.Contains(lower, "exceeded your current quota") ||
		strings.Contains(lower, "payment required") ||
		strings.Contains(lower, "billing"):
		code = "credit_exhausted"
		label = "Kredit / kuota habis"
	case status == 401 || status == 403 ||
		strings.Contains(lower, "api_key_invalid") ||
		strings.Contains(lower, "invalid_api_key") ||
		strings.Contains(lower, "api key not valid") ||
		strings.Contains(lower, "incorrect api key") ||
		strings.Contains(lower, "invalid x-api-key") ||
		strings.Contains(lower, "permission_denied") ||
		strings.Contains(lower, "authentication_error") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "account deactivated") ||
		strings.Contains(lower, "invalid token"):
		code = "invalid_key"
		label = "API Key tidak valid"
	case status == 429 ||
		strings.Contains(lower, "rate_limit") ||
		strings.Contains(lower, "too many requests") ||
		strings.Contains(lower, "resource_exhausted"):
		code = "rate_limit"
		label = "Rate limit / kuota RPM"
	case status == 404 ||
		strings.Contains(lower, "model_not_found") ||
		strings.Contains(lower, "does not exist"):
		code = "model_error"
		label = "Model / endpoint tidak ada"
	case status == 0 ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "deadline exceeded") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "eof"):
		code = "network_error"
		label = "Timeout / gagal koneksi"
	default:
		code = "upstream_error"
		label = "Error upstream"
	}
	return code, label, detail
}

func readKeyFileLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.IndexAny(line, " \t"); idx > 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		out = append(out, line)
	}
	return out
}
