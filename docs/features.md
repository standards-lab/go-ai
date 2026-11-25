# Features

Every feature, package by package, in the detail needed to use it without reading the source.
The package documentation (`go doc`) is the reference for each name's exact signature.

The packages form layers that import one way. `protocol` is the model. `format` is the
contract a wire format meets, and each format is a package below it. The root package, `ai`, is
the client, which sends a `protocol.Request` in the format it was given.

## ai: the client

### Construction

```go
func New(baseURL string, httpClient *http.Client, wireFormat format.Format, opts Options) (*Client, error)
```

- `baseURL` is the endpoint's API root, `/v1` included, such as `http://localhost:8000/v1`. It
  must be an absolute `http` or `https` URL with no query or fragment; `Options.Query` holds the
  query. Each operation appends its route.
- `httpClient` sends every request. Its transport carries the timeouts and any
  instrumentation.
- `wireFormat` is a format's constructor result, such as `completions.New(completions.Options{})`.
  A format's `Options`, its profile, binds it to one deployment, so a program uses one client per
  deployment and shares the `*http.Client` among them.
- A nil `httpClient` or `wireFormat` panics, naming the argument. An unusable base URL, retry
  policy, header, or transcription mode returns an error.

A `Client` is safe for concurrent use when its token source and format are. It sets no limit of
its own.

### Options

Every field's zero value sets nothing.

| Field | What it does |
| --- | --- |
| `Query` | Query parameters added to every URL, such as a cloud endpoint's `api-version`. |
| `Token` | Called before each attempt. A non-empty result is sent as `Authorization: Bearer`. |
| `Header` | Fixed headers sent with every request, such as Anthropic's `x-api-key` or a gateway's key. `New` refuses `Authorization`, `Content-Type`, and `Accept`, which the client sets. |
| `Retry` | The retry policy. The zero value retries nothing. |
| `StreamIdleTimeout` | Ends a started stream when a read gets no bytes for this long. Any bytes reset it, keep-alive comments included. It also bounds the wait for the first bytes, so a reasoning model needs a bound above its time to first token. |
| `TranscriptionMode` | `TranscribeRoute`, the default, or `TranscribeChat`. |

### Retries

`RetryPolicy` has three fields:

- `MaxAttempts` is the most requests sent for one call, the first included. 0 and 1 retry
  nothing.
- `BaseDelay` caps the wait before the second attempt. The cap doubles for each attempt after
  it.
- `MaxDelay` caps every wait, and is the longest Retry-After the client honors.

A request is retried on a 408, 429, 502, 503, or 504 reply, or a connection that fails before
any reply arrives. Nothing else is retried: not a 500, not a cancelled context, not a token
source error, not a `ResponseHeaderTimeout` (the endpoint may still be working), and never a
stream after its reply has started.

Before each retry the client waits a random time between zero and the current cap (full
jitter). A reply's Retry-After, in seconds or as an HTTP date, replaces that wait. When the wait
would be longer than `MaxDelay`, or would end past the context's deadline, the client stops and
returns the last error at once. Its `APIError.RetryAfter` holds the wait, for a caller that
redelivers later.

Every operation but `Models` is a POST. A retried POST may run twice on the endpoint, and be
billed twice. go-ai sends no idempotency key.

### Generate and GenerateStream

`Generate` sends a request and returns the whole `*protocol.Response`.

`GenerateStream` returns an `iter.Seq2[protocol.Event, error]`. Nothing is validated or sent
until the sequence is ranged, and each range sends a new request. Events are read inside the
caller's loop, with no goroutine. Breaking out of the loop closes the connection.

Each pair holds an event or an error, never both. The events run from `ResponseCreated` to one
terminal event. An error is always the last pair:

- A refused request, a token error, a transport failure, or a non-2xx reply is the only pair.
- A cancelled context returns the context's error.
- A stream that ends before its terminal event, or a read that fails, matches
  `io.ErrUnexpectedEOF`.
- An idle timeout matches both `io.ErrUnexpectedEOF` and `os.ErrDeadlineExceeded`.

A failure the endpoint reports inside the stream is not an error pair. The stream ends with
`protocol.ErrorEvent` and `protocol.ResponseFailed`, and the folded response's status is
`StatusFailed`.

