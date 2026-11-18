// Package responses is the Open Responses wire format: the adapter that encodes the protocol's
// request model as a /responses request and decodes the reply into its response model. It
// speaks spec 2026-04-24 (https://www.openresponses.org), in its stateless subset, under the
// spec's names, as the protocol package models it, and it targets the endpoints that serve it,
// such as llama.cpp's router. A caller builds it with New at the composition root, as ai.New's
// wireFormat.
//
// New takes Options, a profile of the one deployment the format targets: the gaps where that
// deployment departs from the spec. The zero Options declare none and are the spec's form.
// RefusedEfforts declares the reasoning efforts the deployment refuses; a request of one is
// refused before anything is sent, a format.UnsupportedError of Declared true. NoAllowedTools
// declares that the deployment ignores an allowed_tools choice, which the format then narrows to
// the tools it allows and its mode as a plain tool_choice. ResendReasoningText declares that it
// reads a reasoning item's text as content, which then goes back so, as below. A profile binds
// the format to one deployment, so a caller of several deployments builds one format and one
// client per deployment, sharing the http.Client. Options decodes from JSON under
// refused_efforts, no_allowed_tools, and resend_reasoning_text; the module ships no loader of
// them.
//
// It sends the request under the spec's names: model, instructions, the input items, tools, tool
// choice, parallel_tool_calls, reasoning, text, and max_output_tokens, with Native's members, the
// native tier, merged into the body's top level, and a tool's strict flag always sent.
//
// Every request is stateless: it carries store false and include
// ["reasoning.encrypted_content"], and never previous_response_id, so the conversation goes out
// whole on each request and the reasoning comes back in a form the next one can send. A Native
// previous_response_id or background member is refused, never sent: the client's model is
// stateless, a request's Input being the whole conversation, and each member asks the endpoint
// to keep state, a prior response or a running one, that the model has no place for. Stateful
// use would be a new capability of the client, typed fields with operations to retrieve, poll,
// cancel, and delete a stored response, never a member a request carries.
//
// Reasoning goes back through a reasoning item's encrypted_content, where DecodeResponse writes
// the format's envelope, go-ai:responses/v1: and base64 of
// {"encrypted_content","content"}: the server's encrypted_content, and the texts of the item's
// reasoning_text parts. In the spec's form, the zero Options, EncodeRequest sends the server's
// encrypted_content back, and never content, dropping an item whose server encrypted_content is
// empty. Options.ResendReasoningText sends the texts back as content too: the one exception to
// protocol.Reasoning's rule that a format never sends a reasoning item's text on input, for an
// endpoint such as llama.cpp's router or vLLM 0.34.0, which gives no encrypted_content and reads
// the reasoning from content; under it, a run of reasoning that no assistant message or
// function_call follows is dropped, as the router refuses one. An envelope under another prefix,
// another format's, another version's, or another implementor's, is dropped on encode.
//
// Audio input goes out as the protocol's client extension, the spec having no audio input
// part: the content part {"type":"go-ai:input_audio","data","format"}, its data the
// clip's bytes in standard base64 and its format wav or mp3.
//
// It refuses, before anything is sent, the efforts its Options declare refused; a
// MaxOutputTokens below MinMaxOutputTokens, the spec's minimum of 16; a reasoning effort the spec
// lacks, minimal or max; a Native member that clashes with one the typed fields set, stream,
// store, and include among them; and a Native previous_response_id or background, as above.
//
// The spec has a form for every hint the protocol carries, so the format drops none: an image's
// detail, reasoning's summary, text's verbosity, and a JSON-schema format's name, description,
// and strict flag each go out when set, and whether they reach a model is up to the endpoint. It
// drops a reasoning item whose encrypted_content isn't its own envelope, and, in the spec's form,
// one whose server encrypted_content is empty, never an error.
//
// A reply's output item of a type the protocol has no form for, such as a hosted tool's call, and
// a message's content part of a type that isn't an output part, are skipped, whole and streamed,
// never an error, the output and content indexes after them closing up.
//
// A streamed reply becomes the spec's semantic events, under the spec's names, each numbered by
// the decoder in order and placed at its item's output_index. A stream that departs from the
// spec, as llama.cpp's router's does, is normalized: an event of no sequence_number,
// output_index, or content_index is placed by arrival and by its item_id; reasoning streamed
// under OpenAI's names, response.reasoning_text.delta and .done, is read as the spec's
// response.reasoning.delta and .done; and the events the server leaves out are filled in: a
// reasoning item's content_part.added and content_part.done, response.reasoning.done,
// response.function_call_arguments.done, and a part's output_text.done or content_part.done. It
// never repeats an event the server sent. A reasoning item added with encrypted_content, which the
// spec allows to be incomplete while the item is in progress, or with a summary, is added with
// neither; its output_item.done gives both, the encrypted_content going into the envelope. An
// item added with content or arguments is refused. Items may be open at once, and an item the
// server gives no status at its output_item.done is completed there. The Response the events
// fold into is
// DecodeResponse's of the terminal event's response, with one exception: an item the terminal
// response gives no status takes the completed status of its output_item.done, so the folded
// Response agrees with the events. An error frame, the spec's error event or an OpenAI-style
// {"error":{...}} of no type, closes the stream with the error event and response.failed.
// NewStreamDecoder gives the whole account.
//
// Its exported identifiers:
//
//   - [Format] is the format, a format.Format.
//   - [Options] is the profile of the deployment the format targets, its zero value the spec.
//   - [MinMaxOutputTokens] is the least positive MaxOutputTokens it sends, the spec's 16.
//   - [New] returns the format under a profile.
//   - [Format.Route] is the route it posts to, responses.
//   - [Format.EncodeRequest] encodes a request as a create-response body: model, instructions,
//     input items, tools, tool choice, parallel_tool_calls, reasoning, text, max_output_tokens,
//     and the native tier.
//   - [Format.DecodeResponse] decodes a response resource into a response: its id, model, and
//     usage, and its messages, reasoning, and function calls, with their ids and statuses the
//     server's.
//   - [Format.EncodeStreamRequest] encodes a request as EncodeRequest does, with stream true.
//   - [Format.EventStream] is the framing its replies stream in, format.SSE.
//   - [Format.NewStreamDecoder] returns a decoder of a streamed reply into the protocol's
//     streaming events, whose folded Response is DecodeResponse's of the terminal response, an
//     item of no status there taking its output_item.done's.
//   - [Format.DecodeError] decodes an OpenAI-style error body.
package responses
