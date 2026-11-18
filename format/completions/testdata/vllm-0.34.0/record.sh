#!/usr/bin/env bash
# Records the Chat Completions gap replies in this directory from vLLM 0.34.0. Run it from
# anywhere; it writes each reply's body, as it arrived, beside itself. Name files' stems, such as
# `effort_none allowed_tools`, to record those alone. See README.md.
#
# Each request prints one line: the time, the path, the stem, and the HTTP status. Each stem
# expects one reply, the gap's: its status, and a jq test of its body. Any other reply is
# discarded, so it never replaces a recording, and stops the script.
set -euo pipefail
cd "$(dirname "$0")"

AGENT=http://gb10-1:8000 # openai/gpt-oss-120b, served as the model agent

# version base: stops unless base's GET /version reports vLLM 0.34.0, the version this set is of.
version() {
  local v
  v=$(curl -sS "$1/version" | jq -r .version)
  if [ "$v" != 0.34.0 ]; then
    echo "$1: vLLM $v, not 0.34.0; stopping" >&2
    exit 1
  fi
}

# wanted name: whether name is to be recorded, every name when none was given.
names=" $* "
wanted() { [ "$names" = "  " ] || [[ $names == *" $1 "* ]]; }

# gap name status test body: posts body to the agent's chat/completions, unstreamed as
# EncodeRequest gives it, and prints its line. A reply of the status whose body passes the jq
# test is written to name.json; any other reply is discarded and stops the script.
gap() {
  local name=$1 want=$2 test=$3 body=$4 status
  wanted "$name" || return 0
  status=$(curl -sS -o "$name.json.part" -w '%{http_code}' -H 'Content-Type: application/json' \
    -d "$body" "$AGENT/v1/chat/completions")
  printf '%s /chat/completions %s %s\n' "$(date -u +%FT%TZ)" "$name" "$status"
  if [ "$status" != "$want" ] || ! jq -e "$test" "$name.json.part" >/dev/null; then
    rm -f "$name.json.part"
    echo "$name: HTTP $status, not the gap's reply (HTTP $want, $test); stopping" >&2
    exit 1
  fi
  mv "$name.json.part" "$name.json"
}

version "$AGENT"

# Each body is in full, its members in the order EncodeRequest gives them, the sampling members
# temperature and seed last, as the native tier merges them.
cities='"messages":[{"role":"user","content":"Name three French cities, comma-separated, nothing else."}]'
tools='"tools":[{"type":"function","function":{"name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}},{"type":"function","function":{"name":"get_time","description":"Get the current local time in a city.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]'
sampling='"temperature":0,"seed":7'

# The efforts gpt-oss-120b refuses: each a 400 naming reasoning_effort.
for effort in none minimal xhigh max; do
  gap "effort_$effort" 400 '.error.param == "reasoning_effort"' \
    '{"model":"agent",'"$cities"',"reasoning_effort":"'"$effort"'","max_completion_tokens":256,"stream":false,'"$sampling"'}'
done

# An allowed_tools choice, of mode required: a 400 naming tool_choice.
gap allowed_tools 400 '.error.param | startswith("tool_choice")' \
  '{"model":"agent","messages":[{"role":"user","content":"What is the weather in Paris right now?"}],'"$tools"',"tool_choice":{"type":"allowed_tools","allowed_tools":{"mode":"required","tools":[{"type":"function","function":{"name":"get_time"}}]}},"reasoning_effort":"low","max_completion_tokens":256,"stream":false,'"$sampling"'}'
