# infer

A command-line client for model endpoints. `infer` lists models, chats, embeds, and transcribes
through go-ai, in Chat Completions, Anthropic Messages, or Open Responses, with no Go program of
your own.

```sh
go install github.com/standards-lab/go-ai/tools/infer/cmd/infer@v0.1.0
```

## Quick use

```sh
export INFER_BASE_URL=http://localhost:8000/v1

infer models                                  # each model ID, one per line
infer chat "Is 1009 prime?"                   # the reply, streamed; its summary on stderr
infer embed "first passage" "second passage"  # one JSON vector per input, one per line
infer transcribe --mode chat clip.wav         # the transcript
```

New to `infer`? The [quick start](docs/quick-start.md) walks through each command with real
output, and ends with the common failures and their fixes. This README is the reference.

## Commands

| Command | Input | stdout | stderr |
| --- | --- | --- | --- |
| `models` | none | each model ID, in the endpoint's order | |
| `chat` | the prompt: the arguments joined by spaces, or stdin | the reply's text | the reply's status, usage, and reasoning item count |
| `embed` | each argument, or each line of stdin | one JSON array per input, in input order | the token usage |
| `transcribe` | one `.wav` or `.mp3` file | the transcript | the token usage |
| `version` | none | the installed module version | |

`infer` alone, or `--help` on any command, prints help. Help and `version` read no
configuration.

## Configuration

Each run talks to one endpoint, in one format. A flag wins over its variable, even when given
empty. Root flags work before or after the command.

| Flag | Variable | Default | Used by |
| --- | --- | --- | --- |
| `--base-url` | `INFER_BASE_URL` | none; required | every command but `version` |
| `--format` | `INFER_FORMAT` | `completions` | every command but `version` |
| `--model` | `INFER_MODEL` | the one model the endpoint lists | chat, embed, transcribe |
| `--retries` | `INFER_RETRIES` | `0`: no retry | every command but `version` |
| `--retry-base-delay` | `INFER_RETRY_BASE_DELAY` | `0` | every command but `version` |
| `--retry-max-delay` | `INFER_RETRY_MAX_DELAY` | `0` | every command but `version` |
| `--idle-timeout` | `INFER_IDLE_TIMEOUT` | `0`: no bound | chat |
| `--mode` | `INFER_TRANSCRIPTION_MODE` | `route` | transcribe |
| | `INFER_TOKEN` | none | every command but `version` |
| | `INFER_HEADER` | none | every command but `version` |

- `--base-url` is the API root, `/v1` included.
- `--format` is `completions`, `messages`, or `responses`. `models`, `embed`, and route-mode
  `transcribe` use the OpenAI-compatible routes whatever the format.
- `INFER_SHUTDOWN_TIMEOUT` bounds shutdown, as go-core's lifecycle setting.

A missing or invalid setting exits 2 before anything is sent. The message names the flag and its
variable.

### Credentials

Credentials have no flag, so they never appear in a command line or a shell history.

- `INFER_TOKEN` is sent as the bearer token.
- `INFER_HEADER` holds headers for an endpoint that takes its key in a header of its own, such as
  a gateway's `api-key`. Write one `Name: value` per line. It refuses `Authorization`,
  `Content-Type`, and `Accept`, which go-ai sets, and never echoes a value in an error.

```sh
export INFER_BASE_URL=https://gateway.example/agent/v1
export INFER_HEADER='api-key: …'
```

### The model

With no `--model`, `chat`, `embed`, and `transcribe` list the endpoint's models and send to the
one listed. An endpoint that serves one model, such as one vLLM instance or a gateway's
per-purpose `/<alias>/v1`, needs no `--model`. An endpoint that lists several, or none, exits 1
and asks for `--model`.

### Retries

Retry is off until `--retries` is above 1. `--retries` counts every attempt, so `3` retries
twice. A request is retried on a 408, 429, 502, 503, or 504 reply, or a connection that fails
before any reply.

Before each retry, infer waits a random time up to `--retry-base-delay`, doubling each time,
never above `--retry-max-delay`. A reply's Retry-After replaces the wait. A Retry-After longer
than `--retry-max-delay` stops the retries. Set all three:

