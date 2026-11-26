# KaRouter Android APK (`mobile/`)

Folder build mandiri untuk aplikasi Android **KaRouter** (Kotlin + Jetpack Compose Material 3).
Mendukung login JWT 24 jam (wajib logout otomatis setelah 24 jam), pengisian API Key multi-baris / tempel banyak / import file (`.txt`, `.csv`, `.json`, `.env`) dengan penamaan urut otomatis (`gemini01`, `gemini02`), manajemen client key per kategori, log error lengkap, dan Backup Neon.

## Menyesuaikan Endpoint & Build di VPS

### 1. Siapkan Java 17 & Android SDK di VPS (Ubuntu/Debian)

```bash
sudo apt-get update && sudo apt-get install -y openjdk-17-jdk unzip curl
mkdir -p /opt/android-sdk/cmdline-tools
curl -fsSL https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip -o /tmp/cmdline.zip
unzip -q /tmp/cmdline.zip -d /opt/android-sdk/cmdline-tools
mv /opt/android-sdk/cmdline-tools/cmdline-tools /opt/android-sdk/cmdline-tools/latest
export ANDROID_HOME=/opt/android-sdk
yes | /opt/android-sdk/cmdline-tools/latest/bin/sdkmanager --licenses
/opt/android-sdk/cmdline-tools/latest/bin/sdkmanager "platform-tools" "platforms;android-34" "build-tools;34.0.0"
```

### 2. Sesuaikan Endpoint & Compile `.apk`

Ganti URL VPS Anda langsung lewat argumen `./build-apk.sh` (atau edit `KAROUTER_DEFAULT_URL` di `mobile/gradle.properties`):

```bash
cd mobile
chmod +x build-apk.sh
./build-apk.sh https://domain-vps-anda.com
```

Script otomatis menghasilkan `karouter.apk` di root proyek (`../karouter.apk`). Backend Go langsung mendeteksinya dan tombol **Download APK** di dashboard website (**Atur → Aplikasi**) otomatis aktif untuk mengunduh APK tersebut.

## Autentikasi JWT 24 Jam

- Aplikasi login ke `POST /api/auth/login` dan menyimpan token JWT HS256 (`Authorization: Bearer <token>`) beserta `expires_at` (`24 jam`).
- Timer otomatis + validasi server (`401`) memaksa logout saat masa berlaku 24 jam habis.
