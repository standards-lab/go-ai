package messages_test

import (
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/protocol"
)

// The thinking of the vLLM replies recorded in testdata/vllm.
const (
	vllmCitiesThinking  = "Need to output three French cities, comma-separated, nothing else."
	vllmWeatherThinking = "We need to call function get_weather."
)

// TestDecodeVLLM decodes the vLLM replies recorded in testdata/vllm. vLLM signs each thinking
// block, which the envelope keeps, and reports no cache counts.
func TestDecodeVLLM(t *testing.T) {
	message := func(id, s string) protocol.Message {
		return protocol.Message{
			ID:      id,
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: []protocol.ContentPart{protocol.OutputText{Text: s}},
		}
	}
	reasoning := func(id, thinking, signature string) protocol.Reasoning {
		return protocol.Reasoning{
			ID:               id,
			Status:           protocol.StatusCompleted,
			Content:          []protocol.ReasoningText{{Text: thinking}},
			EncryptedContent: seal(`{"thinking":` + quote(thinking) + `,"signature":` + quote(signature) + `}`),
		}
	}
	cases := []struct {
		file string
		want *protocol.Response
	}{
		{
			file: "text.json",
			want: &protocol.Response{
				Output: []protocol.Item{message("chatcmpl-93cdbdc1b27eb8c2/0", "Paris, Lyon, Marseille")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 25, OutputTokens: 6, TotalTokens: 31},
			},
		},
		{
			file: "reasoning.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("chatcmpl-a27537d77e8fb570/0", vllmCitiesThinking, "cd03ad08121346fa9164bc3aabab3445"),
					message("chatcmpl-a27537d77e8fb570/1", "Paris, Lyon, Marseille"),
				},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 78, OutputTokens: 28, TotalTokens: 106},
			},
		},
		{
			file: "tool_use.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("chatcmpl-be61c70f8fb2ff38/0", vllmWeatherThinking, "9a35add13ed9412d9dbb5b1d7815cd2e"),
					protocol.FunctionCall{
						ID:        "chatcmpl-be61c70f8fb2ff38/1",
						Status:    protocol.StatusCompleted,
						CallID:    "chatcmpl-tool-938d5a210506a167",
						Name:      "get_weather",
						Arguments: `{"city":"Paris"}`,
					},
				},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 134, OutputTokens: 36, TotalTokens: 170},
			},
		},
		{
			file: "structured.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("chatcmpl-aab14de85610a507/0", "Simple.", "0f788c623ffd4ec5a59e0eb5000b5bd3"),
					message("chatcmpl-aab14de85610a507/1", "{\n  \"name\": \"Paris\"\n}"),
				},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 73, OutputTokens: 24, TotalTokens: 97},
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

// TestStreamVLLM translates the vLLM streams recorded in testdata/vllm to events, and folds
// them to the Response DecodeResponse gives for the same request unstreamed, given the stream's
// ids and thinking signature and its calls' arguments text. vLLM stops each content block before
// the next begins, as Anthropic does.
func TestStreamVLLM(t *testing.T) {
	cases := []struct {
		name     string
		thinking string
	}{
		{"text", ""},
		{"reasoning", vllmCitiesThinking},
		{"tool_use", vllmWeatherThinking},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := string(readTestdata(t, "vllm/"+tc.name+".sse"))
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			if _, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted); !ok {
				t.Fatalf("last event = %#v, want response.completed", r.events[len(r.events)-1])
			}
			got := fold(t, r.events)
			if tc.thinking != "" {
				if rs, ok := got.Output[0].(protocol.Reasoning); !ok || rs.Content[0].Text != tc.thinking {
					t.Errorf("Output[0] = %#v, want the reasoning %q", got.Output[0], tc.thinking)
				}
			}
			want, err := decode(asStreamed(t, string(readTestdata(t, "vllm/"+tc.name+".json")), body))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			sameArguments(t, got, want)
			assertResponse(t, got, want)
		})
	}
}

// TestDecodeErrorVLLM decodes vLLM's 404 body for an unknown model, Anthropic's error shape.
func TestDecodeErrorVLLM(t *testing.T) {
	message, typ, code := messages.New().DecodeError(readTestdata(t, "vllm/not_found.json"))
	const want = "The model `go-ai/no-such-model` does not exist. Valid aliases: openai/gpt-oss-120b."
	if message != want || typ != "not_found_error" || code != "" {
		t.Errorf("DecodeError = {%q %q %q}, want {%q %q %q}", message, typ, code, want, "not_found_error", "")
	}
}
