package responses

import (
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// Options is a profile: it declares the gaps of the one deployment the format targets, where the
// deployment departs from the Open Responses spec. Its zero value declares none and is the spec's
// form, which a deployment that follows the spec takes as it is. A declared gap is refused before
// anything is sent, or emulated where the format can, never sent for the host to refuse or ignore.
//
// Options decodes from JSON under its snake_case names alone, refused_efforts, no_allowed_tools,
// and resend_reasoning_text, matched case-sensitively whatever the decoder's options, so a member
// spelled as the Go field is unknown, an error under strict decoding. Each zero field is omitted
// on encoding.
type Options struct {
	// RefusedEfforts are the reasoning efforts the deployment refuses, as vLLM 0.34.0's
	// gpt-oss-120b refuses none and xhigh. A request whose effort is among them is refused,
	// Declared, its Feature "reasoning effort" and its Value the effort. An effort the spec
	// lacks, minimal or max, is refused as the spec's gap, Declared false, listed or not. A
	// request of no effort sends none and is never refused.
	RefusedEfforts []protocol.ReasoningEffort `json:"refused_efforts,omitzero,case:strict"`
	// NoAllowedTools declares that the deployment ignores an allowed_tools tool choice, as vLLM
	// 0.34.0 calls a tool outside the allowed set. An allowed_tools choice is narrowed instead of
	// sent: only the tools it allows go out, in the request's order, and its mode goes out as a
	// plain tool_choice, as the messages format sends one. An allowed tool the request doesn't
	// carry sends nothing. The spec has a form for each mode, so every mode is narrowed, none
	// among them.
	NoAllowedTools bool `json:"no_allowed_tools,omitzero,case:strict"`
	// ResendReasoningText sends a reasoning item back with its reasoning text as content, beside
	// its encrypted_content, for a deployment that reads the reasoning from there and gives no
	// encrypted_content to send instead, as llama.cpp's router and vLLM 0.34.0 do. The text is
	// what the format's envelope holds, the reply's reasoning_text parts, never the item's
	// Content. The spec's input reasoning item has no content, so the zero value, false, never
	// sends it: this field is the one exception to protocol.Reasoning's rule that a format never
	// sends a reasoning item's text on input.
	ResendReasoningText bool `json:"resend_reasoning_text,omitzero,case:strict"`
}

// Format is the Open Responses wire format, a format.Format. Build it with New; it is safe for
// concurrent use.
type Format struct {
	opts Options
}

// New returns the Open Responses format under the profile opts, for ai.New's wireFormat. The zero
// Options are the spec's form. A profile binds the format to one deployment, so a caller of
// several deployments builds one format, and one client, per deployment, the clients sharing one
// http.Client. Each field of Options states the gap it declares.
//
// New keeps its own copy of RefusedEfforts, so a change to opts after New changes nothing.
func New(opts Options) *Format {
	opts.RefusedEfforts = append([]protocol.ReasoningEffort(nil), opts.RefusedEfforts...)
	return &Format{opts: opts}
}

// Route is "responses", the Open Responses endpoint below a base URL such as
// "http://localhost:8080/v1".
func (*Format) Route() string {
	return "responses"
}

// EventStream is format.SSE: Open Responses streams its reply as server-sent events.
func (*Format) EventStream() format.EventStream {
	return format.SSE
}
