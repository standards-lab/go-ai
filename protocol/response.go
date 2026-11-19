package protocol

import "strings"

// Response is a model's response: Open Responses' response object, in its stateless subset.
type Response struct {
	// ID is the reply's id, as the endpoint reports it, and empty when it reports none.
	ID string
	// Model is the model that answered, as the endpoint reports it, and empty when it reports
	// none. It may differ from the request's Model, such as a dated version of an alias.
	Model string
	// Output is what the model produced, in order.
	Output []Item
	// Status is StatusCompleted, or StatusIncomplete when the model stopped early. A streamed
	// response is StatusInProgress until its stream finishes, and StatusFailed when its stream
	// ends with response.failed.
	Status Status
	// IncompleteDetails says why the response is incomplete. It is nil when it is complete.
	IncompleteDetails *IncompleteDetails
	// Error is the error the response failed with, the spec's error member. It is set on a
	// response in StatusFailed, and nil otherwise.
	Error *ResponseError
	// Usage counts the tokens the request used.
	Usage Usage
}

// OutputText returns the text of r's output: every OutputText part of every Message item in
// Output, in order, joined with nothing between. It reads messages of any role, and nothing
// else: a Refusal part, a Reasoning item, and a FunctionCall item add nothing. A response with
// no OutputText part returns "".
func (r *Response) OutputText() string {
	var b strings.Builder
	for _, item := range r.Output {
		m, ok := item.(Message)
		if !ok {
			continue
		}
		for _, part := range m.Content {
			if t, ok := part.(OutputText); ok {
				b.WriteString(t.Text)
			}
		}
	}
	return b.String()
}

// ResponseError is the error a failed response holds: the spec's Error object, which a stream's
// response.failed carries in its Response.
type ResponseError struct {
	// Code is the machine-readable error code. The spec requires one; it is empty when the
	// endpoint gave none.
	Code string
	// Message is the human-readable description of the error.
	Message string
}

// IncompleteDetails says why a response is incomplete.
type IncompleteDetails struct {
	// Reason is why the model stopped.
	Reason IncompleteReason
}

// IncompleteReason is why a response is incomplete.
type IncompleteReason string

// The reasons a response is incomplete.
const (
	// ReasonMaxOutputTokens is a response cut off at the output token bound.
	ReasonMaxOutputTokens IncompleteReason = "max_output_tokens"
	// ReasonContentFilter is a response stopped by the endpoint's content filter.
	ReasonContentFilter IncompleteReason = "content_filter"
	// ReasonContextWindow is a response cut off because the input and output together filled
	// the model's context window, before the output token bound. The spec defines no reason
	// for it, so it is a client extension under go-ai's slug.
	ReasonContextWindow IncompleteReason = "go-ai:context_window"
)

// Usage counts the tokens a request used, as the endpoint reports them.
type Usage struct {
	// InputTokens counts the tokens of the input, instructions included.
	InputTokens int
	// OutputTokens counts the tokens generated, reasoning included.
	OutputTokens int
	// TotalTokens counts input and output tokens together.
	TotalTokens int
	// InputTokensDetails breaks InputTokens down.
	InputTokensDetails InputTokensDetails
	// OutputTokensDetails breaks OutputTokens down.
	OutputTokensDetails OutputTokensDetails
}

// InputTokensDetails breaks a request's input tokens down.
type InputTokensDetails struct {
	// CachedTokens counts the input tokens served from the endpoint's cache, when the endpoint
	// reports them.
	CachedTokens int
}

// OutputTokensDetails breaks a response's output tokens down.
type OutputTokensDetails struct {
	// ReasoningTokens counts the output tokens spent reasoning, when the endpoint reports
	// them.
	ReasoningTokens int
}
