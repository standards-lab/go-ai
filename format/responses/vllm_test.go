package responses_test

import (
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
)

// The reasoning of the vLLM replies recorded in testdata/vllm.
const (
	vllmCitiesReasoning  = "Need to output three French cities, comma-separated, nothing else."
	vllmWeatherReasoning = "We need to call function get_weather."
)

// TestDecodeVLLM decodes the vLLM replies recorded in testdata/vllm. vLLM gives a reasoning
// item a null status and a null encrypted_content, which decode as empty: the item's status is
// the server's, and the envelope holds an empty encrypted_content and the reasoning text.
func TestDecodeVLLM(t *testing.T) {
	message := func(id string, status protocol.Status, s string) protocol.Message {
		return protocol.Message{
			ID:      id,
			Status:  status,
			Role:    protocol.RoleAssistant,
			Content: []protocol.ContentPart{protocol.OutputText{Text: s}},
		}
	}
	reasoning := func(id, s string) protocol.Reasoning {
		return protocol.Reasoning{
			ID:               id,
			Content:          []protocol.ReasoningText{{Text: s}},
			EncryptedContent: seal(t, "", s),
		}
	}
	cases := []struct {
		file string
		want *protocol.Response
	}{
		{
			file: "text.json",
			want: &protocol.Response{
				Output: []protocol.Item{message("msg_830f9ee65cbe4ece", protocol.StatusCompleted, "Paris, Lyon, Marseille")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 25, OutputTokens: 6, TotalTokens: 31},
			},
		},
		{
			file: "reasoning.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("rs_aa8461d746adb539", vllmCitiesReasoning),
					message("msg_a59a9b5ccca1bb9f", protocol.StatusCompleted, "Paris, Lyon, Marseille"),
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 78, OutputTokens: 28, TotalTokens: 106,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 13}},
			},
		},
		{
			file: "tool_call.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("rs_82377d4907887a4d", vllmWeatherReasoning),
					protocol.FunctionCall{
						ID:        "fc_bdc2beac5a90b8a1",
						Status:    protocol.StatusCompleted,
						CallID:    "call_bdc2beac5a90b8a1",
						Name:      "get_weather",
						Arguments: "{\n  \"city\": \"Paris\"\n}",
					},
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 134, OutputTokens: 36, TotalTokens: 170,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 17}},
			},
		},
		{
			file: "structured.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("rs_9d945646a40c4fae", "Simple."),
					message("msg_a8252ac23b460f33", protocol.StatusCompleted, "{\n  \"name\": \"Paris\"\n}"),
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 73, OutputTokens: 24, TotalTokens: 97,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 2}},
			},
		},
		{
			// Stopped at max_output_tokens, which vLLM reports, unlike llama.cpp's router:
			// the response and its message incomplete.
			file: "length.json",
			want: &protocol.Response{
				Output:            []protocol.Item{message("msg_87b77c8f5d9a7dd0", protocol.StatusIncomplete, "1 2 3 4 ")},
				Status:            protocol.StatusIncomplete,
				IncompleteDetails: &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens},
				Usage:             protocol.Usage{InputTokens: 26, OutputTokens: 8, TotalTokens: 34},
			},
		},
		{
			file: "image.json",
			want: &protocol.Response{
				Output: []protocol.Item{message("msg_92710856b6965835", protocol.StatusCompleted, "Red")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 282, OutputTokens: 2, TotalTokens: 284},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			resp, err := decode(string(readTestdata(t, "vllm/"+tc.file)))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("DecodeResponse = %#v\nwant            %#v", resp, tc.want)
			}
		})
	}
}

// TestStreamVLLM translates the vLLM streams recorded in testdata/vllm, which carry every
// event's sequence_number, output_index, and content_index, and folds them to the Response
// DecodeResponse gives for the terminal event's response, its reasoning item, of null status
// there, completed as its output_item.done gives it.
func TestStreamVLLM(t *testing.T) {
	cases := []struct {
		name      string
		events    int
		terminal  protocol.Event
		reasoning string
	}{
		{"text", 12, protocol.ResponseCompleted{}, ""},
		{"reasoning", 30, protocol.ResponseCompleted{}, vllmCitiesReasoning},
		{"tool_call", 27, protocol.ResponseCompleted{}, vllmWeatherReasoning},
		{"length", 15, protocol.ResponseIncomplete{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := string(readTestdata(t, "vllm/"+tc.name+".sse"))
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			// Every event vLLM sent but response.in_progress makes one, nothing filled in.
			if len(r.events) != tc.events {
				t.Errorf("%d events, want %d:\n%s", len(r.events), tc.events, trace(r.events))
			}
			last := r.events[len(r.events)-1]
			if reflect.TypeOf(last) != reflect.TypeOf(tc.terminal) {
				t.Fatalf("last event = %#v, want a %T", last, tc.terminal)
			}
			want := terminalResponse(t, body)
			got := fold(t, r.events)
			if tc.reasoning != "" {
				if rs, ok := got.Output[0].(protocol.Reasoning); !ok || rs.Content[0].Text != tc.reasoning {
					t.Errorf("Output[0] = %#v, want the reasoning %q", got.Output[0], tc.reasoning)
				}
			}
			assertResponse(t, got, want)
			assertDoneItems(t, r.events, want)
		})
	}
}

// TestDecodeErrorVLLM decodes vLLM's 404 body for an unknown model, an OpenAI-style error of a
// numeric code.
func TestDecodeErrorVLLM(t *testing.T) {
	message, typ, code := responses.New(responses.Options{}).DecodeError(readTestdata(t, "vllm/not_found.json"))
	const want = "The model `go-ai/no-such-model` does not exist. Valid aliases: openai/gpt-oss-120b."
	if message != want || typ != "NotFoundError" || code != "404" {
		t.Errorf("DecodeError = {%q %q %q}, want {%q %q %q}", message, typ, code, want, "NotFoundError", "404")
	}
}
