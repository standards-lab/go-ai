package completions_test

import (
	"encoding/json/jsontext"
	"net/http"
	"testing"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// unknownItem, unknownPart, unknownChoice, and unknownFormat are an item, a content part, a tool
// choice, and a text format of a type the format doesn't know, each taking its interface's
// method from the protocol type it embeds.
type (
	unknownItem   struct{ protocol.Message }
	unknownChoice struct{ protocol.FunctionToolChoice }
	unknownFormat struct{ protocol.PlainTextFormat }
)

// TestEncodeRefusalIsUnsupported wants each request the format refuses refused with a
// *format.UnsupportedError of the feature and value refused, a spec-level refusal, and the
// refusal's message unchanged.
func TestEncodeRefusalIsUnsupported(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	req := func(mod func(*protocol.Request)) *protocol.Request {
		r := &protocol.Request{Model: "m", Input: []protocol.Item{user}}
		mod(r)
		return r
	}
	cases := []struct {
		name    string
		req     *protocol.Request
		feature string
		value   string
		message string
	}{
		{"native stream_options", req(func(r *protocol.Request) { r.Native = jsontext.Value(`{"stream_options":{}}`) }),
			"native member", "stream_options",
			"completions: native member stream_options clashes with the format's streaming"},
		{"native clash", req(func(r *protocol.Request) { r.Native = jsontext.Value(`{"model":"other"}`) }),
			"native member", "model",
			"completions: native member model clashes with a member the request sets"},
		{"input item", req(func(r *protocol.Request) { r.Input = []protocol.Item{unknownItem{user}} }),
			"input item", "completions_test.unknownItem",
			"completions: input 0: item completions_test.unknownItem has no Chat Completions form"},
		{"content part", req(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.ReasoningText{Text: "hm"}}}}
		}), "content part", "protocol.ReasoningText",
			"completions: input 0: content 0: part protocol.ReasoningText has no Chat Completions form"},
		{"output part", req(func(r *protocol.Request) {
			r.Input = []protocol.Item{protocol.FunctionCallOutput{CallID: "c1", OutputParts: []protocol.ContentPart{
				protocol.InputText{Text: "21"}, protocol.ReasoningText{Text: "hm"}}}}
		}), "content part", "protocol.ReasoningText",
			"completions: input 0: content 1: part protocol.ReasoningText has no Chat Completions form"},
		{"allowed_tools of mode none", withTools(protocol.AllowedToolChoice{
			Tools: []protocol.FunctionToolChoice{{Name: "get_time"}},
			Mode:  protocol.ToolChoiceNone,
		}), "allowed_tools mode", "none",
			"completions: tool choice: allowed_tools of mode none has no Chat Completions form"},
		{"tool choice", withTools(unknownChoice{protocol.FunctionToolChoice{Name: "get_time"}}),
			"tool choice", "completions_test.unknownChoice",
			"completions: tool choice: tool choice completions_test.unknownChoice has no Chat Completions form"},
		{"text format", req(func(r *protocol.Request) { r.Text.Format = unknownFormat{} }),
			"text format", "completions_test.unknownFormat",
			"completions: text format: text format completions_test.unknownFormat has no Chat Completions form"},
	}
	f := completions.New()
	encodings := map[string]func(*protocol.Request, http.Header) ([]byte, error){
		"EncodeRequest":       f.EncodeRequest,
		"EncodeStreamRequest": f.EncodeStreamRequest,
	}
	for name, encode := range encodings {
		for _, tc := range cases {
			if name == "EncodeStreamRequest" && tc.name == "native stream_options" {
				// The streamed body sets stream_options, so the member is a clash there, which
				// the native clash case covers.
				continue
			}
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				body, err := encode(tc.req, make(http.Header))
				if err == nil {
					t.Fatalf("%s = %s, nil; want a refusal", name, body)
				}
				aitest.AssertUnsupported(t, err, format.UnsupportedError{
					Format: "completions", Feature: tc.feature, Value: tc.value,
				}, tc.message)
			})
		}
	}
}
