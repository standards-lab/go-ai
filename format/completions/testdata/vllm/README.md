# completions testdata: vLLM

Chat Completions replies recorded from vLLM 0.33.0 (`GET /version` gives `{"version":"0.33.0"}`)
on 2026-11-12, one model per endpoint:

| Endpoint                   | Model                           |
| -------------------------- | ------------------------------- |
| `http://gb10-1:8000/v1`    | `openai/gpt-oss-120b`           |
| `http://gb10-2:8000/v1`    | `nvidia/Gemma-4-26B-A4B-NVFP4`  |
| `http://framework:8001/v1` | `google/gemma-4-E4B-it`         |

`record.sh` made every file here: it posts each request with curl to
`/v1/chat/completions` and writes the reply's body as it arrived, with `curl -o`. Nothing is
stripped or edited. Run it again to re-record; the ids and some texts will differ, and the tests
that pin them must follow. Each request is in the script verbatim, sent with `temperature` 0 and
`seed` 7 and a small token bound. Each unstreamed request adds `"stream":false`, and each streamed
one `"stream":true,"stream_options":{"include_usage":true}`, as `EncodeStreamRequest` does. A
`.sse` and the `.json` beside it are one request, sent twice in the same run; the stream tests fold
the `.sse` and compare it with what `DecodeResponse` gives for the `.json`, so a run whose two
replies disagree, as a first request to a cold endpoint did once, is run again.

- `text.json`, `text.sse`: Gemma 4 26B A4B asked "Name three French cities, comma-separated,
  nothing else."; content alone, `reasoning` null, finish_reason `stop`.
- `reasoning.json`, `reasoning.sse`: gpt-oss-120b, `reasoning_effort` low, the same question;
  the reasoning in vLLM's `reasoning` member, never `reasoning_content`, then the content. The
  stream opens with a chunk of role and empty content, and its usage comes in a last chunk of no
  choices.
- `tool_call.json`, `tool_call.sse`: gpt-oss-120b, `reasoning_effort` low, asked "What is the
  weather in Paris right now?" with the `get_weather` tool and `tool_choice` `required`; the
  reasoning, then one call, finish_reason `tool_calls`. The unstreamed call's arguments are
  `{"city": "Paris"}`, which vLLM rewrites, and the stream's are the model's own text,
  `{\n  "city": "Paris"\n}`; the content is `""`.
- `structured.json`: gpt-oss-120b asked "Give the capital of France." with a `json_schema`
  `response_format` of a required string `name`, strict; the reasoning, then the JSON.
- `image.json`: Gemma 4 26B A4B asked "What color is this image? Answer in one word." with
  `red.png` as a data URL `image_url` part; "Red".
- `input_audio.json`: Gemma 4 E4B asked "Describe this sound in a few words." with `tone.wav` as
  an `input_audio` part, format `wav`.
- `not_found.json`: the 404 body for the model `go-ai/no-such-model`, an OpenAI-style error of
  type `NotFoundError`, param `model`, and the numeric code 404.

The inputs are made by this Python, the standard library alone: `red.png` a 32 by 32 RGB image of
pure red, and `tone.wav` one second of a 440 Hz sine, 16 kHz, 16-bit mono.

```python
import math, struct, wave, zlib

def chunk(t, d):
    return struct.pack('>I', len(d)) + t + d + struct.pack('>I', zlib.crc32(t + d) & 0xffffffff)

raw = b''.join(b'\x00' + b'\xff\x00\x00' * 32 for _ in range(32))
open('red.png', 'wb').write(b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', 32, 32, 8, 2, 0, 0, 0))
                            + chunk(b'IDAT', zlib.compress(raw, 9)) + chunk(b'IEND', b''))
with wave.open('tone.wav', 'wb') as f:
    f.setnchannels(1); f.setsampwidth(2); f.setframerate(16000)
    f.writeframes(b''.join(struct.pack('<h', int(12000 * math.sin(2 * math.pi * 440 * i / 16000)))
                           for i in range(16000)))
```

## What vLLM departs from

vLLM sends a message's and a delta's reasoning as `reasoning`, where llama.cpp sends
`reasoning_content`; the format reads either. It sends `null` for the members a reply leaves
unset, such as `refusal`, and members of its own, such as `stop_reason` and `token_ids`, which the
format ignores. An unstreamed reply's tool call arguments are the arguments parsed and written
back, while the stream carries the model's text.
