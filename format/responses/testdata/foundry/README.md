# responses testdata: Azure AI Foundry

Responses replies recorded from an Azure AI Foundry account's OpenAI v1 endpoint,
`https://<account>.services.ai.azure.com/openai/v1`, on 2026-11-17, with no `api-version`, from
the `vision` role's deployment `gpt-5-mini` (model `gpt-5-mini`, a `gpt-5-mini-2025-08-07`),
named as the request's `model`. The account's other deployment, gpt-oss-120b, serves Chat
Completions alone.

`record.sh` made every file here: it posts each request with curl to `/responses` and writes the
reply's body as it arrived, with `curl -o`. Nothing is stripped or edited. It reads
`AI_VISION_BASE_URL` and `AI_VISION_HEADER`, the variables the integration tiers read, the header
here `api-key: <key>`, and hands the URL and the headers to curl in a config on stdin (`curl -K -`),
so neither the account's name nor its key is in curl's arguments, its output, or a file; the
header lines reach that config through a pipe from `printf`, a shell builtin, so no value is in
any process's arguments or a temporary file either; curl runs with `-s` and without `-S`, so an
error prints no host. The script prints one line per request, its
time, path, model, whether it streamed, and its HTTP status, and stops at any status but 200,
discarding that reply so it never replaces a recording. Run it again to re-record; the ids and some
texts will differ, and the tests that pin them must follow.

Each request is in the script verbatim, in the shape `EncodeRequest` gives, with a
`max_output_tokens` of 1024 and no `temperature`, which gpt-5-mini refuses other than 1. Each
carries `"store":false` and `"include":["reasoning.encrypted_content"]`, with `"stream":false`
unstreamed and `"stream":true` streamed. The decode tests compare what `DecodeResponse` gives for
each `.json` with the response it should be; the stream tests fold each `.sse` and compare it with
what `DecodeResponse` gives for its terminal event's response, since the `.json` beside it, the
same request sent again, has other token counts and encrypted content.

- `text.json`, `text.sse`: gpt-5-mini asked "Name three French cities, comma-separated, nothing
  else.", with no `reasoning`; the deployment reasons at its default effort, medium, so a
  reasoning item, then the message.
- `reasoning.json`, `reasoning.sse`: gpt-5-mini, `reasoning` `{"effort":"low"}`, the same
  question; a reasoning item, then the message.

## The gap stem

One stem probes a gap a profile declares, for the format-profiles task: `parallel_tool_calls`. It
is sent only when named, so a run of no names records the files above and sends none of it. Its
request is in the script verbatim, unstreamed and stateless, its
members in the order `EncodeRequest` gives them: gpt-5-mini, `reasoning.effort` low, asked "What
is the weather in Paris and in London right now?" with the `get_weather` tool and
`parallel_tool_calls` false. It is a probe: a 400 is kept, as the backing of
`NoParallelToolCalls` (`no_parallel_tool_calls`), and a 200, the host taking the member, is
reported and discarded; any other status, a 429 among them, stops the script. Its line ends with
`azure=N`, the running count of Azure requests, which starts from `AZURE_REQUESTS`, the count the
task sent before the run, or 0.

```
AZURE_REQUESTS=3 ./record.sh parallel_tool_calls
```

Run on 2026-11-18, gpt-5-mini answered 200, taking `parallel_tool_calls` false, so it declares no
field and nothing is kept.

## What Foundry departs from

Foundry adds gpt-5-mini's reasoning item with an `encrypted_content` already set, 1188
characters in both streams. The item's `response.output_item.done` gives another, and the
terminal response a third, of the same length as the done one; none is a prefix of another:

```
event: response.output_item.added
data: {"type":"response.output_item.added","item":{"id":"rs_…","type":"reasoning","content":[],"encrypted_content":"gAAAAA…","summary":[]},"output_index":0,"sequence_number":2}
```

OpenAI's reference allows it. Of `response.output_item.added`'s `item` it says: "The output item
that was added. For reasoning items, encrypted_content may be incomplete while the item is in
progress. Use the reasoning item from the corresponding response.output_item.done event when
passing it as input to a subsequent request."
([OpenAI API reference, Responses streaming events](https://developers.openai.com/api/reference/resources/responses/streaming-events)).
The Open Responses specification says of the added event: "The item is echoed in the payload
with as much detail as is available at that time.", and of reasoning, that `encrypted_content`'s
"format and cryptographic properties are provider-specific" and clients "treat this field as
opaque and non-inspectable"
([Open Responses specification](https://www.openresponses.org/specification), "Items are
streamable" and "Reasoning"). A decoder that refuses a reasoning item added with
`encrypted_content` fails both streams at their third event: `reasoning item "rs_…" is added with
content`. go-ai's decoder adds the item with no encrypted content; the done event's item holds its
`output_item.done`'s in the envelope, and the folded Response the terminal response's.

The reasoning item has no `status` in its `output_item.done`, the terminal response, or the
unstreamed reply, and no text and no summary: its `content` and `summary` are `[]`. No reasoning
part or text events are sent; the item is added and done back to back. The message is added
with `"encrypted_content":null`. Each event is named in an `event:` line, every one carries its
`sequence_number`, and each `output_text.delta` an `obfuscation` of Azure's own. The response
carries members the format ignores, such as `content_filters`, the content filter's verdicts on
the prompt and the completion, `reasoning.context` and `reasoning.mode`, `tool_usage`, and
`access_programs`; its `usage.input_tokens_details` adds `cache_write_tokens`.

What `/openai/v1/models` lists there is in `../../../completions/testdata/foundry/README.md`.
