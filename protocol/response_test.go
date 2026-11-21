package protocol_test

import (
	"testing"

	"github.com/standards-lab/go-ai/protocol"
)

func assistant(parts ...protocol.ContentPart) protocol.Message {
	return protocol.Message{Role: protocol.RoleAssistant, Content: parts}
}

func TestResponseOutputText(t *testing.T) {
	cases := []struct {
		name   string
		output []protocol.Item
		want   string
	}{
		{"nil output", nil, ""},
		{"one part", []protocol.Item{
			assistant(protocol.OutputText{Text: "Paris."}),
		}, "Paris."},
		{"parts of one message joined with nothing between", []protocol.Item{
			assistant(protocol.OutputText{Text: "Par"}, protocol.OutputText{Text: "is."}),
		}, "Paris."},
		{"text split around a reasoning item", []protocol.Item{
			assistant(protocol.OutputText{Text: "The capital "}),
			protocol.Reasoning{
				Content: []protocol.ReasoningText{{Text: "thinking"}},
				Summary: []protocol.SummaryText{{Text: "thought"}},
			},
			assistant(protocol.OutputText{Text: "is Paris."}),
		}, "The capital is Paris."},
		{"several messages in order", []protocol.Item{
			assistant(protocol.OutputText{Text: "one "}),
			assistant(protocol.OutputText{Text: "two "}),
			assistant(protocol.OutputText{Text: "three"}),
		}, "one two three"},
		{"refusal parts excluded", []protocol.Item{
			assistant(protocol.OutputText{Text: "I can say "}, protocol.Refusal{Refusal: "no"},
				protocol.OutputText{Text: "this."}),
		}, "I can say this."},
		{"refusal alone is empty", []protocol.Item{
			assistant(protocol.Refusal{Refusal: "I can't help with that."}),
		}, ""},
		{"function calls excluded", []protocol.Item{
			assistant(protocol.OutputText{Text: "Checking."}),
			protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
		}, "Checking."},
		{"function call alone is empty", []protocol.Item{
			protocol.FunctionCall{CallID: "c1", Name: "get_weather", Arguments: `{}`},
		}, ""},
		{"no role filter", []protocol.Item{
			protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.OutputText{Text: "any role "},
			}},
			assistant(protocol.OutputText{Text: "counts"}),
		}, "any role counts"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := &protocol.Response{Output: c.output}
			if got := resp.OutputText(); got != c.want {
				t.Errorf("OutputText() = %q, want %q", got, c.want)
			}
		})
	}
}
