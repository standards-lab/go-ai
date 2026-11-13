#!/usr/bin/env bash
# Records the Open Responses replies in this directory from vLLM 0.33.0. Run it from anywhere;
# it writes each reply's body, as it arrived, beside itself. See README.md.
set -euo pipefail
cd "$(dirname "$0")"

AGENT=http://gb10-1:8000/v1   # openai/gpt-oss-120b
VISION=http://gb10-2:8000/v1  # nvidia/Gemma-4-26B-A4B-NVFP4

# record name base body: posts body to base's responses unstreamed, writing name.json.
record() {
  curl -sS -o "$1.json" -H 'Content-Type: application/json' \
    -d "$3,\"stream\":false,\"store\":false,\"include\":[\"reasoning.encrypted_content\"]}" "$2/responses"
}

# record_stream name base body: posts body streamed, as EncodeStreamRequest asks, writing name.sse.
record_stream() {
  curl -sS -N -o "$1.sse" -H 'Content-Type: application/json' -H 'Accept: text/event-stream' \
    -d "$3,\"stream\":true,\"store\":false,\"include\":[\"reasoning.encrypted_content\"]}" "$2/responses"
}

# Each body is left open, its stream and stateless members appended by record and record_stream.
cities='"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Name three French cities, comma-separated, nothing else."}]}]'
weather='"tools":[{"type":"function","name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":false}]'

text='{"model":"nvidia/Gemma-4-26B-A4B-NVFP4",'"$cities"',"max_output_tokens":64,"temperature":0,"seed":7'
record text "$VISION" "$text"
record_stream text "$VISION" "$text"

reasoning='{"model":"openai/gpt-oss-120b",'"$cities"',"reasoning":{"effort":"low"},"max_output_tokens":256,"temperature":0,"seed":7'
record reasoning "$AGENT" "$reasoning"
record_stream reasoning "$AGENT" "$reasoning"

tool_call='{"model":"openai/gpt-oss-120b","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"What is the weather in Paris right now?"}]}],'"$weather"',"tool_choice":"required","reasoning":{"effort":"low"},"max_output_tokens":256,"temperature":0,"seed":7'
record tool_call "$AGENT" "$tool_call"
record_stream tool_call "$AGENT" "$tool_call"

structured='{"model":"openai/gpt-oss-120b","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Give the capital of France."}]}],"reasoning":{"effort":"low"},"text":{"format":{"type":"json_schema","name":"city","schema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false},"strict":true}},"max_output_tokens":256,"temperature":0,"seed":7'
record structured "$AGENT" "$structured"

length='{"model":"nvidia/Gemma-4-26B-A4B-NVFP4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Count from 1 to 20, separated by spaces."}]}],"max_output_tokens":8,"temperature":0,"seed":7'
record length "$VISION" "$length"
record_stream length "$VISION" "$length"

image='{"model":"nvidia/Gemma-4-26B-A4B-NVFP4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"What color is this image? Answer in one word."},{"type":"input_image","image_url":"data:image/png;base64,'"$(base64 -w0 red.png)"'"}]}],"max_output_tokens":16,"temperature":0,"seed":7'
record image "$VISION" "$image"

not_found='{"model":"go-ai/no-such-model",'"$cities"',"max_output_tokens":16'
record not_found "$AGENT" "$not_found"
