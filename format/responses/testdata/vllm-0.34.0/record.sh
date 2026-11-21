#!/usr/bin/env bash
# Records the Open Responses gap replies in this directory from vLLM 0.34.0, or those whose stems are
# named.
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
    -d "$body" "$AGENT/v1/responses")
  printf '%s /responses %s %s\n' "$(date -u +%FT%TZ)" "$name" "$status"
  if [ "$status" != "$want" ] || ! jq -e "$test" "$name.json.part" >/dev/null; then
    rm -f "$name.json.part"
    echo "$name: HTTP $status, not the gap's reply (HTTP $want, $test); stopping" >&2
    exit 1
  fi
  mv "$name.json.part" "$name.json"
}

require_version "$AGENT"

cities='"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Name three French cities, comma-separated, nothing else."}]}]'
tools='"tools":[{"type":"function","name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":false},{"type":"function","name":"get_time","description":"Get the current local time in a city.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":false}]'
tail='"max_output_tokens":256,"stream":false,"store":false,"include":["reasoning.encrypted_content"],"temperature":0,"seed":7'

record_refused_efforts() {
  local effort
  for effort in none xhigh; do
    gap "effort_$effort" 400 '.error.param == "reasoning_effort"' \
      '{"model":"agent",'"$cities"',"reasoning":{"effort":"'"$effort"'"},'"$tail"'}'
  done
}

record_refused_efforts

gap allowed_tools 200 'any(.output[]; .type == "function_call" and .name != "get_time")' \
  '{"model":"agent","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"What is the weather in Paris right now?"}]}],'"$tools"',"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","name":"get_time"}],"mode":"required"},"reasoning":{"effort":"low"},'"$tail"'}'

gap reasoning_text 200 \
  'any(.output[]; .type == "reasoning" and (.content | length > 0) and .encrypted_content == null)' \
  '{"model":"agent",'"$cities"',"reasoning":{"effort":"low"},'"$tail"'}'
