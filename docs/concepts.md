# Concepts

go-ai has three layers: a request model, the wire formats that carry it, and a client that
sends it. This document shows each one by example. The [glossary](glossary.md) defines the
terms. The [features](features.md) document states every rule.

## The request model

`protocol` is the request and response model. It follows
[Open Responses](https://www.openresponses.org), spec 2026-04-24, in its stateless subset, and
uses the spec's names. A request names a model and carries a conversation as a list of items:

```go
req := &protocol.Request{
	Model:        "openai/gpt-oss-120b",
	Instructions: "Answer in one sentence.",
	Input: []protocol.Item{
		protocol.Message{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{protocol.InputText{Text: "What is a wire format?"}},
		},
	},
	MaxOutputTokens: 512,
}
```

An **item** is one unit of the conversation. Items include a `Message`, a `FunctionCall` the
model made, the `FunctionCallOutput` that answers it, and the model's `Reasoning`. A message
holds **content parts**: `InputText`, `InputImage`, and `InputAudio` going in, and
`OutputText` and `Refusal` coming out.

The response has the same shape. `Output` is a list of items, `Status` says whether the reply
is whole, and `Usage` counts the tokens:

```go
for _, item := range resp.Output {
	switch it := item.(type) {
	case protocol.Message:
		// it.Content holds OutputText or Refusal parts
	case protocol.FunctionCall:
		// it.Name and it.Arguments, to answer with a FunctionCallOutput of it.CallID
	case protocol.Reasoning:
		// the model's reasoning, which the next request sends back
	}
}
```

## Stateless conversations

The endpoint keeps nothing between requests. To continue a conversation, append the reply's
output, and anything new, to `Input`, and send the whole conversation again:

```go
req.Input = append(req.Input, resp.Output...)
req.Input = append(req.Input, protocol.FunctionCallOutput{CallID: call.CallID, Output: result})
resp, err = client.Generate(ctx, req)
```

A reasoning model's reasoning rides along in the `Reasoning` items. Each format stores what it
needs to send the reasoning back in the item's `EncryptedContent`, under a prefix of its own,
such as `go-ai:completions/v1:`. A format drops another format's reasoning on the way out, so a
conversation can move between formats.

## Two tiers

The **standard tier** is what the spec defines: the typed fields of `Request`. The **native
tier** is everything else an engine accepts. It goes in `Native` as a raw JSON object, merged
into the top level of the request body:

```go
req.Native = jsontext.Value(`{"temperature": 0.2, "top_k": 40}`)
```

A native member means whatever the engine makes it mean. go-ai never adds a typed field for an
engine's parameter, and refuses a native member that clashes with one the typed fields set, such
as `model` or `stream`.

## Formats

A **wire format** turns a `protocol.Request` into one API's request body, and that API's reply
back into a `protocol.Response`. go-ai has three:

| Package | API | Route |
| --- | --- | --- |
| `format/completions` | OpenAI Chat Completions | `chat/completions` |
| `format/messages` | Anthropic Messages | `messages` |
| `format/responses` | Open Responses | `responses` |

The caller picks one by calling its constructor and passing it to `ai.New`:

```go
completions.New()
messages.New()
responses.New(responses.Options{})
```

The same request is valid in every format, but not every API can say everything. A format
handles each gap in one of three ways, and its package comment lists which:

- It **refuses** what it can't send and the request needs, before anything is sent. Messages
  refuses audio input and a request with no `MaxOutputTokens`.
- It **drops** a hint its API has no form for. Chat Completions drops a reasoning summary
  request.
- It **normalizes** the reply to the spec's model. Chat Completions gives no item ids, so the
  format derives them.

## The client

`ai.New` joins an API root, the caller's `*http.Client`, and a format into a `*ai.Client`.
Optional settings go in `ai.Options`, whose zero value sets nothing:

```go
client, err := ai.New("http://localhost:8000/v1", httpClient, completions.New(), ai.Options{
	Token: func(context.Context) (string, error) { return os.Getenv("AI_TOKEN"), nil },
	Retry: ai.RetryPolicy{MaxAttempts: 4, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second},
})
```

A nil HTTP client or format is a wiring mistake, so `New` panics and names it. A base URL,
header, or retry policy it can't use is an error.

The client has five operations. `Generate` and `GenerateStream` go through the format.
`Embed`, `Models`, and route-mode `Transcribe` go to the OpenAI-compatible routes
(`embeddings`, `models`, and `audio/transcriptions`) whatever the format, because the engines
serve those routes beside every format's own.

## Streaming

`GenerateStream` yields the spec's **streaming events**. A stream opens with
`ResponseCreated`, carries item, content part, and text events, and ends with one **terminal
event**: `ResponseCompleted`, `ResponseIncomplete`, or `ResponseFailed`.

```go
var acc protocol.Accumulator
for event, err := range client.GenerateStream(ctx, req) {
	if err != nil {
		return err
	}
	if d, ok := event.(protocol.OutputTextDelta); ok {
		fmt.Print(d.Delta)
	}
	if err := acc.Add(event); err != nil {
		return err
	}
}
resp := acc.Response()
```

The **accumulator** folds the events into the `Response` that `Generate` would have returned.
After a break, it holds what arrived so far.

A stream fails in one of two ways:

- **It breaks or never starts.** The loop gets an error, and the stream ends. A refused request,
  an endpoint's error reply, a dropped connection, a cancelled context, and an idle timeout all
  end this way.
- **The endpoint reports a failure inside the stream.** That isn't an error in the loop. The
  stream ends with `ErrorEvent` and then `ResponseFailed`, and the folded response has
  `Status == protocol.StatusFailed`. Check the status as you would for `StatusIncomplete`.

## Client extensions

The spec has no audio input yet. go-ai adds it as a **client extension**, `protocol.InputAudio`,
whose wire type carries go-ai's slug: `go-ai:input_audio`. Chat Completions sends it as its own
`input_audio` part, and Messages refuses it. When the spec adds audio input, go-ai moves to the
standard type, and callers' Go code doesn't change.

## Alternatives the design rejected

- **A vendor SDK.** None covers all three formats, and each brings its vendor's dependencies.
- **A format registry, or a format chosen by name.** The caller constructs the format, so a new
  format is one import at the composition root.
- **Defaults for timeouts, retries, or limits.** These are the application's policy. Every
  option's zero value sets none.
- **A typed field for an engine parameter.** The standard tier stays the spec. An engine's own
  parameter is native, in `Native`.
- **Probing the transcription route and falling back to chat.** A probe costs a request on every
  call and hides a misconfigured endpoint. The caller sets the mode once.
- **An idempotency key on a retried request.** It would be one endpoint's header, not the
  standard's. A caller who sets a retry policy accepts that a POST may run twice.
- **A logger or tracer in the client.** Instrumentation is a `RoundTripper` on the caller's
  `*http.Client`.
