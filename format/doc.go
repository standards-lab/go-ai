// Package format is the contract a wire format meets: [Format] encodes the protocol package's
// request in one API's wire shape and decodes the reply into its response, whole or streamed as
// the protocol's events. It imports only protocol, and the module's internal SSE decoder, which
// [SSE] wraps; each implementation is a package below it, named for the endpoint it targets:
// completions for Chat Completions, messages for Anthropic Messages, and responses for Open
// Responses. None imports the client.
//
// It holds the contract and what the formats share: the OpenAI-style error body, which every
// OpenAI-compatible endpoint answers with, so completions decodes its errors, and its streams'
// error frames, with it; so does messages, as Anthropic's error body keeps its message and type
// in an error object too and llama.cpp's router answers its Messages route with the OpenAI-style
// one; so does responses, as the spec defines no error body and its endpoints answer with the
// OpenAI-style one; and so does the client for its operations outside the Format, such as
// embeddings.
//
// Its exported identifiers:
//
//   - [Format] is a wire format, the adapter between the protocol and one API's shape.
//   - [StreamDecoder] turns one streamed reply's events into the protocol's streaming events.
//   - [WireEvent] is one event of a streamed reply as its framing delivers it.
//   - [WireEventReader] reads a streamed reply's events, one per call.
//   - [EventStream] is a streamed reply's framing: its media type and its reader.
//   - [SSE] is the server-sent event stream, the EventStream of every format here.
//   - [DecodeOpenAIError] decodes an OpenAI-style error body's message, type, and code.
package format
