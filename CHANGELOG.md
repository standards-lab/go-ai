# Changelog

All notable changes to `github.com/standards-lab/go-ai` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers the base module
only; `tools/infer` keeps its own.

## [v0.3.0] - 2026-11-18

### Added

- `ai.Model.Native`, each entry of the endpoint's model list as the endpoint sent it, the whole
  JSON object, id and all, so a caller can read members go-ai has no field for, such as vLLM's
  `root` and `max_model_len`, or llama.cpp's router's `status`.

## [v0.2.0] - 2026-11-18

This release conforms each format to its own spec: a format declares the framing its replies
stream in, types every refusal it makes before sending, and sends or refuses each reasoning effort
and token cap as its spec has it. It also gives each format a profile, its `Options`, in which a
deployment that departs from the spec declares its gaps. The zero value is the spec. A declared
gap is refused before sending, as an `UnsupportedError` with `Declared` true, or, for
`allowed_tools`, narrowed to the allowed tools. Each field is backed by a reply recorded from the
deployment it was seen on, vLLM 0.34.0 or Azure AI Foundry. go-ai ships no loader and no presets.

### Added

- `format.EventStream`, the framing a streamed reply arrives in: its media type and a
  `WireEventReader` of a body's `WireEvent`s, each the framing's `Type` and its `Data`.
  `format.SSE` is the server-sent event stream, the one every format here streams in.
- `format.UnsupportedError`, the type of every refusal a format makes while it encodes a request,
  before anything is sent or any token is fetched, matched with `errors.As` through the format's
  and the client's wrapping. It names the `Format`, the refused `Feature` and its `Value`, and
  whether the refusal is of a gap a deployment's profile declares (`Declared`) or of one the
  format's spec has (`Declared` false). Each refusal's message is unchanged.
- The reasoning efforts `protocol.ReasoningEffortMinimal` and `protocol.ReasoningEffortMax`. An
  effort is the protocol's when an implemented format's spec defines it. Chat Completions sends
  all seven as `reasoning_effort`; Open Responses refuses `minimal` and `max`, and Messages
  `minimal`, which their specs lack. Messages sends `max` as `output_config.effort` with adaptive
  thinking.
- `responses.MinMaxOutputTokens`, 16, the Open Responses spec's least `max_output_tokens`, below
  which the format refuses a `MaxOutputTokens`.
- `protocol.Request.ParallelToolCalls`, whether the model may call several tools in one turn; nil
  leaves it to the endpoint. Chat Completions and Open Responses send it as
  `parallel_tool_calls`, and Messages sends false as `tool_choice`'s
  `disable_parallel_tool_use`. A `Native` member that would set the same is refused as a clash.
- `protocol.Response.ID` and `Model`, the reply's id and the model that answered, decoded by
  every format, whole and streamed; the `Accumulator` keeps the created response's where the
  terminal one leaves them empty.
- `protocol.Usage.InputTokensDetails.CachedTokens`, the input tokens served from the endpoint's
  cache, read from Chat Completions' `prompt_tokens_details`, Open Responses'
  `input_tokens_details`, and Messages' `cache_read_input_tokens`.
- `protocol.ReasonContextWindow`, `go-ai:context_window`, a reply cut off by the model's context
  window, a client extension the spec defines no reason for. Messages reports its
  `model_context_window_exceeded` stop reason as it.
- `completions.Options`, the Chat Completions profile: `RefusedEfforts` (`refused_efforts`), the
  reasoning efforts the deployment refuses; `NoToolChoiceRequired` (`no_tool_choice_required`),
  which refuses `tool_choice` `required` and an `allowed_tools` choice of mode `required`; and
  `NoAllowedTools` (`no_allowed_tools`), which narrows an `allowed_tools` choice to its tools,
  in the request's order, its mode sent as a plain tool choice.
- `responses.Options` gains `RefusedEfforts` (`refused_efforts`) and `NoAllowedTools`
  (`no_allowed_tools`), as Chat Completions has them.
- `messages.Options`, the Messages profile: `RefusedEfforts` (`refused_efforts`), the reasoning
  efforts the deployment refuses or ignores.
- A declared gap's refusal is an `UnsupportedError` with `Declared` true, its `Feature`
  `reasoning effort` or `tool choice`. An effort a format's spec lacks stays the spec's refusal,
  `Declared` false, listed or not.
- `protocol.ReasoningEffort.UnmarshalText`, which accepts only the seven protocol efforts, so a
  mistyped entry in a profile's `refused_efforts` fails to decode.
- Each `Options` decodes from JSON under its snake_case names only, and a caller decoding a
  profile strictly, with json/v2's `RejectUnknownMembers(true)`, gets an error for an unknown
  member.

### Changed

- **Breaking:** `completions.New` and `messages.New` take their format's `Options`, as
  `responses.New` does. `completions.New(completions.Options{})` and
  `messages.New(messages.Options{})` send what `completions.New()` and `messages.New()` sent.
