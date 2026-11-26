# KaRouter — Multi-Provider AI Gateway & Sub-Millisecond Key Rotation

**KaRouter** (`v4.1.0`) adalah AI API Gateway ringan berbasis **Go + SQLite WAL** dengan rotasi API Key di RAM (`< 1 ms`), manajemen provider & custom endpoint tanpa hardcode, **validasi & auto-deduplikasi API key**, **Backup & Restore Neon Postgres otomatis tiap 7 hari**, single-file **Next.js UI** tertanam langsung di binary (`go:embed`), serta aplikasi **Android (Kotlin + Jetpack Compose)** dengan sesi **HS256 JWT 24 jam**.

Dirancang **zero-secret untuk VPS Production**: file `.env` sengaja kosong — seluruh konfigurasi server, hash password admin, JWT secret, URL Neon Postgres, daftar provider, akses client key, log error, dan pool API key tersimpan terpusat di SQLite WAL (`gateway.db`).

---

## Arsitektur & Fitur Utama

1. **Zero-Secret Production Storage (SQLite WAL)**
   - Tidak perlu menyimpan API key, password, JWT secret, atau `DATABASE_URL` di `.env` VPS.
   - `gateway.db` (mode WAL + file permission `0600`) menyimpan:
     - Kredensial admin ter-hash (*salted SHA-256*, `12.000` putaran) & rotasi otomatis *HMAC-SHA256 JWT secret* setiap ganti password.
     - URL koneksi & jadwal **Backup Neon Postgres** (`meta_config`).
     - Konfigurasi rotasi, metadata APK (`apk_file_path`), daftar provider, API key pool, client key ter-hash SHA-256, dan log request terakhir.

2. **Validasi Ketat & Auto-Deduplikasi API Key**
   - **Preflight & Import Validation** (`POST /api/keys` & `POST /api/keys/audit`):
     - Normalisasi otomatis karakter tak terlihat (*zero-width space*), kutip/koma berlebih, dan prefix `"Bearer "`.
     - Menolak key cacat sebelum masuk database: spasi/tab di tengah, karakter non-ASCII/kontrol, URL endpoint (`http://`/`https://`), potongan JSON tak terurai, key placeholder (`YOUR_API_KEY`, `sk-xxx`, karakter berulang), key terlalu pendek (`< 8` char), format salah provider (mis. `sk-` dimasukkan ke Gemini), dan key terpotong (`AIza < 14` / Agnes `sk- < 20`).
     - Memberi peringatan cerdas untuk panjang key `AIza` tidak standar (`< 35` char), prefix `groq`/`openrouter` tak lazim, atau duplikasi lintas provider.
   - **Deduplikasi Otomatis & Audit Pool**:
     - Memisahkan deteksi **duplikat dalam batch input** (*batch duplicates*), **duplikat di pool provider aktif** (*pool duplicates*), dan **duplikat lintas provider**.
     - Tombol **Bersihkan** di dialog *Tambah Key* menghapus baris invalid & duplikat secara instan sebelum simpan.
     - Tombol **Dedup** di tab **Pool** (`POST /api/keys/bulk` `"auto_clean"`) mengaudit SQLite/RAM, mempertahankan 1 salinan key paling sehat (aktif, tidak mati, gagal paling sedikit, `ok_count` tertinggi, latensi tercepat), serta menghapus duplikat & key rusak.

3. **Provider Custom Tanpa Hardcode + 14 Ikon Resmi**
   - Halaman penuh **Tambah Provider** (bukan modal) mendukung **OpenAI-Compat** maupun **Gemini Native**, metode auth `Bearer`, `x-api-key`, atau `query_key`, kategori kustom (`gratis`, `coding`, dll.), bobot (`1–100`), dan prioritas.
   - Seluruh 14 ikon (`Gemini`, `Agnes`, `GLM`, `Groq`, `OpenRouter`, `Mistral`, `Qwen`, `HuggingFace`, `Cloudflare`, `NVIDIA`, `Cohere`, `Ollama`, `Web`, `Custom`) terpaket sebagai inline data URI sehingga UI bekerja 100% offline dalam 1 file binary.

