#!/usr/bin/env bash
# Records the Chat Completions replies in this directory from Azure AI Foundry, or those whose
# stems are named.
# See README.md
set -euo pipefail
cd "$(dirname "$0")"

AGENT=gpt-oss-120b
VISION=gpt-5-mini

quote() {
  local v=${2//\\/\\\\}
  v=${v//\"/\\\"}
  printf '%s = "%s"\n' "$1" "$v"
}

config() {
  local base="AI_${1}_BASE_URL" header="AI_${1}_HEADER" line
  quote url "${!base%/}$2"
  while IFS= read -r line; do
    if [ -n "$line" ]; then quote header "$line"; fi
  done < <(printf '%s\n' "${!header:-}")
}

send() {
  local file=$1 role=$2 path=$3 model=$4 streamed=$5 base="AI_${2}_BASE_URL" status
  shift 5
  if [ -z "${!base:-}" ]; then
    echo "$base is not set" >&2
    exit 1
  fi
  status=$(config "$role" "$path" | curl -s -K - -o "$file.part" -w '%{http_code}' "$@") || true
  printf '%s %s %s stream=%s %s\n' "$(date -u +%FT%TZ)" "$path" "$model" "$streamed" "$status"
  if [ "$status" != 200 ]; then
    rm -f "$file.part"
    echo "$file: HTTP $status; stopping" >&2
    exit 1
  fi
  mv "$file.part" "$file"
}

names=" $* "
wanted() { [ "$names" = "  " ] || [[ $names == *" $1 "* ]]; }

record() {
  wanted "$1" || return 0
  send "$1.json" "$2" /chat/completions "$3" n -H 'Content-Type: application/json' \
    -d "{\"model\":\"$3\",$4,\"stream\":false}"
  send "$1.sse" "$2" /chat/completions "$3" y -N -H 'Content-Type: application/json' \
    -H 'Accept: text/event-stream' \
    -d "{\"model\":\"$3\",$4,\"stream\":true,\"stream_options\":{\"include_usage\":true}}"
}

cities='"messages":[{"role":"user","content":"Name three French cities, comma-separated, nothing else."}]'

record text VISION "$VISION" "$cities"',"max_completion_tokens":1024'
record reasoning VISION "$VISION" "$cities"',"reasoning_effort":"low","max_completion_tokens":1024'
record oss_reasoning AGENT "$AGENT" "$cities"',"reasoning_effort":"low","max_completion_tokens":256,"temperature":0,"seed":7'
record oss_length AGENT "$AGENT" "$cities"',"max_completion_tokens":16,"temperature":0,"seed":7'

summarize_models() {
  send models.json VISION /models - n
  jq -c '{count: (.data | length), members: (.data[0] | keys),
    lifecycle_status: (.data | group_by(.lifecycle_status) | map({(.[0].lifecycle_status): length}) | add)}' models.json
  rm models.json
}

if wanted models; then summarize_models; fi

named() { [[ $names == *" $1 "* ]]; }

azure_requests=${AZURE_REQUESTS:-0}

gap() {
  local file=$1 role=$2 model=$3 want=$4 body=$5 base="AI_${2}_BASE_URL" status
  if [ -z "${!base:-}" ]; then
    echo "$base is not set" >&2
    exit 1
  fi
  status=$(config "$role" /chat/completions | curl -s -K - -o "$file.part" -w '%{http_code}' \
    -H 'Content-Type: application/json' -d "$body") || true
  azure_requests=$((azure_requests + 1))
  printf '%s /chat/completions %s stream=n %s azure=%d\n' "$(date -u +%FT%TZ)" "$model" "$status" "$azure_requests"
  if [ "$status" = 400 ]; then
    mv "$file.part" "$file"
    return
  fi
  rm -f "$file.part"
  if [ "$want" = probe ] && [ "$status" = 200 ]; then
    echo "$file: HTTP 200, the host took the request; nothing kept"
    return
  fi
  echo "$file: HTTP $status; stopping" >&2
  exit 1
}

weather='"tools":[{"type":"function","function":{"name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]'
two_cities='"messages":[{"role":"user","content":"What is the weather in Paris and in London right now?"}]'

if named oss_tool_choice_required; then
  gap oss_tool_choice_required.json AGENT "$AGENT" 400 \
    '{"model":"'"$AGENT"'","messages":[{"role":"user","content":"What is the weather in Paris right now?"}],'"$weather"',"tool_choice":"required","reasoning_effort":"low","max_completion_tokens":256,"stream":false,"temperature":0,"seed":7}'
fi

if named oss_parallel_tool_calls; then
  gap oss_parallel_tool_calls.json AGENT "$AGENT" probe \
    '{"model":"'"$AGENT"'",'"$two_cities"','"$weather"',"reasoning_effort":"low","max_completion_tokens":256,"parallel_tool_calls":false,"stream":false,"temperature":0,"seed":7}'
fi
if named parallel_tool_calls; then
  gap parallel_tool_calls.json VISION "$VISION" probe \
    '{"model":"'"$VISION"'",'"$two_cities"','"$weather"',"reasoning_effort":"low","max_completion_tokens":1024,"parallel_tool_calls":false,"stream":false}'
fi
