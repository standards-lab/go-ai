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
// answers with an *APIError. Embed sends a request's input type in the convention the Client's
// EmbeddingProfile declares for it; a type the profile declares nothing for is a hint the model
// has no form for, as OpenAI's embeddings API has none, and is dropped. Transcribe's chat mode
// instead sends the clip as a chat request in the Client's format, which must carry audio, as
// Chat Completions does, for an endpoint whose audio model has no transcription route. Each mode
// has its own native tier, refused by the other before anything is sent: a TranscriptionRequest's
// Native holds the route's form fields, and its ChatNative the chat request's JSON object, such as
// sampling.
//
// Tokenize, like Embed, sits outside the protocol and the Format. It counts a text's tokens in
// the model's own tokenizer through vLLM's tokenize route, which vLLM serves at the server's
// root rather than below /v1, so its Client's base URL is the server's root, or a gateway's API
// root that forwards the route. It sends the model, the text with the prefix the
// EmbeddingProfile declares for its input type, and add_special_tokens true, so the count is what
// Embed of the same text takes; a convention's native members aren't sent.
//
// go-ai's README (https://github.com/standards-lab/go-ai#readme) states the module's place in Go
// Elemental, its dependency line, and its API by example.
//
// Its exported identifiers:
//
//   - [New] returns a Client of an endpoint's base URL, sending with an http.Client in a wire
//     format, adjusted by Options.
//   - [Options] are New's optional inputs: query, token source, fixed headers, retry policy,
//     the stream idle bound, the transcription mode, and the embedding profile.
//   - [TokenSource] returns the bearer token for one attempt.
//   - [RetryPolicy] is the caller's opt-in policy for sending a request again: its most
//     attempts and the backoff's base and most delays.
//   - [Client] makes requests of one endpoint in one wire format.
//   - [Client.Generate] sends a Request and returns its Response.
//   - [Client.GenerateStream] sends a Request when ranged and yields its response's streaming
//     events.
//   - [Client.Embed] sends an [EmbeddingRequest]'s batch as one request and returns its
//     [Embeddings], each vector placed by its reply index, counted by [EmbeddingUsage].
//   - [InputType] is what an embedding request's inputs are: [InputQuery], [InputDocument], or
//     [InputNone], the zero value, which sends them as given.
//   - [InputType.String] returns a type's name, none, query, or document.
//   - [InputType.MarshalText] encodes a type by name, refusing an unknown type.
//   - [InputType.UnmarshalText] decodes a type by name, refusing any other name.
//   - [EmbeddingProfile] maps each input type to its [InputConvention], a prefix before every
//     input and native members merged into the body.
//   - [Client.EmbeddingProfile] returns a copy of the profile the Client applies.
//   - [Client.Tokenize] counts a [TokenizeRequest]'s text in its model's tokenizer, with the
//     prefix of its input type, and returns its [Tokens]: the count, the model's most tokens,
//     and the token ids.
//   - [Client.Models] lists the endpoint's models, each a [Model]: its id, and its list entry
//     as the endpoint sent it.
//   - [Client.Transcribe] sends a [TranscriptionRequest]'s clip and returns its
//     [Transcription], counted by [TranscriptionUsage].
//   - [Client.TranscriptionMode] returns the mode the Client transcribes in.
//   - [TranscriptionMode] is how Transcribe transcribes: [TranscribeRoute], the endpoint's
//     transcription route, or [TranscribeChat], a chat request in the Client's format.
//   - [TranscriptionMode.String] returns a mode's name, route or chat.
//   - [TranscriptionMode.MarshalText] encodes a mode by name, refusing an unknown mode.
//   - [TranscriptionMode.UnmarshalText] decodes a mode by name, refusing any other name.
//   - [TranscriptionInstruction] is the instruction chat mode gives the model when the request
//     has no Prompt.
//   - [IncompleteTranscriptError] is a chat-mode Transcribe's reply that isn't completed, such
//     as one stopped at its output token bound, matched with errors.As, carrying the reply's
//     status and reason.
//   - [IncompleteTranscriptError.Error] reports the status and reason, what they mean, and
//     what the caller can do.
//   - [APIError] is a non-2xx reply, matched with errors.As, carrying the reply's Retry-After.
//   - [APIError.Error] reports the reply's status and the endpoint's message.
package ai
