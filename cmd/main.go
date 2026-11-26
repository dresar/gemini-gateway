package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrPoolExhausted = fmt.Errorf("key pool exhausted")

type Config struct {
	Port          string
	GatewayAPIKey string
	GeminiModel   string
	KeysFile      string
	StateFile     string
}

func loadConfig() Config {
	env := make(map[string]string)
	data, _ := os.ReadFile("/opt/data/gemini-gateway/.env")
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			env[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		}
	}
	return Config{
		Port:          env["PORT"],
		GatewayAPIKey: env["GATEWAY_API_KEY"],
		GeminiModel:   env["GEMINI_MODEL"],
		KeysFile:      env["GEMINI_API_KEYS_FILE"],
		StateFile:     filepath.Join(filepath.Dir(env["GEMINI_API_KEYS_FILE"]), "pool_state.json"),
	}
}

func (c *Config) resolve() {
	if c.Port == "" {
		c.Port = "8031"
	}
	if c.GatewayAPIKey == "" {
		c.GatewayAPIKey = "gw-bughunter-2026"
	}
	if c.GeminiModel == "" {
		c.GeminiModel = "gemini-3.5-flash"
	}
	if c.KeysFile == "" {
		c.KeysFile = "/opt/data/gemini-gateway/keys.pool"
	}
	if c.StateFile == "" {
		c.StateFile = filepath.Join(filepath.Dir(c.KeysFile), "pool_state.json")
	}
}

type PoolState map[string]struct {
	Health        string `json:"health"`
	CooldownUntil int64  `json:"cooldown_until"`
	LastUsedUnix  int64  `json:"last_used_unix"`
	OkCount       int    `json:"ok_count"`
	Fail429       int    `json:"fail_429"`
	Fail403       int    `json:"fail_403"`
}

type KeyPool struct {
	mu        sync.Mutex
	Keys      []string
	States    PoolState
	StateFile string
}

func NewPool(keysFile, stateFile string) *KeyPool {
	p := &KeyPool{States: make(PoolState), StateFile: stateFile}
	data, err := os.ReadFile(keysFile)
	if err != nil {
		log.Printf("[pool] read keys: %v", err)
		return p
	}
	for _, line := range strings.Split(string(data), "\n") {
		k := strings.TrimSpace(line)
		if k != "" {
			p.Keys = append(p.Keys, k)
		}
	}
	stateData, _ := os.ReadFile(stateFile)
	var m PoolState
	json.Unmarshal(stateData, &m)
	p.States = m
	return p
}

func (p *KeyPool) SaveState() {
	data, _ := json.MarshalIndent(p.States, "", "  ")
	tmp := p.StateFile + ".tmp"
	os.WriteFile(tmp, data, 0o600)
	os.Rename(tmp, p.StateFile)
}

func (p *KeyPool) get(i int) (s struct{ Health string; CooldownUntil int64; LastUsedUnix int64 }, ok bool) {
	v, ok := p.States[strconv.Itoa(i)]
	if !ok {
		return struct{ Health string; CooldownUntil int64; LastUsedUnix int64 }{"healthy", 0, 0}, true
	}
	return struct{ Health string; CooldownUntil int64; LastUsedUnix int64 }{v.Health, v.CooldownUntil, v.LastUsedUnix}, true
}

func (p *KeyPool) Select(tried map[int]bool) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().Unix()
	var eligible []int
	for i := range p.Keys {
		if tried[i] {
			continue
		}
		s, _ := p.get(i)
		if s.Health == "dead" || s.CooldownUntil > now {
			continue
		}
		eligible = append(eligible, i)
	}
	if len(eligible) == 0 {
		return -1
	}
	rand.Shuffle(len(eligible), func(a, b int) {
		eligible[a], eligible[b] = eligible[b], eligible[a]
	})
	sort.Slice(eligible, func(a, b int) bool {
		la, _ := p.get(eligible[a])
		lb, _ := p.get(eligible[b])
		return la.LastUsedUnix < lb.LastUsedUnix
	})
	k := len(eligible)
	if k > 8 {
		k = 8
	}
	return eligible[rand.Intn(k)]
}

func (p *KeyPool) Mark429(i int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.States[strconv.Itoa(i)]
	v.Fail429++
	shift := v.Fail429
	if shift > 6 {
		shift = 6
	}
	sec := int64(60) << uint(shift)
	if sec > 3600 {
		sec = 3600
	}
	v.CooldownUntil = time.Now().Unix() + sec
	p.States[strconv.Itoa(i)] = v
	p.SaveState()
	log.Printf("[key #%d] 429 cooldown %ds", i, sec)
}

