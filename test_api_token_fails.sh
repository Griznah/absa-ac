#!/bin/bash
# test_api_token_fails.sh
# Fails-fast: checks that bot fails at startup if API_BEARER_TOKEN is weak/missing when API_ENABLED=true

# Build application binary
set -euo pipefail
BIN=./bot
CONFIG=config.test.json
echo "[test] Building ./bot..."
go build -o "$BIN" .

# Minimal valid config for validation
cat > "$CONFIG" <<EOF
{
  "server_ip": "127.0.0.1",
  "update_interval": 30,
  "category_order": ["Drift"],
  "category_emojis": {"Drift": "🏁"},
  "servers": [{"name": "S1", "port": 8080, "category": "Drift"}]
}
EOF

dummy_token="AveryLongButStillRandomStringForDiscoToken"
dummy_channel="1234567890"
fatal_msg="API_BEARER_TOKEN too weak or missing: must be at least 32 random characters"
declare -a weak_tokens=("" "test" "changeme" "CHANGEME-REQUIRED" "$(printf 'a%.0s' {1..32})" "$(printf '1%.0s' {1..50})")

ret=0
for tok in "${weak_tokens[@]}"; do
  echo "[test] API_BEARER_TOKEN=<$tok>"
  API_ENABLED=true API_BEARER_TOKEN="$tok" API_PORT=9099 \
    DISCORD_TOKEN="$dummy_token" CHANNEL_ID="$dummy_channel" \
    "$BIN" -c "$CONFIG" >out.log 2>&1
  code=$?
  if [[ $code -eq 0 ]]; then
    echo "[FAIL] Process exited with code 0 (should fail for weak token: '$tok')"
    ret=1
    continue
  fi
  if ! grep -q "$fatal_msg" out.log; then
    echo "[FAIL] Did not find expected fatal message in output for token: '$tok'"
    cat out.log
    ret=1
    continue
  fi
  echo "[PASS] Correctly failed with fatal error for token: '$tok'"
done

rm -f "$BIN" "$CONFIG" out.log
exit $ret
