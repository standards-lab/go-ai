package completions_test

import (
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	foundryCitiesReasoning       = "Need to output three French cities, comma-separated, nothing else."
	foundryCitiesStreamReasoning = "We just output three French cities separated by commas."
	foundryLengthReasoning       = "The user asks: \"Name three French cities, comma-separated,"
)

func TestDecodeFoundry(t *testing.T) {
	message := func(id, s string) protocol.Message {
		return protocol.Message{
			ID:      id,
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: []protocol.ContentPart{protocol.OutputText{Text: s}},
		}
	}
	reasoning := func(id string, status protocol.Status, s string) protocol.Reasoning {
		return protocol.Reasoning{
			ID:               id,
			Status:           status,
			Content:          []protocol.ReasoningText{{Text: s}},
			EncryptedContent: sealReasoning(t, s),
		}
	}
	cases := []struct {
		file string
		want *protocol.Response
	}{
		{
			file: "text.json",
			want: &protocol.Response{
				ID:     "chatcmpl-ElD2RajNC7lOfavEoT2LWVuR7np9O",
				Model:  "gpt-5-mini-2025-08-07",
				Output: []protocol.Item{message("chatcmpl-ElD2RajNC7lOfavEoT2LWVuR7np9O/0", "Paris, Lyon, Marseille")},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 17, OutputTokens: 142, TotalTokens: 159,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 128}},
			},
		},
		{
			file: "reasoning.json",
			want: &protocol.Response{
				ID:     "chatcmpl-ElD2Vp1SbMDykZjWRiP2okbaIsm9B",
				Model:  "gpt-5-mini-2025-08-07",
				Output: []protocol.Item{message("chatcmpl-ElD2Vp1SbMDykZjWRiP2okbaIsm9B/0", "Paris, Lyon, Marseille")},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 17, OutputTokens: 78, TotalTokens: 95,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 64}},
			},
		},
		{
			file: "oss_reasoning.json",
			want: &protocol.Response{
				ID:    "6fa1f6f05ae2411c932c26011d5474a7",
				Model: "gpt-oss-120b",
				Output: []protocol.Item{
					reasoning("6fa1f6f05ae2411c932c26011d5474a7/0", protocol.StatusCompleted, foundryCitiesReasoning),
					message("6fa1f6f05ae2411c932c26011d5474a7/1", "Paris, Lyon, Marseille"),
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 78, OutputTokens: 28, TotalTokens: 106,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 17}},
			},
		},
		{
			file: "oss_length.json",
			want: &protocol.Response{
				ID:    "79d4bb81100a4cf1afc6a40330a9b0ab",
				Model: "gpt-oss-120b",
				Output: []protocol.Item{
					reasoning("79d4bb81100a4cf1afc6a40330a9b0ab/0", protocol.StatusIncomplete, foundryLengthReasoning),
				},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
				Usage: protocol.Usage{InputTokens: 78, OutputTokens: 16, TotalTokens: 94,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 18}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			resp, err := decode(readTestdata(t, "foundry/"+tc.file))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("DecodeResponse = %#v\nwant            %#v", resp, tc.want)
			}
		})
	}
}

func TestStreamFoundry(t *testing.T) {
	cases := []struct {
		name      string
		reasoning string
		usage     protocol.Usage
	}{
		{
			name:  "text",
			usage: protocol.Usage{InputTokens: 17, OutputTokens: 78, TotalTokens: 95, OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 64}},
		},
		{
			name:  "reasoning",
			usage: protocol.Usage{InputTokens: 17, OutputTokens: 14, TotalTokens: 31},
		},
		{
			name:      "oss_reasoning",
			reasoning: foundryCitiesStreamReasoning,
			usage:     protocol.Usage{InputTokens: 78, OutputTokens: 25, TotalTokens: 103, OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 14}},
		},
		{
			name:      "oss_length",
			reasoning: foundryLengthReasoning,
			usage:     protocol.Usage{InputTokens: 78, OutputTokens: 16, TotalTokens: 94, OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 16}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := readTestdata(t, "foundry/"+tc.name+".sse")
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			got := aitest.Fold(t, r.events)
			want, err := decode(asStreamed(t, readTestdata(t, "foundry/"+tc.name+".json"), body))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			for i, it := range want.Output {
				if rs, ok := it.(protocol.Reasoning); ok {
					rs.Content = []protocol.ReasoningText{{Text: tc.reasoning}}
					rs.EncryptedContent = sealReasoning(t, tc.reasoning)
					want.Output[i] = rs
				}
			}
			want.Usage = tc.usage
			assertCreated(t, r.events, want)
			aitest.AssertResponse(t, got, want)
		})
	}
}
