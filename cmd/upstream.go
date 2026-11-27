package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

func applyUpstreamAuth(httpReq *http.Request, rawURL, authMode, apiKey string) string {
	switch authMode {
	case authModeQueryKey:
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		return rawURL + sep + "key=" + neturl.QueryEscape(apiKey)
	case authModeXAPIKey:
		httpReq.Header.Set("x-api-key", apiKey)
		return rawURL
	default:
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		return rawURL
	}
}

func (g *Gateway) callGeminiOnce(ctx context.Context, prov *Provider, keyRaw, model string, gemReq GeminiRequest) (*OpenAIResponse, error) {
	body, _ := json.Marshal(gemReq)
	base := strings.TrimRight(prov.BaseURL, "/")
	endpoint := fmt.Sprintf("%s/models/%s:generateContent", base, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &upstreamError{Status: 0, Message: err.Error()}
	}
	finalURL := applyUpstreamAuth(httpReq, endpoint, prov.AuthMode, keyRaw)
	if finalURL != endpoint {
		u, err := neturl.Parse(finalURL)
		if err == nil {
			httpReq.URL = u
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, &upstreamError{Status: 0, Message: sanitizeError(err.Error(), keyRaw)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &upstreamError{
			Status:     resp.StatusCode,
			Message:    sanitizeError(fmt.Sprintf("status %d: %s", resp.StatusCode, string(raw)), keyRaw),
			RetryAfter: parseRetryAfter(resp.Header),
		}
	}
	var out GeminiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &upstreamError{Status: 502, Message: "invalid gemini json response"}
	}
	if out.Error != nil {
		status := out.Error.Code
		if strings.Contains(out.Error.Message, "RESOURCE_EXHAUSTED") {
			status = 429
		} else if strings.Contains(out.Error.Message, "PERMISSION_DENIED") || strings.Contains(out.Error.Message, "API_KEY_INVALID") {
			status = 403
		}
		return nil, &upstreamError{Status: status, Message: sanitizeError(out.Error.Message, keyRaw)}
	}
	if len(out.Candidates) == 0 {
		return nil, &upstreamError{Status: 502, Message: "upstream returned no candidates"}
	}
	cand := out.Candidates[0]
	var text strings.Builder
	var toolCalls []OpenAIToolCall
	sawThought := false
	for _, part := range cand.Content.Parts {
		if part.Thought {
			sawThought = true
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
		text.WriteString(part.Text)
	}
	if strings.TrimSpace(text.String()) == "" && len(toolCalls) == 0 {
		if sawThought && strings.EqualFold(cand.FinishReason, "MAX_TOKENS") {
			return nil, &upstreamError{Status: 502, Message: "upstream returned only thought tokens without output"}
		}
		return nil, &upstreamError{Status: 502, Message: "upstream returned empty content"}
	}
	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
	} else if strings.EqualFold(cand.FinishReason, "MAX_TOKENS") {
		finish = "length"
	}
	return &OpenAIResponse{
		ID:      "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []OpenAIChoice{{
			Index: 0,
			Message: OpenAIMessage{
				Role:      "assistant",
				Content:   text.String(),
				ToolCalls: toolCalls,
			},
			FinishReason: finish,
		}},
		Usage: OpenAIUsage{
			PromptTokens:     out.UsageMetadata.PromptTokenCount,
			CompletionTokens: out.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      out.UsageMetadata.TotalTokenCount,
		},
	}, nil
}