- **Breaking:** `responses.Options` decodes from JSON only under its snake_case names, so
  `ResendReasoningText` is `resend_reasoning_text`, matched case-sensitively; a profile that
  named it `ResendReasoningText`, or in another case, no longer sets it.
- **Breaking:** `protocol.ReasoningEffort` decodes from JSON or text only as one of the seven
  protocol efforts, and refuses any other name, the empty one included, where it took any string.
- **Breaking:** `format.Format` requires `EventStream`. The client asks for its media type in a
  streamed request's Accept header and reads the reply only through its reader, never reading the
  reply's Content-Type; it no longer hard-codes server-sent events.
- **Breaking:** `format.StreamDecoder.Decode` takes a `format.WireEvent` in place of the event's
  data alone. An exception the endpoint reports in the stream reaches the decoder as an event,
  which closes the stream with `protocol.ErrorEvent` and then `protocol.ResponseFailed`.
- **Breaking:** the token-cap minimum is each format's. `Request.Validate` refuses only a
  negative `MaxOutputTokens`, no longer one of 1 to 15; Open Responses refuses a cap below its
  spec's 16 as an `UnsupportedError` prefixed `responses:`, Messages sends any positive cap and
  still requires one, and Chat Completions sends any.
- **Breaking:** `format/messages` sends the system and developer messages that lead the input in
  the top-level `system`, after the instructions, and refuses one after the conversation has
  begun, which Messages has no place for, where it sent each as a system message where it stood.
- **Breaking:** a format's refusal is a `*format.UnsupportedError`, a `Native` member's clash
  among them, as the feature `native member`. A caller that matched a refusal by its text
  still can; one that matched the protocol's refusal of a cap of 1 to 15 gets the Open Responses
  format's instead.
- Each format's unit tests also run against the gap replies recorded from vLLM 0.34.0, in a new
  set beside the vLLM 0.33.0 one, which is unchanged, and from Azure AI Foundry gpt-oss-120b's
  refusal of `tool_choice` `required`: each recorded gap reaches the caller as recorded under the
  zero profile. Declared, a refused effort or `tool_choice` `required` is refused before sending,
  `allowed_tools` is narrowed to the allowed tools, and reasoning text is resent.
- `format/messages` sends a reasoning summary as adaptive thinking's display `summarized`, where
  it dropped the summary, and reads `thinking_tokens` as reasoning tokens.
- `format/messages` accepts several `message_delta` events in a stream, keeping the last
  stop_reason given and each cumulative usage count, so the last item's status waits for
  `message_stop`.
- `format/completions` makes `response.created` at the first chunk that claims the reply, or at
  the stream's end, so it bears the reply's id and model past a leading chunk of no id and no
  choices.
- **Breaking:** `format/messages` reports the `model_context_window_exceeded` stop reason as
  `protocol.ReasonContextWindow`, where it reported `protocol.ReasonMaxOutputTokens`.

### Removed

- **Breaking:** `protocol.MinMaxOutputTokens`. The minimum is Open Responses' alone, now
  `responses.MinMaxOutputTokens`.

### Fixed

- `format/responses` skips a reply's output item of a type the protocol has no form for, such as
  a hosted tool's call, and a message's content part that isn't an output part, whole and
  streamed, where it failed the decode. The items and parts after them take the indexes they
  would have without them.

## [v0.1.1] - 2026-11-17

This release fixes two stream decoders that rejected Azure AI Foundry's streams, and reads the
reasoning tokens Foundry's gpt-oss-120b reports.

### Fixed

- `format/completions` accepts a stream chunk with no id and no choices, such as a leading chunk
  that carries only the content filter's verdict on the prompt: the chunk neither sets the
  stream's id nor is checked against it. The stream's id is the first id a chunk carries, and a
  chunk with another id is still an error.
- `format/responses` accepts a reasoning item added with `encrypted_content`, which the spec
  allows to be incomplete while the item is in progress. The `OutputItemAdded` event carries no
  encrypted content; the `OutputItemDone` event's item holds `output_item.done`'s in the envelope,
  and the folded Response the terminal response's, as before.
- `format/completions` reads a usage's `reasoning_tokens` at its top level, beside
  `completion_tokens`, as gpt-oss-120b on Azure AI Foundry gives them, when
  `completion_tokens_details` gives none, unstreamed and streamed; a count under
  `completion_tokens_details` still wins. Such a reply's reasoning tokens had decoded as 0.

### Changed

- The integration tier reads an optional `AI_<ROLE>_MODEL`, which names the role's model for an
  endpoint that lists more than one, such as a cloud account's whole catalog. With it set, a
  scenario sends to that model, and the models scenario wants at least one model listed.
- The integration tier gains a streamed text scenario on the vision role, through Chat Completions
  and Open Responses.
- The Chat Completions and Open Responses unit tests also run against replies recorded from Azure
  AI Foundry: Chat Completions from gpt-5-mini and gpt-oss-120b, and Open Responses from
  gpt-5-mini.
