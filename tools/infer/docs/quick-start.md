# Quick start

A tour of `infer`, from install to each command, ending with the common failures and their
fixes. The [README](../README.md) is the reference for every flag, variable, and exit code.

Every output is from a real run. Outputs vary by model, so each step names its model. The
addresses, such as `http://localhost:8000/v1`, stand in for your own. Long output is trimmed,
marked `…`. The commands that send media use go-ai's own test files, so run them from the root
of a go-ai checkout.

## Prerequisites

- [Go](https://go.dev/dl/) 1.27 or later.
- An endpoint that serves Chat Completions with one chat model, such as a
  [vLLM](https://docs.vllm.ai) instance. Steps 7 to 9 also use a vision, an audio, and an
  embedding model.

## 1. Install

```sh
go install github.com/standards-lab/go-ai/tools/infer/cmd/infer@v0.1.0
```

`infer` alone prints its help. This part shows the commands and the two settings that have no
flag:

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
…
Environment:
  INFER_TOKEN    the bearer token sent with each request; unset or empty sends none
  INFER_HEADER   headers sent with each request, one "Name: value" per line, such as a gateway's api-key
```

Each flag in the full help names the variable it overrides, as in `(env INFER_BASE_URL)`.
`infer <command> --help` adds the command's own flags.

## 2. Point at an endpoint

Name the endpoint by its API root, `/v1` included:

```sh
export INFER_BASE_URL=http://localhost:8000/v1
```

or give it for one run with `--base-url`, before or after the command.

infer has no default model. With no `--model`, it lists the endpoint's models and sends to the
one listed. An endpoint that serves one model, such as one vLLM instance, needs no `--model`, and
this guide sets none. A shared endpoint needs `--model`, or `INFER_MODEL`, set to an ID that
`infer models` lists.

An endpoint that needs a credential takes a bearer token from `INFER_TOKEN`, or a header of its
own from `INFER_HEADER`, one `Name: value` per line:

```sh
export INFER_TOKEN=<your token>
export INFER_HEADER='api-key: <your key>'
```

## 3. List the models

```text
$ infer models
openai/gpt-oss-120b
```

## 4. Chat

Steps 4 to 6 use gpt-oss-120b, a reasoning model. `chat` joins its arguments into one prompt and
streams the reply to stdout. Then it writes a summary to stderr:

```text
$ infer chat "What is a wire format? One sentence."
A wire format is the standardized, low‑level binary or textual representation of data as it is transmitted across a network or communication channel.
status: completed
usage: 76 input tokens, 74 output tokens (37 reasoning), 150 total tokens
reasoning items: 1
```

The summary has three lines:

- `status:` is `completed` for a whole reply. Any other status exits 1.
- `usage:` gives input, output, and total tokens. The parenthesis gives the output tokens spent
  reasoning, which are part of the output, not added to it.
- `reasoning items:` counts the reply's reasoning items.

With no arguments, the prompt is stdin. `--instructions` guides the model, and `--stream=false`
waits for the whole reply:

```text
$ echo "Summarize: The quick brown fox jumps over the lazy dog. It does this every morning before breakfast." | infer chat --instructions "Answer in French, in one sentence."
Le renard brun rapide saute par-dessus le chien paresseux chaque matin avant le petit‑déjeuner.
status: completed
usage: 104 input tokens, 119 output tokens (87 reasoning), 223 total tokens
reasoning items: 1
```

## 5. Tune the reply

`--reasoning` sets the effort: `none`, `low`, `medium`, `high`, or `xhigh`. It changes how much the
model spends:

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

`--max-tokens` caps the tokens generated, reasoning included, at 16 or more. A reply that
reaches the cap ends incomplete and exits 1.

```text
$ infer chat --max-tokens 256 "Name three rivers."
Sure! Here are three well‑known rivers:

1. **The Nile** – Flowing northward through northeastern Africa, …
2. **The Amazon** – Running through South America, …
3. **The Yangtze** – The longest river in Asia, …
status: completed
usage: 71 input tokens, 146 output tokens (45 reasoning), 217 total tokens
reasoning items: 1
```

`--native` merges a JSON object into the request, for an engine parameter the flags don't
cover:

```text
$ infer chat --native '{"temperature": 0.2, "top_k": 40}' "Name a color. One word."
Azure
status: completed
usage: 74 input tokens, 45 output tokens (34 reasoning), 119 total tokens
reasoning items: 1
```

## 6. Change the format

`--format`, or `INFER_FORMAT`, picks the wire format: `completions`, the default, `responses`,
or `messages`. The same endpoint answers in each one it serves:

```text
$ infer --format responses chat "Say hello in five words."
Hello there wonderful human friend.
status: completed
usage: 73 input tokens, 157 output tokens (141 reasoning), 230 total tokens
reasoning items: 1
```

Messages requires a token cap, so it needs `--max-tokens`. Its usage doesn't count reasoning
tokens:

```text
$ infer --format messages chat --max-tokens 512 "Say hello in five words."
Hello, dear friend, welcome aboard!
status: completed
usage: 73 input tokens, 153 output tokens (reasoning not reported), 226 total tokens
reasoning items: 1
```

## 7. Send an image

This step uses Gemma-4-26B-A4B-NVFP4, a vision model:

```sh
export INFER_BASE_URL=http://localhost:8001/v1
```

`--image` attaches an image. A file is sent inline, and an `http(s)` URL is sent as given.
Repeat it for several. Images work in every format:

```text
$ infer chat --image integration/testdata/shapes.png "What shapes are in this image? One sentence."
This image contains a red circle, a blue square, and a green triangle.
status: completed
usage: 281 input tokens, 17 output tokens (0 reasoning), 298 total tokens
reasoning items: 0

$ infer --format messages chat --max-tokens 256 --image integration/testdata/shapes.png "How many shapes? Answer with a number."
3
status: completed
usage: 280 input tokens, 2 output tokens (reasoning not reported), 282 total tokens
reasoning items: 0
```

## 8. Transcribe audio

This step uses gemma-4-E4B-it, an audio chat model:

```sh
export INFER_BASE_URL=http://localhost:8002/v1
```

`transcribe` writes the transcript to stdout and the usage to stderr. An audio chat model often
has no transcription route, so `--mode chat` sends the clip in a chat request instead:

```text
$ infer transcribe --mode chat integration/testdata/phrase.wav
The access code is 7429.
usage: 181 input tokens, 11 output tokens, 192 total tokens
```

`INFER_TRANSCRIPTION_MODE=chat` sets the mode for every run.

## 9. Embed

This step uses granite-embedding-english-r2, which returns 768 numbers per vector:

```sh
export INFER_BASE_URL=http://localhost:8003/v1
```

Each argument is one input, and each vector is one line:

```text
$ infer embed "first passage" "second passage"
[0.18457031,-0.6640625,-0.484375,0.46875,0.89453125,…]
[-0.4453125,-0.76953125,-0.56640625,1.1484375,0.5078125,…]
usage: 8 input tokens, 8 total tokens
```

With no arguments, each line of stdin is one input. The usage goes to stderr, so a redirect
captures only the vectors:

```text
$ infer embed < passages.txt > vectors.jsonl
usage: 16 input tokens, 16 total tokens
$ wc -l vectors.jsonl
2 vectors.jsonl
```

## 10. Retry and bound a stream

Retry is off by default. A working policy sets all three flags:

```text
$ infer --retries 3 --retry-base-delay 500ms --retry-max-delay 10s chat "Say hello in five words."
Hello dear friend, have fun.
status: completed
usage: 73 input tokens, 149 output tokens (132 reasoning), 222 total tokens
reasoning items: 1
```

That sends each request at most three times. Before each retry, infer waits a random time up to
500ms, doubling each time, never above 10s.

`--idle-timeout` ends a streamed chat that sends nothing for that long. Set it well above the
model's time to start answering, such as `--idle-timeout 30s`.

## Troubleshooting

infer reports a failure once on stderr, starting with the command. A usage error also prints the
command's usage line, left out below.

### Refused before sending (exit 2)

| Message | Fix |
| --- | --- |
| `infer models: client: no base URL: set --base-url or INFER_BASE_URL` | Export `INFER_BASE_URL` or pass `--base-url`. |
| `format: unknown format "anthropic": --format (INFER_FORMAT) is completions, messages, or responses` | A format names a wire format, not a vendor. Anthropic's is `messages`. |
| `--format messages needs --max-tokens: messages: request has no max output tokens, …` | Add `--max-tokens`, such as `--max-tokens 512`. |
| `--max-tokens: protocol: request's max output tokens 15 is below the minimum, 16` | Use 16 or more. |
| `--reasoning: protocol: request has unknown reasoning effort "extreme"` | Use `none`, `low`, `medium`, `high`, or `xhigh`. |
| `--native: protocol: request's native field isn't a JSON object with unique member names` | Write a JSON object, such as `'{"temperature": 0.2}'`. |
| `--native: completions: native member stream clashes with a member the request sets` | Use the flag instead: `--stream=false`, `--max-tokens`. |
| `transcription mode: unknown transcription mode "speech": …` | Use `route` or `chat`. |
| `infer embed: no input: give each as an argument, or each as a line of stdin` | Pass inputs as arguments, or pipe them in. |
| `client: INFER_HEADER: ai: Options.Header names Authorization, which the Client sets` | Put the token in `INFER_TOKEN`. Keep `INFER_HEADER` for headers such as `api-key`. The value is never echoed. |
| `client: INFER_HEADER line 1 has no colon: each line is "Name: value"` | Write each header as `Name: value`. |
| `client: --retries (INFER_RETRIES): ai: retry policy has a negative attempt count` | Use 0 or more. |
| `client: --retry-base-delay (INFER_RETRY_BASE_DELAY) needs --retry-max-delay (INFER_RETRY_MAX_DELAY) at least as long` | Set `--retry-max-delay`, such as `10s`. |

### Refused by the endpoint (exit 1)

The request was valid, and the endpoint refused it. The wording after the status is the
endpoint's own.

```text
$ infer --model gpt-4o chat "Say hello."
infer chat: ai: generate stream: 404 Not Found: The model `gpt-4o` does not exist. Valid aliases: openai/gpt-oss-120b. (NotFoundError, 404)
```

Run `infer models` and pass one of its IDs, or drop `--model` on a one-model endpoint.

```text
$ infer chat "hello"
infer chat: several models: agent, vision; pass --model
```

The endpoint lists several models. Pass `--model` or set `INFER_MODEL`. This output is quoted
from infer's unit test, which asserts it exactly.

```text
$ infer chat --reasoning none "Say hello."
infer chat: ai: generate stream: 400 Bad Request: Harmony does not support reasoning_effort='none' (parameter=reasoning_effort) (BadRequestError, 400)
```

gpt-oss always reasons, so it refuses `none`. Use `--reasoning low`, or send `none` to a model
that honors it, such as the vision model in step 7.

```text
$ infer transcribe integration/testdata/phrase.wav
infer transcribe: ai: transcribe: 404 Not Found: {"detail":"Not Found"}
(the endpoint serves no transcription route; try --mode chat)
```

The endpoint has no transcription route. Add `--mode chat`. infer doesn't switch on its own,
because a 404 can also mean a wrong base URL. The same 404 from every command means the base
URL is missing `/v1`.

```text
$ infer --format messages transcribe --mode chat integration/testdata/phrase.wav
infer transcribe: --mode chat needs a format that carries audio; --format messages doesn't: …
```

Messages has no audio input. Use `--format completions`, the default.

### Cut off mid-reply (exit 1)

The endpoint accepted the request, and the reply ended before it was whole. Text already
written is kept.

```text
$ infer chat --max-tokens 16 "Tell me a long story."
The lighthouse at Crow’s End did not warn ships away from rocks; it
status: incomplete
usage: 19 input tokens, 16 output tokens (0 reasoning), 35 total tokens
reasoning items: 0
infer chat: incomplete: max_output_tokens
```

The reply hit the token cap. Raise `--max-tokens`, or drop it. A reasoning model can spend the
whole cap reasoning and write nothing at all.

```text
$ infer chat --idle-timeout 150ms "Write a paragraph about rivers."
infer chat: ai: generate stream: read stream: unexpected EOF: no bytes within the stream idle timeout (150ms): i/o timeout
```

The model took longer than 150ms to start. The bound covers each wait for bytes, the first
included, so set it above the model's time to start answering, such as `--idle-timeout 30s`.
