# messages testdata: vLLM

Anthropic Messages replies recorded from vLLM 0.33.0 (`GET /version` gives
`{"version":"0.33.0"}`) on 2026-11-12, one model per endpoint:

| Endpoint                | Model                          |
| ----------------------- | ------------------------------ |
| `http://gb10-1:8000/v1` | `openai/gpt-oss-120b`          |
| `http://gb10-2:8000/v1` | `nvidia/Gemma-4-26B-A4B-NVFP4` |

`record.sh` made every file here: it posts each request with curl to `/v1/messages`, with the
`anthropic-version: 2026-10-21` header the format sends, and writes the reply's body as it
arrived, with `curl -o`. Nothing is stripped or edited. Run it again to re-record; the ids,
signatures, and some texts will differ, and the tests that pin them must follow. Each request is
in the script verbatim, in the shape `EncodeRequest` gives, sent with `temperature` 0 and `seed` 7
and a small `max_tokens`; reasoning effort low goes out as `"thinking":{"type":"adaptive"}` and
`"output_config":{"effort":"low"}`. Each unstreamed request adds `"stream":false`, and each
streamed one `"stream":true`. A `.sse` and the `.json` beside it are one request, sent twice in
the same run; the stream tests fold the `.sse` and compare it with what `DecodeResponse` gives for
the `.json`, given the stream's ids and thinking signature.

- `text.json`, `text.sse`: Gemma 4 26B A4B asked "Name three French cities, comma-separated,
  nothing else."; a text block, stop_reason `end_turn`.
- `reasoning.json`, `reasoning.sse`: gpt-oss-120b, effort low, the same question; a thinking
  block with a signature, then a text block. The stream gives the signature in a
  `signature_delta` ahead of the thinking block's stop.
- `tool_use.json`, `tool_use.sse`: gpt-oss-120b, effort low, asked "What is the weather in Paris
  right now?" with the custom tool `get_weather` and `tool_choice` `{"type":"any"}`; a thinking
  block, then a tool_use block, stop_reason `tool_use`. The stream's input_json deltas are the
  model's text, `{\n  "city": "Paris"\n}`.
- `structured.json`: gpt-oss-120b asked "Give the capital of France." with
  `output_config.format` a `json_schema` of a required string `name`; a thinking block, then the
  JSON as text.
- `not_found.json`: the 404 body for the model `go-ai/no-such-model`, Anthropic's
  `{"type":"error","error":{"type":"not_found_error","message"}}`.

## What vLLM departs from

vLLM signs each thinking block with a signature of its own, new on every request, where
llama.cpp's router leaves it empty. Unlike the router, it stops each content block before the next
begins, as Anthropic does, and reads `output_config`, so the schema reaches the model. It reports
no cache counts in the usage. An unstreamed reply's tool_use input is the arguments parsed, while
the stream carries the model's text.
