package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	client, ok := g.pool.AuthenticateClientToken(extractClientToken(r))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var req OpenAIRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	if len(req.Messages) == 0 {
		writeJSONError(w, http.StatusBadRequest, "messages cannot be empty", "invalid_request_error")
		return
	}
	if req.Stream {
		_, trace, err := g.executeRoutedChat(r.Context(), w, &req, client, "")
		if err != nil {
			var ue *upstreamError
			code := http.StatusBadGateway
			errType := "api_error"
			if errors.As(err, &ue) && ue.Status == 400 {
				code = http.StatusBadRequest
				errType = "invalid_request_error"
			}
			writeJSONError(w, code, err.Error(), errType)
			return
		}
		log.Printf("[gw] stream scope=%s provider=%s key=#%d route=%dus %dms tokens=%d fallback=%v",
			trace.Scope, trace.Provider, trace.KeyIndex, trace.RouteUs, trace.LatencyMs, trace.Tokens, trace.Fallback)
		return
	}

	resp, trace, err := g.executeRoutedChat(r.Context(), nil, &req, client, "")
	if err != nil || resp == nil {
		if err == nil {
			err = errors.New("gateway tidak mendapat respon")
		}
		var ue *upstreamError
		code := http.StatusBadGateway
		errType := "api_error"
		if errors.As(err, &ue) && ue.Status == 400 {
			code = http.StatusBadRequest
			errType = "invalid_request_error"
		}
		log.Printf("[gw] error: %v", err)
		writeJSONError(w, code, err.Error(), errType)
		return
	}
	w.Header().Set("X-Gateway-Provider", trace.Provider)
	w.Header().Set("X-Gateway-Key-Index", strconv.Itoa(trace.KeyIndex))
	w.Header().Set("X-Gateway-Model", trace.Model)
	w.Header().Set("X-Gateway-Scope", trace.Scope)
	w.Header().Set("X-Gateway-Route-Us", strconv.FormatInt(trace.RouteUs, 10))
	writeJSON(w, http.StatusOK, resp)
	log.Printf("[gw] scope=%s provider=%s key=#%d route=%dus %dms tokens=%d fallback=%v",
		trace.Scope, trace.Provider, trace.KeyIndex, trace.RouteUs, trace.LatencyMs, trace.Tokens, trace.Fallback)
}

func anthropicWriteErr(w http.ResponseWriter, status int, errType, msg string) {
	writeJSON(w, status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    errType,
			"message": msg,
		},
	})
}

