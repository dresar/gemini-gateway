package main

import (
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func backoff(n int) time.Duration {
	if n < 0 {
		n = 0
	}
	if n > 6 {
		n = 6
	}
	ms := 200 << uint(n)
	if ms > 6000 {
		ms = 6000
	}
	return time.Duration(ms)*time.Millisecond + time.Duration(mathrand.Intn(121))*time.Millisecond
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

func extractTextContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, item := range v {
			if mp, ok := item.(map[string]any); ok {
				if t, ok := mp["text"].(string); ok {
					sb.WriteString(t)
				}
			}
		}
		return sb.String()
	default:
		return ""
	}
}

func (g *Gateway) openAIToGemini(messages []OpenAIMessage) ([]GeminiContent, *GeminiContent) {
	var rawContents []GeminiContent
	var system *GeminiContent
	toolNames := map[string]string{}
	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			if tc.ID != "" && tc.Function.Name != "" {
				toolNames[tc.ID] = tc.Function.Name
			}
		}
	}
	for _, m := range messages {
		text := extractTextContent(m.Content)
		if m.Role == "system" {
			if strings.TrimSpace(text) != "" {
				if system == nil {
					system = &GeminiContent{Parts: []GeminiPart{{Text: text}}}
				} else {
					system.Parts = append(system.Parts, GeminiPart{Text: text})
				}
			}
			continue
		}
		if m.Role == "tool" {
			name := toolNames[m.ToolCallID]
			if name == "" {
				name = "tool_result"
			}
			rawContents = append(rawContents, GeminiContent{
				Role: "user",
				Parts: []GeminiPart{{
					FunctionResponse: &GeminiFunctionResponse{
						Name:     name,
						Response: map[string]any{"result": text},
					},
				}},
			})
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
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
					if args == nil {
						args = map[string]any{}
					}
					sig := g.getSig(tc.ID)
					if sig == "" {
						parts = append(parts, GeminiPart{Text: fmt.Sprintf("[tool %s args: %s]", tc.Function.Name, tc.Function.Arguments)})
						continue
					}
					parts = append(parts, GeminiPart{
						FunctionCall:     &GeminiFunctionCall{Name: tc.Function.Name, Args: args},
						ThoughtSignature: sig,
					})
				}
				if len(parts) > 0 {
					rawContents = append(rawContents, GeminiContent{Role: "model", Parts: parts})
				}
				continue
			}
		} else if role != "model" {
			role = "user"
		}
		if text == "" {
			text = " "
		}
		rawContents = append(rawContents, GeminiContent{Role: role, Parts: []GeminiPart{{Text: text}}})
	}
	var contents []GeminiContent
	for _, item := range rawContents {
		if len(item.Parts) == 0 {
			continue
		}
		if len(contents) > 0 && contents[len(contents)-1].Role == item.Role {
			contents[len(contents)-1].Parts = append(contents[len(contents)-1].Parts, item.Parts...)
			continue
		}
		contents = append(contents, item)
	}
	if len(contents) == 0 {
		contents = append(contents, GeminiContent{Role: "user", Parts: []GeminiPart{{Text: " "}}})
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

func openAIToolChoiceToGemini(tc any) map[string]any {
	if tc == nil {
		return nil
	}
	switch v := tc.(type) {
	case string:
		switch strings.ToLower(v) {
		case "none":
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "NONE"}}
		case "required":
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "ANY"}}
		default:
			return map[string]any{"functionCallingConfig": map[string]any{"mode": "AUTO"}}
		}
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok && name != "" {
				return map[string]any{"functionCallingConfig": map[string]any{
					"mode":                 "ANY",
					"allowedFunctionNames": []string{name},
				}}
			}
		}
	}
	return nil
}

func parseRetryAfter(h http.Header) int64 {
	ra := strings.TrimSpace(h.Get("Retry-After"))
	if ra == "" {
		return 0
	}
	if sec, err := strconv.ParseInt(ra, 10, 64); err == nil && sec > 0 {
		return sec
	}
	if t, err := http.ParseTime(ra); err == nil {
		diff := int64(time.Until(t).Seconds())
		if diff > 0 {
			return diff
		}
	}
	return 0
}

func (g *Gateway) buildGeminiReq(req *OpenAIRequest, s RouterSettings) GeminiRequest {
	contents, systemInstr := g.openAIToGemini(req.Messages)
	geminiTools := openAIToolsToGemini(req.Tools)
	genCfg := map[string]any{}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = s.DefaultMaxTokens
	}
	genCfg["maxOutputTokens"] = float64(maxTok)
	if req.Temperature != nil {
		genCfg["temperature"] = *req.Temperature
	}
	return GeminiRequest{
		Contents:          contents,
		SystemInstruction: systemInstr,
		GenerationConfig:  genCfg,
		Tools:             geminiTools,
		ToolConfig:        openAIToolChoiceToGemini(req.ToolChoice),
	}
}

func isForeignAgentAlias(lower string) bool {
	return strings.HasPrefix(lower, "claude-") ||
		strings.HasPrefix(lower, "gpt-") ||
		strings.HasPrefix(lower, "o1") ||
		strings.HasPrefix(lower, "o3") ||
		strings.HasPrefix(lower, "o4")
}

func (g *Gateway) resolveUpstreamModel(prov *Provider, requestedModel, scope string, allProvs []*Provider) string {
	m := strings.TrimSpace(requestedModel)
	lower := strings.ToLower(m)
	if m == "" || lower == "auto" || lower == "router" {
		return prov.DefaultModel
	}
	if strings.HasPrefix(lower, prov.ID+"/") {
		stripped := strings.TrimSpace(m[len(prov.ID)+1:])
		if stripped != "" {
			return stripped
		}
		return prov.DefaultModel
	}
	if prov.APIType == apiTypeGemini && isForeignAgentAlias(lower) {
		return prov.DefaultModel
	}
	if prov.ID == "agnes" && !strings.HasPrefix(lower, "agnes") {
		return prov.DefaultModel
	}
	if scope != "" && scope != scopeGlobal {
		if strings.HasPrefix(lower, prov.ID+"-") || strings.EqualFold(lower, prov.DefaultModel) {
			return m
		}
		for _, other := range allProvs {
			if other.ID != prov.ID && (strings.HasPrefix(lower, other.ID+"-") || strings.HasPrefix(lower, other.ID+"/") || strings.EqualFold(lower, other.DefaultModel)) {
				return prov.DefaultModel
			}
		}
		return m
	}
	for _, other := range allProvs {
		if other.ID != prov.ID && (strings.HasPrefix(lower, other.ID+"-") || strings.HasPrefix(lower, other.ID+"/") || strings.EqualFold(lower, other.DefaultModel)) {
			return prov.DefaultModel
		}
	}
	return m
}
