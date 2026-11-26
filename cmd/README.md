# Backend Go Modules (`cmd/`, `package main`)

Package `main` dipecah menjadi modul terfokus (masing-masing < 750 baris, `go build ./cmd` & `go test ./cmd`):

- `main.go`: entrypoint binary, `embeddedUIFS`, wiring router HTTP (`Gateway.handler`), `selftest`.
- `types.go`: struct konfigurasi, provider, key pool, log, OpenAI/Gemini schema, trace.
- `config.go`: loader `.env` & argumen CLI, normalisasi settings/provider, kripto hash, masking/error classifier.
- `pool_sqlite.go`: skema SQLite WAL, seed default, batch DB writer.
- `auth.go`: verifikasi sandi admin, HS256 JWT 24 jam, session cookie/bearer, client access keys.
- `rotation.go`: rotasi provider (`weighted`, `priority`, `model_prefix`) & pemilihan key (`smart`, `random`, `lru_jitter`, `round_robin`), tracking gagal 5x (`dead`), riwayat log.
- `keys.go`: parser multi-format batch key, penamaan sekuensial (`gemini01..`), CRUD & bulk actions key.
- `keys_validation.go`: normalisasi key, validasi format/placeholder/mismatch, deteksi duplikat batch/pool/lintas provider, audit & auto-dedupe (`dedupe_all`, `purge_invalid`, `auto_clean`).
- `backup_neon.go`: snapshot export/restore ke Neon PostgreSQL & scheduler 7 hari.
- `translate.go`: konversi schema/tool-call OpenAI ↔ Gemini, pemetaan alias model agent asing.
- `upstream.go`: pemanggil HTTP/SSE upstream Gemini & OpenAI, retry & auto-fallback 404 model.
- `models.go`: live `/models` probing upstream & endpoint `/api/models` + `/v1/models`.
- `handlers_admin.go`: handler dashboard admin, APK download (`/api/app/apk`), backup, playground chat.
- `handlers_keys.go`: handler API endpoint manajemen & test upstream/client keys.
- `handlers_protocols.go`: handler endpoint kompatibel `/v1/chat/completions`, `/v1/responses`, `/v1/messages`, `/v1beta/models`.
- `cli.go`: subcommand CLI (`exec`, `ask`, `agent-env`, `health`).
- `main_test.go`: unit & integration tests paket `main`.
- `keys_validation_test.go`: unit tests validasi key, deteksi duplikat, auto-dedupe SQLite, dan startup zero-`.env`.