func (g *Gateway) handleAnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		anthropicWriteErr(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	if _, ok := g.pool.AuthenticateClientToken(extractClientToken(r)); !ok {
		anthropicWriteErr(w, http.StatusUnauthorized, "authentication_error", "unauthorized")
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	tok := len(raw) / 4
	if tok < 1 {
		tok = 1
	}
	writeJSON(w, http.StatusOK, map[string]any{"input_tokens": tok})
}

func (g *Gateway) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		anthropicWriteErr(w, http.StatusMethodNotAllowed, "invalid_request_error", "method not allowed")
		return
	}
	client, ok := g.pool.AuthenticateClientToken(extractClientToken(r))
	if !ok {
		anthropicWriteErr(w, http.StatusUnauthorized, "authentication_error", "unauthorized")
		return
	}
	var body struct {
		Model     string   `json:"model"`
		System    any      `json:"system"`
		MaxTokens int      `json:"max_tokens"`
		Stream    bool     `json:"stream"`
		Temp      *float64 `json:"temperature"`
		Messages  []struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		} `json:"messages"`
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			InputSchema any    `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
		anthropicWriteErr(w, http.StatusBadRequest, "invalid_request_error", "invalid json")
		return
	}

	var msgs []OpenAIMessage
	if sysTxt := strings.TrimSpace(extractTextContent(body.System)); sysTxt != "" {
		msgs = append(msgs, OpenAIMessage{Role: "system", Content: sysTxt})
	}
	for _, m := range body.Messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		if role == "" {
			role = "user"
		}
		switch v := m.Content.(type) {
		case string:
			msgs = append(msgs, OpenAIMessage{Role: role, Content: v})
		case []any:
			var textBuf strings.Builder
			var tCalls []OpenAIToolCall
			for _, rawItem := range v {
				mp, ok := rawItem.(map[string]any)
				if !ok {
					continue
				}
				typ, _ := mp["type"].(string)
				switch typ {
				case "text":
					if t, ok := mp["text"].(string); ok {
						textBuf.WriteString(t)
					}
				case "tool_use":
					id, _ := mp["id"].(string)
					name, _ := mp["name"].(string)
					argsBytes, _ := json.Marshal(mp["input"])
					var tc OpenAIToolCall
					tc.ID = id
					tc.Type = "function"
					tc.Function.Name = name
					tc.Function.Arguments = string(argsBytes)
					tCalls = append(tCalls, tc)
				case "tool_result":
					id, _ := mp["tool_use_id"].(string)
					resTxt := extractTextContent(mp["content"])
					msgs = append(msgs, OpenAIMessage{
						Role:       "tool",
						ToolCallID: id,
						Content:    resTxt,
					})
				}
			}
			if textBuf.Len() > 0 || len(tCalls) > 0 {
				msgs = append(msgs, OpenAIMessage{
					Role:      role,
					Content:   textBuf.String(),
					ToolCalls: tCalls,
				})
			}
		}
	}
	if len(msgs) == 0 {
		anthropicWriteErr(w, http.StatusBadRequest, "invalid_request_error", "messages cannot be empty")
		return
	}

	var openTools []OpenAITool
	for _, t := range body.Tools {
		ft := OpenAITool{Type: "function"}
		ft.Function.Name = t.Name
		ft.Function.Description = t.Description
		ft.Function.Parameters = t.InputSchema
		openTools = append(openTools, ft)
	}

	openReq := OpenAIRequest{
		Model:       body.Model,
		Messages:    msgs,
		Tools:       openTools,
		MaxTokens:   body.MaxTokens,
		Temperature: body.Temp,
		Stream:      false,
	}
	resp, trace, err := g.executeRoutedChat(r.Context(), nil, &openReq, client, "")
	if err != nil || resp == nil {
		if err == nil {
			err = errors.New("gateway tidak mendapat respon")
		}
		anthropicWriteErr(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}

	msgID := "msg_" + randomHex(8)
	outModel := trace.Model
	if outModel == "" {
		outModel = body.Model
	}
	var blocks []map[string]any
	stopReason := "end_turn"
	if len(resp.Choices) > 0 {
		ch := resp.Choices[0]
		if txt := extractTextContent(ch.Message.Content); txt != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": txt})
		}
		for _, tc := range ch.Message.ToolCalls {
			var inp map[string]any
			if json.Unmarshal([]byte(tc.Function.Arguments), &inp) != nil || inp == nil {
				inp = map[string]any{}
			}
			blocks = append(blocks, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": inp,
			})
			stopReason = "tool_use"
		}
		if ch.FinishReason == "length" && stopReason == "end_turn" {
			stopReason = "max_tokens"
		}
	}
	if len(blocks) == 0 {
		blocks = append(blocks, map[string]any{"type": "text", "text": ""})
	}

	w.Header().Set("X-Gateway-Provider", trace.Provider)
	w.Header().Set("X-Gateway-Model", outModel)
	w.Header().Set("X-Gateway-Route-Us", strconv.FormatInt(trace.RouteUs, 10))

	if !body.Stream {
		writeJSON(w, http.StatusOK, map[string]any{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"model":         outModel,
			"content":       blocks,
			"stop_reason":   stopReason,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  resp.Usage.PromptTokens,
				"output_tokens": resp.Usage.CompletionTokens,
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	emitSSE := func(event string, payload any) {
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
		if flusher != nil {
			flusher.Flush()
		}
	}
	emitSSE("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         outModel,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  resp.Usage.PromptTokens,
				"output_tokens": 0,
			},
		},
	})
	for idx, blk := range blocks {
		typ, _ := blk["type"].(string)
		if typ == "text" {
			txt, _ := blk["text"].(string)
			emitSSE("content_block_start", map[string]any{
				"type":          "content_block_start",
				"index":         idx,
				"content_block": map[string]any{"type": "text", "text": ""},
			})
			if txt != "" {
				emitSSE("content_block_delta", map[string]any{
					"type":  "content_block_delta",
					"index": idx,
					"delta": map[string]any{"type": "text_delta", "text": txt},
				})
			}
			emitSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
		} else if typ == "tool_use" {
			emitSSE("content_block_start", map[string]any{
				"type":  "content_block_start",
				"index": idx,
				"content_block": map[string]any{
					"type":  "tool_use",
					"id":    blk["id"],
					"name":  blk["name"],
					"input": map[string]any{},
				},
			})
			inpBytes, _ := json.Marshal(blk["input"])
			emitSSE("content_block_delta", map[string]any{
				"type":  "content_block_delta",
				"index": idx,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": string(inpBytes)},
			})
			emitSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
		}
	}
	emitSSE("message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": resp.Usage.CompletionTokens},
	})
	emitSSE("message_stop", map[string]any{"type": "message_stop"})
}

func (g *Gateway) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	client, ok := g.pool.AuthenticateClientToken(extractClientToken(r))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	var body struct {
		Model           string   `json:"model"`
		Instructions    string   `json:"instructions"`
		Input           any      `json:"input"`
		MaxOutputTokens int      `json:"max_output_tokens"`
		Temperature     *float64 `json:"temperature"`
		Stream          bool     `json:"stream"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}

	var msgs []OpenAIMessage
	if strings.TrimSpace(body.Instructions) != "" {
		msgs = append(msgs, OpenAIMessage{Role: "system", Content: strings.TrimSpace(body.Instructions)})
	}
	switch v := body.Input.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			msgs = append(msgs, OpenAIMessage{Role: "user", Content: v})
		}
	case []any:
		for _, item := range v {
			mp, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role, _ := mp["role"].(string)
			if role == "" {
				role = "user"
			}
			txt := extractTextContent(mp["content"])
			if txt == "" {
				if t, ok := mp["text"].(string); ok {
					txt = t
				}
			}
			if txt != "" {
				msgs = append(msgs, OpenAIMessage{Role: role, Content: txt})
			}
		}
	}
	if len(msgs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "input cannot be empty", "invalid_request_error")
		return
	}

	openReq := OpenAIRequest{
		Model:       body.Model,
		Messages:    msgs,
		MaxTokens:   body.MaxOutputTokens,
		Temperature: body.Temperature,
		Stream:      false,
	}
	resp, trace, err := g.executeRoutedChat(r.Context(), nil, &openReq, client, "")
	if err != nil || resp == nil {
		if err == nil {
			err = errors.New("gateway tidak mendapat respon")
		}
		writeJSONError(w, http.StatusBadGateway, err.Error(), "api_error")
		return
	}

	reply := ""
	if len(resp.Choices) > 0 {
		reply = extractTextContent(resp.Choices[0].Message.Content)
	}
	respID := "resp_" + randomHex(8)
	msgID := "msg_" + randomHex(8)
	outModel := trace.Model
	if outModel == "" {
		outModel = body.Model
	}
	respObj := map[string]any{
		"id":          respID,
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      "completed",
		"model":       outModel,
		"output_text": reply,
		"output": []map[string]any{{
			"id":     msgID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []map[string]any{{
				"type":        "output_text",
				"text":        reply,
				"annotations": []any{},
			}},
		}},
		"usage": map[string]any{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
			"total_tokens":  resp.Usage.TotalTokens,
		},
	}

	w.Header().Set("X-Gateway-Provider", trace.Provider)
	w.Header().Set("X-Gateway-Model", outModel)
	w.Header().Set("X-Gateway-Route-Us", strconv.FormatInt(trace.RouteUs, 10))

	if !body.Stream {
		writeJSON(w, http.StatusOK, respObj)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	emitSSE := func(event string, payload any) {
		b, _ := json.Marshal(payload)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(b))
		if flusher != nil {
			flusher.Flush()
		}
	}
	emitSSE("response.created", map[string]any{"type": "response.created", "response": map[string]any{"id": respID, "status": "in_progress", "model": outModel}})
	emitSSE("response.output_text.delta", map[string]any{"type": "response.output_text.delta", "item_id": msgID, "output_index": 0, "content_index": 0, "delta": reply})
	emitSSE("response.output_text.done", map[string]any{"type": "response.output_text.done", "item_id": msgID, "output_index": 0, "content_index": 0, "text": reply})
	emitSSE("response.completed", map[string]any{"type": "response.completed", "response": respObj})
}

