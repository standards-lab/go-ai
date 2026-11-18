package responses_test

import (
	"net/http"
	"testing"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// unknownItem, unknownChoice, and unknownFormat are an item, a tool choice, and a text format of
// a type the format doesn't know, each taking its interface's method from the protocol type it
// embeds.
type (
	unknownItem   struct{ protocol.Message }
	unknownChoice struct{ protocol.FunctionToolChoice }
	unknownFormat struct{ protocol.PlainTextFormat }
)

// TestEncodeRefusalIsUnsupported wants each request the format refuses refused with a
// *format.UnsupportedError of the feature and value refused, a spec-level refusal, and the
// refusal's message unchanged, streamed or not.
func TestEncodeRefusalIsUnsupported(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	reasoningText := protocol.ReasoningText{Text: "hm"}
	textFormat := ask(user)
	textFormat.Text.Format = unknownFormat{}
	effort := func(e protocol.ReasoningEffort) *protocol.Request {
		r := ask(user)
		r.Reasoning.Effort = e
		return r
	}
	capped := ask(user)
	capped.MaxOutputTokens = responses.MinMaxOutputTokens - 1
	cases := []struct {
		name    string
		req     *protocol.Request
		feature string
		value   string
		message string
	}{
		{"native previous_response_id", withNative(ask(user), `{"previous_response_id":"resp_1"}`),
			"native member", "previous_response_id",
			"responses: native member previous_response_id asks the endpoint to keep state, which a stateless request never does"},
		{"native background", withNative(ask(user), `{"background":false}`),
			"native member", "background",
			"responses: native member background asks the endpoint to keep state, which a stateless request never does"},
		{"native clash", withNative(ask(user), `{"store":true}`),
			"native member", "store",
			"responses: native member store clashes with a member the request sets"},
		{"input item", ask(unknownItem{user}),
			"input item", "responses_test.unknownItem",
			"responses: input 0: item responses_test.unknownItem has no Open Responses form"},
		{"content part", ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{reasoningText}}),
			"content part", "protocol.ReasoningText",
			"responses: input 0: content 0: part protocol.ReasoningText has no Open Responses form"},
		{"output part", ask(protocol.FunctionCallOutput{CallID: "c1", OutputParts: []protocol.ContentPart{reasoningText}}),
			"content part", "protocol.ReasoningText",
			"responses: input 0: output content 0: part protocol.ReasoningText has no Open Responses form"},
		{"tool choice", withTools(unknownChoice{protocol.FunctionToolChoice{Name: "f"}}, protocol.FunctionTool{Name: "f"}),
			"tool choice", "responses_test.unknownChoice",
			"responses: tool choice responses_test.unknownChoice has no Open Responses form"},
		{"max output tokens below the spec's minimum", capped,
			"max output tokens", "15",
			"responses: request's max output tokens 15 is below the minimum, 16"},
		{"reasoning effort minimal", effort(protocol.ReasoningEffortMinimal),
			"reasoning effort", "minimal",
			"responses: reasoning effort minimal has no Open Responses form"},
		{"reasoning effort max", effort(protocol.ReasoningEffortMax),
			"reasoning effort", "max",
			"responses: reasoning effort max has no Open Responses form"},
		{"text format", textFormat,
			"text format", "responses_test.unknownFormat",
			"responses: text format responses_test.unknownFormat has no Open Responses form"},
	}
	f := responses.New(responses.Options{})
	encodings := map[string]func(*protocol.Request, http.Header) ([]byte, error){
		"EncodeRequest":       f.EncodeRequest,
		"EncodeStreamRequest": f.EncodeStreamRequest,
	}
	for name, encode := range encodings {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				body, err := encode(tc.req, make(http.Header))
				if err == nil {
					t.Fatalf("%s = %s, nil; want a refusal", name, body)
				}
				aitest.AssertUnsupported(t, err, format.UnsupportedError{
					Format: "responses", Feature: tc.feature, Value: tc.value,
				}, tc.message)
			})
		}
	}
}
