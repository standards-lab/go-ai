# completions testdata: Azure AI Foundry

Chat Completions replies recorded from an Azure AI Foundry account's OpenAI v1 endpoint,
`https://<account>.services.ai.azure.com/openai/v1`, on 2026-11-17, with no `api-version`, each
request naming the deployment as its `model`:

| Role     | Deployment     | Model the replies name   |
| -------- | -------------- | ------------------------ |
| `agent`  | `gpt-oss-120b` | `gpt-oss-120b`           |
| `vision` | `gpt-5-mini`   | `gpt-5-mini-2025-08-07`  |

`record.sh` made every file here: it posts each request with curl to `/chat/completions` and
writes the reply's body as it arrived, with `curl -o`. Nothing is stripped or edited. It reads
each role's `AI_<ROLE>_BASE_URL` and `AI_<ROLE>_HEADER`, the variables the integration tiers
read, the header here `api-key: <key>`, and hands the URL and the headers to curl in a config on
stdin (`curl -K -`), so neither the account's name nor its key is in curl's arguments, its output,
or a file; curl runs with `-s` and without `-S`, so an error prints no host. The script prints one
line per request, its time, path, model, whether it streamed, and its HTTP status, and stops at
any status but 200. Name stems, such as `./record.sh oss_length`, to record those alone. Run it
again to re-record; the ids and some texts will differ, and the tests that pin them must follow.

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
The decoder takes the stream's id from the first chunk, so the second chunk's fails it:
`chunk of reply "chatcmpl-…" in the stream of reply ""`. gpt-oss-120b's streams have no such
chunk.

gpt-5-mini's replies carry members of Azure's own, which the format ignores: each choice's and
chunk's `content_filter_results`, the reply's `prompt_filter_results`, `obfuscation` on each
chunk, and `routing` and `latency_checkpoint` on the reply and the usage chunk. It gives no
reasoning text in Chat Completions. gpt-oss-120b's replies are of another server's shape: an id
of 32 hex digits with no `chatcmpl-` prefix, `matched_stop` on each choice, `reasoning_content`
null on the chunks that carry none, and the usage's `reasoning_tokens` beside `completion_tokens`
rather than in `completion_tokens_details`, which the format doesn't read, so its reasoning
tokens decode as 0. That count can exceed `completion_tokens`, as `oss_length`'s 18 of 16 does.

## What /models lists

`GET /openai/v1/models` lists the model catalog, not the account's deployments: 488 entries on
2026-11-17, of which 188 `deprecated`, 22 `deprecating`, 213 `generally-available`, 8 `legacy`,
and 57 `preview`, each of `id`, `object` `model`, `created_at`, `status`, `lifecycle_status`,
`deprecation`, and `capabilities` (`inference`, `chat_completion`, `completion`, `embeddings`,
`fine_tune`, and others). The entries `gpt-5-mini` and `gpt-oss-120b` are there as catalog models,
beside dated versions such as `gpt-5-mini-2025-08-07`, so the listing tells nothing of which are
deployed. `./record.sh models` fetches it and prints that summary, and keeps no file.
