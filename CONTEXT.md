# GEMINI GATEWAY SPECIFICATION & ARCHITECTURAL CONTRACT (v2)

## 1. Overview
A lightweight, pure Go HTTP gateway providing an OpenAI-compatible `/v1/chat/completions` endpoint.
Primary Uplink: Google Gemini OpenAI-compatible endpoint (`https://generativelanguage.googleapis.com/v1beta/openai/chat/completions`) utilizing a 47-key API pool with smart anti-detection rotation.
Fallback Uplink: Legacy OAuth uplink (`https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent?alt=sse`) preserved strictly as final fallback only if all keys in the pool are dead or exhausted.

## 2. Binary & Runtime Constraints
- Binary: `geminigw` built from package `./cmd` (split into modular files `< 1,000` lines each; see `cmd/README.md`).
- Compiler: `/opt/data/tools/go/bin/go build -ldflags="-s -w" -o geminigw ./cmd`.
- Target: Binary size < 10MB, Runtime VmRSS < 15MB.
- Standard library ONLY (`net/http`, `os/exec`, `encoding/json`, `time`, `crypto/rand`, `math/rand`, `sync`, etc.). No external frameworks.
- STRICT RULE: NOKOMEN (Zero comments in any Go source file).

## 3. Server Endpoint & Authentication
- Address: `127.0.0.1:8031` (overrideable via `PORT` from `.env`).
- Route: `POST /v1/chat/completions`.
- Authentication:
  - Header: `Authorization: Bearer <GATEWAY_API_KEY>`.
  - Loaded from `/opt/data/gemini-gateway/.env` (`GATEWAY_API_KEY`).
  - Mismatch -> HTTP 401 Unauthorized (`{"error": {"message": "Unauthorized", "type": "auth_error"}}`).
- Request Body (OpenAI format):
  `{"model": "gemini-3.8-flash", "messages": [...], "tools": [...], "stream": false}`.
- Response Body (OpenAI format):
  Standard OpenAI chat completion JSON with `id`, `object`, `created`, `model`, `choices`, `usage`.

## 4. Key Pool & Smart Anti-Detection Rotation
- Key File: Loaded from `GEMINI_API_KEYS_FILE` in `.env` (default: `/opt/data/gemini-gateway/keys.pool`).
  - 47 lines, one key per line, chmod 600.
  - SECURITY: NEVER print, log, or leak API key content anywhere. In all logs, reference keys strictly by their 0-based index (e.g. `key #0`, `key #14`).
- Pool State File: `/opt/data/gemini-gateway/pool_state.json` (or derived from pool file location / `.env`).
  - Per-key fields:
    - `ok_count` (int): Number of successful 200 responses.
    - `fail_429` (int): Count of 429 rate-limit / quota errors.
    - `fail_403` (int): Count of 403 invalid key errors.
    - `last_used_unix` (int64): Unix timestamp in seconds when the key was last used.
    - `cooldown_until` (int64): Unix timestamp in seconds until which the key cannot be selected.
    - `health` (string): `"healthy"` or `"dead"`.
  - State must be persisted to `pool_state.json` on updates using mutex protection.
- Key Selection per Request:
  - Find all eligible keys: `health != "dead"`, `time.Now().Unix() >= cooldown_until`, and key not yet tried in this request.
  - Sort eligible keys by `last_used_unix` ascending (least-recently-used).
  - Jitter: Select randomly among the top 5 least-recently-used keys:
    `k = min(5, len(eligible))`
    `selected = eligible[rand.Intn(k)]`
  - If no keys eligible:
    - If pool is exhausted or all keys dead/in-cooldown, trigger legacy OAuth fallback.
- Failure Handling & Retry:
  - HTTP 429 / Quota:
    - `fail_429++`
    - `delaySec = min(3600, 60 * (1 << min(6, fail_429)))`
    - `cooldown_until = time.Now().Unix() + delaySec`
    - Retry immediately with another key (up to 5 different keys per request).
  - HTTP 403 / Invalid Key:
    - `fail_403++`
    - `health = "dead"` (permanently excluded from rotation, remains in file and state).
    - Retry immediately with another key (up to 5 different keys per request).
  - HTTP 503 / Service Unavailable:
    - Retry with another key (up to 5 different keys per request).
  - Backoff + Jitter between attempts:
    - For attempt index `n` (0, 1, 2, ...):
      `backoff = (200 * (1 << n)) * time.Millisecond + time.Duration(rand.Intn(151)) * time.Millisecond`
      Sleep for `backoff` before making the next attempt.
  - HTTP 200 OK:
    - `ok_count++`
    - `last_used_unix = time.Now().Unix()`
    - Save state.

## 5. Direct OpenAI Relay & Tool Execution Loop
- Uplink URL: `https://generativelanguage.googleapis.com/v1beta/openai/chat/completions`.
- Headers:
  - `Authorization: Bearer <selected_key>`
  - `Content-Type: application/json`
- Model:
  - `GEMINI_MODEL` from `.env` (default `gemini-3.8-flash`). If client specifies a model, use `gemini-3.8-flash`.
- Direct Relay:
  - Tools and messages remain in OpenAI format directly.
- Tool Loop (max 15 iterations):
  1. Post OpenAI payload to uplink with key pool rotation.
  2. Parse `choices[0].message`:
     - If `len(tool_calls) > 0`:
       - Append the assistant message with `tool_calls` to `messages`.
       - For each tool call:
         - `terminal`: parse `command`, exec `bash -c <cmd>` (timeout 60s, env `GATEWAY_ALLOWED=1`).
         - `read_file`: parse `path`, verify prefix `/opt/data`, read content.
         - `write_file`: parse `path`, `content`, verify prefix `/opt/data`, write content.
         - Append tool result message to `messages` (`role: "tool"`, `tool_call_id: id`, `content: output`).
       - Continue loop.
     - If `len(tool_calls) == 0`:
       - Break loop and return final OpenAI JSON response to client.

## 6. Selftest Mode
- If flags contains `--selftest` (or `GATEWAY_TEST=1`):
  Send test prompt `"jawab: OK"` through key pool uplink, print:
  `HTTP Status: 200\nJawaban: <answer>\n`
  and exit 0.

## 7. Logging & Cleanliness
- Log only index numbers of keys: e.g. `[key #3]`. NEVER log raw keys.
- Log request latency and tokens on completion.
