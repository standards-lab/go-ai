// Package ai is go-ai's model client: one transport over the protocol, in the wire format its
// constructor is given, with Go primitives over the inference engines' native APIs and no
// vendor SDK.
//
// The module is four layers, importing one way. Package protocol is the model, Open Responses'
// item model in its stateless subset; package format is the contract a wire format meets; each
// wire format is a package below it, named for the endpoint it targets, format/completions,
// format/messages, and format/responses; and this package is the transport, which sends a
// protocol.Request in the format chosen by its constructor at the caller's composition root.
//
// The library sets no policy of its own: no default model, token budget, timeout, retry, size
// limit, or concurrency bound. The caller's http.Client and context carry the timeouts, and
// every option New takes is the caller's, its zero value setting none: the retry policy, and
// the idle bound on a started stream. The Client logs nothing; instrumentation is a
// RoundTripper on the caller's http.Client.
//
// Embed, Models, and, in its default mode, Transcribe go to the base URL's OpenAI-compatible
// routes, embeddings, models, and audio/transcriptions, on a Client of any format: llama.cpp's
// router serves all three beside every format's route, and an endpoint without the route
// answers with an *APIError. Transcribe's chat mode instead sends the clip as a chat request in
// the Client's format, which must carry audio, as Chat Completions does, for an endpoint whose
// audio model has no transcription route.
//
// go-ai's README (https://github.com/standards-lab/go-ai#readme) states the module's place in Go
// Elemental, its dependency line, and its API by example.
//
// Its exported identifiers:
//
//   - [New] returns a Client of an endpoint's base URL, sending with an http.Client in a wire
//     format, adjusted by Options.
//   - [Options] are New's optional inputs: query, token source, fixed headers, retry policy,
//     the stream idle bound, and the transcription mode.
//   - [TokenSource] returns the bearer token for one attempt.
//   - [RetryPolicy] is the caller's opt-in policy for sending a request again: its most
//     attempts and the backoff's base and most delays.
//   - [Client] makes requests of one endpoint in one wire format.
//   - [Client.Generate] sends a Request and returns its Response.
//   - [Client.GenerateStream] sends a Request when ranged and yields its response's streaming
//     events.
//   - [Client.Embed] sends an [EmbeddingRequest]'s batch as one request and returns its
//     [Embeddings], each vector placed by its reply index, counted by [EmbeddingUsage].
//   - [Client.Models] lists the endpoint's models, each a [Model].
//   - [Client.Transcribe] sends a [TranscriptionRequest]'s clip and returns its
//     [Transcription], counted by [TranscriptionUsage].
//   - [TranscriptionMode] is how Transcribe transcribes: [TranscribeRoute], the endpoint's
//     transcription route, or [TranscribeChat], a chat request in the Client's format.
//   - [TranscriptionInstruction] is the instruction chat mode gives the model when the request
//     has no Prompt.
//   - [APIError] is a non-2xx reply, matched with errors.As, carrying the reply's Retry-After.
//   - [APIError.Error] reports the reply's status and the endpoint's message.
package ai
