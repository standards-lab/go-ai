# go-ai

A Go client for model endpoints: one request model, sent as Chat Completions, Anthropic
Messages, or Open Responses, on the standard library alone.

`github.com/standards-lab/go-ai` is the base module. Its command-line client, `infer`, is the
nested module [`tools/infer`](tools/infer/README.md), released on its own tags.

## The problem

Model engines such as vLLM and llama.cpp serve a few standard wire formats, and so do the hosted
APIs. A vendor SDK ties a program to one of those formats and brings that vendor's dependencies
with it. Switching engines, or talking to two, means rewriting the calls.

go-ai models a request and its response once, as the stateless subset of the
[Open Responses](https://www.openresponses.org) specification. Each wire format is a small
adapter that encodes that model for one API and decodes the reply. A program builds its
requests once and picks a format by changing one constructor.

## Documentation

1. [Concepts](docs/concepts.md): the request model, formats, the client, and streaming, by
   example.
2. [Quick start](docs/quick-start.md): a working program from `go get` to streaming, tools,
   structured output, embeddings, and transcription, with real output.
3. [Features](docs/features.md): every feature, package by package, in the detail needed to use
   it without reading the source.
4. [Glossary](docs/glossary.md): the vocabulary the library uses.

The package documentation, `go doc github.com/standards-lab/go-ai/...`, is the reference for
each exported name. [`infer`'s README](tools/infer/README.md) covers the CLI.

## How it works

A client joins three things: an endpoint's API root, the caller's `*http.Client`, and a wire
format. A request is a `protocol.Request` whatever the format.

```go
client, err := ai.New("http://localhost:8000/v1", http.DefaultClient, completions.New(), ai.Options{})
if err != nil {
	return err
}

resp, err := client.Generate(ctx, &protocol.Request{
	Model:        "openai/gpt-oss-120b",
	Instructions: "Answer in one sentence.",
	Input: []protocol.Item{protocol.Message{
		Role:    protocol.RoleUser,
		Content: []protocol.ContentPart{protocol.InputText{Text: "What is a wire format?"}},
	}},
})
```

`resp.Output` holds the model's output items: messages of `protocol.OutputText`, reasoning, and
function calls. `resp.Status` says whether the reply is whole, and `resp.Usage` counts its
tokens.

To send the same request to an Open Responses endpoint, change the format:

```go
client, err := ai.New(baseURL, http.DefaultClient, responses.New(responses.Options{}), ai.Options{})
```

The client also streams (`GenerateStream`), embeds (`Embed`), lists models (`Models`), and
transcribes audio (`Transcribe`).

## Conventions

go-ai is an infrastructure library of
[Go Elemental](https://github.com/standards-lab/architecture/blob/main/standards/go-elemental/README.md),
the minimal-dependency Go standard. Five of its conventions are stricter than a reader might
expect:

- **The standard library only.** The base module requires nothing. This line is tighter than
  its tier's, which
  [minimal footprint](https://github.com/standards-lab/architecture/blob/main/principles/minimal-footprint.md)
  allows. The check holds it: a depguard rule allows only the standard library and go-ai's own
  packages in every Go file, and `go.mod` may have no `require`.
- **No policy of its own.** There is no default model, token budget, timeout, retry, size limit,
  or concurrency bound. Every option's zero value sets none. Timeouts live on the caller's
  `*http.Client` and context, and instrumentation is a `RoundTripper` on that client.
- **Open Responses is the standard tier.** `protocol` uses the spec's names. An engine
  parameter the spec doesn't cover goes in a request's `Native` field as raw JSON, never as a
  typed field
  ([service tiers](https://github.com/standards-lab/architecture/blob/main/principles/service-tiers.md)).
- **Formats are chosen by construction.** The caller picks a format by calling its package's
  constructor at the composition root. There is no registry and no lookup by name.
- **Validation comes first.** A request the model or the format can't express is refused before
  the token source is called or anything is sent
  ([validation first](https://github.com/standards-lab/architecture/blob/main/principles/validation-first.md)).

## Packages

- `ai` (the root) is the client: `New` and `Options`, `Generate`, `GenerateStream`, `Embed`,
  `Models`, and `Transcribe`, the `RetryPolicy`, and `APIError`.
- `protocol` is the request and response model, the streaming events, and the `Accumulator`
  that folds a stream into a response.
- `format` is the contract a wire format meets.
- `format/completions`, `format/messages`, and `format/responses` are the Chat Completions,
  Anthropic Messages, and Open Responses formats. Each package comment states what its format
  sends, refuses, drops, and normalizes.
- `integration` is the integration tier, behind the `integration` build tag.

## Development

The repository is a Go workspace of two modules, the base module and `tools/infer`. Tasks run
through [mise](https://mise.jdx.dev):

```sh
mise run check        # build, vet, format, fix, tidy, test, and lint every module; writes nothing
mise run currency     # report requirements, Go, tools, and action pins behind their latest
mise run upgrade      # upgrade every module's go directive and requirements, and the tools, to their latest
mise run integration  # run every module's integration tier against the AI_*_BASE_URL endpoints
```

The unit tests are hermetic. They run each format against replies recorded from llama.cpp
b11669 and vLLM 0.33.0, and the client against a loopback server.

The integration tier runs against live endpoints, one per role. Each role's variable holds an
API root, `/v1` included, with no default. A scenario whose variable is unset skips, and each
endpoint must serve one model.

| Variable | The model it serves | Scenarios |
| --- | --- | --- |
| `AI_AGENT_BASE_URL` | reasoning, function calling, structured output | chat, stream, tools, structured, and reasoning, in every format |
| `AI_VISION_BASE_URL` | reads images | image, in every format |
| `AI_AUDIO_BASE_URL` | reads audio in a chat request | chat-mode `Transcribe`, and the endpoint's refusal of audio sent as Open Responses |
| `AI_EMBEDDING_BASE_URL` | embeddings | `Embed` |
| `AI_TRANSCRIPTION_BASE_URL` | serves the transcription route | route-mode `Transcribe` |

An endpoint that needs credentials takes them from `AI_<ROLE>_TOKEN`, sent as the bearer token,
and `AI_<ROLE>_HEADER`, one `Name: value` per line. No scenario prints either value.

```sh
AI_AGENT_BASE_URL=https://gateway.example/agent/v1 AI_AGENT_HEADER='api-key: …' mise run integration
```

`mise run integration` then runs `infer`'s tier against the same variables. The check builds the
tier but never runs it, and no CI job runs it, because the models are on the organization's own
machines.

## Modules and tags

```
go-ai/              module github.com/standards-lab/go-ai, tagged v<semver>
└── tools/infer/    module github.com/standards-lab/go-ai/tools/infer, tagged tools/infer/v<semver>
```

Each module has its own changelog, [CHANGELOG.md](CHANGELOG.md) and
[tools/infer/CHANGELOG.md](tools/infer/CHANGELOG.md), and a tag makes a GitHub release from it.
The nested module sits outside the base module's zip, so importing go-ai never fetches what
`infer` requires. Both modules stay below 1.0 while the surface settles.

## License

[Apache License 2.0](LICENSE).
