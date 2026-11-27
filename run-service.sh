#!/bin/sh
mkdir -p /opt/data/logs
exec >> /opt/data/logs/karouter.log 2>&1
cd /opt/data/gemini-gateway || exit 78
[ -x /opt/data/gemini-gateway/geminigw ] || exit 78
exec /command/s6-setuidgid hermes env -u PORT -u HOST -u LISTEN_ADDR /opt/data/gemini-gateway/geminigw --listen 127.0.0.1:8031 --db /opt/data/gemini-gateway/gateway.db
