package completions

import "github.com/standards-lab/go-ai/format"

// Format is the Chat Completions wire format, a format.Format. Build it with New; it is safe for
// concurrent use.
type Format struct{}

// New returns the Chat Completions format, for ai.New's wireFormat.
func New() *Format {
	return &Format{}
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