func (p *KeyPool) Mark403(i int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.States[strconv.Itoa(i)]
	v.Fail403++
	v.Health = "dead"
	p.States[strconv.Itoa(i)] = v
	p.SaveState()
	log.Printf("[key #%d] 403 dead", i)
}

func (p *KeyPool) Mark200(i int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.States[strconv.Itoa(i)]
	v.OkCount++
	v.LastUsedUnix = time.Now().Unix()
	p.States[strconv.Itoa(i)] = v
	p.SaveState()
}

func (p *KeyPool) HealthyCount() (total, healthy int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	total = len(p.Keys)
	for i := range p.Keys {
		s, _ := p.get(i)
		if s.Health != "dead" {
			healthy++
		}
	}
	return
}

func backoff(n int) time.Duration {
	ms := 200 << uint(n)
	if ms > 16000 {
		ms = 16000
	}
	return time.Duration(ms)*time.Millisecond + time.Duration(rand.Intn(151))*time.Millisecond
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
	Contents          []GeminiContent   `json:"contents"`
	SystemInstruction *GeminiContent    `json:"systemInstruction,omitempty"`
	GenerationConfig  map[string]any    `json:"generationConfig,omitempty"`
	Tools             []GeminiTool      `json:"tools,omitempty"`
}

type GeminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []GeminiPart `json:"parts"`
}

type GeminiPart struct {
	Text              string                 `json:"text,omitempty"`
	Thought           bool                   `json:"thought,omitempty"`
	FunctionCall      *GeminiFunctionCall    `json:"functionCall,omitempty"`
	FunctionResponse  *GeminiFunctionResponse `json:"functionResponse,omitempty"`
	ThoughtSignature  string                 `json:"thoughtSignature,omitempty"`
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
		Content  GeminiContent `json:"content"`
		FinishReason string    `json:"finishReason"`
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

func (g *Gateway) openAIToGemini(messages []OpenAIMessage) ([]GeminiContent, *GeminiContent) {
	var contents []GeminiContent
	var system *GeminiContent
	// peta tool_call_id -> nama fungsi (dari pesan assistant sebelumnya)
	toolNames := map[string]string{}
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" && tc.Function.Name != "" {
				toolNames[tc.ID] = tc.Function.Name
			}
		}
	}
	for _, m := range messages {
		text := ""
		switch v := m.Content.(type) {
		case string:
			text = v
		case []any:
			for _, item := range v {
				if mp, ok := item.(map[string]any); ok {
					if t, ok := mp["text"].(string); ok {
						text += t
					}
				}
			}
		}
		if m.Role == "system" {
			if system == nil {
				system = &GeminiContent{Parts: []GeminiPart{{Text: text}}}
			} else {
				system.Parts = append(system.Parts, GeminiPart{Text: text})
			}
			continue
		}
		if m.Role == "tool" {
			name := toolNames[m.ToolCallID]
			if name == "" {
				name = "tool_result"
			}
			contents = append(contents, GeminiContent{Role: "user", Parts: []GeminiPart{{
				FunctionResponse: &GeminiFunctionResponse{Name: name, Response: map[string]any{"result": text}},
			}}})
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
			if len(m.ToolCalls) > 0 {
				var parts []GeminiPart
				if strings.TrimSpace(text) != "" {
					parts = append(parts, GeminiPart{Text: text})
				}
				for _, tc := range m.ToolCalls {
					var args map[string]any
					json.Unmarshal([]byte(tc.Function.Arguments), &args)
					if args == nil {
						args = map[string]any{}
					}
					sig := g.getSig(tc.ID)
					if sig == "" {
						parts = append(parts, GeminiPart{Text: fmt.Sprintf("[panggilan tool %s argumen: %s]", tc.Function.Name, tc.Function.Arguments)})
						continue
					}
					parts = append(parts, GeminiPart{FunctionCall: &GeminiFunctionCall{Name: tc.Function.Name, Args: args}, ThoughtSignature: sig})
				}
				contents = append(contents, GeminiContent{Role: "model", Parts: parts})
				continue
			}
		} else if role != "model" {
			role = "user"
		}
		contents = append(contents, GeminiContent{Role: role, Parts: []GeminiPart{{Text: text}}})
	}
	if len(contents) == 0 {
		contents = append(contents, GeminiContent{Role: "user", Parts: []GeminiPart{{Text: ""}}})
	}
	return contents, system
}

