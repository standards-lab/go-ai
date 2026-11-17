#!/usr/bin/env bash
# Records the Responses replies in this directory from Azure AI Foundry. Run it from anywhere,
# with the vision role's AI_VISION_BASE_URL and AI_VISION_HEADER set as for the integration
# tiers; it writes each reply's body, as it arrived, beside itself. Name files' stems, such as
# `text`, to record those alone. See README.md.
#
# The base URL and the headers go to curl in a config on stdin, never in its arguments, and curl
# runs silent, with no error text, so neither the account's host nor its key is printed. Each
# request prints one line: the time, the path, the model, whether it streamed, and the HTTP
# status. A status other than 200 stops the script.
set -euo pipefail
cd "$(dirname "$0")"

# The account's deployment, named as the model.
VISION=gpt-5-mini

# quote key value: writes a curl config line setting key to value, quoted.
quote() {
  local v=${2//\\/\\\\}
  v=${v//\"/\\\"}
  printf '%s = "%s"\n' "$1" "$v"
}

# config role path: writes the curl config of a request to the role's base URL and path, with
# each of the role's header lines.
# The header lines are read through a pipe from printf, a builtin, so no value reaches a process's
# arguments or a temporary file.
config() {
  local base="AI_${1}_BASE_URL" header="AI_${1}_HEADER" line
  quote url "${!base%/}$2"
  while IFS= read -r line; do
    if [ -n "$line" ]; then quote header "$line"; fi
  done < <(printf '%s\n' "${!header:-}")
}

# send file role path model streamed [curl arguments...]: sends the request, writing the reply's
# body to file, and prints its line. A reply of another status than 200 is discarded, so it never
# replaces a recording.
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

# wanted name: whether name is to be recorded, every name when none was given.
names=" $* "
wanted() { [ "$names" = "  " ] || [[ $names == *" $1 "* ]]; }

# record name role model body: posts body to the role's responses unstreamed, writing name.json,
# then streamed, as EncodeStreamRequest asks, writing name.sse.
record() {
  wanted "$1" || return 0
  send "$1.json" "$2" /responses "$3" n -H 'Content-Type: application/json' \
    -d "{\"model\":\"$3\",$4,\"stream\":false,\"store\":false,\"include\":[\"reasoning.encrypted_content\"]}"
  send "$1.sse" "$2" /responses "$3" y -N -H 'Content-Type: application/json' \
    -H 'Accept: text/event-stream' \
    -d "{\"model\":\"$3\",$4,\"stream\":true,\"store\":false,\"include\":[\"reasoning.encrypted_content\"]}"
}

# Each body is the members after model, as EncodeRequest gives them, its stream and stateless
# members appended by record.
cities='"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Name three French cities, comma-separated, nothing else."}]}]'

record text VISION "$VISION" "$cities"',"max_output_tokens":1024'
record reasoning VISION "$VISION" "$cities"',"reasoning":{"effort":"low"},"max_output_tokens":1024'
