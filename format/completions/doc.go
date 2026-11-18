// Package completions is the Chat Completions wire format: the adapter that encodes the
// protocol's request model as an OpenAI-compatible /chat/completions request and decodes the
// reply into its response model, whole or streamed. It targets /chat/completions, not the legacy
// /completions API. A caller builds it with New at the composition root, as ai.New's
// wireFormat.
//
// It sends instructions as a leading system message, the input items as messages, a run of
// assistant items joined into one assistant message of reasoning_content, content, and
// tool_calls, the tools and tool choice, reasoning's effort as reasoning_effort, text's format
// as response_format and its verbosity as verbosity, MaxOutputTokens as max_completion_tokens,
// ParallelToolCalls, when set, as parallel_tool_calls, and audio input as its own input_audio
// part. Native's members, the native tier, are merged
// into the body's top level.
//
// It refuses, before anything is sent, an allowed tool choice of mode none, which Chat
// Completions' allowed_tools has no form for, and a Native member that clashes with one the
// typed fields set, stream and stream_options among them, and parallel_tool_calls when
// ParallelToolCalls is set.
//
// It drops the hint Chat Completions has no form for, never refusing it: reasoning's summary. It
// drops a reasoning item whose encrypted_content isn't its own envelope, and a run of reasoning
// that no assistant message or function_call follows, never an error.
//
// Reasoning goes back through a reasoning item's encrypted_content, where DecodeResponse writes
// the format's envelope, go-ai:completions/v1: and base64 of {"reasoning_content"}; an envelope
// under another prefix, another format's, another version's, or another implementor's, is
// dropped on encode.
//
// It normalizes the reply to the spec's model: Chat Completions gives no item ids, so each
// output item's id derives from the reply's id and its place in the output, alike streamed and
// unstreamed; the reasoning, which llama.cpp sends as reasoning_content and vLLM as reasoning,
// becomes a reasoning item ahead of the message, read from either name; and the
// finish_reason gives the status, only the last item taking an incomplete one; the usage's
// reasoning tokens are read from completion_tokens_details or, when a server gives them at the
// usage's top level instead, from there, and its cached input tokens from
// prompt_tokens_details; the reply's id and model become the response's, a stream's on
// response.created and its terminal response alike. A stream's tool call fragments, which may interleave,
// are assembled by index; a chunk carrying no id and no choices, as some endpoints send ahead of
// the reply, claims nothing about it, and neither sets nor checks the stream's id and model,
// response.created waiting for the chunk that does; and an error
// frame ends the stream with the error event and response.failed.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [New] returns the format.
//   - [Format.Route] is the route it posts to, chat/completions.
//   - [Format.EncodeRequest] encodes a request as a Chat Completions body.
//   - [Format.DecodeResponse] decodes a Chat Completions reply into a response.
//   - [Format.EncodeStreamRequest] encodes a request as a streamed Chat Completions body.
//   - [Format.EventStream] is the framing its replies stream in, format.SSE.
//   - [Format.NewStreamDecoder] returns a decoder turning one streamed reply's chunks into the
//     protocol's streaming events.
//   - [Format.DecodeError] decodes an OpenAI-style error body.
package completions
