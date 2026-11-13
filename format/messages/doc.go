// Package messages is the Anthropic Messages wire format: the adapter that encodes the protocol's
// request model as a /messages request and decodes the reply into its response model, whole or
// streamed. It speaks API version 2026-10-21, which every request names in its anthropic-version
// header, and it targets Anthropic's API and the endpoints compatible with it. A caller builds it
// with New at the composition root, as ai.New's wireFormat.
//
// It sends instructions as the top-level system string, MaxOutputTokens as max_tokens, the input
// items as messages of content blocks, a developer message as a system message where it stands,
// a user turn's tool results ahead of its other blocks, and a run of assistant items as one
// assistant message of thinking, text, and tool_use blocks. Tools go out as custom tools, the
// tool choice as tool_choice, an allowed-tools choice as the tools it allows, reasoning's effort
// as thinking and output_config.effort, and a JSON-schema text format as output_config.format.
// Native's members, the native tier, are merged into the body's top level.
//
// It refuses, before anything is sent and before it sets a header, a request with no
// MaxOutputTokens, as Messages requires max_tokens; audio input, which has no Messages form; an
// image data URL that isn't base64 or has no media type; and a Native member that clashes with
// one the typed fields set, stream among them.
//
// It drops the hints Messages has no form for, never refusing them: an image's detail;
// reasoning's summary; text's verbosity; and a JSON-schema text format's name, description, and
// strict flag. It drops a reasoning item whose encrypted_content isn't its own envelope, and a
// run of reasoning that no assistant message or function_call follows, never an error.
//
// Reasoning goes back through a reasoning item's encrypted_content, where DecodeResponse writes
// the format's envelope, go-ai:messages/v1: and base64 of the thinking block's
// {"thinking","signature"} or the redacted_thinking block's {"data"}; an envelope under another
// prefix, another format's, another version's, or another implementor's, is dropped on encode.
//
// It normalizes the reply to the spec's model: each output item's id derives from the reply's id
// and its place in the output, and the stop_reason gives the status, only the last item taking
// an incomplete one. A streamed reply's content blocks may be open at once, as an endpoint may
// stop every block only at the end of the reply; the decoder closes each part at its own block's
// stop, and the folded Response is DecodeResponse's for the same reply. An error frame in the
// stream, Anthropic's {"type":"error",...} or an OpenAI-style {"code","message","type"}, ends it
// with the error event and response.failed. An error body of either shape is decoded.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, messages.
//   - [Format.EncodeRequest] encodes a request as a Messages body: messages, tools, tool choice,
//     reasoning effort, text format, and the native tier.
//   - [Format.DecodeResponse] decodes a Messages reply into a response: reasoning, messages, and
//     function calls.
//   - [Format.EncodeStreamRequest] encodes a request as EncodeRequest does, with stream true.
//   - [Format.NewStreamDecoder] returns a decoder of one streamed reply's events into the
//     protocol's streaming events, whose folded Response is DecodeResponse's for the same reply.
//   - [Format.DecodeError] decodes an Anthropic or an OpenAI-style error body.
package messages
