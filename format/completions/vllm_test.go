package completions_test

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/protocol"
)

// sealReasoning returns the envelope the format writes for reasoning_content r. The envelope's
// literal is pinned by the envelope constants; this spells it for the recorded replies'
// reasoning, which is too long to pin by hand.
func sealReasoning(t *testing.T, r string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"reasoning_content": r})
	if err != nil {
		t.Fatal(err)
	}
	return "go-ai:completions/v1:" + base64.StdEncoding.EncodeToString(b)
}

// sameArguments sets each function call of want to the arguments of got's in its place, failing
// t unless the two are the same JSON value: vLLM parses a call's arguments and writes them back
// in an unstreamed reply, and streams the model's own text, its spacing included.
func sameArguments(t *testing.T, got, want *protocol.Response) {
	t.Helper()
	if len(got.Output) != len(want.Output) {
		return
	}
	for i, it := range want.Output {
		w, ok := it.(protocol.FunctionCall)
		g, ok2 := got.Output[i].(protocol.FunctionCall)
		if !ok || !ok2 {
			continue
		}
		gv, wv := jsontext.Value(g.Arguments), jsontext.Value(w.Arguments)
		if gv.Canonicalize() != nil || wv.Canonicalize() != nil || string(gv) != string(wv) {
			t.Errorf("output %d's arguments %q, want the JSON of %q", i, g.Arguments, w.Arguments)
			continue
		}
		w.Arguments = g.Arguments
		want.Output[i] = w
	}
}

// The reasoning of the vLLM replies recorded in testdata/vllm.
const (
	vllmCitiesReasoning  = "Need to output three French cities, comma-separated, nothing else."
	vllmWeatherReasoning = "We need to call function get_weather."
)

// TestDecodeVLLM decodes the vLLM replies recorded in testdata/vllm, whose reasoning is in
// vLLM's reasoning member.
func TestDecodeVLLM(t *testing.T) {
	message := func(id, s string) protocol.Message {
		return protocol.Message{
			ID:      id,
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: []protocol.ContentPart{protocol.OutputText{Text: s}},
		}
	}
	reasoning := func(id, s string) protocol.Reasoning {
		return protocol.Reasoning{
			ID:               id,
			Status:           protocol.StatusCompleted,
			Content:          []protocol.ReasoningText{{Text: s}},
			EncryptedContent: sealReasoning(t, s),
		}
	}
	cases := []struct {
		file string
		want *protocol.Response
	}{
		{
			// A message alone: Gemma's reasoning is null.
			file: "text.json",
			want: &protocol.Response{
				Output: []protocol.Item{message("chatcmpl-82796f247e0d0dae/0", "Paris, Lyon, Marseille")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 25, OutputTokens: 6, TotalTokens: 31},
			},
		},
		{
			file: "reasoning.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("chatcmpl-aa5f6f9517187b26/0", vllmCitiesReasoning),
					message("chatcmpl-aa5f6f9517187b26/1", "Paris, Lyon, Marseille"),
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 78, OutputTokens: 28, TotalTokens: 106,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 13}},
			},
		},
		{
			// The content is empty, which gives no message, and the call's arguments are the
			// JSON vLLM wrote back.
			file: "tool_call.json",
			want: &protocol.Response{
				Output: []protocol.Item{
					reasoning("chatcmpl-828cc59b58a5c335/0", vllmWeatherReasoning),
					protocol.FunctionCall{
						ID:        "chatcmpl-828cc59b58a5c335/1",
						Status:    protocol.StatusCompleted,
						CallID:    "chatcmpl-tool-b7d2dfa98b1aa945",
						Name:      "get_weather",
						Arguments: `{"city": "Paris"}`,
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
					reasoning("chatcmpl-bde98257390e0f4e/0", "Simple."),
					message("chatcmpl-bde98257390e0f4e/1", "{\n  \"name\": \"Paris\"\n}"),
				},
				Status: protocol.StatusCompleted,
				Usage: protocol.Usage{InputTokens: 73, OutputTokens: 24, TotalTokens: 97,
					OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 2}},
			},
		},
		{
			file: "image.json",
			want: &protocol.Response{
				Output: []protocol.Item{message("chatcmpl-8794636c5d1e5203/0", "Red")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 282, OutputTokens: 2, TotalTokens: 284},
			},
		},
		{
			file: "input_audio.json",
			want: &protocol.Response{
				Output: []protocol.Item{message("chatcmpl-8af898a1110dcfce/0", "It sounds like a gentle, rhythmic tapping.")},
				Status: protocol.StatusCompleted,
				Usage:  protocol.Usage{InputTokens: 44, OutputTokens: 10, TotalTokens: 54},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			resp, err := decode(readTestdata(t, "vllm/"+tc.file))
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
// ids and its calls' arguments text.
func TestStreamVLLM(t *testing.T) {
	cases := []struct {
		name      string
		reasoning string
	}{
		{"text", ""},
		{"reasoning", vllmCitiesReasoning},
		{"tool_call", vllmWeatherReasoning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := readTestdata(t, "vllm/"+tc.name+".sse")
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			if _, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted); !ok {
				t.Fatalf("last event = %#v, want response.completed", r.events[len(r.events)-1])
			}
			got := fold(t, r.events)
			if tc.reasoning != "" {
				if rs, ok := got.Output[0].(protocol.Reasoning); !ok || rs.Content[0].Text != tc.reasoning {
					t.Errorf("Output[0] = %#v, want the reasoning %q", got.Output[0], tc.reasoning)
				}
			}
			want, err := decode(asStreamed(t, readTestdata(t, "vllm/"+tc.name+".json"), body))
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			sameArguments(t, got, want)
			assertResponse(t, got, want)
		})
	}
}

// TestDecodeErrorVLLM decodes vLLM's 404 body for an unknown model, an OpenAI-style error of a
// numeric code.
func TestDecodeErrorVLLM(t *testing.T) {
	message, typ, code := completions.New().DecodeError([]byte(readTestdata(t, "vllm/not_found.json")))
	const want = "The model `go-ai/no-such-model` does not exist. Valid aliases: openai/gpt-oss-120b."
	if message != want || typ != "NotFoundError" || code != "404" {
		t.Errorf("DecodeError = {%q %q %q}, want {%q %q %q}", message, typ, code, want, "NotFoundError", "404")
	}
}
