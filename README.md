# Gemini Gateway (Go)

High-performance Gemini API **key-pool gateway** written in Go — powers the `:8031` upstream on the VPS.

## What it does

- **Key pool rotation**: round-robin / weighted selection across a pool of Gemini API keys (`keys.pool`).
- **Adaptive cooldown**: 429/rate-limited keys cool down with backoff + jitter; exhausted keys are parked.
- **Stateful**: pool state persists to `pool_state.json` so restarts don't reset health tracking.
- **Single entrypoint**: clients authenticate once with the gateway key; the gateway picks a healthy upstream key per request.

## Build & run

```bash
go build -o geminigw ./cmd
./geminigw   # reads /opt/data/gemini-gateway/.env
```

## Config (`.env`)

| Var | Description |
|---|---|
| `PORT` | listen port (default 8031) |
| `GATEWAY_API_KEY` | key clients use to authenticate to the gateway |
| `GEMINI_MODEL` | default upstream model |
| `KEYS_FILE` | path to key pool file |
| `STATE_FILE` | path to pool state file |

## Files NOT in this repo (by design)

- `keys.pool` — actual Gemini API keys
- `pool_state.json` — runtime key health state
- `.env` — secrets

See `CONTEXT.md` for the full architectural contract.

## License

MIT
