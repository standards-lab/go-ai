# Changelog

All notable changes to `github.com/standards-lab/go-ai/tools/infer` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers `infer`
only; go-ai's base module keeps its own, in the repository's root `CHANGELOG.md`.

## [Unreleased]

## [v0.1.0] - 2026-11-13

The first release of `infer`, go-ai's command-line client of one model endpoint: a Go Elemental
CLI laid out from go-cli-sdk-template, over go-ai v0.1.0's released surface alone. It requires
go-ai v0.1.0, go-cli-sdk v0.4.0, and go-core v0.12.0, and nothing else.

### Added

- The commands `models`, `embed`, `chat`, `transcribe`, and `version`. Help and a usage error
  exit 2, any other failure exits 1, reported once on stderr; help and `version` read no
  configuration and send nothing.
- One endpoint per run, configured by the root flags `--base-url`, `--format`, and `--model`,
  accepted at any depth, each over its variable, `INFER_BASE_URL`, `INFER_FORMAT`, and
  `INFER_MODEL`, the flag winning when given. `--format` is `completions`, `messages`, or
  `responses`, and `completions` when unset. The bearer token is `INFER_TOKEN`'s alone, never a
  flag's, and an empty one sends none; the root help names it under `Environment:`. A missing base URL, an unknown format, or a value go-ai's
  `New` refuses exits 2 before any request, naming the flag and its variable.
- The one-model fallback: with no model set, `embed`, `chat`, and `transcribe` list the
  endpoint's models first and send to the one listed, so an endpoint that serves one model needs
  no `--model`. An endpoint that lists none exits 1 saying so, and one that lists several exits 1
  naming each, as in `several models: agent, vision; pass --model`. A set model lists nothing
  first.
- `models`, which writes each model ID the endpoint lists, one per line, in its order.
- `embed`, which sends its inputs, each argument or else each line of stdin, as one batch, and
  writes one JSON array of numbers per input, one per line, in input order, with the token usage
  on stderr.
- `chat`, which sends one prompt, its arguments joined by spaces or else stdin, with
  `--instructions`, `--image` (repeatable: a file inlined as a data URL, or an http(s) URL as
  given), `--reasoning`, `--max-tokens`, and `--native`, a JSON object merged into the request's
  top level. A request go-ai's validation or the format's encoding refuses, such as a
  `--max-tokens` below 16, exits 2 before any request. It streams by default, writing each text
  delta as it arrives; `--stream=false` sends one unstreamed request. Either way it writes the
  reply's status, token usage, and reasoning item count to stderr, as in `usage: 78 input tokens,
  38 output tokens (27 reasoning), 116 total tokens`, or `(reasoning not reported)` through
  `messages`, whose usage doesn't count reasoning tokens; a
  reply that isn't completed keeps the text written and exits 1 naming its status and reason, as
  in `incomplete: max_output_tokens`.
- `transcribe`, which sends one audio file, opened again for each attempt, and writes its
  transcript, with the token usage on stderr. `--mode`, over `INFER_TRANSCRIPTION_MODE`, is
  `route`, the endpoint's transcription route, when unset, or `chat`, a chat request in the
  format, for an endpoint whose audio model has no transcription route. Chat mode through a
  format that can't carry audio exits 1 before the file is opened; an incomplete chat-mode reply
  exits 1 naming its status and reason.
- Retry, opt-in through the root flags `--retries`, `--retry-base-delay`, and
  `--retry-max-delay`, over `INFER_RETRIES`, `INFER_RETRY_BASE_DELAY`, and
  `INFER_RETRY_MAX_DELAY`, which set go-ai's retry policy for every command; unset retries
  nothing. `chat`'s `--idle-timeout`, over `INFER_IDLE_TIMEOUT`, ends a stream that sends nothing
  for that long. Help lists `--retries` as an `int` and the delays and the timeout as
  `duration`s. A negative, inverted, or unparsable value exits 2, naming the flag and its
  variable.
- The unit tier, which runs the App in-process against loopback endpoints with infer's own
  fixtures in `internal/apptest`, and the integration tier, behind the `integration` build tag,
  which runs the built binary against the endpoints the `AI_<ROLE>_BASE_URL` variables name,
  through `mise run integration`; a scenario whose variable is unset skips.

[Unreleased]: https://github.com/standards-lab/go-ai/compare/tools/infer/v0.1.0...HEAD
[v0.1.0]: https://github.com/standards-lab/go-ai/releases/tag/tools/infer/v0.1.0
