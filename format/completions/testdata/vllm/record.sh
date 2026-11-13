#!/usr/bin/env bash
# Records the Chat Completions replies in this directory from vLLM 0.33.0. Run it from anywhere;
# it writes each reply's body, as it arrived, beside itself. See README.md.
set -euo pipefail
cd "$(dirname "$0")"

AGENT=http://gb10-1:8000/v1   # openai/gpt-oss-120b
VISION=http://gb10-2:8000/v1  # nvidia/Gemma-4-26B-A4B-NVFP4
AUDIO=http://framework:8001/v1 # google/gemma-4-E4B-it

# record name base body: posts body to base's chat/completions unstreamed, writing name.json.
record() {
  curl -sS -o "$1.json" -H 'Content-Type: application/json' -d "$3,\"stream\":false}" "$2/chat/completions"
}

# record_stream name base body: posts body streamed, as EncodeStreamRequest asks, writing name.sse.
record_stream() {
  curl -sS -N -o "$1.sse" -H 'Content-Type: application/json' -H 'Accept: text/event-stream' \
    -d "$3,\"stream\":true,\"stream_options\":{\"include_usage\":true}}" "$2/chat/completions"
}

# Each body is left open, its stream members appended by record and record_stream.
cities='"messages":[{"role":"user","content":"Name three French cities, comma-separated, nothing else."}]'
weather='"tools":[{"type":"function","function":{"name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]'

text='{"model":"nvidia/Gemma-4-26B-A4B-NVFP4",'"$cities"',"max_completion_tokens":64,"temperature":0,"seed":7'
record text "$VISION" "$text"
record_stream text "$VISION" "$text"

reasoning='{"model":"openai/gpt-oss-120b",'"$cities"',"reasoning_effort":"low","max_completion_tokens":256,"temperature":0,"seed":7'
record reasoning "$AGENT" "$reasoning"
record_stream reasoning "$AGENT" "$reasoning"

tool_call='{"model":"openai/gpt-oss-120b","messages":[{"role":"user","content":"What is the weather in Paris right now?"}],'"$weather"',"tool_choice":"required","reasoning_effort":"low","max_completion_tokens":256,"temperature":0,"seed":7'
record tool_call "$AGENT" "$tool_call"
record_stream tool_call "$AGENT" "$tool_call"

structured='{"model":"openai/gpt-oss-120b","messages":[{"role":"user","content":"Give the capital of France."}],"reasoning_effort":"low","response_format":{"type":"json_schema","json_schema":{"name":"city","schema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false},"strict":true}},"max_completion_tokens":256,"temperature":0,"seed":7'
record structured "$AGENT" "$structured"

image='{"model":"nvidia/Gemma-4-26B-A4B-NVFP4","messages":[{"role":"user","content":[{"type":"text","text":"What color is this image? Answer in one word."},{"type":"image_url","image_url":{"url":"data:image/png;base64,'"$(base64 -w0 red.png)"'"}}]}],"max_completion_tokens":16,"temperature":0,"seed":7'
record image "$VISION" "$image"

input_audio='{"model":"google/gemma-4-E4B-it","messages":[{"role":"user","content":[{"type":"text","text":"Describe this sound in a few words."},{"type":"input_audio","input_audio":{"data":"'"$(base64 -w0 tone.wav)"'","format":"wav"}}]}],"max_completion_tokens":32,"temperature":0,"seed":7'
record input_audio "$AUDIO" "$input_audio"

not_found='{"model":"go-ai/no-such-model",'"$cities"',"max_completion_tokens":16'
record not_found "$AGENT" "$not_found"
