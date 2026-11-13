# infer

`infer` is go-ai's command-line client of one model endpoint. It lists the endpoint's models,
embeds, chats, and transcribes through go-ai's base module, in any of its three wire formats,
Chat Completions, Anthropic Messages, and Open Responses, without a Go program of your own.

`github.com/standards-lab/go-ai/tools/infer` is a module of its own, nested in go-ai's
repository and released on `tools/infer/v<semver>` tags, with its changes in
[CHANGELOG.md](CHANGELOG.md). Importing go-ai never fetches what `infer` requires.

## Standard

`infer` is a CLI of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md),
the minimal-dependency Go standard, laid out from
[go-cli-sdk-template](https://github.com/standards-lab/go-cli-sdk-template). This README and
each package's `doc.go` document it. Its module requires go-ai v0.1.0, go-cli-sdk v0.4.0, and
go-core v0.12.0, and nothing else: a depguard rule holds every file, tests and the integration
tier included, to the standard library, go-ai's public packages, go-cli-sdk, go-core, and infer's
own packages.

## Install

```sh
go install github.com/standards-lab/go-ai/tools/infer/cmd/infer@v0.1.0
```

`infer version` prints the installed module version; `infer` alone, or `--help` on any command,
prints its help.

## Configuration

Each run talks to one endpoint, in one wire format. Every setting but the token is a flag over an
`INFER_` variable: the flag wins when it is given, even as an empty value, and the variable
applies otherwise. The root flags are accepted at any depth, before or after the command.

| flag | variable | default | read by |
| --- | --- | --- | --- |
| `--base-url` | `INFER_BASE_URL` | none; required | models, embed, chat, transcribe |
| `--format` | `INFER_FORMAT` | `completions` | models, embed, chat, transcribe |
| `--model` | `INFER_MODEL` | none: the one model the endpoint lists | embed, chat, transcribe |
| `--retries` | `INFER_RETRIES` | `0`: no retry | models, embed, chat, transcribe |
| `--retry-base-delay` | `INFER_RETRY_BASE_DELAY` | `0` | models, embed, chat, transcribe |
| `--retry-max-delay` | `INFER_RETRY_MAX_DELAY` | `0` | models, embed, chat, transcribe |
| `--idle-timeout` | `INFER_IDLE_TIMEOUT` | `0`: no bound | chat |
| `--mode` | `INFER_TRANSCRIPTION_MODE` | `route` | transcribe |
| none | `INFER_TOKEN` | none: no Authorization header | models, embed, chat, transcribe |

- `--base-url` is the endpoint's API root, including `/v1`, such as
  `http://localhost:8000/v1`.
- `--format` is `completions`, `messages`, or `responses`. `models` and `embed` go to the
  OpenAI-compatible routes whatever the format, and so does `transcribe` in route mode, but the
  format is still checked.
- `INFER_TOKEN` is sent as the bearer token. It has no flag, so it never appears in a command
  line or a shell history; the root help names it under `Environment:`.
- `--retries` is the most attempts of each request, the first included, so `3` retries twice; `0`
  or `1` retries nothing. A request is sent again on a 408, 429, 502, 503, or 504 reply, or a
  connection failure before any reply.
- `--retry-base-delay` and `--retry-max-delay`, Go durations such as `500ms` and `10s`, bound
  the wait between attempts, as the next section explains.
- `--idle-timeout`, a Go duration, ends a streamed chat reply that sends nothing for that long.
- `--mode` is `route` or `chat`, as [transcribe](#transcribe) explains.
- `INFER_SHUTDOWN_TIMEOUT`, go-core's lifecycle setting, bounds the shutdown of a command that
  builds anything.

A missing base URL, an unknown format or mode, or a value that doesn't parse, is negative, or is
refused by go-ai, such as a base delay above the max delay, exits 2 before any request, naming
the flag and its variable:

```text
$ infer models
infer models: client: no base URL: set --base-url or INFER_BASE_URL
```

Help and `version` read no configuration, so they run with none set.

### Retries

Retry is off until `--retries` is above 1. Before each retry, go-ai waits a full-jitter backoff
of at most `--retry-base-delay`, doubling for each retry after it, and never above
`--retry-max-delay`. A reply's Retry-After replaces the backoff, and one longer than
`--retry-max-delay` ends the retries, returning the last error at once. So:

- With `--retry-max-delay` unset, `0`, any Retry-After above 0 ends the retries at once, and
  retries that need no Retry-After are sent with no wait.
- A base delay needs a max delay at least as long: `--retry-base-delay 500ms` alone exits 2.

A working policy sets all three:

```sh
infer --retries 3 --retry-base-delay 500ms --retry-max-delay 10s chat "Say hello."
```

### The model a command sends to

With no model set, `embed`, `chat`, and `transcribe` list the endpoint's models first and send to
the one listed. A purpose base URL, one that serves a single model, such as a gateway's
`/<alias>/v1` or one vLLM instance, lists one model, so `--model` can be omitted. A shared
endpoint, which lists several, needs `--model`; without it, the command exits 1 naming each:

```text
$ infer chat "Say hello."
infer chat: several models: agent, vision; pass --model
```

An endpoint that lists none exits 1, `no model set, and the endpoint lists none; pass --model`.
With a model set, nothing is listed first.

## Usage

The examples set the endpoint once in the environment:

```sh
export INFER_BASE_URL=http://localhost:8000/v1
export INFER_TOKEN=...          # only where the endpoint asks for one
```

### models

`models` writes each model ID the endpoint lists, one per line, in the endpoint's order.

```sh
infer models
```

### embed

`embed` sends its inputs as one batch, each argument one input or, with none, each line of
stdin, and writes one JSON array of numbers per input, one per line, in input order. The token
usage goes to stderr, so stdout holds the vectors alone.

```text
$ infer embed "first passage" "second passage"
[0.0123,-0.0456,...]
[0.0789,-0.0012,...]
usage: 22 input tokens, 22 total tokens
```

```sh
infer embed < passages.txt > vectors.jsonl
```

### chat

`chat` sends one prompt, its arguments joined by spaces or, with none, stdin, and writes the
reply's text to stdout. It streams by default, writing each piece of text as it arrives;
`--stream=false` sends one request and writes the whole reply. Either way, stderr then holds the
reply's summary:

```text
$ infer chat "What is a wire format? One sentence."
A wire format is the exact layout of data as it is sent between programs.
status: completed
usage: 78 input tokens, 38 output tokens (27 reasoning), 116 total tokens
reasoning items: 1
```

The reasoning tokens are part of the output tokens. Through `messages`, whose usage doesn't count
them, the usage line says so rather than show a zero:

```text
usage: 70 input tokens, 38 output tokens (reasoning not reported), 108 total tokens
```

```sh
# unstreamed, from stdin, with instructions
echo "Summarize this paragraph." | infer chat --stream=false --instructions "Answer in French."

# an image, a file inlined or an http(s) URL as given; repeat --image for several
infer chat --image shapes.png --image https://example.com/chart.png "What do these show?"

# the reasoning effort: none, low, medium, high, or xhigh
infer chat --reasoning low "Is 1009 prime?"

# the most tokens the model generates, reasoning included; 16 or more
infer chat --max-tokens 256 "Name three rivers."

# an engine parameter the typed flags don't cover, merged into the request's top level
infer chat --native '{"temperature": 0.2, "top_k": 40}' "Name a color."

# the Messages format, which needs --max-tokens
infer --format messages chat --max-tokens 512 "Say hello."
```

A request go-ai refuses, such as a `--max-tokens` below 16, an unknown effort, a `--native` that
isn't a JSON object or sets a member a typed flag sets, or a Messages request with no
`--max-tokens`, exits 2 before anything is sent. A reply that isn't completed keeps the text
already written, writes its summary, and exits 1 naming its status and reason:

```text
$ infer chat --max-tokens 16 "Tell me a long story."
Once upon a time
status: incomplete
usage: 70 input tokens, 16 output tokens (0 reasoning), 86 total tokens
reasoning items: 0
infer chat: incomplete: max_output_tokens
```

### transcribe

`transcribe` sends one audio file, opened again for each attempt a retry makes, and writes its
transcript to stdout, with the token usage on stderr. In route mode, the default, it sends the
file to the endpoint's OpenAI-compatible transcription route:

```sh
infer transcribe clip.wav
```

In chat mode it sends the clip as input audio in a chat request in the format, for an endpoint
whose audio model has no transcription route. The file's extension, `.wav` or `.mp3`, gives its
encoding, and the format must carry audio, as `completions` does; `messages` exits 1 before the
file is opened. A chat-mode reply that isn't completed exits 1 naming its status and reason, and
writes no transcript.

```sh
infer transcribe --mode chat clip.wav
INFER_TRANSCRIPTION_MODE=chat infer transcribe clip.wav
```

## Exit codes

| code | meaning |
| --- | --- |
| 0 | the command succeeded |
| 1 | a failure past the command line: the endpoint's error or refusal, a model the fallback can't choose, a reply that isn't completed, a stream idle past `--idle-timeout`, or a file that can't be read |
| 2 | help, or a usage error: an unknown command or flag, a missing or refused setting, no input or prompt, or a request go-ai refuses before sending |

A failure is reported once, on stderr, prefixed with the command, as in `infer chat: ...`.

## Development

The repository's [mise](https://mise.jdx.dev) tasks cover both modules, so they run from the
repository root:

```sh
mise run check        # build, vet, format, fix, tidy, test, and lint every module; writes nothing
mise run integration  # run every module's integration tier against the AI_*_BASE_URL endpoints
```

The unit tier is hermetic: `internal/app`'s tests run the App in process against loopback
endpoints, with infer's own fixtures in `internal/apptest`, and assert what each command sends
and writes.

### The integration tier

The integration tier, in `integration` behind the `integration` build tag, builds `cmd/infer`
and runs it as a subprocess against live endpoints, one per role, each named by a variable
holding its API root, including `/v1`, as go-ai's own tier names them:

- `AI_AGENT_BASE_URL`: chat, streamed and unstreamed, in every format, and an incomplete reply at
  `--max-tokens 16`.
- `AI_VISION_BASE_URL`: an image chat in every format, and `--reasoning none` and `low`.
- `AI_AUDIO_BASE_URL`: `transcribe --mode chat` through Chat Completions.
- `AI_EMBEDDING_BASE_URL`: `embed`.
- `AI_TRANSCRIPTION_BASE_URL`: `transcribe` in route mode.

`models` runs on every role. Each scenario maps its variable onto `INFER_BASE_URL`, after
clearing every `INFER_` variable it inherited, and sets no model, so each endpoint must list one;
a scenario whose variable is unset skips, naming it. `mise run integration` runs the tier verbose:

```sh
AI_AGENT_BASE_URL=http://localhost:8000/v1 mise run integration
```

The suite sends the media in go-ai's `integration/testdata`, so it runs from a checkout of the
repository. No CI job runs it, as the endpoints are the organization's own machines.

## License

[Apache License 2.0](../../LICENSE).
