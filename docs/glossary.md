# Glossary

The library's vocabulary, grouped by the layer each term belongs to. The
[concepts](concepts.md) document shows each one in use.

## The model

- **Open Responses**: the open specification go-ai's model follows, spec 2026-04-24, in its
  stateless subset. `protocol` uses its names.
- **Request**: a `protocol.Request`: a model, optional instructions, the input, and the
  settings for tools, reasoning, text, and output length.
- **Response**: a `protocol.Response`: the output items, a status, and the token usage.
- **Item**: one unit of a conversation, in `Input` or `Output`: a message, a function call, a
  function call's output, or reasoning.
- **Content part**: one piece of a message: input text, an image, or audio going in; output text
  or a refusal coming out.
- **Status**: whether a response or item is `completed`, `incomplete`, `failed`, or
  `in_progress`. Only `completed` is a whole reply.
- **Stateless**: every request carries the whole conversation. The endpoint keeps nothing
  between requests.
- **Standard tier**: what the spec defines, the typed fields of a request.
- **Native tier**: an engine's own parameters, sent as raw JSON in a request's `Native` field.
- **Client extension**: a feature the spec doesn't define yet, added under the spec's extension
  rule with go-ai's slug on its wire type, such as `go-ai:input_audio`.

## Formats

- **Wire format**: the adapter between the model and one API's request and reply bodies: Chat
  Completions, Anthropic Messages, or Open Responses.
- **Route**: the path a format posts to under the API root, such as `chat/completions`.
- **Refuse**: fail a request before anything is sent, because the format can't send something
  it needs.
- **Drop**: leave out a hint the format's API has no form for, with no error.
- **Normalize**: reshape a reply into the spec's model, such as deriving item ids an API doesn't
  give.
- **Reasoning envelope**: the prefixed form, such as `go-ai:completions/v1:`, in which a format
  keeps a reasoning item's data in its `EncryptedContent` to send it back.

## The client

- **API root**: the base URL a client is built with, `/v1` included.
- **Composition root**: the place a program builds its dependencies, where the caller picks a
  format by calling its constructor.
- **Token source**: the function `Options.Token` that returns the bearer token for each attempt.
- **Retry policy**: `ai.RetryPolicy`, the caller's opt-in rule for sending a request again: its
  most attempts and its delays.
- **Full jitter**: a backoff that waits a random time between zero and the current cap.
- **Retry-After**: a reply header asking the client to wait. It replaces the backoff, up to
  `MaxDelay`.
- **Stream idle timeout**: the longest a started stream may send nothing before the client ends
  it.
- **Transcription mode**: how `Transcribe` sends a clip. Route mode uses the endpoint's
  transcription route. Chat mode sends the clip as input audio in a chat request.

## Streaming

- **Streaming event**: one of the spec's events in a streamed reply, such as
  `OutputTextDelta`.
- **Terminal event**: the event that ends a stream: `ResponseCompleted`, `ResponseIncomplete`,
  or `ResponseFailed`.
- **Accumulator**: `protocol.Accumulator`, which folds a stream's events into the response an
  unstreamed request returns.
