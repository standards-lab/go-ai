// Package aitest is a fake OpenAI-compatible model endpoint for tests: a loopback HTTP server
// that records every request it receives and answers each route with a function the test gives.
// It answers the OpenAI-compatible routes go-ai's Client sends to, decoding each request for its
// route function and writing the function's typed reply in the route's wire shape, and any other
// route below /v1, such as messages or responses, with a raw reply of status, headers, and a JSON
// body or a stream of server-sent events. A route without a function answers 404 with an
// OpenAI-style error body, as an endpoint without the route does.
//
// It imports only the standard library and go-ai's own packages, so any module's tests can use
// it.
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
//   - [ChatRequest] is a Chat Completions request: its arrival number and body.
//   - [EmbeddingRequest] is an embeddings request: its arrival number, model, and inputs.
//   - [TranscriptionRequest] is a transcription upload: its arrival number, model, prompt,
//     language, file name and bytes, and other form fields.
//   - [ModelsRequest] is a models request: its arrival number.
//   - [RawRequest] is a request to a raw route: its arrival number and the Request.
//   - [Reply] is what a route function answers with, one of the package's reply types.
//   - [EmbeddingsReply] is vectors and usage, written as base64 little-endian float32s.
//   - [TranscriptionReply] is a transcript and usage, written as response_format json.
//   - [ModelsReply] is a model list of whole JSON entries, written as given.
//   - [RawReply] is a status, headers, and a JSON body or [Frame]s, written as given.
//   - [Frame] is one server-sent event of a RawReply, flushed on its own.
package aitest
