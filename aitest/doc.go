// Package aitest is a fake OpenAI-compatible model endpoint for tests: a loopback HTTP server
// that records every request it receives and answers each route with a function the test gives.
// It answers the OpenAI-compatible routes go-ai's Client sends to, decoding each request for its
// route function and writing the function's typed reply in the route's wire shape, and any other
// route below /v1, such as messages or responses, with a raw reply of status, headers, and a JSON
// body or a stream of server-sent events. A route without a function answers 404 with an
// OpenAI-style error body, as an endpoint without the route does.
//
// It imports only the standard library, so any module's tests can use it.
//
// Its exported identifiers:
//
//   - [Serve] starts an [Endpoint] answering with [Routes], closed when the test ends.
//   - [Routes] gives the function answering each route: Chat Completions, embeddings,
//     transcriptions, models, and raw routes by path.
//   - [Endpoint] is the loopback endpoint.
//   - [Endpoint.URL] returns its API root, ending in /v1.
//   - [Endpoint.Requests] returns every [Request] it received, in order.
//   - [Request] is one request as it arrived: method, path, query, header, and body.
//   - [ChatRequest] is a Chat Completions request: its arrival number and body, and decoded,
//     its model, stream flag, max tokens, reasoning effort, JSON schema name, and messages.
//   - [ChatMessage] is one message of a ChatRequest: its role, text, images, and audio.
//   - [ChatImage] is an image of a ChatMessage: a data URL's media type and bytes, or a URL.
//   - [ChatAudio] is an audio clip of a ChatMessage: its format and bytes.
//   - [EmbeddingRequest] is an embeddings request: its arrival number, model, and inputs.
//   - [TranscriptionRequest] is a transcription upload: its arrival number, model, prompt,
//     language, file name as sent and bytes, and other form fields.
//   - [ModelsRequest] is a models request: its arrival number.
//   - [RawRequest] is a request to a raw route: its arrival number and the Request.
//   - [Reply] is what a route function answers with, one of the package's reply types.
//   - [ChatReply] is a chat reply of text or a refusal, the reasoning that led to it, its finish
//     reason, and usage with reasoning tokens, written as a chat.completion, or streamed as
//     chunks when the request asks to stream, able to break off mid-stream, and to be held
//     after a piece of text on a [Gate].
//   - [EmbeddingsReply] is vectors and usage, written as base64 little-endian float32s.
//   - [TranscriptionReply] is a transcript and usage, written as response_format json.
//   - [ModelsReply] is a model list of whole JSON entries, written as given.
//   - [ErrorReply] is an OpenAI-style error of any status, with a Retry-After header as given,
//     on any route.
//   - [RawReply] is a status, headers, and a JSON body or [Frame]s, written as given, a stream
//     of frames able to be held after a frame on a [Gate].
//   - [Frame] is one server-sent event of a RawReply, flushed on its own.
//   - [Gate] parks requests inside route functions, and holds streams a reply's Hold names,
//     until the test lets them go; its zero value is ready to use.
//   - [Gate.Wait] parks a request until it is let go, or until its context ends.
//   - [Gate.Await] blocks the test until n requests are parked, failing it after ten seconds.
//   - [Gate.Parked] returns how many requests are parked now.
//   - [Gate.Release] lets n parked or later requests go.
//   - [Gate.Open] lets every parked and later request go.
package aitest