var geminiSchemaKeys = map[string]bool{
	"type": true, "format": true, "title": true, "description": true,
	"nullable": true, "enum": true, "maxItems": true, "minItems": true,
	"properties": true, "required": true, "minProperties": true, "maxProperties": true,
	"minLength": true, "maxLength": true, "pattern": true, "example": true,
	"anyOf": true, "propertyOrdering": true, "default": true, "items": true,
}

func sanitizeSchema(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	for k, val := range m {
		if !geminiSchemaKeys[k] {
			continue
		}
		switch k {
		case "type":
			switch t := val.(type) {
			case string:
				out[k] = t
			case []any:
				nullable := false
				mainType := ""
				for _, item := range t {
					s, _ := item.(string)
					if s == "null" {
						nullable = true
					} else if mainType == "" {
						mainType = s
					}
				}
				if mainType != "" {
					out[k] = mainType
				}
				if nullable {
					out["nullable"] = true
				}
			}
		case "properties":
			if props, ok := val.(map[string]any); ok {
				clean := map[string]any{}
				for pk, pv := range props {
					clean[pk] = sanitizeSchema(pv)
				}
				out[k] = clean
			}
		case "items":
			out[k] = sanitizeSchema(val)
		case "anyOf":
			if list, ok := val.([]any); ok {
				var clean []any
				for _, item := range list {
					clean = append(clean, sanitizeSchema(item))
				}
				out[k] = clean
			}
		default:
			out[k] = val
		}
	}
	return out
}

func openAIToolsToGemini(tools []OpenAITool) []GeminiTool {
	if len(tools) == 0 {
		return nil
	}
	var decls []GeminiFunctionDecl
	for _, t := range tools {
		if t.Type == "function" || t.Type == "" {
			decls = append(decls, GeminiFunctionDecl{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  sanitizeSchema(t.Function.Parameters),
			})
		}
	}
	if len(decls) == 0 {
		return nil
	}
	return []GeminiTool{{FunctionDeclarations: decls}}
}

func getStringArg(args map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			switch s := v.(type) {
			case string:
				return s
			default:
				b, _ := json.Marshal(s)
				return string(b)
			}
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type Gateway struct {
	cfg  Config
	pool *KeyPool

	sigMu    sync.Mutex
	sigOrder []string
	sigMap   map[string]string
}

func (g *Gateway) putSig(id, sig string) {
	if id == "" || sig == "" {
		return
	}
	g.sigMu.Lock()
	defer g.sigMu.Unlock()
	if g.sigMap == nil {
		g.sigMap = map[string]string{}
	}
	if _, exists := g.sigMap[id]; !exists {
		g.sigOrder = append(g.sigOrder, id)
		if len(g.sigOrder) > 2000 {
			for _, old := range g.sigOrder[:500] {
				delete(g.sigMap, old)
			}
			g.sigOrder = g.sigOrder[500:]
		}
	}
	g.sigMap[id] = sig
}

func (g *Gateway) getSig(id string) string {
	if id == "" {
		return ""
	}
	g.sigMu.Lock()
	defer g.sigMu.Unlock()
	return g.sigMap[id]
}

func uplinkOnce(ctx context.Context, pool *KeyPool, keyIdx int, model string, geminiReq GeminiRequest) (*GeminiResponse, int, error) {
	body, _ := json.Marshal(geminiReq)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, pool.Keys[keyIdx])
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, string(raw[:minInt(len(raw), 200)]))
	}
	var out GeminiResponse
	if json.Unmarshal(raw, &out) != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode error: %s", string(raw[:minInt(len(raw), 100)]))
	}
	return &out, resp.StatusCode, nil
}

