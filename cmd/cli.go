package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

func runCLIMode(args []string) bool {
	if len(args) == 0 {
		return false
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	switch cmd {
	case "ask", "exec", "agent-env", "health", "help", "--help", "-h":
	default:
		return false
	}

	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		fmt.Println("KaRouter CLI:")
		fmt.Println("  geminigw [--listen 127.0.0.1:8031] [--db ./gateway.db]")
		fmt.Println("  geminigw exec|ask [--provider auto] [--model auto] [--url URL] [--key KEY] [--json] \"prompt\"")
		fmt.Println("  geminigw agent-env [--url http://127.0.0.1:8031] [--key kr_...] [--shell bash|pwsh]")
		fmt.Println("  geminigw health [--db ./gateway.db]")
		return true
	}

	var dbFlag, urlFlag, keyFlag, provFlag, modelFlag, sysFlag, shellFlag string
	var jsonOut bool
	var rest []string
	for i := 1; i < len(args); i++ {
		a := args[i]
		nextVal := func() string {
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch {
		case a == "--db":
			dbFlag = nextVal()
		case strings.HasPrefix(a, "--db="):
			dbFlag = strings.TrimPrefix(a, "--db=")
		case a == "--url":
			urlFlag = nextVal()
		case strings.HasPrefix(a, "--url="):
			urlFlag = strings.TrimPrefix(a, "--url=")
		case a == "--key":
			keyFlag = nextVal()
		case strings.HasPrefix(a, "--key="):
			keyFlag = strings.TrimPrefix(a, "--key=")
		case a == "--provider":
			provFlag = nextVal()
		case strings.HasPrefix(a, "--provider="):
			provFlag = strings.TrimPrefix(a, "--provider=")
		case a == "--model":
			modelFlag = nextVal()
		case strings.HasPrefix(a, "--model="):
			modelFlag = strings.TrimPrefix(a, "--model=")
		case a == "--system":
			sysFlag = nextVal()
		case strings.HasPrefix(a, "--system="):
			sysFlag = strings.TrimPrefix(a, "--system=")
		case a == "--shell":
			shellFlag = nextVal()
		case strings.HasPrefix(a, "--shell="):
			shellFlag = strings.TrimPrefix(a, "--shell=")
		case a == "--json":
			jsonOut = true
		default:
			rest = append(rest, a)
		}
	}

	if cmd == "agent-env" {
		if urlFlag == "" {
			urlFlag = "http://127.0.0.1:8031"
		}
		urlFlag = strings.TrimRight(urlFlag, "/")
		if keyFlag == "" {
			keyFlag = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
			if keyFlag == "" {
				keyFlag = "MASUKKAN_KEY_AKSES"
			}
		}
		isPwsh := strings.EqualFold(shellFlag, "pwsh") || (shellFlag == "" && os.Getenv("PSModulePath") != "" && os.PathSeparator == '\\')
		if isPwsh && !strings.EqualFold(shellFlag, "bash") {
			fmt.Printf("$env:OPENAI_BASE_URL=%q\n", urlFlag+"/v1")
			fmt.Printf("$env:OPENAI_API_KEY=%q\n", keyFlag)
			fmt.Printf("$env:ANTHROPIC_BASE_URL=%q\n", urlFlag)
			fmt.Printf("$env:ANTHROPIC_API_KEY=%q\n", keyFlag)
			fmt.Printf("$env:GOOGLE_GEMINI_BASE_URL=%q\n", urlFlag)
			fmt.Printf("$env:GEMINI_API_KEY=%q\n", keyFlag)
		} else {
			fmt.Printf("export OPENAI_BASE_URL=%q\n", urlFlag+"/v1")
			fmt.Printf("export OPENAI_API_KEY=%q\n", keyFlag)
			fmt.Printf("export ANTHROPIC_BASE_URL=%q\n", urlFlag)
			fmt.Printf("export ANTHROPIC_API_KEY=%q\n", keyFlag)
			fmt.Printf("export GOOGLE_GEMINI_BASE_URL=%q\n", urlFlag)
			fmt.Printf("export GEMINI_API_KEY=%q\n", keyFlag)
		}
		return true
	}

	cfg := loadConfig()
	if dbFlag != "" {
		cfg.DBPath = dbFlag
	}
	gw, err := newGateway(cfg)
	if err != nil {
		log.Fatalf("init db error: %v", err)
	}
	defer gw.pool.Close()

	if cmd == "health" {
		b, _ := json.MarshalIndent(gw.pool.HealthSummary(), "", "  ")
		fmt.Println(string(b))
		return true
	}

	prompt := strings.TrimSpace(strings.Join(rest, " "))
	if info, err := os.Stdin.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
		stdinBytes, _ := io.ReadAll(io.LimitReader(os.Stdin, 2<<20))
		inTxt := strings.TrimSpace(string(stdinBytes))
		if inTxt != "" {
			if prompt != "" {
				prompt = prompt + "\n\n" + inTxt
			} else {
				prompt = inTxt
			}
		}
	}
	if prompt == "" {
		fmt.Fprintln(os.Stderr, "error: prompt wajib diisi")
		os.Exit(1)
	}
	if modelFlag == "" {
		modelFlag = "automodel"
	}

	if urlFlag != "" {
		reqBody, _ := json.Marshal(map[string]any{
			"model": modelFlag,
			"messages": func() []map[string]string {
				var ms []map[string]string
				if sysFlag != "" {
					ms = append(ms, map[string]string{"role": "system", "content": sysFlag})
				}
				return append(ms, map[string]string{"role": "user", "content": prompt})
			}(),
		})
		endpoint := strings.TrimRight(urlFlag, "/") + "/v1/chat/completions"
		httpReq, _ := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(reqBody))
		httpReq.Header.Set("Content-Type", "application/json")
		if keyFlag != "" {
			httpReq.Header.Set("Authorization", "Bearer "+keyFlag)
		}
		httpRes, err := gw.httpClient.Do(httpReq)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		defer httpRes.Body.Close()
		raw, _ := io.ReadAll(httpRes.Body)
		if httpRes.StatusCode >= 300 {
			fmt.Fprintf(os.Stderr, "error (%d): %s\n", httpRes.StatusCode, string(raw))
			os.Exit(1)
		}
		if jsonOut {
			fmt.Println(string(raw))
			return true
		}
		var out OpenAIResponse
		if json.Unmarshal(raw, &out) == nil && len(out.Choices) > 0 {
			fmt.Println(extractTextContent(out.Choices[0].Message.Content))
		} else {
			fmt.Println(string(raw))
		}
		return true
	}

	var msgs []OpenAIMessage
	if sysFlag != "" {
		msgs = append(msgs, OpenAIMessage{Role: "system", Content: sysFlag})
	}
	msgs = append(msgs, OpenAIMessage{Role: "user", Content: prompt})
	req := &OpenAIRequest{Model: modelFlag, Messages: msgs}
	client := ClientKey{Name: "cli", Scope: scopeGlobal, Enabled: true}
	resp, trace, err := gw.executeRoutedChat(context.Background(), nil, req, client, provFlag)
	gw.pool.FlushPending()
	if err != nil || resp == nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	reply := ""
	if len(resp.Choices) > 0 {
		reply = extractTextContent(resp.Choices[0].Message.Content)
	}
	if jsonOut {
		b, _ := json.Marshal(map[string]any{
			"ok":       true,
			"provider": trace.Provider,
			"model":    trace.Model,
			"route_us": trace.RouteUs,
			"reply":    reply,
		})
		fmt.Println(string(b))
		return true
	}
	fmt.Println(reply)
	return true
}
