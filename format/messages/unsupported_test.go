package messages_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/messages"
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
	part := func(p protocol.ContentPart) *protocol.Request {
		return ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{p}})
	}
	audio := protocol.InputAudio{Data: []byte("RIFF"), Format: protocol.AudioFormatWAV}
	textFormat := ask(user)
	textFormat.Text.Format = unknownFormat{}
	minimal := ask(user)
	minimal.Reasoning.Effort = protocol.ReasoningEffortMinimal
	choice := ask(user)
	choice.Tools = []protocol.FunctionTool{{Name: "f"}}
	choice.ToolChoice = unknownChoice{protocol.FunctionToolChoice{Name: "f"}}
	// parallel is a request of a tool and ParallelToolCalls, its Native tool_choice clashing with
	// the member ParallelToolCalls sets, whether or not the request sends it.
	parallel := func(p bool, choice protocol.ToolChoice) *protocol.Request {
		req := withNative(ask(user), `{"tool_choice":{"type":"any"}}`)
		req.Tools = []protocol.FunctionTool{{Name: "f"}}
		req.ToolChoice = choice
		req.ParallelToolCalls = &p
		return req
	}
	const parallelClash = "messages: native member tool_choice clashes with a member the request sets"
	cases := []struct {
		name    string
		req     *protocol.Request
		feature string
		value   string
		message string
	}{
		{"no max output tokens", &protocol.Request{Model: "m", Input: []protocol.Item{user}},
			"max output tokens", "",
			"messages: request has no max output tokens, which Messages requires as max_tokens"},
		{"reasoning effort minimal", minimal,
			"reasoning effort", "minimal",
			"messages: reasoning effort minimal has no Messages form"},
		{"native clash", withNative(ask(user), `{"max_tokens":8}`),
			"native member", "max_tokens",
			"messages: native member max_tokens clashes with a member the request sets"},
		{"native tool_choice beside ParallelToolCalls false", parallel(false, nil),
			"native member", "tool_choice", parallelClash},
		{"native tool_choice beside ParallelToolCalls true", parallel(true, nil),
			"native member", "tool_choice", parallelClash},
		{"native tool_choice beside a choice of none and ParallelToolCalls false", parallel(false, protocol.ToolChoiceNone),
			"native member", "tool_choice", parallelClash},
		{"a system message after the conversation has begun", ask(user, text(protocol.RoleSystem, "s")),
			"system message placement", "system",
			"messages: input 1: system message after the conversation has begun has no Messages form, as Messages takes system content only ahead of the messages"},
		{"a developer message after the conversation has begun", ask(user, text(protocol.RoleDeveloper, "d")),
			"system message placement", "developer",
			"messages: input 1: developer message after the conversation has begun has no Messages form, as Messages takes system content only ahead of the messages"},
		{"input item", ask(unknownItem{user}),
			"input item", "messages_test.unknownItem",
			"messages: input 0: item messages_test.unknownItem has no Messages form"},
		{"content part", part(audio),
			"content part", "protocol.InputAudio",
			"messages: input 0: content 0: part protocol.InputAudio has no Messages form"},
		{"output part", ask(protocol.FunctionCallOutput{CallID: "c1", OutputParts: []protocol.ContentPart{audio}}),
			"content part", "protocol.InputAudio",
			"messages: input 0: output content 0: part protocol.InputAudio has no Messages form"},
		{"image data URL not base64", part(protocol.InputImage{ImageURL: "data:image/png,%89PNG"}),
			"image data URL encoding", "",
			"messages: input 0: content 0: image data URL isn't base64, as a base64 source requires"},
		{"image data URL of no media type", part(protocol.InputImage{ImageURL: "data:;base64,iVBORw0KGgo="}),
			"image data URL media type", "",
			"messages: input 0: content 0: image data URL has no media type, which a base64 source requires"},
		{"tool choice", choice,
			"tool choice", "messages_test.unknownChoice",
			"messages: tool choice messages_test.unknownChoice has no Messages form"},
		{"text format", textFormat,
			"text format", "messages_test.unknownFormat",
			"messages: text format messages_test.unknownFormat has no Messages form"},
	}
	f := messages.New(messages.Options{})
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
					Format: "messages", Feature: tc.feature, Value: tc.value,
				}, tc.message)
			})
		}
	}
}

// TestEncodeMalformedDataURLIsNotUnsupported wants an image data URL of no data, which is
// malformed rather than a feature Messages has no form for, refused with an error that isn't a
// *format.UnsupportedError.
func TestEncodeMalformedDataURLIsNotUnsupported(t *testing.T) {
	req := ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
		protocol.InputImage{ImageURL: "data:image/png;base64"},
	}})
	_, err := messages.New(messages.Options{}).EncodeRequest(req, make(http.Header))
	if want := "messages: input 0: content 0: image data URL has no data"; err == nil || err.Error() != want {
		t.Fatalf("EncodeRequest error = %v, want %q", err, want)
	}
	if _, ok := errors.AsType[*format.UnsupportedError](err); ok {
		t.Errorf("EncodeRequest error %q is a *format.UnsupportedError, want an error of a malformed value", err)
	}
}
