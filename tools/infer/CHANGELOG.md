# Changelog

All notable changes to `github.com/standards-lab/go-ai/tools/infer` are documented here. The
format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the module adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). This changelog covers `infer`
only; go-ai's base module keeps its own, in the repository's root `CHANGELOG.md`.

## [Unreleased]

## [v0.3.1] - 2026-11-19

This release moves `infer` to go-ai v0.4.0, with no user-visible change: every command's output
and exit status are as in v0.3.0. It requires go-ai v0.4.0, go-cli-sdk v0.4.0, and go-core
v0.12.0, and nothing else.

### Changed

- Requires go-ai v0.4.0, in place of v0.3.0.
- `infer chat` reads an unstreamed reply's text with go-ai's `Response.OutputText`, in place of
  infer's own copy of it.
- `infer transcribe` reads the transcription mode setting with `ai.TranscriptionMode`'s text
  form, in place of infer's own map of mode names. An unknown mode is the same usage error as
  before, naming `--mode`, `INFER_TRANSCRIPTION_MODE`, `route`, and `chat`.
- The unit tier runs against go-ai's public `aitest` endpoint, in place of the endpoint infer's
  `internal/apptest` held. `internal/apptest` keeps only the graph fixtures.

## [v0.3.0] - 2026-11-18

This release moves `infer` to go-ai v0.3.0, whose listed models keep each listing entry as the
endpoint sent it. It requires go-ai v0.3.0, go-cli-sdk v0.4.0, and go-core v0.12.0, and nothing
else.

### Changed

- `infer models` writes each entry's `root` beside its ID, after a tab, where the listing carries
  a non-empty string one, as vLLM does for a served alias: `agent\topenai/gpt-oss-120b`. An entry
  whose `root` is missing, null, empty, or not a string is written as its ID alone, as before.
- Requires go-ai v0.3.0, in place of v0.2.0, for the listing entry go-ai's `Model.Native` keeps.

## [v0.2.0] - 2026-11-18

This release moves `infer` to go-ai v0.2.0 and adds the deployment's profile, in which an
endpoint that departs from a format's spec declares its gaps. It requires go-ai v0.2.0,
go-cli-sdk v0.4.0, and go-core v0.12.0, and nothing else.

### Added

- `--profile`, over `INFER_PROFILE`, the deployment's profile: a JSON object keyed by
  `completions`, `messages`, or `responses`, each member that go-ai format's `Options` under
  their JSON names, such as `{"responses":{"refused_efforts":["none"]}}`. infer builds the format
  in use with its member; unset or empty, each format is its spec. It is decoded strictly, so an
  unknown format name or field, a Go field name, or a value a field refuses exits 2 before any
  request, naming the flag and its variable. `models` and `embed` send nothing a profile changes
  and ignore it.
- `chat --reasoning` takes `minimal` and `max`, so its efforts are `none`, `minimal`, `low`,
  `medium`, `high`, `xhigh`, and `max`. A format whose spec lacks one refuses it before sending:
  Open Responses `minimal` and `max`, Messages `minimal`.
- The integration tier reads each role's optional `AI_<ROLE>_PROFILE`, in `--profile`'s shape,
  and maps it onto `INFER_PROFILE`. It is decoded strictly first, and a malformed one fails the
  role's scenarios naming the variable. A run refused for a gap the profile declares skips its
  scenario, quoting the refusal.

### Changed

- **Breaking:** requires go-ai v0.2.0, in place of v0.1.0.
- A chat refusal names its flag from go-ai's typed `UnsupportedError`. A refusal of a gap the
  profile declares ends with `; declared by --profile (INFER_PROFILE)`, as in `--reasoning:
  responses: reasoning effort none is refused by the deployment, as its profile declares;
  declared by --profile (INFER_PROFILE)`. A refusal of the spec's own has no suffix.
- `--max-tokens` from 1 to 15 is refused only under `--format responses`, whose spec sets the
  minimum, as `--max-tokens: responses: request's max output tokens 15 is below the minimum, 16`.
  Through `completions` and `messages` it is sent as given.
- `transcribe --mode chat` names the mode and format only when go-ai refuses the audio part;
  any other refusal, such as a file name with no audio extension, is go-ai's error alone.
- `chat`'s usage line prints `(N reasoning)` when the endpoint reports reasoning tokens, and
  `(reasoning not reported)` only when it reports none and the reply holds a reasoning item, in
  any format. Otherwise it prints no parenthesis, where it printed `(0 reasoning)`, or `(reasoning
  not reported)` for every `messages` reply.
- Retries by default: 3 attempts, 500ms base, 30s most; `--retries 1` sends once. Each of
  `--retries`, `--retry-base-delay`, and `--retry-max-delay`, and its variable, unset or empty
  is its default, shown in help, where each was zero and infer retried nothing. A
  `--retry-max-delay` below 500ms caps the default base delay at it, so `--retry-max-delay`
  alone is a working policy.

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
  flag's, and an empty one sends none. `INFER_HEADER`, never a flag's either, holds headers sent
  with every request, such as a gateway's `api-key`, one `Name: value` per line, each line
  trimmed and a blank one skipped. A header line with no colon or no name, a name of
  `Authorization`, `Content-Type`, or `Accept`, or a name or value that can't be sent exits 2
  before any request, naming `INFER_HEADER` and never echoing the value. The root help names both
  variables under `Environment:`. A missing base URL, an unknown format, or a value go-ai's `New`
  refuses exits 2 before any request, naming the flag and its variable.
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
  `--max-tokens` below 16, exits 2 before any request, naming the flag to change. It streams by
  default, writing each text delta as it arrives; `--stream=false` sends one unstreamed request.
  Either way it writes the reply's status, token usage, and reasoning item count to stderr, as
  in `usage: 78 input tokens, 38 output tokens (27 reasoning), 116 total tokens`, or `(reasoning
  not reported)` through `messages`, whose usage doesn't count reasoning tokens. A reply that
  isn't completed keeps the text written and exits 1 naming its status and reason, as in
  `incomplete: max_output_tokens`.
- `transcribe`, which sends one audio file, opened again for each attempt, and writes its
  transcript, with the token usage on stderr. `--mode`, over `INFER_TRANSCRIPTION_MODE`, is
  `route`, the endpoint's transcription route, when unset, or `chat`, a chat request in the
  format, for an endpoint whose audio model has no transcription route. A route-mode 404 exits 1
  with a hint to try `--mode chat`. Chat mode through a format that can't carry audio exits 1
  before the file is opened, naming `--mode` and `--format`; an incomplete chat-mode reply exits 1
  naming its status and reason.
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
  through `mise run integration`; a scenario whose variable is unset skips. A role's optional
  `AI_<ROLE>_TOKEN` and `AI_<ROLE>_HEADER` map onto `INFER_TOKEN` and `INFER_HEADER`, and no
  scenario's log shows their values.

[Unreleased]: https://github.com/standards-lab/go-ai/compare/tools/infer/v0.3.1...HEAD
[v0.3.1]: https://github.com/standards-lab/go-ai/releases/tag/tools/infer/v0.3.1
[v0.3.0]: https://github.com/standards-lab/go-ai/releases/tag/tools/infer/v0.3.0
[v0.2.0]: https://github.com/standards-lab/go-ai/releases/tag/tools/infer/v0.2.0
[v0.1.0]: https://github.com/standards-lab/go-ai/releases/tag/tools/infer/v0.1.0
