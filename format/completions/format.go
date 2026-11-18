package completions

import (
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// Options is a profile: it declares the gaps of the one deployment the format targets, where the
// deployment departs from Chat Completions' spec. Its zero value declares none and is the spec's
// form. A declared gap is refused before anything is sent, or narrowed where the format can
// emulate the feature, never sent for the host to refuse.
//
// Options decodes from JSON under its snake_case names alone, refused_efforts,
// no_tool_choice_required, and no_allowed_tools, matched case-sensitively whatever the decoder's
// options, so a member spelled as the Go field is unknown, an error under strict decoding. Each
// zero field is omitted on encoding.
type Options struct {
	// RefusedEfforts are the reasoning efforts the deployment refuses, as vLLM 0.34.0's
	// gpt-oss-120b refuses none, minimal, xhigh, and max. A request whose effort is among them
	// is refused, Declared, its Feature "reasoning effort" and its Value the effort. A request
	// of no effort sends none and is never refused.
	RefusedEfforts []protocol.ReasoningEffort `json:"refused_efforts,omitzero,case:strict"`
	// NoToolChoiceRequired declares that the deployment refuses tool_choice required, as
	// Foundry's gpt-oss-120b does. A tool choice of mode required is refused, Declared, its
	// Feature "tool choice" and its Value "required", and so is an allowed_tools choice of mode
	// required, sent or narrowed, under the same Feature and Value: it is the same gap.
	NoToolChoiceRequired bool `json:"no_tool_choice_required,omitzero,case:strict"`
	// NoAllowedTools declares that the deployment has no allowed_tools tool choice, as vLLM
	// 0.34.0 has none. An allowed_tools choice is narrowed instead of sent: only the tools it
	// allows go out, in the request's order, and its mode goes out as a plain tool_choice, as the
	// messages format sends one. An allowed tool the request doesn't carry sends nothing. An
	// allowed_tools choice of mode none, which the spec has no form for, is refused as the spec's
	// gap, never narrowed.
	NoAllowedTools bool `json:"no_allowed_tools,omitzero,case:strict"`
}

// Format is the Chat Completions wire format, a format.Format. Build it with New; it is safe for
// concurrent use.
type Format struct {
	opts Options
}

// New returns the Chat Completions format under the profile opts, for ai.New's wireFormat. The
// zero Options are the spec's form. A profile binds the format to one deployment, so a caller of
// several deployments builds one format, and one client, per deployment, the clients sharing one
// http.Client. opts declares:
//
//   - RefusedEfforts: the reasoning efforts the deployment refuses, each refused before sending.
//   - NoToolChoiceRequired: that the deployment refuses tool_choice required, which is refused
//     before sending, of a mode or of an allowed_tools choice.
//   - NoAllowedTools: that the deployment has no allowed_tools choice, which is narrowed to the
//     tools it allows and its mode.
//
// New keeps its own copy of RefusedEfforts, so a change to opts after New changes nothing.
func New(opts Options) *Format {
	opts.RefusedEfforts = append([]protocol.ReasoningEffort(nil), opts.RefusedEfforts...)
	return &Format{opts: opts}
}

// Route is "chat/completions", the Chat Completions endpoint below an OpenAI-compatible base
// URL such as "http://localhost:8080/v1".
func (*Format) Route() string {
	return "chat/completions"
}

// EventStream is format.SSE: Chat Completions streams its reply as server-sent events.
func (*Format) EventStream() format.EventStream {
	return format.SSE
}
