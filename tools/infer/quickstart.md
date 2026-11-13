# infer quickstart

This guide walks through `infer` from install to each command, then through the ways a command
fails. [README.md](README.md) is the reference for every flag, variable, and exit code; this is
the guided tour.

Every output below is from a real run. Outputs vary by model, so each section names the model
its output came from. The endpoint addresses, such as `http://localhost:8000/v1`, are
placeholders for your own. Long output is trimmed, and the trim is marked `…`. A terminal shows the
reply on stdout and the summary on stderr together, as here. The media are go-ai's own test
files, so the commands that send them run from the root of a go-ai checkout.

## Install, version, and help

```sh
go install github.com/standards-lab/go-ai/tools/infer/cmd/infer@v0.1.0
```

`infer version` prints the installed module version. A build from a checkout prints a
pseudo-version:

```text
$ infer version
v0.0.0-20261113152752-dad3c096e072
```

`infer` alone prints the root help. Help and `version` read no configuration, so they run before
any endpoint is set:

```text
$ infer
infer sends requests to one model endpoint through go-ai.

Usage:
  infer <command> [flags]

Commands:
  version      Print the infer version
  models       List the endpoint's model IDs, one per line
  embed        Embed each argument, or each line of stdin, writing one JSON vector per line
  chat         Send a prompt, from the arguments or stdin, and write the model's reply
  transcribe   Transcribe an audio file, writing its transcript

Flags:
  --base-url string             the endpoint's API root, such as http://localhost:8000/v1 (env INFER_BASE_URL)
  --format string               the wire format: completions, messages, or responses (env INFER_FORMAT) (default "completions")
  --model string                the model the request names (env INFER_MODEL)
  --retries int                 the most attempts of each request, the first included, as an int, so 3 retries twice, when the endpoint answers 408, 429, 502, 503, or 504 or the connection fails; 0 or 1 retries nothing (env INFER_RETRIES)
  --retry-base-delay duration   the longest wait before the first retry, a duration such as 500ms, doubling for each retry after it; at most --retry-max-delay (env INFER_RETRY_BASE_DELAY)
  --retry-max-delay duration    the longest wait between attempts, a duration such as 30s, and the longest Retry-After waited for; a longer Retry-After ends the retries (env INFER_RETRY_MAX_DELAY)
  --help                        Show help for infer

Run 'infer <command> --help' for help on a command.

Environment:
  INFER_TOKEN    the bearer token sent with each request; unset or empty sends none
  INFER_HEADER   headers sent with each request, one "Name: value" per line, such as a gateway's api-key
```

Each flag names the variable it overrides, as in `(env INFER_BASE_URL)`. The flag wins when it is
given, and the variable applies otherwise. The `Environment:` note names the two settings that
have no flag, `INFER_TOKEN` and `INFER_HEADER`. A credential is a secret, so it never goes on a
command line or into a shell history. `infer <command> --help` adds the command's own flags, such
as chat's `--max-tokens`, above these. Help exits 2, as a usage error does.

## Pointing at an endpoint

Each run talks to one endpoint, named by its API root, `/v1` included. Set it once in the
environment:

```sh
export INFER_BASE_URL=<your chat endpoint, such as http://localhost:8000/v1>
```

or give it for one run with `--base-url`, before or after the command:

```text
$ infer --base-url http://localhost:8000/v1 chat "Say hello in five words."
Hello there, wonderful humans today!
status: completed
usage: 73 input tokens, 79 output tokens (62 reasoning), 152 total tokens
reasoning items: 1
```

(output from gpt-oss-120b)

A root left without `/v1` reaches the server but not its API, so the endpoint answers 404. A root
with no scheme, such as `localhost:8000/v1`, is refused before anything is sent.

### The one-model fallback

With no `--model`, a command that sends to a model lists the endpoint's models first and sends to
the one listed. An endpoint that serves one model, such as one vLLM instance or a gateway's
per-purpose root, needs no `--model`, and the guide's examples set none.

A shared endpoint, which lists several models, needs `--model` or `INFER_MODEL`, set to an ID
`infer models` lists:

