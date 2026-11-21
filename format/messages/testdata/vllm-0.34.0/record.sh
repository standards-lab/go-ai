#!/usr/bin/env bash
# Records the Anthropic Messages gap replies in this directory from vLLM 0.34.0, or those whose
# stems are named.
# See README.md
set -euo pipefail
cd "$(dirname "$0")"

AGENT=http://gb10-1:8000

require_version() {
  local v
  v=$(curl -sS "$1/version" | jq -r .version)
  if [ "$v" != 0.34.0 ]; then
    echo "$1: vLLM $v, not 0.34.0; stopping" >&2
    exit 1
  fi
}

names=" $* "
wanted() { [ "$names" = "  " ] || [[ $names == *" $1 "* ]]; }

gap() {
  local name=$1 want=$2 test=$3 body=$4 status
  wanted "$name" || return 0
  status=$(curl -sS -o "$name.json.part" -w '%{http_code}' -H 'Content-Type: application/json' \
    -H 'anthropic-version: 2026-10-21' -d "$body" "$AGENT/v1/messages")
  printf '%s /messages %s %s\n' "$(date -u +%FT%TZ)" "$name" "$status"
  if [ "$status" != "$want" ] || ! jq -e "$test" "$name.json.part" >/dev/null; then
    rm -f "$name.json.part"
    echo "$name: HTTP $status, not the gap's reply (HTTP $want, $test); stopping" >&2
    exit 1
  fi
  mv "$name.json.part" "$name.json"
}

require_version "$AGENT"

cities='"messages":[{"role":"user","content":[{"type":"text","text":"Name three French cities, comma-separated, nothing else."}]}]'
sampling='"temperature":0,"seed":7'

gap effort_none 200 '.content[0].type == "thinking"' \
  '{"model":"agent","max_tokens":256,'"$cities"',"thinking":{"type":"disabled"},"stream":false,'"$sampling"'}'

record_refused_efforts() {
  local effort
  for effort in xhigh max; do
    gap "effort_$effort" 400 '.error.type == "invalid_request_error" and (.error.message | contains("reasoning_effort"))' \
      '{"model":"agent","max_tokens":256,'"$cities"',"thinking":{"type":"adaptive"},"output_config":{"effort":"'"$effort"'"},"stream":false,'"$sampling"'}'
  done
}

record_refused_efforts
