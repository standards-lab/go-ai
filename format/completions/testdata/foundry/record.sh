#!/usr/bin/env bash
# Records the Chat Completions replies in this directory from Azure AI Foundry. Run it from
# anywhere, with the agent and vision roles' AI_<ROLE>_BASE_URL and AI_<ROLE>_HEADER set as for
# the integration tiers; it writes each reply's body, as it arrived, beside itself. Name files'
# stems, such as `text models`, to record those alone. See README.md.
#
# The base URL and the headers go to curl in a config on stdin, never in its arguments, and curl
# runs silent, with no error text, so neither the account's host nor its key is printed. Each
# request prints one line: the time, the path, the model, whether it streamed, and the HTTP
# status. A status other than 200 stops the script.
set -euo pipefail
cd "$(dirname "$0")"

# The account's deployments, named as the model.
AGENT=gpt-oss-120b
VISION=gpt-5-mini

# quote key value: writes a curl config line setting key to value, quoted.
quote() {
  local v=${2//\\/\\\\}
  v=${v//\"/\\\"}
  printf '%s = "%s"\n' "$1" "$v"
}

# config role path: writes the curl config of a request to the role's base URL and path, with
# each of the role's header lines.
config() {
  local base="AI_${1}_BASE_URL" header="AI_${1}_HEADER" line
  if [ -z "${!base:-}" ]; then
    echo "$base is not set" >&2
    exit 1
  fi
  quote url "${!base%/}$2"
  while IFS= read -r line; do
    if [ -n "$line" ]; then quote header "$line"; fi
  done <<<"${!header:-}"
}

# send file role path model streamed [curl arguments...]: sends the request, writing the reply's
# body to file, and prints its line.
send() {
  local file=$1 role=$2 path=$3 model=$4 streamed=$5 status
  shift 5
  status=$(config "$role" "$path" | curl -s -K - -o "$file" -w '%{http_code}' "$@") || true
  printf '%s %s %s stream=%s %s\n' "$(date -u +%FT%TZ)" "$path" "$model" "$streamed" "$status"
  if [ "$status" != 200 ]; then
    echo "$file: HTTP $status; stopping" >&2
    exit 1
  fi
}

# wanted name: whether name is to be recorded, every name when none was given.
names=" $* "
wanted() { [ "$names" = "  " ] || [[ $names == *" $1 "* ]]; }

# record name role model body: posts body to the role's chat/completions unstreamed, writing
# name.json, then streamed, as EncodeStreamRequest asks, writing name.sse.
record() {
  wanted "$1" || return 0
  send "$1.json" "$2" /chat/completions "$3" n -H 'Content-Type: application/json' \
    -d "{\"model\":\"$3\",$4,\"stream\":false}"
  send "$1.sse" "$2" /chat/completions "$3" y -N -H 'Content-Type: application/json' \
    -H 'Accept: text/event-stream' \
    -d "{\"model\":\"$3\",$4,\"stream\":true,\"stream_options\":{\"include_usage\":true}}"
}

# Each body is the members after model, as EncodeRequest gives them, its stream members
# appended by record.
cities='"messages":[{"role":"user","content":"Name three French cities, comma-separated, nothing else."}]'

record text VISION "$VISION" "$cities"',"max_completion_tokens":1024'
record reasoning VISION "$VISION" "$cities"',"reasoning_effort":"low","max_completion_tokens":1024'
record oss_reasoning AGENT "$AGENT" "$cities"',"reasoning_effort":"low","max_completion_tokens":256,"temperature":0,"seed":7'
record oss_length AGENT "$AGENT" "$cities"',"max_completion_tokens":16,"temperature":0,"seed":7'

# models: what the account's /models lists, through the vision role. It lists the model catalog,
# some 500 entries, too many to keep, so the listing is summarized, its count, an entry's members,
# and the count of each lifecycle_status, and removed.
if wanted models; then
  send models.json VISION /models - n
  jq -c '{count: (.data | length), members: (.data[0] | keys),
    lifecycle_status: (.data | group_by(.lifecycle_status) | map({(.[0].lifecycle_status): length}) | add)}' models.json
  rm models.json
fi