4. **Rotasi Cerdas, Demote Otomatis & Aturan Mati 5x Gagal (`consec_fails >= 5`)**
   - Mode rotasi: `weighted` (SWRR), `priority`, dan `model_prefix`; strategi pilih key: `smart`, `random`, `lru_jitter`, `round_robin`.
   - Key yang gagal otomatis diturunkan prioritasnya ke antrean bawah + *cooldown* eksponensial; setelah **5 kali gagal beruntun**, key ditandai **Mati (`dead`)** dan dicatat jelas di tab **Pool** & **Uji → Riwayat** (`credit_exhausted`, `invalid_key`, `rate_limit`, `model_error`, `network_error`).

5. **Backup & Disaster-Recovery Restore Neon Postgres (Tiap 7 Hari)**
   - Scheduler internal memeriksa jadwal otomatis setiap 1 jam dan membuat snapshot penuh (settings, providers, seluruh API keys + status kesehatan, client access keys ter-hash) ke tabel PostgreSQL `karouter_backups` di Neon.
   - Dapat dijalankan manual (**Backup**), diuji (**Tes**), dimuat daftar snapshot-nya (**Muat**), dan dipulihkan kapan saja ke VPS baru (**Restore**).

6. **Universal Endpoint untuk Semua AI Agent + Aplikasi Mobile Android**
   - Satu URL gateway melayani **OpenAI Chat/Responses SDK**, **Anthropic Claude Code**, **Google Gemini CLI**, **OpenCode**, **Aider**, **Cursor**, **Cline**, hingga eksekusi terminal cepat (`geminigw exec|ask`).
   - Aplikasi Android (`mobile/`) memakai otentikasi **HS256 JWT** (wajib logout otomatis setiap 24 jam) dan tersedia untuk diunduh melalui `/api/app/apk` dari tab **Atur → Aplikasi Android**.

---

## Panduan Deployment Production VPS (Ubuntu / Debian)

### 1. Build Binary Production (di mesin build atau langsung di VPS)

```bash
# 1. Pastikan modul Go & frontend sinkron (opsional jika mengubah UI web/)
cd web && npm install && npm run build && cd ..

# 2. Jalankan test suite lengkap
go vet ./cmd && go test -v ./cmd

# 3. Compile binary statis Linux amd64 untuk VPS
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o karouter-linux ./cmd
```

### 2. Instalasi di VPS Linux

```bash
# Buat user non-root & direktori aplikasi
sudo useradd --system --home /opt/karouter --shell /usr/sbin/nologin karouter || true
sudo mkdir -p /opt/karouter

# Salin binary & file systemd service (atau scp dari mesin build)
sudo cp karouter-linux /opt/karouter/karouter-linux
sudo chmod +x /opt/karouter/karouter-linux
sudo cp deploy/karouter.service /etc/systemd/system/karouter.service
sudo chown -R karouter:karouter /opt/karouter

# Aktifkan dan jalankan service
sudo systemctl daemon-reload
sudo systemctl enable --now karouter
sudo systemctl status karouter
```

> **Catatan Production `.env`**: File `.env` tidak wajib dibuat atau boleh dibiarkan kosong. `karouter-linux` membaca seluruh pengaturan dari `/opt/karouter/gateway.db`. Jika memakai Nginx/Caddy sebagai reverse proxy di VPS yang sama, ubah `--listen` di [`deploy/karouter.service`](deploy/karouter.service) menjadi `--listen 127.0.0.1:8031`.

### 3. Reverse Proxy HTTPS (Caddy atau Nginx)

