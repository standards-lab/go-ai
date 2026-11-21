#!/usr/bin/env bash
# Records the Responses replies in this directory from Azure AI Foundry, or those whose stems are
# named.
# See README.md
set -euo pipefail
cd "$(dirname "$0")"

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
  send "$1.json" "$2" /responses "$3" n -H 'Content-Type: application/json' \
    -d "{\"model\":\"$3\",$4,\"stream\":false,\"store\":false,\"include\":[\"reasoning.encrypted_content\"]}"
  send "$1.sse" "$2" /responses "$3" y -N -H 'Content-Type: application/json' \
    -H 'Accept: text/event-stream' \
    -d "{\"model\":\"$3\",$4,\"stream\":true,\"store\":false,\"include\":[\"reasoning.encrypted_content\"]}"
}

cities='"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Name three French cities, comma-separated, nothing else."}]}]'

record text VISION "$VISION" "$cities"',"max_output_tokens":1024'
record reasoning VISION "$VISION" "$cities"',"reasoning":{"effort":"low"},"max_output_tokens":1024'

named() { [[ $names == *" $1 "* ]]; }

azure_requests=${AZURE_REQUESTS:-0}

probe() {
  local file=$1 role=$2 model=$3 body=$4 base="AI_${2}_BASE_URL" status
  if [ -z "${!base:-}" ]; then
    echo "$base is not set" >&2
    exit 1
  fi
  status=$(config "$role" /responses | curl -s -K - -o "$file.part" -w '%{http_code}' \
    -H 'Content-Type: application/json' -d "$body") || true
  azure_requests=$((azure_requests + 1))
  printf '%s /responses %s stream=n %s azure=%d\n' "$(date -u +%FT%TZ)" "$model" "$status" "$azure_requests"
  case $status in
  400) mv "$file.part" "$file" ;;
  200)
    rm -f "$file.part"
    echo "$file: HTTP 200, the host took the request; nothing kept"
    ;;
  *)
    rm -f "$file.part"
    echo "$file: HTTP $status; stopping" >&2
    exit 1
    ;;
  esac
}

if named parallel_tool_calls; then
  probe parallel_tool_calls.json VISION "$VISION" \
    '{"model":"'"$VISION"'","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"What is the weather in Paris and in London right now?"}]}],"tools":[{"type":"function","name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":false}],"reasoning":{"effort":"low"},"max_output_tokens":1024,"parallel_tool_calls":false,"stream":false,"store":false,"include":["reasoning.encrypted_content"]}'
fi