### Embed

`Embed` sends an `EmbeddingRequest` to the `embeddings` route, whatever the client's format:

- `Model` and at least one `Input` are required. The whole input is one request. Batch size and
  concurrency are the caller's.
- `Native` is merged into the body, for a parameter such as `dimensions`. It may not set
  `model`, `input`, or `encoding_format`.
- `InputType`, `InputQuery` or `InputDocument`, is sent in the convention the client's
  `Options.Embedding` profile declares for it: a prefix before every input, native members, or
  both (see [concepts](concepts.md#embedding-profiles)). The zero type, and a type the profile
  declares nothing for, send the input as given.
- The reply's `Vectors[i]` is the embedding of `Input[i]`. `Usage` has `PromptTokens` and
  `TotalTokens`, the endpoint's names.

### Models

`Models` GETs the `models` route and returns each model's `ID`, in the endpoint's order, with
its list entry as the endpoint sent it in `Native`, for members such as vLLM's `root`. It
goes through the same token, headers, query, and retry as every other call. A GET is safe to
repeat, so its retry runs nothing twice.

### Transcribe

`Transcribe` sends a `TranscriptionRequest` and returns a `Transcription`, its `Text` and its
`Usage`:

- `Model`, `Open`, and `FileName` are required. `Open` is called once per attempt, so a retry
  reads the clip again.
- `Language` and `Prompt` are optional.
- `Native` (form fields, route mode only) carries a parameter such as `temperature`.

The client's `TranscriptionMode` picks how it sends:

| | `TranscribeRoute` (default) | `TranscribeChat` |
| --- | --- | --- |
| Sends to | the `audio/transcriptions` route, whatever the format | the format's chat route, as input audio |
| The clip | streamed as a file upload, never held whole | read whole into the request |
| `Language` | the route's `language` field, such as `en` | named in the instruction, such as `English` |
| `Prompt` | the route's `prompt` field | the instruction, in place of `TranscriptionInstruction(Language)` |
| Needs | an endpoint with the route | a format that carries audio (Chat Completions); `.wav` or `.mp3` in `FileName` |

`Client.TranscriptionMode` returns the mode the client was built with. A `TranscriptionMode`
reads and writes as text, `route` or `chat`, through `String`, `MarshalText`, and
`UnmarshalText`, which refuses any other name, so a configuration can name the mode.

An endpoint without the transcription route answers 404. go-ai never falls back to chat mode on
its own: a 404 can also mean a wrong base URL.

In chat mode, a reply that isn't complete returns an `*ai.IncompleteTranscriptError` with its
`Status` and `Reason`, and no transcript. It isn't retried, since the same clip meets the same
bound again.

### Errors

Every error a call returns is prefixed with its operation, such as `ai: generate:`, and wraps
its cause. Three causes are typed, matched with `errors.As`:

- `*ai.APIError` is any non-2xx reply. It carries `Status`, the endpoint's `Message`, `Type`,
  and `Code`, the raw `Body`, and `RetryAfter`, the wait the reply asked for.
- `*ai.IncompleteTranscriptError` is chat-mode transcription's incomplete reply.
- `*format.UnsupportedError` is a format's refusal of a request feature, before anything is
  sent: its `Format`, `Feature`, and `Value`, and `Declared`, true when the gap is one the
  format's profile declares and false when it is the spec's.

## protocol: the model

### Requests

| Field | What it holds |
| --- | --- |
| `Model` | The model's ID. Required. |
| `Instructions` | Guidance ahead of the input. |
| `Input` | The conversation, oldest item first. At least one item. |
| `Tools` | `FunctionTool`s the model may call: a name, a description, a JSON Schema of the arguments, and `Strict`. |
| `ToolChoice` | `ToolChoiceAuto`, `ToolChoiceNone`, `ToolChoiceRequired`, a `FunctionToolChoice` naming one function, or an `AllowedToolChoice` narrowing the set. Nil leaves it to the endpoint. |
| `Reasoning` | `Effort` (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`) and `Summary`. Empty leaves both to the endpoint. A format refuses an effort its spec lacks. |
| `Text` | `Format`, plain text or a `JSONSchemaFormat`, and `Verbosity`. |
| `MaxOutputTokens` | The most tokens generated, reasoning included. Zero leaves it to the endpoint, and a negative bound is refused. The least positive bound is each format's. |
| `ParallelToolCalls` | Whether the model may call several tools in one turn. Nil leaves it to the endpoint. |
| `Native` | A raw JSON object merged into the body's top level, for engine parameters the spec doesn't cover. |

`Request.Validate` refuses a request the model can't describe. The client calls it before
anything is sent.

### Items and content parts

| Item | Holds |
| --- | --- |
| `Message` | A `Role` (`user`, `system`, `developer`, `assistant`) and content parts. |
| `FunctionCall` | The model's call: `CallID`, `Name`, and `Arguments` as JSON text. |
| `FunctionCallOutput` | The result for a `CallID`, as a string or content parts. |
| `Reasoning` | The model's reasoning: `Content`, `Summary`, and the format's `EncryptedContent`. |

| Content part | Holds |
| --- | --- |
| `InputText` | Text. |
| `InputImage` | An image by URL, or inline as a data URL that `protocol.DataURL` builds, with an `ImageDetail`. |
| `InputAudio` | A clip's bytes and its `AudioFormat`, `wav` or `mp3`. A client extension. |
| `OutputText` | The model's text. |
| `Refusal` | The model's refusal. |

### Responses

A `Response` holds the reply's `ID` and the `Model` that answered, as the endpoint reports them,
`Output`, a `Status` (`completed`, `incomplete`, `failed`, or `in_progress`),
`IncompleteDetails` with a `Reason` (`max_output_tokens`, `content_filter`, or
`go-ai:context_window`, a client extension for a reply cut off by the model's context window),
an `Error` for a failed response, and `Usage`: `InputTokens`, `OutputTokens`, `TotalTokens`,
`InputTokensDetails.CachedTokens`, and `OutputTokensDetails.ReasoningTokens`.

`Response.OutputText` returns the response's text: every `OutputText` part of every message in
`Output`, in order, joined with nothing between. Refusals, reasoning, and function calls add
nothing.

### Streaming events and the accumulator

The events are the spec's, in a subset: the state events (`ResponseCreated` and the three
terminal events), `ErrorEvent`, the item events, the content part events, the text, refusal, and
reasoning deltas and their done events, and the function-call argument events.

`Accumulator.Add` folds the next event, and refuses one that doesn't follow the events before
it. `Accumulator.Response` returns the response folded so far.

## The formats

Each format is a package with a `New` constructor, which takes the format's `Options`. Its
package comment lists exactly what it sends, refuses, drops, and normalizes. In short:

| | `completions` | `messages` | `responses` |
| --- | --- | --- | --- |
| API | OpenAI Chat Completions | Anthropic Messages, version 2026-10-21 | Open Responses, spec 2026-04-24 |
| Route | `chat/completions` | `messages` | `responses` |
| Instructions | a leading system message | the top-level `system`, with the system and developer messages that lead the input; a later one is refused | `instructions` |
| `MaxOutputTokens` | `max_completion_tokens`, any positive value | `max_tokens`, required | `max_output_tokens`, at least 16 (`responses.MinMaxOutputTokens`) |
| Reasoning effort | all seven as `reasoning_effort` | `output_config.effort`; `minimal` refused | `reasoning.effort`; `minimal` and `max` refused |
| `ParallelToolCalls` | `parallel_tool_calls` | false as `tool_choice`'s `disable_parallel_tool_use` | `parallel_tool_calls` |
| Audio input | its own `input_audio` part | refused | the `go-ai:input_audio` extension |
| Hints dropped | a reasoning summary request | an image's detail, a reasoning summary's detail and a summary without adaptive thinking, `ParallelToolCalls` false where no tool can be called, verbosity, and a JSON schema's name, description, and strict flag | nothing |
| Item ids | derived from the reply's id | derived from the reply's id | the server's |
| `Options` | `RefusedEfforts`, `NoToolChoiceRequired`, `NoAllowedTools` | `RefusedEfforts` | `RefusedEfforts`, `NoAllowedTools`, `ResendReasoningText` |

Every Open Responses request is stateless. It sends `store: false`, and a `Native`
`previous_response_id` or `background` is refused.

A format refuses a request before anything is sent or any token is fetched, and each refusal
is a `*format.UnsupportedError`, matched with `errors.As`, naming the `Format`, the `Feature`
refused, and its `Value`, and whether the gap is the profile's (`Declared`).

### Profiles

A deployment that departs from its format's spec declares its gaps in the format's `Options`,
its profile. The zero value is the spec, and declares none. A declared gap is handled before
anything is sent or any token is fetched: it is refused as a `*format.UnsupportedError` with
`Declared` true, or, for `allowed_tools`, narrowed to the allowed tools.

| Field | Formats | What a request that needs the gap gets |
| --- | --- | --- |
| `RefusedEfforts` (`refused_efforts`) | all three | An effort in the list is refused, `Feature` `reasoning effort` and `Value` the effort. An effort the format's spec lacks stays the spec's refusal, `Declared` false, listed or not. A request of no effort is never refused. |
| `NoToolChoiceRequired` (`no_tool_choice_required`) | `completions` | `tool_choice` `required` is refused, `Feature` `tool choice` and `Value` `required`, and so is an `allowed_tools` choice of mode `required`, sent or narrowed. |
| `NoAllowedTools` (`no_allowed_tools`) | `completions`, `responses` | An `allowed_tools` choice is narrowed: only its tools go out, in the request's order, and its mode goes out as a plain tool choice. A mode the spec has no form for stays the spec's refusal. |
| `ResendReasoningText` (`resend_reasoning_text`) | `responses` | A reasoning item goes back with its reasoning text as content, for an endpoint that gives no encrypted content, such as llama.cpp's router or vLLM 0.34.0. |

Each `Options` decodes from JSON under the snake_case names in the table, and only those. Decode
it strictly, with `encoding/json/v2`'s `json.RejectUnknownMembers(true)`, so that an unknown
member is an error. A `refused_efforts` entry that isn't one of the seven efforts is always an
error: `protocol.ReasoningEffort` decodes only those. go-ai ships no loader and no presets; the
[concepts](concepts.md#profiles) show a profile for each format.

A profile binds a format to one deployment. A program that talks to several builds one client
per deployment, each with its own format and profile, and shares one `*http.Client` among them.

### Streams and error bodies

All three stream their replies as server-sent events, `format.SSE`. All three decode an
OpenAI-style error body, and `messages` also decodes Anthropic's.

## format: the contract

`format.Format` is the interface a format meets: its route, request encoding, reply decoding,
its event stream, stream decoding, and error decoding. A `format.EventStream` is the framing a
streamed reply arrives in: the media type the client asks for in its Accept header, and a reader
of the reply's `WireEvent`s, each the framing's `Type` and its `Data`. The client reads a stream
only through it, never by the reply's Content-Type, and hands each event to the format's
`StreamDecoder`; `format.SSE` is the server-sent event stream. `format.UnsupportedError` is a
format's refusal. `format.DecodeOpenAIError` decodes the OpenAI-style error body most endpoints
return. A new format is a package that implements the interface, and a caller uses it by passing
it to `ai.New`.

## aitest: a fake endpoint for tests

`aitest.Serve` starts a loopback OpenAI-compatible endpoint for a test, closed when the test
ends. It records every request, in order, in `Endpoint.Requests`, and answers each route with the
function `Routes` gives: Chat Completions, embeddings, transcriptions, and models, each given its
request decoded and numbered by arrival, and any other path below `/v1`, such as `messages` or
`responses`, raw. A route without a function answers 404 with an OpenAI-style error body.

A route function answers with a typed reply in its route's wire shape (`ChatReply`, streamed
when the request asks, `EmbeddingsReply`, `TranscriptionReply`, `ModelsReply`), an `ErrorReply`
of any status with an optional Retry-After, or a `RawReply` of a JSON body or server-sent event
`Frame`s, written as given. A `Gate` parks requests inside route functions, and holds a
streamed reply after a given piece, until the test releases them, to prove a caller's
concurrency bound or cancellation. The package imports only the standard library, so any
module's tests can use it.

## integration: the live tier

The integration tier drives the exported API against live endpoints, behind the `integration`
build tag. The [README](../README.md#development) lists its roles and variables.