- CI runs jdx/mise-action v5.2.9.

## [v0.1.0] - 2026-11-12

The first release of go-ai's base module: a model client over the wire formats the model engines
serve natively, on the standard library alone. It is ported from spike-ai-pipeline-client v0.4.0
under go-ai's module path, with go-ai's own design; none of that module's versions precede this
one.

### Added

- `protocol`: Open Responses' item model, spec 2026-04-24, in its stateless subset, under the
  spec's names: `Request` with `Validate`, which refuses a `MaxOutputTokens` of 1 to 15 and a
  `Native` that isn't a JSON object of unique members before anything is sent; `Response`; the
  items, content parts, function tools, and tool choices; the closed set of streaming events;
  and `Accumulator`, which folds a stream into the `Response` an unstreamed request returns. Audio
  input is the client extension `go-ai:input_audio`.
- `format`: the `Format` contract a wire format meets, with `StreamDecoder` and
  `DecodeOpenAIError`.
- `format/completions`, `format/messages`, and `format/responses`: Chat Completions, Anthropic
  Messages, and Open Responses, each built by its typed constructor and each stating what it
  sends, refuses, drops, and normalizes. Reasoning goes back through `encrypted_content` under
  each format's envelope, `go-ai:<format>/v1:`; an envelope under another prefix is dropped on
  encode. `completions` reads a reply's reasoning from `reasoning_content` or `reasoning`.
  `messages` refuses a request with no `MaxOutputTokens` or with audio input.
  `responses.Options.ResendReasoningText` sends a reasoning item's text back for an endpoint that
  gives no `encrypted_content`.
- The root package `ai`: `New(baseURL, httpClient, wireFormat, Options)`, which panics by name on
  a nil HTTP client or format and returns an error for an unusable base URL, retry policy,
  header, or transcription mode. `Options` holds the query, the `TokenSource`, fixed request
  headers in `Header`, the `RetryPolicy`, `StreamIdleTimeout`, and `TranscriptionMode`; its zero
  value sets none of them and transcribes through the route.
- `Client.Generate`, and `Client.GenerateStream`, a lazy `iter.Seq2` of streaming events read in
  the caller's loop with no goroutine, which cancelling the context closes. With
  `StreamIdleTimeout` set, a read of a started stream that gets no bytes for that long ends it
  with an error matching `io.ErrUnexpectedEOF` and `os.ErrDeadlineExceeded`.
- `Client.Embed`, which sends an `EmbeddingRequest`'s batch as one request and places each vector
  by its reply index; and `Client.Models`, a GET of the endpoint's model list. Both go to the
  OpenAI-compatible routes whatever the Client's format.
- `Client.Transcribe`, which opens the `TranscriptionRequest`'s clip once per attempt. In
  `TranscribeRoute`, the default, it streams the clip to the OpenAI-compatible transcription
  route without holding it whole. In `TranscribeChat`, for an endpoint whose audio model has no
  transcription route, it sends the clip as input audio beside an instruction, the request's
  `Prompt` or `TranscriptionInstruction`'s, in one request through the Client's format, which
  must carry audio, as Chat Completions does. Like the route, it returns a whole transcript or an
  error: a reply that isn't completed, such as one stopped at the engine's output token bound, is
  an `IncompleteTranscriptError`, not retried.
- Retry, opt-in through `Options.Retry`: Generate, a stream before it starts, Embed, Models, and
  Transcribe are sent again on a 408, 429, 502, 503, or 504 reply or a connection failure before
  any reply, after a full-jitter backoff that a Retry-After replaces. A wait past `MaxDelay` or
  the context's deadline returns the last error at once. `APIError` carries the reply's
  `RetryAfter` as a duration.
- The integration tier, behind the `integration` build tag, run by `mise run integration` against
  the endpoints `AI_AGENT_BASE_URL`, `AI_VISION_BASE_URL`, `AI_AUDIO_BASE_URL`,
  `AI_EMBEDDING_BASE_URL`, and `AI_TRANSCRIPTION_BASE_URL` name; a scenario whose variable is
  unset skips. Each format's unit tests run against replies recorded from llama.cpp b11669 and
  vLLM 0.33.0.
- The check holds the base module to the standard library: a strict depguard rule over every Go
  file, and an assertion that `go.mod` has no `require`. `mise run upgrade` moves the go
  directive and the mise tools to their latest.
- The release workflow, which makes a GitHub release from `CHANGELOG.md` for a `v*` tag and from
  `tools/infer/CHANGELOG.md` for a `tools/infer/v*` tag.

[v0.3.0]: https://github.com/standards-lab/go-ai/releases/tag/v0.3.0
[v0.2.0]: https://github.com/standards-lab/go-ai/releases/tag/v0.2.0
[v0.1.1]: https://github.com/standards-lab/go-ai/releases/tag/v0.1.1
[v0.1.0]: https://github.com/standards-lab/go-ai/releases/tag/v0.1.0
