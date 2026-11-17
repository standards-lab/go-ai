package completions_test

import (
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// The reasoning of the gpt-oss-120b replies recorded in testdata/foundry, unstreamed and
// streamed. Foundry's two replies to one request differ, even at temperature 0 and seed 7.
const (
	foundryCitiesReasoning       = "Need to output three French cities, comma-separated, nothing else."
	foundryCitiesStreamReasoning = "We just output three French cities separated by commas."
	foundryLengthReasoning       = "The user asks: \"Name three French cities, comma-separated,"
)

// TestDecodeFoundry decodes the Azure AI Foundry replies recorded in testdata/foundry. gpt-5-mini
// gives no reasoning text in Chat Completions, only its count; gpt-oss-120b gives its reasoning
// as reasoning_content, and its usage's reasoning_tokens beside completion_tokens rather than in
// completion_tokens_details, which the format doesn't read, so its reasoning tokens decode as 0.
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
				Output: []protocol.Item{message("chatcmpl-ElD2RajNC7lOfavEoT2LWVuR7np9O/0", "Paris, Lyon, Marseille")},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 17, OutputTokens: 142, TotalTokens: 159,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 128}},
			},
		},
		{
			file: "reasoning.json",
			want: &protocol.Response{
				Output: []protocol.Item{message("chatcmpl-ElD2Vp1SbMDykZjWRiP2okbaIsm9B/0", "Paris, Lyon, Marseille")},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 17, OutputTokens: 78, TotalTokens: 95,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 64}},
			},
		},
		{
			file: "oss_reasoning.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("6fa1f6f05ae2411c932c26011d5474a7/0", protocol.StatusCompleted, foundryCitiesReasoning),
					message("6fa1f6f05ae2411c932c26011d5474a7/1", "Paris, Lyon, Marseille"),
				},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 78, OutputTokens: 28, TotalTokens: 106},
			},
		},
		{
			// Stopped at max_completion_tokens 16 in the reasoning: the content is empty, which
			// gives no message, and the reasoning is incomplete.
			file: "oss_length.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("79d4bb81100a4cf1afc6a40330a9b0ab/0", protocol.StatusIncomplete, foundryLengthReasoning),
				},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
				Usage:             protocol.Usage{InputTokens: 78, OutputTokens: 16, TotalTokens: 94},
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

// TestStreamFoundry translates the Azure AI Foundry streams recorded in testdata/foundry to
// events, and folds them to the Response DecodeResponse gives for the same request unstreamed,
// given the stream's ids, its reasoning text, and its usage. Foundry's two replies to one request
// differ in their reasoning and their token counts, so those are pinned from the stream.
//
// gpt-5-mini's streams open with a chunk of an empty id and no choices, its prompt's content
// filter results, and the decoder takes the stream's id from that chunk, so the next chunk's id
// fails it. Until the decoder reads that chunk, those cases assert the error, and the fix turns
// them into the comparison.
func TestStreamFoundry(t *testing.T) {
	cases := []struct {
		name string
		// err is the error the stream fails with, empty when it decodes.
		err       string
		reasoning string
		usage     protocol.Usage
	}{
		{
			name:  "text",
			err:   `completions: stream: chunk of reply "chatcmpl-ElD2Uc6V9huK5IIhxiDepA1YOEdWQ" in the stream of reply ""`,
			usage: protocol.Usage{InputTokens: 17, OutputTokens: 78, TotalTokens: 95, OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 64}},
		},
		{
			name:  "reasoning",
			err:   `completions: stream: chunk of reply "chatcmpl-ElD2XFRN5GI9qBkwXxP9iQah4s8q8" in the stream of reply ""`,
			usage: protocol.Usage{InputTokens: 17, OutputTokens: 14, TotalTokens: 31},
		},
		{
			name:      "oss_reasoning",
			reasoning: foundryCitiesStreamReasoning,
			usage:     protocol.Usage{InputTokens: 78, OutputTokens: 25, TotalTokens: 103},
		},
		{
			name:      "oss_length",
			reasoning: foundryLengthReasoning,
			usage:     protocol.Usage{InputTokens: 78, OutputTokens: 16, TotalTokens: 94},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := readTestdata(t, "foundry/"+tc.name+".sse")
			r := decodeStream(t, body)
			if tc.err != "" {
				if r.err == nil || r.err.Error() != tc.err {
					t.Fatalf("stream error = %v, want %q", r.err, tc.err)
				}
				return
			}
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
			aitest.AssertResponse(t, got, want)
		})
	}
}