**Opsi A — Caddy (Otomatis HTTPS Let's Encrypt):**
```caddyfile
ai.domain-anda.com {
    reverse_proxy 127.0.0.1:8031 {
        flush_interval -1
    }
}
```

**Opsi B — Nginx (Support Streaming SSE):**
```nginx
server {
    listen 80;
    server_name ai.domain-anda.com;

    location / {
        proxy_pass http://127.0.0.1:8031;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 300s;
    }
}
```

### 4. Konfigurasi Awal & Restore Key dari Neon Postgres di VPS Baru

1. Buka `https://ai.domain-anda.com` (atau `http://IP-VPS:8031`).
2. Login memakai password bootstrap bawaan lalu **segera ubah password** di tab **Atur → Keamanan**.
3. Untuk memulihkan seluruh provider & API key dari Neon Postgres ke VPS baru:
   - Buka tab **Atur → Backup Neon**.
   - Masukkan Connection String Neon (`postgresql://user:pass@host/db?sslmode=require`).
   - Klik **Simpan** → **Tes** → daftar snapshot otomatis dimuat (**Muat**).
   - Pilih snapshot terbaru (mis. `#3 · ... · 56 key`) → klik **Restore**.
   - Seluruh provider, API key, dan konfigurasi rotasi langsung tersimpan ke SQLite `gateway.db` VPS!

---

## Panduan Penggunaan & Integrasi AI Agent

Buat **Client Key** terlebih dahulu di tab **Akses** (pilih scope `global`, `cat:<kategori>`, atau `<provider_id>`):

### Export Variabel Environment Terminal Sekaligus

```bash
# Bash / Linux / macOS
export OPENAI_BASE_URL="https://ai.domain-anda.com/v1"
export OPENAI_API_KEY="kr_glb_..."
export ANTHROPIC_BASE_URL="https://ai.domain-anda.com"
export ANTHROPIC_API_KEY="kr_glb_..."
export GOOGLE_GEMINI_BASE_URL="https://ai.domain-anda.com"
export GEMINI_API_KEY="kr_glb_..."
```

```powershell
# Windows PowerShell
$env:OPENAI_BASE_URL="http://127.0.0.1:8031/v1"
$env:OPENAI_API_KEY="kr_glb_..."
$env:ANTHROPIC_BASE_URL="http://127.0.0.1:8031"
$env:ANTHROPIC_API_KEY="kr_glb_..."
$env:GOOGLE_GEMINI_BASE_URL="http://127.0.0.1:8031"
$env:GEMINI_API_KEY="kr_glb_..."
```

Atau cetak perintah siap-eval langsung dari binary:
```bash
./karouter-linux agent-env --url https://ai.domain-anda.com --key kr_glb_...
```

### Eksekusi Cepat CLI (`exec`, `ask`, `health`)

```bash
# Jalankan prompt via SQLite lokal (<1ms routing)
./karouter-linux exec "Review arsitektur service ini" --model auto

# Kirim input via stdin ke kategori/provider spesifik
cat cmd/keys.go | ./karouter-linux ask "Temukan potensi race condition" --provider gemini

# Cek ringkasan kesehatan pool
./karouter-linux health --db /opt/karouter/gateway.db
```

### Ringkasan Endpoint API

| Endpoint | Fungsi |
|---|---|
| `POST /v1/chat/completions` | OpenAI Chat Completions (OpenCode, Aider, Cursor, Cline) |
| `POST /v1/responses` | OpenAI Responses API (Codex CLI) |
| `POST /v1/messages` | Anthropic Messages API (Claude Code) |
| `POST /v1beta/models/{m}:generateContent` | Google Gemini Native API (Gemini CLI / GenAI SDK) |
| `GET /v1/models` | Daftar model tersedia sesuai scope client key (`?probe=1` untuk probe live) |
| `GET /health` | Status server, storage (`sqlite-wal`), dan kesehatan pool |
| `GET /api/keys/audit` | Audit duplikat, bentrok lintas provider, & key invalid |
| `POST /api/keys/bulk` | Aksi massal (`test_all`, `revive_all`, `purge_dead`, `dedupe_all`, `purge_invalid`, `auto_clean`) |
| `GET/POST /api/backup/*` | Manajemen backup & restore Neon Postgres |
| `GET /api/app/apk` | Unduh APK Android (`/karouter.apk`) untuk client terotentikasi |

---

## Aplikasi Mobile Android (`mobile/`)

Folder `mobile/` berisi project Android native (**Kotlin + Jetpack Compose**) terpisah untuk dikompilasi di VPS Linux sekaligus mengonfigurasi URL endpoint gateway:

```bash
cd mobile
chmod +x build-apk.sh
./build-apk.sh https://ai.domain-anda.com
```

Hasil build tersimpan di `./karouter.apk` dan otomatis muncul di dashboard Web tab **Atur → Aplikasi Android → Download APK**. Seluruh sesi Web maupun Android menerapkan **HS256 JWT** dengan durasi wajib **24 jam** lalu otomatis logout saat masa aktif habis. Detail instalasi SDK Android di VPS dapat dilihat di [mobile/README.md](mobile/README.md).

---

## Keamanan Repository

- `.gitignore` mengecualikan `.env`, `.env.*` (kecuali `.env.example`), `*.db*`, `*.apk`, `*.jks`, `*.keystore`, seluruh binary executable (`geminigw`, `geminigw.exe`, `karouter-linux`), `mobile/.gradle/`, `mobile/app/build/`, serta `web/node_modules/`.
- Jangan pernah melakukan commit terhadap file database (`gateway.db`), URL Neon Postgres, token API upstream, atau password admin ke GitHub.