```text
$ infer --model openai/gpt-oss-120b chat "Say hello in five words."
Hello, dear friend, how's day?
status: completed
usage: 73 input tokens, 223 output tokens (204 reasoning), 296 total tokens
reasoning items: 1
```

```sh
export INFER_MODEL=openai/gpt-oss-120b   # for every run after it
```

With a model set, nothing is listed first.

### A credential

This part is setup only: these endpoints take no credential, so it has no output to show.

An endpoint that takes a bearer token takes it from `INFER_TOKEN`:

```sh
export INFER_BASE_URL=<your gateway or cloud endpoint, such as https://gateway.example/agent/v1>
export INFER_TOKEN=<your token>
```

An endpoint that takes its key in a header of its own, such as a gateway's or a cloud endpoint's
`api-key`, takes it from `INFER_HEADER`, one `Name: value` per line:

```sh
export INFER_HEADER='api-key: <your key>'
```

Several headers go on separate lines of the one variable. `INFER_HEADER` refuses
`Authorization`, `Content-Type`, and `Accept`, which infer sets itself. The bearer token goes in
`INFER_TOKEN`.

## models

`models` writes each model ID the endpoint lists, one per line:

```text
$ infer models
openai/gpt-oss-120b
```

(output from a chat endpoint serving gpt-oss-120b)

## chat

The examples in this section send to a chat model:

```sh
export INFER_BASE_URL=<your chat endpoint, such as http://localhost:8000/v1>
```

The outputs are from gpt-oss-120b, a reasoning model.

### Streamed, the default

`chat` joins its arguments into one prompt and streams the reply to stdout as it arrives. Then
it writes the reply's summary to stderr:

```text
$ infer chat "What is a wire format? One sentence."
A wire format is the standardized, low‑level binary or textual representation of data as it is transmitted across a network or communication channel.
status: completed
usage: 76 input tokens, 74 output tokens (37 reasoning), 150 total tokens
reasoning items: 1
```

### Unstreamed

`--stream=false` sends one request and writes the whole reply once it is complete:

```text
$ infer chat --stream=false "What is a wire format? One sentence."
A wire format is the exact binary or textual encoding of data as it is transmitted across a network or communication channel.
status: completed
usage: 76 input tokens, 81 output tokens (48 reasoning), 157 total tokens
reasoning items: 1
```

### From stdin, with instructions

With no arguments, the prompt is stdin. `--instructions` guides the model ahead of the prompt:

```text
$ echo "Summarize: The quick brown fox jumps over the lazy dog. It does this every morning before breakfast." | infer chat --instructions "Answer in French, in one sentence."
Le renard brun rapide saute par-dessus le chien paresseux chaque matin avant le petit‑déjeuner.
status: completed
usage: 104 input tokens, 119 output tokens (87 reasoning), 223 total tokens
reasoning items: 1
```

### The reasoning effort

`--reasoning` is `none`, `low`, `medium`, `high`, or `xhigh`. Unset, the effort is left to the
endpoint. The effort changes what the model spends on reasoning:

```text
$ infer chat --reasoning low "Is 1009 prime? Answer yes or no."
Yes.
status: completed
usage: 78 input tokens, 24 output tokens (12 reasoning), 102 total tokens
reasoning items: 1

$ infer chat --reasoning high "Is 1009 prime? Answer yes or no."
Yes.
status: completed
usage: 78 input tokens, 431 output tokens (419 reasoning), 509 total tokens
reasoning items: 1
```