func (g *Gateway) streamGeminiOnce(ctx context.Context, w http.ResponseWriter, prov *Provider, keyRaw, model string, gemReq GeminiRequest, trace routeTrace) (int, error) {
	body, _ := json.Marshal(gemReq)
	base := strings.TrimRight(prov.BaseURL, "/")
	endpoint := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse", base, model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, &upstreamError{Status: 0, Message: err.Error()}
	}
	finalURL := applyUpstreamAuth(httpReq, endpoint, prov.AuthMode, keyRaw)
	if finalURL != endpoint {
		if u, err := neturl.Parse(finalURL); err == nil {
			httpReq.URL = u
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := g.streamClient.Do(httpReq)
	if err != nil {
		return 0, &upstreamError{Status: 0, Message: sanitizeError(err.Error(), keyRaw)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return 0, &upstreamError{
			Status:     resp.StatusCode,
			Message:    sanitizeError(fmt.Sprintf("status %d: %s", resp.StatusCode, string(raw)), keyRaw),
			RetryAfter: parseRetryAfter(resp.Header),
		}
	}

	writeSSEHeaders(w, trace)
	flusher, _ := w.(http.Flusher)
	chatID := "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	created := time.Now().Unix()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	totalTokens := 0
	sentRole := false
	finishReason := "stop"

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk GeminiResponse
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		if chunk.UsageMetadata.TotalTokenCount > totalTokens {
			totalTokens = chunk.UsageMetadata.TotalTokenCount
		}
		if len(chunk.Candidates) == 0 {
			continue
		}
		cand := chunk.Candidates[0]
		if strings.EqualFold(cand.FinishReason, "MAX_TOKENS") {
			finishReason = "length"
		}
		var text strings.Builder
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
				finishReason = "tool_calls"
				continue
			}
			text.WriteString(part.Text)
		}
		if text.Len() == 0 && len(toolCalls) == 0 {
			continue
		}
		delta := map[string]any{}
		if !sentRole {
			delta["role"] = "assistant"
			sentRole = true
		}
		if text.Len() > 0 {
			delta["content"] = text.String()
		}
		if len(toolCalls) > 0 {
			delta["tool_calls"] = toolCalls
		}
		sseObj := map[string]any{
			"id":      chatID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}},
		}
		b, _ := json.Marshal(sseObj)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}

	finalObj := map[string]any{
		"id":      chatID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": finishReason}},
	}
	fb, _ := json.Marshal(finalObj)
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", fb)
	if flusher != nil {
		flusher.Flush()
	}
	return totalTokens, nil
}

func buildOpenAIPayload(req *OpenAIRequest, model string, s RouterSettings, stream bool) []byte {
	payload := map[string]any{
		"model":    model,
		"messages": req.Messages,
		"stream":   stream,
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = s.DefaultMaxTokens
	}
	payload["max_tokens"] = maxTok
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if len(req.Tools) > 0 {
		payload["tools"] = req.Tools
	}
	if req.ToolChoice != nil {
		payload["tool_choice"] = req.ToolChoice
	}
	b, _ := json.Marshal(payload)
	return b
}

func openAIChatEndpoint(baseURL string) string {
	b := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(b, "/chat/completions") {
		return b
	}
	return b + "/chat/completions"
}

func (g *Gateway) callOpenAIOnce(ctx context.Context, prov *Provider, keyRaw, model string, req *OpenAIRequest, s RouterSettings) (*OpenAIResponse, error) {
	body := buildOpenAIPayload(req, model, s, false)
	endpoint := openAIChatEndpoint(prov.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, &upstreamError{Status: 0, Message: err.Error()}
	}
	finalURL := applyUpstreamAuth(httpReq, endpoint, prov.AuthMode, keyRaw)
	if finalURL != endpoint {
		if u, err := neturl.Parse(finalURL); err == nil {
			httpReq.URL = u
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, &upstreamError{Status: 0, Message: sanitizeError(err.Error(), keyRaw)}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, &upstreamError{
			Status:     resp.StatusCode,
			Message:    sanitizeError(fmt.Sprintf("status %d: %s", resp.StatusCode, string(raw)), keyRaw),
			RetryAfter: parseRetryAfter(resp.Header),
		}
	}
	var out OpenAIResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, &upstreamError{Status: 502, Message: "invalid json response"}
	}
	if len(out.Choices) == 0 {
		return nil, &upstreamError{Status: 502, Message: "upstream returned no choices"}
	}
	if out.Object == "" {
		out.Object = "chat.completion"
	}
	if out.Model == "" {
		out.Model = model
	}
	if out.ID == "" {
		out.ID = "chatcmpl-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	if out.Created == 0 {
		out.Created = time.Now().Unix()
	}
	return &out, nil
}