func executeTool(name, rawArgs string) (string, error) {
	var args map[string]any
	json.Unmarshal([]byte(rawArgs), &args)
	switch name {
	case "terminal":
		cmdStr := getStringArg(args, "command", "cmd")
		if cmdStr == "" {
			return "", fmt.Errorf("empty command")
		}
		tc, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		c := exec.CommandContext(tc, "bash", "-c", cmdStr)
		c.Env = append(os.Environ(), "GATEWAY_ALLOWED=1")
		out, err := c.CombinedOutput()
		res := string(out)
		if err != nil && res == "" {
			res = err.Error()
		}
		return res, nil
	case "read_file":
		p := getStringArg(args, "path", "file_path", "filepath")
		clean := filepath.Clean(p)
		if !strings.HasPrefix(clean, "/opt/data") {
			return "", fmt.Errorf("forbidden: %s", p)
		}
		data, err := os.ReadFile(clean)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case "write_file":
		p := getStringArg(args, "path", "file_path", "filepath")
		content := getStringArg(args, "content", "data", "text")
		clean := filepath.Clean(p)
		if !strings.HasPrefix(clean, "/opt/data") {
			return "", fmt.Errorf("forbidden: %s", p)
		}
		os.MkdirAll(filepath.Dir(clean), 0o755)
		if err := os.WriteFile(clean, []byte(content), 0o644); err != nil {
			return "", err
		}
		return "ok: " + clean, nil
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (g *Gateway) runRequest(ctx context.Context, req *OpenAIRequest) (*OpenAIResponse, error) {
	model := req.Model
	if model == "" {
		model = g.cfg.GeminiModel
	}
	geminiModels, systemInstr := g.openAIToGemini(req.Messages)
	geminiTools := openAIToolsToGemini(req.Tools)
	msgs := geminiModels

	tried := map[int]bool{}
	var lastErr error
	needBackoff := false
	for n := 0; n < 10; n++ {
		if n > 0 && needBackoff {
			time.Sleep(backoff(n - 1))
		}
		needBackoff = false
		ki := g.pool.Select(tried)
		if ki < 0 {
			if lastErr == nil {
				lastErr = fmt.Errorf("semua key lagi cooldown, coba sebentar lagi")
			}
			if n >= 2 {
				break
			}
			time.Sleep(10 * time.Second)
			continue
		}
		tried[ki] = true
		genCfg := map[string]any{}
		if req.MaxTokens > 0 {
			genCfg["maxOutputTokens"] = float64(req.MaxTokens)
		} else {
			genCfg["maxOutputTokens"] = 8192
		}
		geminiReq := GeminiRequest{
			Contents:          msgs,
			SystemInstruction: systemInstr,
			GenerationConfig:  genCfg,
			Tools:             geminiTools,
		}
		geminiResp, status, err := uplinkOnce(ctx, g.pool, ki, model, geminiReq)
		if err != nil {
			switch status {
			case 429:
				g.pool.Mark429(ki)
				lastErr = fmt.Errorf("key #%d 429 quota exhausted", ki)
			case 403:
				g.pool.Mark403(ki)
				lastErr = fmt.Errorf("key #%d 403 forbidden", ki)
			case 400:
				return nil, fmt.Errorf("bad request: %v", err)
			default:
				needBackoff = true
				lastErr = fmt.Errorf("key #%d: %v", ki, err)
			}
			continue
		}
		if geminiResp.Error != nil {
			code := geminiResp.Error.Code
			if code == 429 || strings.Contains(geminiResp.Error.Message, "RESOURCE_EXHAUSTED") {
				g.pool.Mark429(ki)
				lastErr = fmt.Errorf("key #%d quota exhausted", ki)
			} else if code == 403 || strings.Contains(geminiResp.Error.Message, "PERMISSION_DENIED") {
				g.pool.Mark403(ki)
				lastErr = fmt.Errorf("key #%d forbidden", ki)
			} else {
				needBackoff = true
				lastErr = fmt.Errorf("key #%d: %s", ki, geminiResp.Error.Message)
			}
			continue
		}
		if len(geminiResp.Candidates) == 0 {
			lastErr = fmt.Errorf("no candidates from key #%d", ki)
			continue
		}
		cand := geminiResp.Candidates[0]
		var text string
		var toolCalls []OpenAIToolCall
		for _, part := range cand.Content.Parts {
			if part.Thought {
				continue
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				tc := OpenAIToolCall{
					ID:   "call_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + strconv.Itoa(len(toolCalls)),
					Type: "function",
				}
				tc.Function.Name = part.FunctionCall.Name
				tc.Function.Arguments = string(argsJSON)
				toolCalls = append(toolCalls, tc)
				g.putSig(tc.ID, part.ThoughtSignature)
				continue
			}
			text += part.Text
		}
		if strings.TrimSpace(text) == "" && len(toolCalls) == 0 {
			lastErr = fmt.Errorf("key #%d returned empty content", ki)
			continue
		}
		usage := OpenAIUsage{
			PromptTokens:     geminiResp.UsageMetadata.PromptTokenCount,
			CompletionTokens: geminiResp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      geminiResp.UsageMetadata.TotalTokenCount,
		}
		g.pool.Mark200(ki)
		finish := "stop"
		if len(toolCalls) > 0 {
			finish = "tool_calls"
		}
		return &OpenAIResponse{
			ID:      "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []OpenAIChoice{{
				Index: 0,
				Message: OpenAIMessage{
					Role:      "assistant",
					Content:   text,
					ToolCalls: toolCalls,
				},
				FinishReason: finish,
			}},
			Usage: usage,
		}, nil
	}
	return nil, lastErr
}

