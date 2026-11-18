package messages

import (
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// version is the Messages API version the format speaks, sent with every request as its
// anthropic-version header.
const version = "2026-10-21"

// Options is a profile: it declares the gaps of the one deployment the format targets, where the
// deployment departs from the Messages API. Its zero value declares none and is the spec's form,
// which a deployment that follows the spec takes as it is. A declared gap is refused before
// anything is sent, never sent for the host to refuse or ignore.
//
// Options decodes from JSON under its snake_case name alone, refused_efforts, matched
// case-sensitively whatever the decoder's options, so a member spelled as the Go field is
// unknown, an error under strict decoding. A zero field is omitted on encoding.
type Options struct {
	// RefusedEfforts are the reasoning efforts the deployment refuses or ignores, as vLLM
	// 0.34.0's gpt-oss-120b refuses xhigh and max, and reasons anyway under none, sent as
	// thinking disabled. A request whose effort is among them is refused, Declared, its Feature
	// "reasoning effort" and its Value the effort. An effort Messages lacks, minimal, is refused
	// as the spec's gap, Declared false, listed or not. A request of no effort sends none and is
	// never refused.
	RefusedEfforts []protocol.ReasoningEffort `json:"refused_efforts,omitzero,case:strict"`
}

// Format is the Anthropic Messages wire format, a format.Format. Build it with New; it is safe
// for concurrent use.
type Format struct {
	opts Options
}

// New returns the Anthropic Messages format under the profile opts, for ai.New's wireFormat. The
// zero Options are the spec's form. A profile binds the format to one deployment, so a caller of
// several deployments builds one format, and one client, per deployment, the clients sharing one
// http.Client. opts declares:
//
//   - RefusedEfforts: the reasoning efforts the deployment refuses, each refused before sending.
//
// New keeps its own copy of RefusedEfforts, so a change to opts after New changes nothing.
func New(opts Options) *Format {
	opts.RefusedEfforts = append([]protocol.ReasoningEffort(nil), opts.RefusedEfforts...)
	return &Format{opts: opts}
}

// Route is "messages", the Messages endpoint below a base URL such as
// "https://api.anthropic.com/v1" or "http://localhost:8080/v1".
func (*Format) Route() string {
	return "messages"
}

// EventStream is format.SSE: Messages streams its reply as server-sent events.
func (*Format) EventStream() format.EventStream {
	return format.SSE
}