Not every model takes every effort. gpt-oss always reasons, so it refuses `none`, as
[When a command fails](#reasoning-none-on-a-model-that-always-reasons) shows.

### The token cap

`--max-tokens` caps the tokens the model generates, reasoning included. It must be 16 or more:

```text
$ infer chat --max-tokens 256 "Name three rivers."
Sure! Here are three well‑known rivers:

1. **The Nile** – Flowing northward through northeastern Africa, it’s traditionally considered the longest river in the world.  
2. **The Amazon** – Running through South America, it carries the largest volume of water of any river on Earth.  
3. **The Yangtze** – The longest river in Asia, winding across China from the Tibetan plateau to the East China Sea.
status: completed
usage: 71 input tokens, 146 output tokens (45 reasoning), 217 total tokens
reasoning items: 1
```

A reply that reaches the cap ends incomplete, as
[an incomplete reply](#an-incomplete-reply-at-the-token-cap) shows.

### Engine parameters with --native

`--native` takes a JSON object and merges it into the request's top level. It carries an engine
parameter that the typed flags don't cover:

```text
$ infer chat --native '{"temperature": 0.2, "top_k": 40}' "Name a color. One word."
Azure
status: completed
usage: 74 input tokens, 45 output tokens (34 reasoning), 119 total tokens
reasoning items: 1
```

`--native` can't set a member that the request already sets, such as `model` or `stream`, or the
format's cap member, `max_completion_tokens` in completions, when `--max-tokens` is given.

### Another format

`--format`, or `INFER_FORMAT`, picks the wire format: `completions`, the default, `responses`, or
`messages`. The same endpoint answers in each one it serves:

```text
$ infer --format responses chat "Say hello in five words."
Hello there wonderful human friend.
status: completed
usage: 73 input tokens, 157 output tokens (141 reasoning), 230 total tokens
reasoning items: 1
```

The Messages format requires a token cap, so it needs `--max-tokens`:

```text
$ infer --format messages chat --max-tokens 512 "Say hello in five words."
Hello, dear friend, welcome aboard!
status: completed
usage: 73 input tokens, 153 output tokens (reasoning not reported), 226 total tokens
reasoning items: 1
```

### Reading the summary

The summary on stderr has three lines:

- `status:` is `completed` for a whole reply. Any other status also exits 1.
- `usage:` gives the input, output, and total tokens. The parenthesis gives the output tokens
  spent reasoning, which are part of the output tokens, not added to them.
- `reasoning items:` counts the reasoning items in the reply. A model that didn't reason shows
  `0`.

Through `messages`, the usage doesn't count reasoning tokens, so the parenthesis says
`(reasoning not reported)` rather than show a zero. The item count is still the reply's own: the
messages reply above reasoned, with `reasoning items: 1`.

## Images

The examples in this section send to a vision model:

```sh
export INFER_BASE_URL=<your vision endpoint, such as http://localhost:8001/v1>
```

`--image` attaches an image to the prompt. A file is sent inline, and an `http(s)` URL is sent as
given. Repeat `--image` to attach several:

```text
$ infer chat --image integration/testdata/shapes.png "What shapes are in this image? One sentence."
This image contains a red circle, a blue square, and a green triangle.
status: completed
usage: 281 input tokens, 17 output tokens (0 reasoning), 298 total tokens
reasoning items: 0
```

(output from Gemma-4-26B-A4B-NVFP4)

This model takes `--reasoning none`. Images work in every format, Messages included:

```text
$ infer chat --reasoning none --image integration/testdata/shapes.png "What colors are the shapes? One sentence."
The circle is red, the square is blue, and the triangle is green.
status: completed
usage: 280 input tokens, 17 output tokens (0 reasoning), 297 total tokens
reasoning items: 0

$ infer --format messages chat --max-tokens 256 --image integration/testdata/shapes.png "How many shapes? Answer with a number."
3
status: completed
usage: 280 input tokens, 2 output tokens (reasoning not reported), 282 total tokens
reasoning items: 0
```

## Audio

The examples in this section send to an audio model:

```sh
export INFER_BASE_URL=<your audio endpoint, such as http://localhost:8002/v1>
```

`transcribe` writes an audio file's transcript to stdout and the token usage to stderr. By
default it uses the endpoint's transcription route. An audio chat model often has no such route,
so `--mode chat` sends the clip as input audio in a chat request instead:

```text
$ infer transcribe --mode chat integration/testdata/phrase.wav
The access code is 7429.
usage: 181 input tokens, 11 output tokens, 192 total tokens
```

(output from gemma-4-E4B-it)

`INFER_TRANSCRIPTION_MODE=chat` sets the same mode for every run. The file's extension, `.wav` or
`.mp3`, gives its encoding.

## embed

The examples in this section send to an embedding model:

```sh
export INFER_BASE_URL=<your embedding endpoint, such as http://localhost:8003/v1>
```

`embed` writes one JSON array of numbers per input, one per line, in input order. Each argument
is one input:

```text
$ infer embed "first passage" "second passage"
[0.18457031,-0.6640625,-0.484375,0.46875,0.89453125,…]
[-0.4453125,-0.76953125,-0.56640625,1.1484375,0.5078125,…]
usage: 8 input tokens, 8 total tokens
```

(output from granite-embedding-english-r2, 768 numbers per vector)

With no arguments, each line of stdin is one input. The usage goes to stderr, so a redirect
captures the vectors alone:

```text
$ cat passages.txt
The Nile flows north.
The Amazon carries the most water.
$ infer embed < passages.txt > vectors.jsonl
usage: 16 input tokens, 16 total tokens
$ wc -l vectors.jsonl
2 vectors.jsonl
```

## Retries and the idle bound

Retry is off by default. A working policy sets all three retry flags, here against the chat
endpoint:

```text
$ infer --retries 3 --retry-base-delay 500ms --retry-max-delay 10s chat "Say hello in five words."
Hello dear friend, have fun.
status: completed
usage: 73 input tokens, 149 output tokens (132 reasoning), 222 total tokens
reasoning items: 1
```

`--retries 3` sends a request at most three times: once, and then two retries. A retry follows a
408, 429, 502, 503, or 504 reply, or a connection that fails before any reply. Before each retry,
infer waits a random time of up to 500ms, doubling for each later retry, and never above 10s. The
same policy can come from `INFER_RETRIES`, `INFER_RETRY_BASE_DELAY`, and `INFER_RETRY_MAX_DELAY`.

`--idle-timeout` bounds a streamed chat: a stream that sends nothing for that long ends with exit
1. Set it well above the time the model takes to start answering:

```text
$ infer chat --idle-timeout 30s "Say hello in five words."
Hello, how are you today?
status: completed
usage: 73 input tokens, 84 output tokens (67 reasoning), 157 total tokens
reasoning items: 1
```

## Exit codes

| code | meaning |
| --- | --- |
| 0 | the command succeeded |
| 1 | the endpoint's error or refusal, a model the fallback can't choose, a reply that isn't completed, a stream idle past `--idle-timeout`, or a file that can't be read |
| 2 | help, or a usage error: a missing or refused setting, no input or prompt, or a request go-ai refuses before sending |

## When a command fails

infer reports a failure once, on stderr, starting with the command, as in `infer chat: `. A usage
error, exit 2, adds the command's usage line. The failures fall into three groups by when they
happen: before anything is sent, from the endpoint, and mid-reply.

### Before anything is sent (exit 2)

These failures are in the command line or the environment. infer refuses them before it sends a
request, and names the flag or variable to change.

#### No base URL

```text
$ infer models
infer models: client: no base URL: set --base-url or INFER_BASE_URL
Usage: infer models [flags]
Run 'infer models --help' for details.
```

Exits 2. No endpoint is set. Export `INFER_BASE_URL` or pass `--base-url`.

#### An unknown format

```text
$ infer --format anthropic models
infer models: format: unknown format "anthropic": --format (INFER_FORMAT) is completions, messages, or responses
Usage: infer models [flags]
Run 'infer models --help' for details.
```

Exits 2. A format names a wire format, not a vendor. Anthropic's is `messages`.

#### The Messages format with no --max-tokens

```text
$ infer --format messages chat "Say hello."
infer chat: --format messages needs --max-tokens: messages: request has no max output tokens, which Messages requires as max_tokens
Usage: infer chat [flags] [<prompt> ...]
Run 'infer chat --help' for details.
```

Exits 2. The Messages format requires a token cap. Add `--max-tokens`, such as
`--max-tokens 512`.

#### --max-tokens below 16

```text
$ infer chat --max-tokens 15 "Say hello."
infer chat: --max-tokens: protocol: request's max output tokens 15 is below the minimum, 16
Usage: infer chat [flags] [<prompt> ...]
Run 'infer chat --help' for details.
```

Exits 2. go-ai sends no cap below 16. Raise `--max-tokens` to 16 or more.

#### An unknown effort

```text
$ infer chat --reasoning extreme "Say hello."
infer chat: --reasoning: protocol: request has unknown reasoning effort "extreme"
Usage: infer chat [flags] [<prompt> ...]
Run 'infer chat --help' for details.
```

Exits 2. Use `none`, `low`, `medium`, `high`, or `xhigh`.

#### A bad --native

```text
$ infer chat --native 'temperature=0.2' "Say hello."
infer chat: --native: protocol: request's native field isn't a JSON object with unique member names
Usage: infer chat [flags] [<prompt> ...]
Run 'infer chat --help' for details.

$ infer chat --native '{"stream": false}' "Say hello."
infer chat: --native: completions: native member stream clashes with a member the request sets
Usage: infer chat [flags] [<prompt> ...]
Run 'infer chat --help' for details.
```

Exits 2. `--native` must be a JSON object, written `'{"temperature": 0.2}'`. It can't set a member
that a typed flag or infer sets. To unstream, use `--stream=false`. For a cap, use
`--max-tokens`.

#### An unknown --mode

```text
$ infer transcribe --mode speech integration/testdata/phrase.wav
infer transcribe: transcription mode: unknown transcription mode "speech": --mode (INFER_TRANSCRIPTION_MODE) is route or chat
Usage: infer transcribe [flags] <file>
Run 'infer transcribe --help' for details.
```

Exits 2. The mode is `route` or `chat`. The same message names `INFER_TRANSCRIPTION_MODE`,
because a bad value there is refused the same way.

#### embed with no input

```text
$ infer embed < /dev/null
infer embed: no input: give each as an argument, or each as a line of stdin
Usage: infer embed [flags] [<input> ...]
Run 'infer embed --help' for details.
```

Exits 2. `embed` got no arguments and an empty stdin. Give each input as an argument, or pipe
the inputs in one per line.

#### A refused INFER_HEADER

```text
$ INFER_HEADER='Authorization: Bearer s3cret-value' infer models
infer models: client: INFER_HEADER: ai: Options.Header names Authorization, which the Client sets
Usage: infer models [flags]
Run 'infer models --help' for details.
```

Exits 2. infer sets `Authorization` itself, from `INFER_TOKEN`. Put the token in `INFER_TOKEN`
and keep `INFER_HEADER` for a header of the endpoint's own, such as `api-key`. The message names
the header but never echoes its value: `s3cret-value` appears nowhere in it. A line with no colon
is refused by its line number alone:

```text
$ INFER_HEADER='api-key s3cret' infer models
infer models: client: INFER_HEADER line 1 has no colon: each line is "Name: value"
Usage: infer models [flags]
Run 'infer models --help' for details.
```

#### --retries -1

```text
$ infer models --retries -1
infer models: client: --retries (INFER_RETRIES): ai: retry policy has a negative attempt count
Usage: infer models [flags]
Run 'infer models --help' for details.
```

Exits 2. `--retries` is a count of attempts, 0 or more. 0 or 1 retries nothing.

#### A base delay with no max delay

```text
$ infer models --retry-base-delay 500ms
infer models: client: --retry-base-delay (INFER_RETRY_BASE_DELAY) needs --retry-max-delay (INFER_RETRY_MAX_DELAY) at least as long
Usage: infer models [flags]
Run 'infer models --help' for details.
```

Exits 2. The max delay caps every wait, and it defaults to 0. Set `--retry-max-delay` at least as
long as the base delay, such as `--retry-max-delay 10s`.

### From the endpoint (exit 1)

These requests are valid, and the endpoint refuses them, or answers in a way that leaves the
command with no model to send to. infer reports the endpoint's reply as it came.

#### A model the endpoint doesn't have

```text
$ infer --model gpt-4o chat "Say hello."
infer chat: ai: generate stream: 404 Not Found: The model `gpt-4o` does not exist. Valid aliases: openai/gpt-oss-120b. (NotFoundError, 404)
```

Exits 1. The model has to be one the endpoint serves. Run `infer models` and pass one of its IDs,
or drop `--model` on an endpoint that serves one model. The wording after `404 Not Found:` is the
endpoint's own.

#### Several models with no --model

No shared endpoint was at hand for this guide, so this output is quoted from infer's unit test,
which asserts it exactly, against a test endpoint that lists `agent` and `vision`:

```text
$ infer chat "hello"
infer chat: several models: agent, vision; pass --model
```

Exits 1. The endpoint lists several models, so infer can't pick one. Pass `--model` or set
`INFER_MODEL` to one of the IDs it names. No chat request is sent.

#### Reasoning none on a model that always reasons

```text
$ infer chat --reasoning none "Say hello."
infer chat: ai: generate stream: 400 Bad Request: Harmony does not support reasoning_effort='none' (parameter=reasoning_effort) (BadRequestError, 400)
```

(output from gpt-oss-120b)

Exits 1. go-ai sends `none` as valid, and the endpoint refuses it: gpt-oss always reasons. Use
`--reasoning low` for the least reasoning, or send `none` to a model that honors it, as the
vision model in [Images](#images) does.

#### Route-mode transcribe on an endpoint with no route

```text
$ infer transcribe integration/testdata/phrase.wav
infer transcribe: ai: transcribe: 404 Not Found: {"detail":"Not Found"}
(the endpoint serves no transcription route; try --mode chat)
```

(the audio endpoint, serving gemma-4-E4B-it)

Exits 1. Route mode, the default, posts to the endpoint's transcription route, and this endpoint
serves none. Add `--mode chat`, as [Audio](#audio) does. infer doesn't switch modes by itself,
because a 404 can also mean a wrong base URL.

#### Chat-mode transcribe through messages

```text
$ infer --format messages transcribe --mode chat integration/testdata/phrase.wav
infer transcribe: --mode chat needs a format that carries audio; --format messages doesn't: ai: transcribe: messages: input 0: content 0: part protocol.InputAudio has no Messages form
```

Exits 1. go-ai refuses before the file is opened or anything is sent: the Messages format has no
audio input. Use `--format completions`, the default, for a chat-mode transcription.

### Mid-reply (exit 1)

These requests were accepted, and the reply then ended before it was whole. Text already written
to stdout is kept.

#### An incomplete reply at the token cap

```text
$ infer chat --max-tokens 16 "Tell me a long story."
The lighthouse at Crow’s End did not warn ships away from rocks; it
status: incomplete
usage: 19 input tokens, 16 output tokens (0 reasoning), 35 total tokens
reasoning items: 0
infer chat: incomplete: max_output_tokens
```

(output from Gemma-4-26B-A4B-NVFP4)

Exits 1. The model reached the cap mid-sentence. The text so far and the summary are written,
and the last line names the status and its reason. Raise `--max-tokens`, or drop it to leave the
cap to the endpoint. A reasoning model can spend all 16 tokens reasoning, so its reply is an
empty line before the same summary:

```text
$ infer chat --max-tokens 16 "Tell me a long story."

status: incomplete
usage: 73 input tokens, 16 output tokens (13 reasoning), 89 total tokens
reasoning items: 1
infer chat: incomplete: max_output_tokens
```

(output from gpt-oss-120b)

#### --idle-timeout too short

```text
$ infer chat --idle-timeout 150ms "Write a paragraph about rivers."
infer chat: ai: generate stream: read stream: unexpected EOF: no bytes within the stream idle timeout (150ms): i/o timeout
```

(output from gpt-oss-120b)

Exits 1. The endpoint accepted the request and opened the stream. Its first bytes then took
longer than 150ms, so the bound ended the stream. Text already written would have been kept, but
none had arrived. The bound covers each wait for the next bytes, the first included, so set it
above the model's time to start answering, such as `--idle-timeout 30s`. On this endpoint 200ms
was enough. A negative value is refused before anything is sent, with exit 2.
