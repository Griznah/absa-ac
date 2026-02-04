#!/bin/bash
# test_cors_fails.sh
# Verifies that the bot refuses to start with unsafe CORS config in production, allows only with correct dev flag.

set -euo pipefail
BIN=./bot
CONFIG=config.cors.test.json

echo "[test] Building ./bot..."
go build -o "$BIN" .

dummy_token="AveryLongButStillRandomStringForDiscoToken"
dummy_channel="1234567890"
API_BEARER_TOKEN="ABCDEFGHIJKLMNOPQRSTUVWXYZrandombearertoken1234567890"
fatal_msg_cors_any="CORS security error: In production, you MUST provide an explicit allowlist via API_CORS_ORIGINS. Wildcard '*' is forbidden unless ALLOW_CORS_ANY=true for dev/test. See README.md for secure config instructions."
fatal_msg_combo="CORS configuration error: wildcard '*' cannot be combined with specific origins"

echo '[test] Creating minimal valid config.'
cat > "$CONFIG" <<EOF
{
  "server_ip": "127.0.0.1",
  "update_interval": 30,
  "category_order": ["Drift"],
  "category_emojis": {"Drift": "🏁"},
  "servers": [{"name": "S1", "port": 3001, "category": "Drift"}]
}
EOF

ret=0
# Test 1: Prod mode + '*' only => must fail
API_ENABLED=true API_BEARER_TOKEN="$API_BEARER_TOKEN" API_CORS_ORIGINS="*" ALLOW_CORS_ANY=false \
  DISCORD_TOKEN="$dummy_token" CHANNEL_ID="$dummy_channel" \
  "$BIN" -c "$CONFIG" >out.log 2>&1 || true
code=$?
echo "[test] '*' only, prod mode, exit code: $code"
if [[ $code -eq 0 ]]; then
  echo "[FAIL] Bot started with '*' in production mode."
  ret=1
elif ! grep -q "$fatal_msg_cors_any" out.log; then
  echo "[FAIL] Expected fatal CORS prod message not found (should block wildcard in prod!)"
  cat out.log
  ret=1
else
  echo "[PASS] Correctly failed to start with '*' in prod mode."
fi
# Test 2: Dev flag allows wildcard
API_ENABLED=true API_BEARER_TOKEN="$API_BEARER_TOKEN" API_CORS_ORIGINS="*" ALLOW_CORS_ANY=true \
  DISCORD_TOKEN="$dummy_token" CHANNEL_ID="$dummy_channel" \
  "$BIN" -c "$CONFIG" >out.log 2>&1
code=$?
echo "[test] '*' only, dev override, exit code: $code"
if [[ $code -ne 0 ]]; then
  echo "[FAIL] Bot failed to start with '*' and ALLOW_CORS_ANY=true (should allow in dev mode!)"
  cat out.log
  ret=1
elif ! grep -Fq "[WARNING] ALLOW_CORS_ANY=true" out.log; then
  echo "[FAIL] Did not log bold warning for dev flag."
  cat out.log
  ret=1
else
  echo "[PASS] Started and logged warning with '*' and dev flag."
fi
# Test 3: '*' mixed with explicit origins (should always fail)
API_ENABLED=true API_BEARER_TOKEN="$API_BEARER_TOKEN" API_CORS_ORIGINS="*,https://good.com" ALLOW_CORS_ANY=true \
  DISCORD_TOKEN="$dummy_token" CHANNEL_ID="$dummy_channel" \
  "$BIN" -c "$CONFIG" >out.log 2>&1
code=$?
echo "[test] Mix '*' and explicit, exit code: $code"
if [[ $code -eq 0 ]]; then
  echo "[FAIL] Bot started with '*' mixed with allowlist (should always refuse)."
  ret=1
elif ! grep -q "$fatal_msg_combo" out.log; then
  echo "[FAIL] Did not find expected fatal combo message"
  cat out.log
  ret=1
else
  echo "[PASS] Correctly failed to start with wildcards and allowlist."
fi

rm -f "$BIN" "$CONFIG" out.log
exit $ret