func (g *Gateway) handleGeminiNative(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed", "invalid_request_error")
		return
	}
	client, ok := g.pool.AuthenticateClientToken(extractClientToken(r))
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized", "auth_error")
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, "/v1beta/models/")
	parts := strings.SplitN(tail, ":", 2)
	model := strings.TrimSpace(parts[0])
	action := "generateContent"
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		action = strings.TrimSpace(parts[1])
	}
	if action == "countTokens" {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		tok := len(raw) / 4
		if tok < 1 {
			tok = 1
		}
		writeJSON(w, http.StatusOK, map[string]any{"totalTokens": tok})
		return
	}

	var body GeminiRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json", "invalid_request_error")
		return
	}
	var msgs []OpenAIMessage
	if body.SystemInstruction != nil {
		var sysSB strings.Builder
		for _, p := range body.SystemInstruction.Parts {
			sysSB.WriteString(p.Text)
		}
		if strings.TrimSpace(sysSB.String()) != "" {
			msgs = append(msgs, OpenAIMessage{Role: "system", Content: sysSB.String()})
		}
	}
	for _, c := range body.Contents {
		role := c.Role
		if role == "model" {
			role = "assistant"
		} else if role == "" {
			role = "user"
		}
		var sb strings.Builder
		for _, p := range c.Parts {
			sb.WriteString(p.Text)
		}
		msgs = append(msgs, OpenAIMessage{Role: role, Content: sb.String()})
	}
	if len(msgs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "contents cannot be empty", "invalid_request_error")
		return
	}
	openReq := OpenAIRequest{
		Model:    model,
		Messages: msgs,
	}
	if body.GenerationConfig != nil {
		if mv, ok := body.GenerationConfig["maxOutputTokens"].(float64); ok && int(mv) > 0 {
			openReq.MaxTokens = int(mv)
		}
	}
	resp, trace, err := g.executeRoutedChat(r.Context(), nil, &openReq, client, "")
	if err != nil || resp == nil {
		if err == nil {
			err = errors.New("gateway tidak mendapat respon")
		}
		writeJSONError(w, http.StatusBadGateway, err.Error(), "api_error")
		return
	}
	reply := ""
	if len(resp.Choices) > 0 {
		reply = extractTextContent(resp.Choices[0].Message.Content)
	}
	gemResp := map[string]any{
		"candidates": []map[string]any{{
			"content": map[string]any{
				"role":  "model",
				"parts": []map[string]any{{"text": reply}},
			},
			"finishReason": "STOP",
		}},
		"usageMetadata": map[string]any{
			"promptTokenCount":     resp.Usage.PromptTokens,
			"candidatesTokenCount": resp.Usage.CompletionTokens,
			"totalTokenCount":      resp.Usage.TotalTokens,
		},
	}
	w.Header().Set("X-Gateway-Provider", trace.Provider)
	w.Header().Set("X-Gateway-Model", trace.Model)
	if strings.Contains(action, "streamGenerateContent") && r.URL.Query().Get("alt") == "sse" {
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(gemResp)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		return
	}
	writeJSON(w, http.StatusOK, gemResp)
}
