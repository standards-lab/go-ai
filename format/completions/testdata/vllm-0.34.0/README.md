# completions testdata: vLLM 0.34.0 gaps

Chat Completions replies recorded from vLLM 0.34.0 (`GET /version` gives `{"version":"0.34.0"}`)
on 2026-11-18, each backing a gap a profile declares. One model answers them:

| Endpoint                | Model                 | Served as |
| ----------------------- | --------------------- | --------- |
| `http://gb10-1:8000/v1` | `openai/gpt-oss-120b` | `agent`   |

vLLM 0.34.0 serves each workstation's model under its role's name, so a request names the model
`agent`; a request naming `openai/gpt-oss-120b` gets a 404 that says
"Valid aliases: agent.". The vision and audio rows report vLLM 0.34.0 too,
`http://gb10-2:8000/v1` serving `nvidia/Gemma-4-26B-A4B-NVFP4` as `vision` and
`http://framework:8001/v1` serving `google/gemma-4-E4B-it` as `audio`; no gap here is theirs.
The set beside this one, `../vllm`, is of vLLM 0.33.0 and stays as it was.

`record.sh` made every file here: it checks that the host reports vLLM 0.34.0, then posts each
request with curl to `/v1/chat/completions` and writes the reply's body as it arrived. Nothing is
stripped or edited. Each request is in the script verbatim, unstreamed, its members in the order
`EncodeRequest` gives them, with `temperature` 0 and `seed` 7 last, as the native tier merges
them. The script prints one line per request, its time, path, stem, and HTTP status. Each stem
expects the gap's reply alone, a status and a jq test of its body; any other reply is discarded,
so it never replaces a recording, and stops the script. Name stems, such as
`./record.sh effort_none`, to record those alone.

## The gaps recorded

- `effort_none.json`, `effort_minimal.json`, `effort_xhigh.json`: gpt-oss-120b asked "Name three
  French cities, comma-separated, nothing else." with `reasoning_effort` none, minimal, and xhigh,
  three of the efforts Chat Completions defines. Each is a 400 of type `BadRequestError`, param
  `reasoning_effort`: "Harmony does not support reasoning_effort='none'", and for minimal and
  xhigh "reasoning_effort='minimal' is not supported by Harmony. Supported values are: high,
  medium, low.". They back `RefusedEfforts` (`refused_efforts`) of none, minimal, and xhigh.
- `effort_max.json`: the same request with `reasoning_effort` max, the 400 of the same text. Max
  is not among Chat Completions' efforts, but the format sends it by its own name, so a host that
  refuses it is backed here for `RefusedEfforts` too.
- `allowed_tools.json`: gpt-oss-120b, `reasoning_effort` low, asked "What is the weather in Paris
  right now?" with the tools `get_weather` and `get_time` and the tool choice
  `{"type":"allowed_tools","allowed_tools":{"mode":"required","tools":[get_time]}}`. A 400 of
  param `tool_choice.function`: "Invalid value for `function`: `None` in `tool_choice`!"; vLLM
  reads every object choice as a named function. It backs `NoAllowedTools` (`no_allowed_tools`).

## The gaps that didn't reproduce

Each was probed with a request of the same shape and got a reply the spec allows, so it declares
no field and nothing is kept:

- `tool_choice` `required`, with the `get_weather` tool: a 200 calling `get_weather`. No
  `NoToolChoiceRequired` (`no_tool_choice_required`) on vLLM.
- `parallel_tool_calls` false and true, asked for the weather in Paris and in London with the
  `get_weather` tool: a 200 each, both calling `get_weather` once, for Paris. No
  `NoParallelToolCalls` (`no_parallel_tool_calls`) on vLLM.
- `reasoning_effort` low, medium, and high: a 200 each, the reasoning then the answer.
