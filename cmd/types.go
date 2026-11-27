package main

import (
	"database/sql"
	"net/http"
	"sync"
	"sync/atomic"
)

type Config struct {
	Host             string
	Port             string
	DBPath           string
	LegacyGatewayKey string
	AdminPasswordEnv string
	NeonDBURLEnv     string
	NeonIntervalDays int
	GeminiModel      string
	GeminiBaseURL    string
	AgnesModel       string
	AgnesBaseURL     string
	LegacyGemKeys    string
	LegacyAgnKeys    string
	LegacyStateFile  string
	LegacyPoolState  string
	UIFile           string
	APKFile          string
}

type Provider struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Category             string   `json:"category"`
	APIType              string   `json:"api_type"`
	AuthMode             string   `json:"auth_mode"`
	BaseURL              string   `json:"base_url"`
	DefaultModel         string   `json:"default_model"`
	Icon                 string   `json:"icon"`
	Enabled              bool     `json:"enabled"`
	Weight               int      `json:"weight"`
	Priority             int      `json:"priority"`
	ModelRotationEnabled bool     `json:"model_rotation_enabled"`
	RotationModels       []string `json:"rotation_models"`
	CreatedAt            int64    `json:"created_at"`
}

type ProviderView struct {
	Provider
	TotalKeys    int   `json:"total_keys"`
	HealthyKeys  int   `json:"healthy_keys"`
	EligibleKeys int   `json:"eligible_keys"`
	ReqCount     int64 `json:"req_count"`
}

type RouterSettings struct {
	RotationMode          string `json:"rotation_mode"`
	ModelRoutingPolicy    string `json:"model_routing_policy"`
	ModelRotationMode     string `json:"model_rotation_mode"`
	KeySelection          string `json:"key_selection"`
	CrossFallback         bool   `json:"cross_fallback"`
	MaxRetriesPerProvider int    `json:"max_retries_per_provider"`
	CooldownBaseSec       int    `json:"cooldown_base_sec"`
	DefaultMaxTokens      int    `json:"default_max_tokens"`
	RequestTimeoutSec     int    `json:"request_timeout_sec"`
}

type BackupConfig struct {
	NeonURL        string `json:"-"`
	HasNeonURL     bool   `json:"has_neon_url"`
	NeonURLMasked  string `json:"neon_url_masked"`
	AutoBackup     bool   `json:"auto_backup"`
	IntervalDays   int    `json:"interval_days"`
	LastBackupUnix int64  `json:"last_backup_unix"`
	NextBackupUnix int64  `json:"next_backup_unix"`
	LastStatus     string `json:"last_status"`
	LastError      string `json:"last_error"`
	LastSummary    string `json:"last_summary"`
}

type BackupSnapshot struct {
	Version      int            `json:"version"`
	ExportedAt   int64          `json:"exported_at"`
	Settings     RouterSettings `json:"settings"`
	Providers    []Provider     `json:"providers"`
	ProviderKeys []BackupKeyRow `json:"provider_keys"`
	ClientKeys   []BackupClient `json:"client_keys"`
}

type BackupKeyRow struct {
	ID            string `json:"id"`
	Provider      string `json:"provider"`
	APIKey        string `json:"api_key"`
	Label         string `json:"label"`
	Enabled       bool   `json:"enabled"`
	Health        string `json:"health"`
	CooldownUntil int64  `json:"cooldown_until"`
	LastUsedUnix  int64  `json:"last_used_unix"`
	OkCount       int    `json:"ok_count"`
	Fail429       int    `json:"fail_429"`
	FailAuth      int    `json:"fail_auth"`
	FailOther     int    `json:"fail_other"`
	ConsecFails   int    `json:"consec_fails"`
	TotalTokens   int64  `json:"total_tokens"`
	LastLatencyMs int64  `json:"last_latency_ms"`
	LastErrorCode string `json:"last_error_code"`
	LastError     string `json:"last_error"`
	CreatedAt     int64  `json:"created_at"`
}

type BackupClient struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Scope        string `json:"scope"`
	KeyHash      string `json:"key_hash"`
	Masked       string `json:"masked"`
	Enabled      bool   `json:"enabled"`
	UsageCount   int64  `json:"usage_count"`
	TotalTokens  int64  `json:"total_tokens"`
	LastUsedUnix int64  `json:"last_used_unix"`
	CreatedAt    int64  `json:"created_at"`
}

