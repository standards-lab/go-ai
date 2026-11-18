# messages testdata: vLLM 0.34.0 gaps

Anthropic Messages replies recorded from vLLM 0.34.0 (`GET /version` gives
`{"version":"0.34.0"}`) on 2026-11-18, each backing a gap a profile declares. One model answers
them:

| Endpoint                | Model                 | Served as |
| ----------------------- | --------------------- | --------- |
| `http://gb10-1:8000/v1` | `openai/gpt-oss-120b` | `agent`   |

vLLM 0.34.0 serves each workstation's model under its role's name, so a request names the model
`agent`. The vision and audio rows report vLLM 0.34.0 too,
`http://gb10-2:8000/v1` serving `nvidia/Gemma-4-26B-A4B-NVFP4` as `vision` and
`http://framework:8001/v1` serving `google/gemma-4-E4B-it` as `audio`; no gap here is theirs.
The set beside this one, `../vllm`, is of vLLM 0.33.0 and stays as it was.

`record.sh` made every file here: it checks that the host reports vLLM 0.34.0, then posts each
request with curl to `/v1/messages`, with the `anthropic-version: 2026-10-21` header the format
sends, and writes the reply's body as it arrived. Nothing is stripped or edited. Each request is
in the script verbatim, unstreamed, its members in the order `EncodeRequest` gives them, with
`temperature` 0 and `seed` 7 last, as the native tier merges them. The script prints one line per
request, its time, path, stem, and HTTP status. Each stem expects the gap's reply alone, a status
and a jq test of its body; any other reply is discarded, so it never replaces a recording, and
stops the script. Name stems, such as `./record.sh effort_none`, to record those alone.

## The gaps recorded

All three back `RefusedEfforts` (`refused_efforts`), of none, xhigh, and max. Each asks
gpt-oss-120b "Name three French cities, comma-separated, nothing else.".

- `effort_none.json`: effort none, which the format sends as `"thinking":{"type":"disabled"}`. A
  200 that reasons anyway: a thinking block, "We just output three French cities separated by
  commas.", then the text, as effort low's reply does.
- `effort_xhigh.json`, `effort_max.json`: effort xhigh and max, sent as
  `"thinking":{"type":"adaptive"}` and `output_config.effort`. Each is a 400 of type
  `invalid_request_error`: "reasoning_effort='xhigh' is not supported by Harmony. Supported values
  are: high, medium, low. (parameter=reasoning_effort)".

## The gaps that didn't reproduce

Effort low, medium, and high were probed with the same request and got a 200 each, a thinking
block then the text; they declare no field and nothing is kept.