func writeJSONError(w http.ResponseWriter, status int, msg, errType string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"message": msg, "type": errType}})
}

func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	auth := r.Header.Get("Authorization")
	if auth != "Bearer "+g.cfg.GatewayAPIKey && auth != g.cfg.GatewayAPIKey {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var req OpenAIRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	resp, err := g.runRequest(r.Context(), &req)
	if err != nil || resp == nil {
		if err == nil {
			err = fmt.Errorf("gateway tidak dapat respon")
		}
		log.Printf("[gw] error: %v", err)
		writeJSONError(w, http.StatusInternalServerError, err.Error(), "api_error")
		return
	}
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		flusher, _ := w.(http.Flusher)
		content := ""
		var toolCalls []OpenAIToolCall
		finish := "stop"
		if len(resp.Choices) > 0 {
			if s, ok := resp.Choices[0].Message.Content.(string); ok {
				content = s
			}
			toolCalls = resp.Choices[0].Message.ToolCalls
			finish = resp.Choices[0].FinishReason
		}
		delta := map[string]any{"role": "assistant", "content": content}
		if len(toolCalls) > 0 {
			delta["tool_calls"] = toolCalls
		}
		chunk1 := map[string]any{
			"id": resp.ID, "object": "chat.completion.chunk", "created": resp.Created, "model": resp.Model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}},
		}
		b1, _ := json.Marshal(chunk1)
		fmt.Fprintf(w, "data: %s\n\n", b1)
		chunk2 := map[string]any{
			"id": resp.ID, "object": "chat.completion.chunk", "created": resp.Created, "model": resp.Model,
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finish}},
		}
		b2, _ := json.Marshal(chunk2)
		fmt.Fprintf(w, "data: %s\n\n", b2)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		log.Printf("[gw] stream model=%s %dms tools=%d", req.Model, time.Since(start).Milliseconds(), len(toolCalls))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
	total, healthy := g.pool.HealthyCount()
	log.Printf("[gw] model=%s %dms keys=%d/%d\n", req.Model, time.Since(start).Milliseconds(), healthy, total)
}

func (g *Gateway) handleHealth(w http.ResponseWriter, _ *http.Request) {
	total, healthy := g.pool.HealthyCount()
	json.NewEncoder(w).Encode(map[string]any{"status": "ok", "pool": map[string]int{"total": total, "healthy": healthy}})
}

func (g *Gateway) selftest() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ki := g.pool.Select(map[int]bool{})
	if ki < 0 {
		fmt.Println("HTTP Status: 500\nError: no key available")
		os.Exit(1)
	}
	geminiReq := GeminiRequest{
		Contents: []GeminiContent{{Role: "user", Parts: []GeminiPart{{Text: "jawab: OK"}}}},
		GenerationConfig: map[string]any{"maxOutputTokens": 10},
	}
	body, _ := json.Marshal(geminiReq)
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", g.cfg.GeminiModel, g.pool.Keys[ki])
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		fmt.Printf("HTTP Status: 500\nError: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("HTTP Status: %d\nError: %s\n", resp.StatusCode, string(raw[:minInt(len(raw), 300)]))
		os.Exit(1)
	}
	var out GeminiResponse
	json.Unmarshal(raw, &out)
	answer := ""
	if len(out.Candidates) > 0 {
		for _, part := range out.Candidates[0].Content.Parts {
			answer += part.Text
		}
	}
	fmt.Printf("HTTP Status: %d\nJawaban: %s\n", resp.StatusCode, answer)
	os.Exit(0)
}

func main() {
	cfg := loadConfig()
	cfg.resolve()
	pool := NewPool(cfg.KeysFile, cfg.StateFile)
	gw := &Gateway{cfg: cfg, pool: pool}

	isSelftest := false
	for _, arg := range os.Args[1:] {
		if arg == "--selftest" {
			isSelftest = true
			break
		}
	}
	if os.Getenv("GATEWAY_TEST") == "1" {
		isSelftest = true
	}
	if isSelftest {
		gw.selftest()
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", gw.handleChat)
	mux.HandleFunc("/health", gw.handleHealth)

	// tameng: kalau ada panic di handler, balas 500 jangan putus koneksi
	safeMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[gw] panic ketangkap: %v", rec)
				writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("panic: %v", rec), "api_error")
			}
		}()
		mux.ServeHTTP(w, r)
	})

	addr := "127.0.0.1:" + cfg.Port
	total, healthy := gw.pool.HealthyCount()
	log.Printf("[gw] starting on %s | keys: %d healthy / %d total", addr, healthy, total)
	server := &http.Server{Addr: addr, Handler: safeMux}
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