```sh
infer --retries 3 --retry-base-delay 500ms --retry-max-delay 10s chat "Say hello."
```

## chat

`chat` streams the reply by default. Its flags:

| Flag | What it does |
| --- | --- |
| `--stream=false` | Sends one request and writes the whole reply. |
| `--instructions` | Guides the model, ahead of the prompt. |
| `--image` | Attaches an image: a file, sent inline, or an `http(s)` URL, sent as given. Repeatable. |
| `--reasoning` | The reasoning effort: `none`, `low`, `medium`, `high`, or `xhigh`. |
| `--max-tokens` | The most tokens generated, reasoning included; 16 or more. Required with `--format messages`. |
| `--native` | A JSON object merged into the request, for engine parameters the flags don't cover, such as `'{"temperature": 0.2}'`. It can't set what a flag sets. |
| `--idle-timeout` | Ends a stream that sends nothing for this long. |

The summary on stderr:

```text
status: completed
usage: 78 input tokens, 38 output tokens (27 reasoning), 116 total tokens
reasoning items: 1
```

Reasoning tokens are part of the output tokens. Through `messages`, which doesn't count them, the
usage line says `(reasoning not reported)`. A reply that isn't completed keeps the text already
written, prints its summary, and exits 1.

## transcribe

`--mode route`, the default, uploads the file to the endpoint's transcription route. `--mode
chat` sends it as input audio in a chat request, for an audio model with no transcription route.
Chat mode needs a format that carries audio, so it works with `completions` and not `messages`.

An endpoint with no route answers 404. infer suggests `--mode chat` but never switches on its
own, because a 404 can also mean a wrong base URL.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success. |
| 1 | A failure past the command line: the endpoint's error, a model infer can't choose, a reply that isn't completed, an idle stream, or a file that can't be read. |
| 2 | Help, or a usage error: an unknown command or flag, a missing or invalid setting, no input, or a request go-ai refuses before sending. |

A failure is reported once on stderr, prefixed with the command, as in `infer chat: …`.

## Standard

`infer` is a CLI of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md),
laid out from [go-cli-sdk-template](https://github.com/standards-lab/go-cli-sdk-template). Its
module requires go-ai v0.1.0, go-cli-sdk v0.4.0, and go-core v0.12.0, and nothing else. A
depguard rule holds every file, tests included, to the standard library, go-ai's public
packages, go-cli-sdk, go-core, and infer's own packages.

`github.com/standards-lab/go-ai/tools/infer` is a module of its own, released on
`tools/infer/v<semver>` tags, with its changes in [CHANGELOG.md](CHANGELOG.md). Importing go-ai
never fetches what `infer` requires.

## Development

The repository's [mise](https://mise.jdx.dev) tasks cover both modules and run from the
repository root:

```sh
mise run check        # build, vet, format, fix, tidy, test, and lint every module; writes nothing
mise run integration  # run every module's integration tier against the AI_*_BASE_URL endpoints
```

The unit tier runs the app in process against loopback endpoints and asserts what each command
sends and writes.

The integration tier builds `cmd/infer` and runs it against live endpoints. It reads the same
role variables as [go-ai's tier](../../README.md#development), mapping each role's
`AI_<ROLE>_BASE_URL`, `AI_<ROLE>_TOKEN`, and `AI_<ROLE>_HEADER` onto `INFER_BASE_URL`,
`INFER_TOKEN`, and `INFER_HEADER`:

| Variable | Scenarios |
| --- | --- |
| `AI_AGENT_BASE_URL` | chat, streamed and not, in every format, and an incomplete reply |
| `AI_VISION_BASE_URL` | an image chat in every format, and `--reasoning none` and `low` |
| `AI_AUDIO_BASE_URL` | `transcribe --mode chat` |
| `AI_EMBEDDING_BASE_URL` | `embed` |
| `AI_TRANSCRIPTION_BASE_URL` | route-mode `transcribe` |

Every role also runs `models`. A role whose variable is unset skips. A credentials scenario
against a loopback endpoint always runs, and checks that the token and header arrive and that no
log shows them. The suite sends go-ai's `integration/testdata` media, so it runs from a checkout.

## License

[Apache License 2.0](../../LICENSE).
