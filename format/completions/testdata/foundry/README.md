# completions testdata: Azure AI Foundry

Chat Completions replies recorded from an Azure AI Foundry account's OpenAI v1 endpoint,
`https://<account>.services.ai.azure.com/openai/v1`, on 2026-11-17, with no `api-version`, each
request naming the deployment as its `model`:

| Role     | Deployment     | Model the replies name   |
| -------- | -------------- | ------------------------ |
| `agent`  | `gpt-oss-120b` | `gpt-oss-120b`           |
| `vision` | `gpt-5-mini`   | `gpt-5-mini-2025-08-07`  |

`record.sh` made every file here: it posts each request with curl to `/chat/completions` and writes
the reply's body as it arrived, with `curl -o`. Nothing is stripped or edited. It reads each role's
`AI_<ROLE>_BASE_URL` and `AI_<ROLE>_HEADER`, the variables the integration tiers read, the header
here `api-key: <key>`, and hands the URL and the headers to curl in a config on stdin (`curl -K -`),
so neither the account's name nor its key is in curl's arguments, its output, or a file. The
header lines reach that config through a pipe from `printf`, a shell builtin, so no value is in
any process's arguments or a temporary file either. curl runs with `-s` and without `-S`, so an
error prints no host. The script prints one line per request, its
time, path, model, whether it streamed, and its HTTP status, and stops at any status but 200,
discarding that reply so it never replaces a recording. Name stems, such as
`./record.sh oss_length`, to record those alone. Run it again to re-record; the ids and some texts
will differ, and the tests that pin them must follow.

Each request is in the script verbatim, its members as `EncodeRequest` gives them, the bound as
`max_completion_tokens`. Each unstreamed request adds `"stream":false`, and each streamed one
`"stream":true,"stream_options":{"include_usage":true}`, as `EncodeStreamRequest` does. A `.sse`
and the `.json` beside it are one request, sent twice in the same run. gpt-5-mini refuses a
`temperature` other than 1, so its requests set none; gpt-oss-120b's set `temperature` 0 and
`seed` 7. Even so, Foundry's two replies to one request differ in their reasoning text and their
token counts, so the stream tests fold each `.sse` and compare it with what `DecodeResponse` gives
for the `.json`, given the stream's ids, reasoning text, and usage, which they pin.

- `text.json`, `text.sse`: gpt-5-mini asked "Name three French cities, comma-separated, nothing
  else.", with no reasoning effort, the deployment's default; content alone, and the reasoning
  only as `completion_tokens_details.reasoning_tokens`, 128 unstreamed and 64 streamed.
- `reasoning.json`, `reasoning.sse`: gpt-5-mini, `reasoning_effort` low, the same question; the
  same shape, 64 reasoning tokens unstreamed and 0 streamed.
- `oss_reasoning.json`, `oss_reasoning.sse`: gpt-oss-120b, `reasoning_effort` low, the same
  question; the reasoning in `reasoning_content`, then the content.
- `oss_length.json`, `oss_length.sse`: gpt-oss-120b, the same question at
  `max_completion_tokens` 16; it stops in the reasoning, finish_reason `length`, the content `""`.

## The gap stems

Three stems probe the gaps a profile declares, for the format-profiles task: `oss_tool_choice_required`,
`oss_parallel_tool_calls`, and `parallel_tool_calls`. They are sent only when named, so a run of no
names records the files above and sends none of them. Each request is in
the script verbatim, unstreamed, its members in the order `EncodeRequest` gives them, and prints
its line with `azure=N`, the running count of Azure requests, which starts from `AZURE_REQUESTS`,
the count the task sent before the run, or 0. A 429 stops the script.

- `oss_tool_choice_required`: gpt-oss-120b, `reasoning_effort` low, asked "What is the weather in
  Paris right now?" with the `get_weather` tool and `tool_choice` `required`, `temperature` 0 and
  `seed` 7. A 400 is kept, as the backing of `NoToolChoiceRequired` (`no_tool_choice_required`);
  any other status stops the script.
- `oss_parallel_tool_calls`, `parallel_tool_calls`: gpt-oss-120b and gpt-5-mini, `reasoning_effort`
  low, asked "What is the weather in Paris and in London right now?" with the `get_weather` tool
  and `parallel_tool_calls` false. Each is a probe: a 400 is kept, as the backing of
  `NoParallelToolCalls` (`no_parallel_tool_calls`), and a 200, the host taking the member, is
  reported and discarded; any other status stops the script.

```
AZURE_REQUESTS=0 ./record.sh oss_tool_choice_required oss_parallel_tool_calls parallel_tool_calls
```

Run on 2026-11-18:

- `oss_tool_choice_required.json`: gpt-oss-120b answered 400, code `UnsupportedToolUse`,
  "tool_choice 'required' is not supported for the model." It backs `NoToolChoiceRequired`.
- `oss_parallel_tool_calls`, `parallel_tool_calls`: both deployments answered 200, taking
  `parallel_tool_calls` false, so neither declares a field and nothing is kept.

## What Foundry departs from

gpt-5-mini's stream opens with a chunk of an empty `id`, no choices, `created` 0, empty `model`
and `object`, and the prompt's `prompt_filter_results`, the content filter's verdict on the
prompt:

```
data: {"choices":[],"created":0,"id":"","model":"","object":"","prompt_filter_results":[...]}
```

Every later chunk carries the reply's id. OpenAI's chat completion chunk says of `id`: "A unique
identifier for the chat completion. Each chunk has the same ID.", and of `choices`: "Can also be
empty for the last chunk if you set `stream_options: {"include_usage": true}`."
([OpenAI API reference, chat completion streaming events](https://developers.openai.com/api/reference/resources/chat/subresources/completions/streaming-events)).
A decoder that takes the stream's id from the first chunk fails the second:
`chunk of reply "chatcmpl-…" in the stream of reply ""`. go-ai's decoder takes a chunk of no id and
no choices to claim nothing about the reply, so the stream's id is the first id a chunk carries.
gpt-oss-120b's streams have no such chunk.

gpt-5-mini's replies carry members of Azure's own, which the format ignores: each choice's and
chunk's `content_filter_results`, the reply's `prompt_filter_results`, `obfuscation` on each
chunk, and `routing` and `latency_checkpoint` on the reply and the usage chunk. It gives no
reasoning text in Chat Completions. gpt-oss-120b's replies are of another server's shape: an id
of 32 hex digits with no `chatcmpl-` prefix, `matched_stop` on each choice, `reasoning_content`
null on the chunks that carry none, and the usage's `reasoning_tokens` beside `completion_tokens`
rather than in `completion_tokens_details`. The format reads that top-level count when
`completion_tokens_details` gives none, so the reasoning tokens decode as Foundry reports them:
17 and 18 unstreamed, 14 and 16 streamed. The count can exceed `completion_tokens`, as
`oss_length`'s 18 of 16 does, and is taken as given.

## What /models lists

`GET /openai/v1/models` lists the model catalog, not the account's deployments: 488 entries on
2026-11-17, of which 188 `deprecated`, 22 `deprecating`, 213 `generally-available`, 8 `legacy`,
and 57 `preview`, each of `id`, `object` `model`, `created_at`, `status`, `lifecycle_status`,
`deprecation`, and `capabilities` (`inference`, `chat_completion`, `completion`, `embeddings`,
`fine_tune`, and others). The entries `gpt-5-mini` and `gpt-oss-120b` are there as catalog models,
beside dated versions such as `gpt-5-mini-2025-08-07`, so the listing tells nothing of which are
deployed. `./record.sh models` fetches it and prints that summary, and keeps no file.
