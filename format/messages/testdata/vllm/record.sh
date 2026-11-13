#!/usr/bin/env bash
# Records the Anthropic Messages replies in this directory from vLLM 0.33.0. Run it from
# anywhere; it writes each reply's body, as it arrived, beside itself. See README.md.
set -euo pipefail
cd "$(dirname "$0")"

AGENT=http://gb10-1:8000/v1   # openai/gpt-oss-120b
VISION=http://gb10-2:8000/v1  # nvidia/Gemma-4-26B-A4B-NVFP4

# record name base body: posts body to base's messages unstreamed, writing name.json.
record() {
  curl -sS -o "$1.json" -H 'Content-Type: application/json' -H 'anthropic-version: 2026-10-21' \
    -d "$3,\"stream\":false}" "$2/messages"
}

# record_stream name base body: posts body streamed, as EncodeStreamRequest asks, writing name.sse.
record_stream() {
  curl -sS -N -o "$1.sse" -H 'Content-Type: application/json' -H 'anthropic-version: 2026-10-21' \
    -H 'Accept: text/event-stream' -d "$3,\"stream\":true}" "$2/messages"
}

# Each body is left open, its stream member appended by record and record_stream.
cities='"messages":[{"role":"user","content":[{"type":"text","text":"Name three French cities, comma-separated, nothing else."}]}]'
weather='"tools":[{"type":"custom","name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}]'
low='"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}'

text='{"model":"nvidia/Gemma-4-26B-A4B-NVFP4",'"$cities"',"max_tokens":64,"temperature":0,"seed":7'
record text "$VISION" "$text"
record_stream text "$VISION" "$text"

reasoning='{"model":"openai/gpt-oss-120b",'"$cities"','"$low"',"max_tokens":256,"temperature":0,"seed":7'
record reasoning "$AGENT" "$reasoning"
record_stream reasoning "$AGENT" "$reasoning"

tool_use='{"model":"openai/gpt-oss-120b","messages":[{"role":"user","content":[{"type":"text","text":"What is the weather in Paris right now?"}]}],'"$weather"',"tool_choice":{"type":"any"},'"$low"',"max_tokens":256,"temperature":0,"seed":7'
record tool_use "$AGENT" "$tool_use"
record_stream tool_use "$AGENT" "$tool_use"

structured='{"model":"openai/gpt-oss-120b","messages":[{"role":"user","content":[{"type":"text","text":"Give the capital of France."}]}],"thinking":{"type":"adaptive"},"output_config":{"effort":"low","format":{"type":"json_schema","schema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}}},"max_tokens":256,"temperature":0,"seed":7'
record structured "$AGENT" "$structured"

not_found='{"model":"go-ai/no-such-model",'"$cities"',"max_tokens":16'
record not_found "$AGENT" "$not_found"