type NeonBackupMeta struct {
	ID                int64  `json:"id"`
	CreatedAt         int64  `json:"created_at"`
	TriggerType       string `json:"trigger_type"`
	ProvidersCount    int    `json:"providers_count"`
	ProviderKeysCount int    `json:"provider_keys_count"`
	ClientKeysCount   int    `json:"client_keys_count"`
}

type KeyStat struct {
	Label         string `json:"label"`
	Enabled       bool   `json:"enabled"`
	Health        string `json:"health"`
	CooldownUntil int64  `json:"cooldown_until"`
	LastUsedUnix  int64  `json:"last_used_unix"`
	OkCount       int    `json:"ok_count"`
	Fail429       int    `json:"fail_429"`
	FailAuth      int    `json:"fail_auth"`
	FailOther     int    `json:"fail_other"`
	ConsecFails   int    `json:"consec_fails"`
	TotalTokens   int64  `json:"total_tokens"`
	LastLatencyMs int64  `json:"last_latency_ms"`
	LastErrorCode string `json:"last_error_code"`
	LastError     string `json:"last_error"`
}

type KeyEntry struct {
	ID             string
	Provider       string
	ProviderIndex  int
	Key            string
	Masked         string
	LastPickedNano int64
	CreatedAt      int64
	Stat           KeyStat
}

type KeyView struct {
	ID                   string `json:"id"`
	Index                int    `json:"index"`
	Provider             string `json:"provider"`
	Label                string `json:"label"`
	Masked               string `json:"masked"`
	Enabled              bool   `json:"enabled"`
	Status               string `json:"status"`
	Health               string `json:"health"`
	CooldownUntil        int64  `json:"cooldown_until"`
	CooldownRemainingSec int64  `json:"cooldown_remaining_sec"`
	LastUsedUnix         int64  `json:"last_used_unix"`
	OkCount              int    `json:"ok_count"`
	Fail429              int    `json:"fail_429"`
	FailAuth             int    `json:"fail_auth"`
	FailOther            int    `json:"fail_other"`
	ConsecFails          int    `json:"consec_fails"`
	TotalTokens          int64  `json:"total_tokens"`
	LastLatencyMs        int64  `json:"last_latency_ms"`
	LastErrorCode        string `json:"last_error_code"`
	LastError            string `json:"last_error"`
}

type ClientKey struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Scope        string `json:"scope"`
	KeyHash      string `json:"-"`
	Masked       string `json:"masked"`
	Enabled      bool   `json:"enabled"`
	UsageCount   int64  `json:"usage_count"`
	TotalTokens  int64  `json:"total_tokens"`
	LastUsedUnix int64  `json:"last_used_unix"`
	CreatedAt    int64  `json:"created_at"`
}

type RequestLog struct {
	ID             int64  `json:"id"`
	Timestamp      int64  `json:"ts"`
	Provider       string `json:"provider"`
	ProviderName   string `json:"provider_name,omitempty"`
	KeyIndex       int    `json:"key_index"`
	KeyMasked      string `json:"key_masked"`
	ClientName     string `json:"client_name,omitempty"`
	Scope          string `json:"scope,omitempty"`
	RequestedModel string `json:"requested_model"`
	UpstreamModel  string `json:"upstream_model"`
	Status         int    `json:"status"`
	RouteUs        int64  `json:"route_us"`
	LatencyMs      int64  `json:"latency_ms"`
	Tokens         int    `json:"tokens"`
	Stream         bool   `json:"stream"`
	Fallback       bool   `json:"fallback"`
	Attempts       int    `json:"attempts"`
	ErrorCode      string `json:"error_code,omitempty"`
	Error          string `json:"error,omitempty"`
}

type LegacyRouterFile struct {
	Settings struct {
		RotationMode          string `json:"rotation_mode"`
		ModelRoutingPolicy    string `json:"model_routing_policy"`
		KeySelection          string `json:"key_selection"`
		GeminiEnabled         bool   `json:"gemini_enabled"`
		AgnesEnabled          bool   `json:"agnes_enabled"`
		GeminiWeight          int    `json:"gemini_weight"`
		AgnesWeight           int    `json:"agnes_weight"`
		GeminiModel           string `json:"gemini_model"`
		AgnesModel            string `json:"agnes_model"`
		AgnesBaseURL          string `json:"agnes_base_url"`
		CrossFallback         bool   `json:"cross_fallback"`
		MaxRetriesPerProvider int    `json:"max_retries_per_provider"`
		CooldownBaseSec       int    `json:"cooldown_base_sec"`
		DefaultMaxTokens      int    `json:"default_max_tokens"`
		RequestTimeoutSec     int    `json:"request_timeout_sec"`
	} `json:"settings"`
	KeyStats map[string]KeyStat `json:"key_stats"`
}

