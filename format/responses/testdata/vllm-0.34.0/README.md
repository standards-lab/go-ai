# responses testdata: vLLM 0.34.0 gaps

Open Responses replies recorded from vLLM 0.34.0 (`GET /version` gives `{"version":"0.34.0"}`)
on 2026-11-18, each backing a gap a profile declares. One model answers them:

| Endpoint                | Model                 | Served as |
| ----------------------- | --------------------- | --------- |
| `http://gb10-1:8000/v1` | `openai/gpt-oss-120b` | `agent`   |

vLLM 0.34.0 serves each workstation's model under its role's name, so a request names the model
`agent`. The vision and audio rows report vLLM 0.34.0 too,
`http://gb10-2:8000/v1` serving `nvidia/Gemma-4-26B-A4B-NVFP4` as `vision` and
`http://framework:8001/v1` serving `google/gemma-4-E4B-it` as `audio`; no gap here is theirs.
The set beside this one, `../vllm`, is of vLLM 0.33.0 and stays as it was.

`record.sh` made every file here: it checks that the host reports vLLM 0.34.0, then posts each
request with curl to `/v1/responses` and writes the reply's body as it arrived. Nothing is
stripped or edited. Each request is in the script verbatim, unstreamed and stateless, its members
in the order `EncodeRequest` gives them, `stream` false, `store` false, and `include`
`["reasoning.encrypted_content"]` among them, with `temperature` 0 and `seed` 7 last, as the
native tier merges them. The script prints one line per request, its time, path, stem, and HTTP
status. Each stem expects the gap's reply alone, a status and a jq test of its body; any other
reply is discarded, so it never replaces a recording, and stops the script. Name stems, such as
`./record.sh allowed_tools`, to record those alone.

## The gaps recorded

- `effort_none.json`, `effort_xhigh.json`: gpt-oss-120b asked "Name three French cities,
  comma-separated, nothing else." with `reasoning.effort` none and xhigh, two of the spec's
  efforts. Each is a 400 of type `BadRequestError`, param `reasoning_effort`:
  "reasoning_effort='none' is not supported by Harmony. Supported values are: high, medium,
  low.". vLLM 0.33.0 refused neither with a 400. They back `RefusedEfforts` (`refused_efforts`)
  of none and xhigh.
- `allowed_tools.json`: gpt-oss-120b, `reasoning.effort` low, asked "What is the weather in Paris
  right now?" with the tools `get_weather` and `get_time` and the tool choice
  `{"type":"allowed_tools","tools":[get_time],"mode":"required"}`, which allows `get_time` alone.
  A 200 whose reply calls `get_weather`, outside the allowed set: its reasoning is "We need to
  call get_weather.", and it echoes the allowed_tools choice it ignored as its `tool_choice`. It
  backs `NoAllowedTools` (`no_allowed_tools`).
- `reasoning_text.json`: gpt-oss-120b, `reasoning.effort` low, the cities question. A 200 whose
  reasoning item carries its text as a `reasoning_text` content part and `encrypted_content`
  null, though the request's `include` asks for it, so only the text can go back. It backs
  `ResendReasoningText` (`resend_reasoning_text`).

## The gaps that didn't reproduce

Each was probed with a request of the same shape and got a reply the spec allows, so it declares
no field and nothing is kept:

- `parallel_tool_calls` false and true, asked for the weather in Paris and in London with the
  `get_weather` tool: a 200 each, both calling `get_weather` once, for Paris, and echoing the
  value sent. No `NoParallelToolCalls` (`no_parallel_tool_calls`) on vLLM.
- `reasoning.effort` low, medium, and high: a 200 each, the reasoning then the answer.