func (g *Gateway) streamOpenAIOnce(ctx context.Context, w http.ResponseWriter, prov *Provider, keyRaw, model string, req *OpenAIRequest, s RouterSettings, trace routeTrace) (int, error) {
	body := buildOpenAIPayload(req, model, s, true)
	endpoint := openAIChatEndpoint(prov.BaseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, &upstreamError{Status: 0, Message: err.Error()}
	}
	finalURL := applyUpstreamAuth(httpReq, endpoint, prov.AuthMode, keyRaw)
	if finalURL != endpoint {
		if u, err := neturl.Parse(finalURL); err == nil {
			httpReq.URL = u
		}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := g.streamClient.Do(httpReq)
	if err != nil {
		return 0, &upstreamError{Status: 0, Message: sanitizeError(err.Error(), keyRaw)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return 0, &upstreamError{
			Status:     resp.StatusCode,
			Message:    sanitizeError(fmt.Sprintf("status %d: %s", resp.StatusCode, string(raw)), keyRaw),
			RetryAfter: parseRetryAfter(resp.Header),
		}
	}

	writeSSEHeaders(w, trace)
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReaderSize(resp.Body, 32*1024)
	totalTokens := 0
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			_, _ = w.Write([]byte(line))
			if strings.HasPrefix(line, "data:") {
				trimmed := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if trimmed != "" && trimmed != "[DONE]" && strings.Contains(trimmed, `"total_tokens"`) {
					var meta struct {
						Usage OpenAIUsage `json:"usage"`
					}
					if json.Unmarshal([]byte(trimmed), &meta) == nil && meta.Usage.TotalTokens > 0 {
						totalTokens = meta.Usage.TotalTokens
					}
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
		}
		if err != nil {
			break
		}
	}
	if flusher != nil {
		flusher.Flush()
	}
	return totalTokens, nil
}

func writeSSEHeaders(w http.ResponseWriter, trace routeTrace) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Gateway-Provider", trace.Provider)
	w.Header().Set("X-Gateway-Key-Index", strconv.Itoa(trace.KeyIndex))
	w.Header().Set("X-Gateway-Model", trace.Model)
	w.Header().Set("X-Gateway-Scope", trace.Scope)
	w.Header().Set("X-Gateway-Route-Us", strconv.FormatInt(trace.RouteUs, 10))
}

func isModelUnavailable404(err error) bool {
	var ue *upstreamError
	return errors.As(err, &ue) && ue.Status == http.StatusNotFound
}

func geminiFallbackModels(primary string) []string {
	candidates := []string{"gemini-3-flash-preview", "gemini-2.5-flash-lite", "gemini-2.5-flash"}
	out := make([]string, 0, len(candidates))
	for _, m := range candidates {
		if !strings.EqualFold(m, strings.TrimSpace(primary)) {
			out = append(out, m)
		}
	}
	return out
}

func (g *Gateway) handleUpstreamFailure(key *KeyEntry, err error) (abortBadRequest bool, status int, errCode string, cleanMsg string) {
	msg := err.Error()
	var retryAfter int64
	var ue *upstreamError
	if errors.As(err, &ue) {
		status = ue.Status
		msg = ue.Message
		retryAfter = ue.RetryAfter
	}
	if status == 0 {
		status = 502
	}
	code, label, detail := classifyUpstreamError(status, sanitizeError(msg, key.Key))
	if status == 400 && code != "invalid_key" && code != "credit_exhausted" {
		full := sanitizeError(fmt.Sprintf("%s (400): %s", label, detail), key.Key)
		return true, 400, code, full
	}
	_, code, cleanMsg = g.pool.MarkFail(key.ID, status, retryAfter, msg)
	return false, status, code, cleanMsg
}

func (g *Gateway) executeRoutedChat(ctx context.Context, w http.ResponseWriter, req *OpenAIRequest, client ClientKey, forcedProvider string) (*OpenAIResponse, routeTrace, error) {
	routeStart := time.Now()
	s := g.pool.Settings()
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(s.RequestTimeoutSec)*time.Second)
	defer cancel()

	scope := client.Scope
	if scope == "" {
		scope = scopeGlobal
	}
	var providers []*Provider
	if forcedProvider != "" {
		if pr, ok := g.pool.GetProvider(forcedProvider); ok && pr.Enabled {
			providers = []*Provider{&pr}
		}
	} else {
		providers = g.pool.ProviderOrder(req.Model, scope)
	}
	if len(providers) == 0 {
		return nil, routeTrace{Scope: scope}, fmt.Errorf("tidak ada provider aktif untuk kategori %s", scope)
	}

	g.reqTotal.Add(1)
	tried := make(map[string]bool)
	var lastErr error
	attempts := 0
	loggedFailure := false

	for pIdx, prov := range providers {
		g.pool.IncProviderReq(prov.ID)
		if pIdx > 0 {
			g.reqFall.Add(1)
		}

		baseTargetModel := g.resolveUpstreamModel(prov, req.Model, scope, providers)
		trimmedReqModel := strings.TrimSpace(req.Model)
		isAutoReq := trimmedReqModel == "" || strings.EqualFold(trimmedReqModel, "auto") || strings.EqualFold(trimmedReqModel, "router") || strings.EqualFold(trimmedReqModel, prov.ID)
		rotateModels := prov.ModelRotationEnabled && s.ModelRotationMode != "off" && len(prov.RotationModels) > 0 && (isAutoReq || s.ModelRoutingPolicy == "always_rotate")
		triedModels := make(map[string]bool)
		var gemReq GeminiRequest
		if prov.APIType == apiTypeGemini {
			gemReq = g.buildGeminiReq(req, s)
		}

		for n := 0; n < s.MaxRetriesPerProvider; n++ {
			if n > 0 {
				select {
				case <-reqCtx.Done():
					return nil, routeTrace{}, reqCtx.Err()
				case <-time.After(backoff(n - 1)):
				}
			}
			targetModel := baseTargetModel
			if rotateModels {
				pickedModel := g.pool.SelectProviderModel(prov.ID, triedModels)
				if pickedModel == "" && len(triedModels) > 0 {
					triedModels = make(map[string]bool)
					pickedModel = g.pool.SelectProviderModel(prov.ID, triedModels)
				}
				if pickedModel != "" {
					targetModel = pickedModel
				}
			}
			pickStart := time.Now()
			key := g.pool.SelectKey(prov.ID, tried)
			if key == nil && rotateModels && len(triedModels) < len(prov.RotationModels) {
				key = g.pool.SelectKey(prov.ID, nil)
			}
			routeUs := time.Since(pickStart).Microseconds()
			if n == 0 && pIdx == 0 {
				routeUs = time.Since(routeStart).Microseconds()
			}
			if key == nil {
				if lastErr == nil {
					lastErr = fmt.Errorf("semua API key %s sedang cooldown, mati, atau kosong", prov.Name)
				}
				break
			}
			tried[key.ID] = true
			attempts++
			if attempts > 1 {
				g.reqRetry.Add(1)
			}

			trace := routeTrace{
				Provider:     prov.ID,
				ProviderName: prov.Name,
				KeyIndex:     key.ProviderIndex,
				KeyMasked:    key.Masked,
				ClientName:   client.Name,
				Scope:        scope,
				Model:        targetModel,
				RouteUs:      routeUs,
				Attempts:     attempts,
				Fallback:     pIdx > 0,
			}

			callStart := time.Now()
			allowGeminiModelFallback := !rotateModels && prov.APIType == apiTypeGemini &&
				(targetModel == prov.DefaultModel || trimmedReqModel == "" || strings.EqualFold(trimmedReqModel, "auto") || strings.EqualFold(trimmedReqModel, "router"))
			if req.Stream && w != nil {
				var tokens int
				var err error
				if prov.APIType == apiTypeGemini {
					tokens, err = g.streamGeminiOnce(reqCtx, w, prov, key.Key, targetModel, gemReq, trace)
					if err != nil && allowGeminiModelFallback && isModelUnavailable404(err) {
						for _, fbModel := range geminiFallbackModels(targetModel) {
							trace.Model = fbModel
							tokens, err = g.streamGeminiOnce(reqCtx, w, prov, key.Key, fbModel, gemReq, trace)
							if err == nil {
								targetModel = fbModel
								break
							}
							if !isModelUnavailable404(err) {
								break
							}
						}
					}
				} else {
					tokens, err = g.streamOpenAIOnce(reqCtx, w, prov, key.Key, targetModel, req, s, trace)
				}
				lat := time.Since(callStart).Milliseconds()
				trace.LatencyMs = lat
				trace.Tokens = tokens
				if err == nil {
					g.pool.Mark200(key.ID, lat, routeUs, tokens)
					if rotateModels {
						g.pool.MarkModelOK(prov.ID, targetModel)
					}
					g.pool.RecordClientUsage(client.ID, tokens)
					g.reqOK.Add(1)
					g.tokTotal.Add(int64(tokens))
					g.pool.RecordLog(RequestLog{
						Provider:       prov.ID,
						ProviderName:   prov.Name,
						KeyIndex:       key.ProviderIndex,
						KeyMasked:      key.Masked,
						ClientName:     client.Name,
						Scope:          scope,
						RequestedModel: req.Model,
						UpstreamModel:  targetModel,
						Status:         200,
						RouteUs:        routeUs,
						LatencyMs:      lat,
						Tokens:         tokens,
						Stream:         true,
						Fallback:       pIdx > 0,
						Attempts:       attempts,
					})
					return nil, trace, nil
				}
				lastErr = err
				abortBad := g.recordAttemptFailure(prov, s, key, targetModel, req.Model, client, scope, rotateModels, tried, triedModels, routeUs, lat, true, pIdx > 0, attempts, err)
				loggedFailure = true
				if abortBad {
					g.reqErr.Add(1)
					return nil, trace, err
				}
				continue
			}

			var resp *OpenAIResponse
			var err error
			if prov.APIType == apiTypeGemini {
				resp, err = g.callGeminiOnce(reqCtx, prov, key.Key, targetModel, gemReq)
				if err != nil && allowGeminiModelFallback && isModelUnavailable404(err) {
					for _, fbModel := range geminiFallbackModels(targetModel) {
						trace.Model = fbModel
						resp, err = g.callGeminiOnce(reqCtx, prov, key.Key, fbModel, gemReq)
						if err == nil && resp != nil {
							targetModel = fbModel
							break
						}
						if !isModelUnavailable404(err) {
							break
						}
					}
				}
			} else {
				resp, err = g.callOpenAIOnce(reqCtx, prov, key.Key, targetModel, req, s)
			}
			lat := time.Since(callStart).Milliseconds()
			if err == nil && resp != nil {
				tokens := resp.Usage.TotalTokens
				trace.LatencyMs = lat
				trace.Tokens = tokens
				g.pool.Mark200(key.ID, lat, routeUs, tokens)
				if rotateModels {
					g.pool.MarkModelOK(prov.ID, targetModel)
				}
				g.pool.RecordClientUsage(client.ID, tokens)
				g.reqOK.Add(1)
				g.tokTotal.Add(int64(tokens))
				g.pool.RecordLog(RequestLog{
					Provider:       prov.ID,
					ProviderName:   prov.Name,
					KeyIndex:       key.ProviderIndex,
					KeyMasked:      key.Masked,
					ClientName:     client.Name,
					Scope:          scope,
					RequestedModel: req.Model,
					UpstreamModel:  targetModel,
					Status:         200,
					RouteUs:        routeUs,
					LatencyMs:      lat,
					Tokens:         tokens,
					Stream:         false,
					Fallback:       pIdx > 0,
					Attempts:       attempts,
				})
				return resp, trace, nil
			}
			lastErr = err
			abortBad := g.recordAttemptFailure(prov, s, key, targetModel, req.Model, client, scope, rotateModels, tried, triedModels, routeUs, lat, false, pIdx > 0, attempts, err)
			loggedFailure = true
			if abortBad {
				g.reqErr.Add(1)
				return nil, trace, err
			}
		}
	}

	g.reqErr.Add(1)
	if lastErr == nil {
		lastErr = errNoEligibleKey
	}
	if !loggedFailure {
		lastProv := providers[len(providers)-1]
		g.pool.RecordLog(RequestLog{
			Provider:       lastProv.ID,
			ProviderName:   lastProv.Name,
			KeyIndex:       -1,
			ClientName:     client.Name,
			Scope:          scope,
			RequestedModel: req.Model,
			Status:         503,
			Stream:         req.Stream,
			Fallback:       len(providers) > 1,
			Attempts:       attempts,
			ErrorCode:      "pool_empty",
			Error:          lastErr.Error(),
		})
	}
	return nil, routeTrace{Attempts: attempts, Scope: scope}, lastErr
}

