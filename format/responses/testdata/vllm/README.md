# responses testdata: vLLM

Open Responses replies recorded from vLLM 0.33.0 (`GET /version` gives `{"version":"0.33.0"}`)
on 2026-11-12, one model per endpoint:

| Endpoint                | Model                          |
| ----------------------- | ------------------------------ |
| `http://gb10-1:8000/v1` | `openai/gpt-oss-120b`          |
| `http://gb10-2:8000/v1` | `nvidia/Gemma-4-26B-A4B-NVFP4` |

`record.sh` made every file here: it posts each request with curl to `/v1/responses` and writes
the reply's body as it arrived, with `curl -o`. Nothing is stripped or edited. Run it again to
re-record; the ids and some texts will differ, and the tests that pin them must follow. Each
request is in the script verbatim, in the shape `EncodeRequest` gives, sent with `temperature` 0
and `seed` 7 and a small `max_output_tokens`. Each carries `"store":false` and
`"include":["reasoning.encrypted_content"]`, with `"stream":false` unstreamed and `"stream":true`
streamed. The decode tests compare what `DecodeResponse` gives for each `.json` with the response
it should be; the stream tests fold each `.sse` and compare it with what `DecodeResponse` gives
for its terminal event's response.

- `text.json`, `text.sse`: Gemma 4 26B A4B asked "Name three French cities, comma-separated,
  nothing else."; one message.
- `reasoning.json`, `reasoning.sse`: gpt-oss-120b, `reasoning` `{"effort":"low"}`, the same
  question; a reasoning item of one reasoning_text part, then the message.
- `tool_call.json`, `tool_call.sse`: gpt-oss-120b, effort low, asked "What is the weather in
  Paris right now?" with the `get_weather` tool (`strict` false) and `tool_choice` `required`; a
  reasoning item, then a function_call of `{\n  "city": "Paris"\n}`.
- `structured.json`: gpt-oss-120b asked "Give the capital of France." with `text.format` a
  `json_schema` of a required string `name`, strict; a reasoning item, then the JSON.
- `length.json`, `length.sse`: Gemma 4 26B A4B asked "Count from 1 to 20, separated by spaces."
  with `max_output_tokens` 8; status `incomplete`, `incomplete_details` `max_output_tokens`, the
  message incomplete, and the stream's terminal event `response.incomplete`.
- `image.json`: Gemma 4 26B A4B asked "What color is this image? Answer in one word." with
  `red.png`, a 32 by 32 image of pure red made as `../../../completions/testdata/vllm/README.md`
  shows, as a data URL `input_image`; "Red".
- `not_found.json`: the 404 body for the model `go-ai/no-such-model`, an OpenAI-style error of
  type `NotFoundError`, param `model`, and the numeric code 404.

## What vLLM departs from

vLLM follows the spec more closely than llama.cpp's router: every event carries its
`sequence_number`, and its `output_index` and `content_index` where the spec has them; a
reasoning item's part events and its done event are sent; each item closes before the next opens;
and a response stopped at the bound is `incomplete`. It streams reasoning under OpenAI's names,
`response.reasoning_text.delta` and `.done`, which the decoder reads as the spec's. A reasoning
item has status `null` and `encrypted_content` `null` in the terminal and the unstreamed response,
though its `output_item.done` gives it `completed`; `DecodeResponse` takes the status as the
server gives it, so the item's is empty unstreamed, and completed when streamed. It sends members
of its own, such as `phase`, `logprobs`, and `async`, and in a function call's
`output_item.done` an item `sequence_number` of -1, which the format ignores.
