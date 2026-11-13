# Changelog

All notable changes to `github.com/standards-lab/go-ai` are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers the base module
only; `tools/infer` keeps its own.

## [Unreleased]

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

[Unreleased]: https://github.com/standards-lab/go-ai/compare/v0.1.0...HEAD
[v0.1.0]: https://github.com/standards-lab/go-ai/releases/tag/v0.1.0