func (g *Gateway) recordAttemptFailure(
	prov *Provider,
	s RouterSettings,
	key *KeyEntry,
	targetModel, requestedModel string,
	client ClientKey,
	scope string,
	rotateModels bool,
	triedKeys, triedModels map[string]bool,
	routeUs, lat int64,
	stream, fallback bool,
	attempts int,
	err error,
) bool {
	status := 502
	msg := err.Error()
	var ue *upstreamError
	if errors.As(err, &ue) {
		if ue.Status > 0 {
			status = ue.Status
		}
		msg = ue.Message
	}
	errCode, label, detail := classifyUpstreamError(status, sanitizeError(msg, key.Key))
	cleanMsg := sanitizeError(fmt.Sprintf("%s (%d): %s", label, status, detail), key.Key)
	if status == http.StatusBadRequest && errCode != "invalid_key" && errCode != "credit_exhausted" {
		g.pool.RecordLog(RequestLog{
			Provider:       prov.ID,
			ProviderName:   prov.Name,
			KeyIndex:       key.ProviderIndex,
			KeyMasked:      key.Masked,
			ClientName:     client.Name,
			Scope:          scope,
			RequestedModel: requestedModel,
			UpstreamModel:  targetModel,
			Status:         status,
			RouteUs:        routeUs,
			LatencyMs:      lat,
			Stream:         stream,
			Fallback:       fallback,
			Attempts:       attempts,
			ErrorCode:      errCode,
			Error:          cleanMsg,
		})
		return true
	}

	var abortBad bool
	if rotateModels && errCode != "invalid_key" && errCode != "permission_denied" && errCode != "credit_exhausted" && len(triedModels)+1 < len(prov.RotationModels) {
		triedModels[targetModel] = true
		g.pool.MarkModelCooldown(prov.ID, targetModel, int64(s.CooldownBaseSec))
		delete(triedKeys, key.ID)
	} else {
		if rotateModels {
			triedModels[targetModel] = true
			g.pool.MarkModelCooldown(prov.ID, targetModel, int64(s.CooldownBaseSec))
		}
		abortBad, status, errCode, cleanMsg = g.handleUpstreamFailure(key, err)
	}
	g.pool.RecordLog(RequestLog{
		Provider:       prov.ID,
		ProviderName:   prov.Name,
		KeyIndex:       key.ProviderIndex,
		KeyMasked:      key.Masked,
		ClientName:     client.Name,
		Scope:          scope,
		RequestedModel: requestedModel,
		UpstreamModel:  targetModel,
		Status:         status,
		RouteUs:        routeUs,
		LatencyMs:      lat,
		Stream:         stream,
		Fallback:       fallback,
		Attempts:       attempts,
		ErrorCode:      errCode,
		Error:          cleanMsg,
	})
	return abortBad
}