type LegacyPoolState map[string]struct {
	Health        string `json:"health"`
	CooldownUntil int64  `json:"cooldown_until"`
	LastUsedUnix  int64  `json:"last_used_unix"`
	OkCount       int    `json:"ok_count"`
	Fail429       int    `json:"fail_429"`
	Fail403       int    `json:"fail_403"`
}

type RuntimeStats struct {
	TotalRequests  int64 `json:"total_requests"`
	TotalSuccess   int64 `json:"total_success"`
	TotalErrors    int64 `json:"total_errors"`
	TotalRetries   int64 `json:"total_retries"`
	TotalFallbacks int64 `json:"total_fallbacks"`
	TotalTokens    int64 `json:"total_tokens"`
	AvgLatencyMs   int64 `json:"avg_latency_ms"`
	AvgRouteUs     int64 `json:"avg_route_us"`
}

type writeEvent struct {
	providerKeyID string
	clientKeyID   string
	logEntry      *RequestLog
}

type KeyPool struct {
	mu            sync.RWMutex
	db            *sql.DB
	cfg           Config
	settings      RouterSettings
	backup        BackupConfig
	providers     []*Provider
	providerByID  map[string]*Provider
	providerReqs  map[string]int64
	keys          []*KeyEntry
	byID          map[string]*KeyEntry
	clients       []*ClientKey
	clientByID    map[string]*ClientKey
	clientByHash  map[string]*ClientKey
	sessionSecret []byte
	adminSalt     string
	adminHash     string
	swrrCurrent   map[string]int
	rrCursor      map[string]int
	modelCursor   map[string]int
	modelCooldown map[string]int64
	modelLastUsed map[string]int64
	logs          [maxLogEntries]RequestLog
	logHead       int
	logCount      int
	nextLogID     int64
	latencySum    int64
	latencyCount  int64
	routeUsSum    int64
	routeUsCount  int64
	writeCh       chan writeEvent
}

type OpenAIRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Tools       []OpenAITool    `json:"tools,omitempty"`
	ToolChoice  any             `json:"tool_choice,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream"`
}

type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type OpenAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type OpenAIResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   OpenAIUsage    `json:"usage"`
}

type OpenAIChoice struct {
	Index        int           `json:"index"`
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type OpenAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type GeminiRequest struct {
	Contents          []GeminiContent `json:"contents"`
	SystemInstruction *GeminiContent  `json:"systemInstruction,omitempty"`
	GenerationConfig  map[string]any  `json:"generationConfig,omitempty"`
	Tools             []GeminiTool    `json:"tools,omitempty"`
	ToolConfig        map[string]any  `json:"toolConfig,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	FunctionCall     *GeminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *GeminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
}

type GeminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type GeminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type GeminiTool struct {
	FunctionDeclarations []GeminiFunctionDecl `json:"functionDeclarations,omitempty"`
}

type GeminiFunctionDecl struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type GeminiResponse struct {
	Candidates []struct {
		Content      GeminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type upstreamError struct {
	Status     int
	Message    string
	RetryAfter int64
}

func (e *upstreamError) Error() string {
	return e.Message
}

type routeTrace struct {
	Provider     string
	ProviderName string
	KeyIndex     int
	KeyMasked    string
	ClientName   string
	Scope        string
	Model        string
	RouteUs      int64
	Attempts     int
	Fallback     bool
	LatencyMs    int64
	Tokens       int
}

type authFailureState struct {
	count        int
	blockedUntil int64
}

type Gateway struct {
	cfg          Config
	pool         *KeyPool
	startedAt    int64
	httpClient   *http.Client
	streamClient *http.Client
	uiHTML       []byte
	uiHTMLGz     []byte

	sigMu    sync.Mutex
	sigOrder []string
	sigMap   map[string]string

	loginMu    sync.Mutex
	loginFails map[string]*authFailureState

	backupMu sync.Mutex

	reqTotal atomic.Int64
	reqOK    atomic.Int64
	reqErr   atomic.Int64
	reqRetry atomic.Int64
	reqFall  atomic.Int64
	tokTotal atomic.Int64
}

type KeyItemInput struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type discoveredUpstreamModel struct {
	ID               string
	DisplayName      string
	Description      string
	OwnedBy          string
	InputTokenLimit  int
	OutputTokenLimit int
	ContextLength    int
	Methods          []string
}
