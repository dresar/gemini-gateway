#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$SCRIPT_DIR"

API_URL="${1:-${KAROUTER_DEFAULT_URL:-}}"
if [[ -z "$API_URL" ]]; then
  API_URL="$(grep -E '^KAROUTER_DEFAULT_URL=' gradle.properties | cut -d= -f2- || true)"
fi
API_URL="${API_URL:-http://127.0.0.1:8031}"
API_URL="${API_URL%/}"

echo "[KaRouter Mobile] Target Endpoint: $API_URL"

if [[ -n "${ANDROID_HOME:-}" && -d "${ANDROID_HOME}" ]]; then
  echo "sdk.dir=${ANDROID_HOME}" > local.properties
elif [[ -n "${ANDROID_SDK_ROOT:-}" && -d "${ANDROID_SDK_ROOT}" ]]; then
  echo "sdk.dir=${ANDROID_SDK_ROOT}" > local.properties
elif [[ ! -f local.properties ]]; then
  echo "ERROR: ANDROID_HOME / ANDROID_SDK_ROOT belum diatur dan local.properties belum ada."
  echo "Install Android SDK cmdline-tools di VPS, lalu set export ANDROID_HOME=/opt/android-sdk"
  exit 1
fi

GRADLE_CMD=""
if command -v gradle >/dev/null 2>&1; then
  GRADLE_CMD="gradle"
elif [[ -x "$HOME/.gradle/gradle-8.7/bin/gradle" ]]; then
  GRADLE_CMD="$HOME/.gradle/gradle-8.7/bin/gradle"
else
  echo "[KaRouter Mobile] Mengunduh Gradle 8.7..."
  mkdir -p "$HOME/.gradle"
  curl -fsSL "https://services.gradle.org/distributions/gradle-8.7-bin.zip" -o /tmp/gradle-8.7.zip
  unzip -q -o /tmp/gradle-8.7.zip -d "$HOME/.gradle"
  rm -f /tmp/gradle-8.7.zip
  GRADLE_CMD="$HOME/.gradle/gradle-8.7/bin/gradle"
fi

"$GRADLE_CMD" --no-daemon assembleRelease -PKAROUTER_DEFAULT_URL="$API_URL"

OUT_APK="$SCRIPT_DIR/app/build/outputs/apk/release/app-release.apk"
if [[ ! -f "$OUT_APK" ]]; then
  echo "ERROR: APK output tidak ditemukan di $OUT_APK"
  exit 1
fi

cp -f "$OUT_APK" "$ROOT_DIR/karouter.apk"
ls -lh "$ROOT_DIR/karouter.apk"
echo "[KaRouter Mobile] Selesai! APK siap diunduh di /api/app/apk (tab Atur -> Download APK)"
