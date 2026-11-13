# go-ai

go-ai is the model client infrastructure library of Go Elemental, the Standards Lab
organization's Go implementation of the Elemental Architecture. It is one client over the wire
formats the model engines serve natively, Chat Completions, Anthropic Messages, and Open
Responses, with generation, streaming, embeddings, transcription, and the model listing, and no
vendor SDK.

`github.com/standards-lab/go-ai` is the base module. Its convenience CLI, `infer`, comes as the
nested module `tools/infer`, released on its own tags.

## Standard

`go-ai` is an infrastructure library of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md),
the minimal-dependency Go standard. This README and each package's `doc.go` document the
repository; the standard's principles it enhances are stated below. Its repository-level
principles:

- The base module depends on the standard library alone, a line tighter than its tier's, which
  [minimal footprint](https://github.com/standards-lab/architecture/blob/main/principles/minimal-footprint.md)
  allows. The check holds it there in two steps: a strict depguard rule allows only the standard
  library and the module's own packages in every Go file, tests and the integration tier
  included, and the check asserts that `go.mod` has no `require`. Moving the line means changing
  the check.
- The standard tier is an external standard: `protocol` is Open Responses' item model, spec
  2026-04-24, in its stateless subset, under the spec's names. Every wire format maps to and from
  it, and an engine parameter the standard doesn't cover goes in a request's `Native`, raw JSON,
  never a typed field
  ([service tiers](https://github.com/standards-lab/architecture/blob/main/principles/service-tiers.md)).
- A wire format is chosen by its package's typed constructor at the caller's composition root,
  never by a registry or by name, and each format is a package of the base module, as none pins
  a vendor library.
- The library ships no policy the application must own: no default model, token budget,
  timeout, retry, size limit, or concurrency bound. Every option's zero value sets none; the
  caller's `*http.Client` and context carry the timeouts, and instrumentation is a
  `RoundTripper` on that client.
- A request the model or the format can't express is refused before the token source is called
  or anything is sent
  ([validation first](https://github.com/standards-lab/architecture/blob/main/principles/validation-first.md)).

## Packages

- `ai` (the root package): `New` and `Options`, the `Client` with `Generate`, `GenerateStream`,
  `Embed`, `Models`, and `Transcribe`, the `RetryPolicy`, and `APIError`.
- `protocol`: the request and response model, the streaming events, and the `Accumulator` that
  folds a stream.
- `format`: the contract a wire format meets.
- `format/completions`, `format/messages`, and `format/responses`: Chat Completions, Anthropic
  Messages, and Open Responses. Each package comment states what its format sends, refuses,
  drops, and normalizes.
- `integration`: the integration tier, behind the `integration` build tag.

## Usage

### A client

`New` takes the endpoint's API root, the caller's `*http.Client`, and a wire format, then
`Options`, whose zero value selects every default. A nil HTTP client or format panics by name;
an unusable base URL, retry policy, header, or transcription mode is an error.

```go
client, err := ai.New("http://localhost:8000/v1", httpClient, completions.New(), ai.Options{
	Token: func(context.Context) (string, error) { return os.Getenv("AI_TOKEN"), nil },
	Retry: ai.RetryPolicy{
		MaxAttempts: 4,
		BaseDelay:   500 * time.Millisecond,
		MaxDelay:    30 * time.Second,
	},
	StreamIdleTimeout: 2 * time.Minute,
})
```

`Token` is called once per attempt and sent as the bearer token; `Header` adds fixed headers,
such as Anthropic's `x-api-key`; `Query` is added to every URL. `Retry` sends a request again on a
408, 429, 502, 503, or 504 reply, or a connection failure before any reply, after a full-jitter
backoff that a Retry-After replaces; a retried POST may run twice on the endpoint.
`StreamIdleTimeout` bounds each read of a started stream.

### Generate

```go
req := &protocol.Request{
	Model:        "gpt-oss-120b",
	Instructions: "Answer in one sentence.",
	Input: []protocol.Item{protocol.Message{
		Role:    protocol.RoleUser,
		Content: []protocol.ContentPart{protocol.InputText{Text: "What is a wire format?"}},
	}},
	MaxOutputTokens: 512,
}
resp, err := client.Generate(ctx, req)
var apiErr *ai.APIError
if errors.As(err, &apiErr) {
	// apiErr.Status, apiErr.Message, and apiErr.RetryAfter, the wait the reply asked for
}
for _, item := range resp.Output {
	if m, ok := item.(protocol.Message); ok {
		for _, part := range m.Content {
			if t, ok := part.(protocol.OutputText); ok {
				fmt.Println(t.Text)
			}
		}
	}
}
```

### GenerateStream with the accumulator

`GenerateStream` is lazy: ranging it sends the request, and the events are read in the caller's
loop, with no goroutine. Cancelling the context closes the connection. An `Accumulator` folds the
events into the `Response` that `Generate` would have returned, and after a break it holds the
output made so far.

```go
var acc protocol.Accumulator
for event, err := range client.GenerateStream(ctx, req) {
	if err != nil {
		return err // acc.Response() holds the partial output
	}
	if d, ok := event.(protocol.OutputTextDelta); ok {
		fmt.Print(d.Delta)
	}
	if err := acc.Add(event); err != nil {
		return err
	}
}
final := acc.Response()
```

A failure the endpoint reports inside the stream is no error: the stream ends with
`protocol.ErrorEvent` and `protocol.ResponseFailed`, so the caller checks `final.Status`.

### Embed

`Embed` sends the whole batch as one request to the endpoint's OpenAI-compatible embeddings
route, whatever the client's format, and places each vector by its input's index. How large a
batch is, and how many run at once, are the caller's.

```go
emb, err := client.Embed(ctx, &ai.EmbeddingRequest{
	Model: "granite-embedding-english-r2",
	Input: []string{"first passage", "second passage"},
})
// emb.Vectors[i] is Input[i]'s; emb.Usage.PromptTokens counts the batch
```

### Models

`Models` GETs the endpoint's OpenAI-compatible models route, through the same token, header,
query, and retry path.

```go
models, err := client.Models(ctx)
for _, m := range models {
	fmt.Println(m.ID)
}
```

### Transcribe

`Transcribe` calls `Open` once per attempt, so a retried transcription opens the clip again. By
default, `TranscribeRoute`, it streams the clip to the endpoint's OpenAI-compatible
`audio/transcriptions` route, whatever the client's format, never holding it whole.

```go
tr, err := client.Transcribe(ctx, &ai.TranscriptionRequest{
	Model:    "whisper-large-v3",
	Open:     func(context.Context) (io.ReadCloser, error) { return os.Open("clip.wav") },
	FileName: "clip.wav",
	Language: "en",
})
fmt.Println(tr.Text)
```

Chat mode, `TranscribeChat`, is for an endpoint whose audio model has no transcription route. It
reads the clip whole and sends one request in the client's own format: the clip as input audio,
its format from `FileName`'s extension (.wav or .mp3), beside an instruction, `Prompt` or else
`ai.TranscriptionInstruction(Language)`. It needs a format that carries audio, as Chat Completions
does; a format that refuses audio, such as Messages, is refused before anything is sent.
`Language` is a name, such as "English", which the instruction reads as given.

Like the route, chat mode returns a whole transcript or an error. A reply the model didn't
complete, such as one stopped at the engine's output token bound, returns an
`*ai.IncompleteTranscriptError` and no transcript, and isn't retried, since the same clip meets
the same bound again.

```go
audio, err := ai.New("http://localhost:8002/v1", httpClient, completions.New(), ai.Options{
	TranscriptionMode: ai.TranscribeChat,
})
tr, err := audio.Transcribe(ctx, &ai.TranscriptionRequest{
	Model:    "gemma-4-e4b",
	Open:     func(context.Context) (io.ReadCloser, error) { return os.Open("clip.wav") },
	FileName: "clip.wav",
	Language: "English",
})
var incomplete *ai.IncompleteTranscriptError
if errors.As(err, &incomplete) {
	// incomplete.Status and incomplete.Reason, such as protocol.ReasonMaxOutputTokens
}
// otherwise tr.Text is the reply's output text, and tr.Usage its usage
```

## infer

`infer` is go-ai's command-line client, in the nested module `tools/infer`, built on the base
module's released surface alone. Each run talks to the endpoint `--base-url` or `INFER_BASE_URL`
names, in the format `--format` or `INFER_FORMAT` names, `completions` by default, with the
bearer token from `INFER_TOKEN`. With no `--model` set, it sends to the one model the endpoint
lists.

```sh
go install github.com/standards-lab/go-ai/tools/infer/cmd/infer@v0.1.0
export INFER_BASE_URL=http://localhost:8000/v1

infer models                                  # each model ID, one per line
infer embed "first passage" "second passage"  # one JSON vector per input, one per line
infer chat --reasoning low "Is 1009 prime?"   # the reply streamed, its summary on stderr
infer transcribe --mode chat clip.wav         # the transcript, sent over chat
```

[tools/infer/README.md](tools/infer/README.md) documents every flag, variable, and exit code.

## Development

The repository uses [mise](https://mise.jdx.dev):

```
mise run check        # build, vet, format, fix, tidy, test, and lint every module; writes nothing
mise run currency     # report requirements, Go, tools, and action pins behind their latest
mise run upgrade      # upgrade every module's go directive and requirements, and the tools, to their latest
mise run integration  # run the integration tier against the endpoints the AI_*_BASE_URL variables name
```

The unit tests are hermetic: they run each format against replies recorded from llama.cpp b11669
and vLLM 0.33.0, and the client against a loopback server.

### The integration tier

The integration tier drives the exported API against live endpoints, one per role, each named by
a variable holding the endpoint's API root, including `/v1`, with no default:

- `AI_AGENT_BASE_URL`: a reasoning model with function calling and structured output, for the
  chat, stream, tools, structured, and reasoning scenarios in every format.
- `AI_VISION_BASE_URL`: a model that reads images, for the image scenario in every format.
- `AI_AUDIO_BASE_URL`: a model that reads audio in a chat request, for chat-mode `Transcribe`.
- `AI_EMBEDDING_BASE_URL`: an embedding model, for `Embed`.
- `AI_TRANSCRIPTION_BASE_URL`: an endpoint that serves the transcription route, for route-mode
  `Transcribe`.

`mise run integration` runs the tier verbose, and then infer's, which runs the built `infer`
against the same variables. A scenario whose variable is unset skips, naming it, so with none
set the run skips whole. Each scenario sends to the one model its endpoint's
`/models` lists. The check builds, vets, and lints the tier under its tag but never runs it, and
no CI job runs it, as the models are on the organization's own machines.

## Modules and tags

```
go-ai/              module github.com/standards-lab/go-ai, tagged v<semver>
└── tools/infer/    module github.com/standards-lab/go-ai/tools/infer, tagged tools/infer/v<semver>
```

The base module is released on `v<semver>` tags, from `v0.1.0`, with its changes in
[CHANGELOG.md](CHANGELOG.md). `infer` comes as the nested module `tools/infer`, released on
`tools/infer/v<semver>` tags, from `tools/infer/v0.1.0`, with its changes in
[tools/infer/CHANGELOG.md](tools/infer/CHANGELOG.md); a nested module's directory is outside the
base module's zip, so importing go-ai never fetches what `infer` requires. A tag of either form
makes a GitHub release from its module's changelog. Both modules stay below 1.0 while the surface
settles.

## License

[Apache License 2.0](LICENSE).
