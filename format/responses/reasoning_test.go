package responses_test

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

var call = protocol.FunctionCall{CallID: "call_1", Name: "get_weather", Arguments: `{"city":"Paris"}`}

const callJSON = `{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}`

var answer = protocol.Message{Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "17"}}}

const answerJSON = `{"type":"message","role":"assistant","content":[{"type":"output_text","text":"17"}]}`

var callOutput = protocol.FunctionCallOutput{CallID: "call_1", Output: "17"}

const callOutputJSON = `{"type":"function_call_output","call_id":"call_1","output":"17"}`

type reasoningCase struct {
	name  string
	input []protocol.Item
	want  string
}

func runReasoning(t *testing.T, opts responses.Options, cases []reasoningCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aitest.AssertJSON(t, encodeWith(t, opts, ask(tc.input...)), `{"model":"m",`+stateless+`,"input":`+tc.want+`}`)
		})
	}
}

func TestEncodeReasoningSpecForm(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	cases := []reasoningCase{
		{
			name: "id, summary, and the server's encrypted_content; the texts and Content never sent",
			input: []protocol.Item{user, protocol.Reasoning{
				ID:               "rs_1",
				Status:           protocol.StatusCompleted,
				Content:          []protocol.ReasoningText{{Text: "never sent"}},
				Summary:          []protocol.SummaryText{{Text: "Looked."}, {Text: "Called."}},
				EncryptedContent: seal(t, "enc", "hm"),
			}, call},
			want: `[` + userHi + `,
				{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Looked."},{"type":"summary_text","text":"Called."}],
					"encrypted_content":"enc"},` + callJSON + `]`,
		},
		{
			name: "no id and no summary, its summary an empty array",
			input: []protocol.Item{user, protocol.Reasoning{
				EncryptedContent: "go-ai:responses/v1:eyJlbmNyeXB0ZWRfY29udGVudCI6ImVuYyIsImNvbnRlbnQiOlsiaG0iXX0=",
			}, answer},
			want: `[` + userHi + `,{"type":"reasoning","summary":[],"encrypted_content":"enc"},` + answerJSON + `]`,
		},
		{
			name:  "lone reasoning goes back as it came",
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: seal(t, "enc")}},
			want:  `[` + userHi + `,{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"enc"}]`,
		},
		{
			name: "reasoning ahead of a user message goes back where it stands",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc")}, call, callOutput,
				protocol.Reasoning{EncryptedContent: seal(t, "enc2")}, text(protocol.RoleUser, "hi")},
			want: `[` + userHi + `,{"type":"reasoning","summary":[],"encrypted_content":"enc"},` + callJSON + `,` + callOutputJSON + `,
				{"type":"reasoning","summary":[],"encrypted_content":"enc2"},` + userHi + `]`,
		},
		{
			name:  "spec form drops the router's reasoning (empty encrypted_content)",
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: seal(t, "", "User asks.")}, call},
			want:  `[` + userHi + `,` + callJSON + `]`,
		},
	}
	cases = append(cases, dropCases(t, user)...)
	runReasoning(t, responses.Options{}, cases)
}

func TestEncodeReasoningResendText(t *testing.T) {
	user := text(protocol.RoleUser, "hi")
	cases := []reasoningCase{
		{
			name:  "the router's reasoning ahead of a call: content, no encrypted_content",
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: seal(t, "", "User asks.")}, call},
			want: `[` + userHi + `,{"type":"reasoning","id":"rs_1","summary":[],
				"content":[{"type":"reasoning_text","text":"User asks."}]},` + callJSON + `]`,
		},
		{
			name: "texts and encrypted_content ahead of an assistant message, a part per text, the summary kept",
			input: []protocol.Item{user, protocol.Reasoning{
				Content:          []protocol.ReasoningText{{Text: "never sent"}},
				Summary:          []protocol.SummaryText{{Text: "Looked."}},
				EncryptedContent: seal(t, "enc", "hm", "ok"),
			}, answer},
			want: `[` + userHi + `,{"type":"reasoning","summary":[{"type":"summary_text","text":"Looked."}],
				"content":[{"type":"reasoning_text","text":"hm"},{"type":"reasoning_text","text":"ok"}],
				"encrypted_content":"enc"},` + answerJSON + `]`,
		},
		{
			name:  "encrypted_content alone, no content",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc")}, call},
			want:  `[` + userHi + `,{"type":"reasoning","summary":[],"encrypted_content":"enc"},` + callJSON + `]`,
		},
		{
			name: "a run of reasoning ahead of a call, a dropped item within it",
			input: []protocol.Item{user,
				protocol.Reasoning{EncryptedContent: seal(t, "", "a")},
				protocol.Reasoning{EncryptedContent: "foreign"},
				protocol.Reasoning{EncryptedContent: seal(t, "", "b")},
				call},
			want: `[` + userHi + `,
				{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"a"}]},
				{"type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"b"}]},` + callJSON + `]`,
		},
		{
			name:  "an envelope of neither text nor encrypted_content dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "")}, call},
			want:  `[` + userHi + `,` + callJSON + `]`,
		},
		{
			name:  "lone reasoning dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")}},
			want:  `[` + userHi + `]`,
		},
		{
			name:  "reasoning ahead of a user message dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")}, user},
			want:  `[` + userHi + `,` + userHi + `]`,
		},
		{
			name:  "reasoning ahead of a function_call_output dropped",
			input: []protocol.Item{user, call, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")}, callOutput},
			want:  `[` + userHi + `,` + callJSON + `,` + callOutputJSON + `]`,
		},
		{
			name: "reasoning ahead of a system message dropped",
			input: []protocol.Item{user, protocol.Reasoning{EncryptedContent: seal(t, "enc", "hm")},
				text(protocol.RoleSystem, "s")},
			want: `[` + userHi + `,{"type":"message","role":"system","content":[{"type":"input_text","text":"s"}]}]`,
		},
	}
	cases = append(cases, dropCases(t, user)...)
	runReasoning(t, responses.Options{ResendReasoningText: true}, cases)
}

func dropCases(t *testing.T, user protocol.Message) []reasoningCase {
	t.Helper()
	cases := []struct{ name, encrypted string }{
		{"no envelope dropped", ""},
		{"another format's envelope dropped", "go-ai:messages/v1:eyJkYXRhIjoieCJ9"},
		{"another version's envelope dropped", "go-ai:responses/v0:" + seal(t, "enc", "hm")[len("go-ai:responses/v1:"):]},
		{"another implementor's envelope dropped", "other:responses/v1:" + seal(t, "enc", "hm")[len("go-ai:responses/v1:"):]},
		{"an opaque server value dropped", "gAAAAABopaque"},
		{"an envelope not base64 dropped", "go-ai:responses/v1:not base64!"},
		{"an envelope of a member missing dropped", "go-ai:responses/v1:eyJlbmNyeXB0ZWRfY29udGVudCI6ImVuYyJ9"},
		{"an envelope not an object dropped", "go-ai:responses/v1:WzFd"},
	}
	out := make([]reasoningCase, 0, len(cases))
	for _, c := range cases {
		out = append(out, reasoningCase{
			name:  c.name,
			input: []protocol.Item{user, protocol.Reasoning{ID: "rs_1", EncryptedContent: c.encrypted}, call},
			want:  `[` + userHi + `,` + callJSON + `]`,
		})
	}
	return out
}

func TestReasoningRoundTripRecorded(t *testing.T) {
	cases := []struct {
		name                                  string
		reply                                 string
		question                              string
		follow                                protocol.Item
		reasoning                             string
		reasoningJSON, answerJSON, followJSON string
	}{
		{
			name:      "llama.cpp router",
			reply:     "tool_call.json",
			question:  "What is the weather in Paris right now?",
			follow:    protocol.FunctionCallOutput{CallID: "call_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc", Output: `{"celsius":17}`},
			reasoning: "User asks current weather in Paris. Use function get_weather.",
			reasoningJSON: `{"type":"reasoning","id":"rs_H4pfaCFvrGf1ljLr7M56OB81gR3sEvYK","summary":[],
				"content":[{"type":"reasoning_text","text":"User asks current weather in Paris. Use function get_weather."}]}`,
			answerJSON: `{"type":"function_call","id":"fc_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc","call_id":"call_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc",
				"name":"get_weather","arguments":"{\n  \"city\": \"Paris\"\n}"}`,
			followJSON: `{"type":"function_call_output","call_id":"call_HMhSOwdRkvjir1D59ezHFChk5pTnYdJc","output":"{\"celsius\":17}"}`,
		},
		{
			name:      "vLLM 0.34.0",
			reply:     "vllm-0.34.0/reasoning_text.json",
			question:  "Name three French cities, comma-separated, nothing else.",
			follow:    text(protocol.RoleUser, "And three German ones?"),
			reasoning: "We just output three French cities separated by commas.",
			reasoningJSON: `{"type":"reasoning","id":"rs_8e31363d3477e577","summary":[],
				"content":[{"type":"reasoning_text","text":"We just output three French cities separated by commas."}]}`,
			answerJSON: `{"type":"message","id":"msg_a78b6a7988ed022b","role":"assistant",
				"content":[{"type":"output_text","text":"Paris, Lyon, Marseille"}]}`,
			followJSON: `{"type":"message","role":"user","content":[{"type":"input_text","text":"And three German ones?"}]}`,
		},
	}
	for _, tc := range cases {
		question := text(protocol.RoleUser, tc.question)
		questionJSON, err := json.Marshal(tc.question)
		if err != nil {
			t.Fatal(err)
		}
		head := `{"model":"m",` + stateless + `,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":` +
			string(questionJSON) + `}]},`
		profiles := []struct {
			name string
			opts responses.Options
			want string
		}{
			{"ResendReasoningText", responses.Options{ResendReasoningText: true},
				head + tc.reasoningJSON + `,` + tc.answerJSON + `,` + tc.followJSON + `]}`},
			{"spec form", responses.Options{}, head + tc.answerJSON + `,` + tc.followJSON + `]}`},
		}
		for _, p := range profiles {
			t.Run(tc.name+"/"+p.name, func(t *testing.T) {
				srv := serve(t, http.StatusOK, string(aitest.ReadTestdata(t, tc.reply)))
				c := newProfileClient(t, srv.URL(), p.opts, &aitest.TokenCounter{})
				resp, err := c.Generate(t.Context(), ask(question))
				if err != nil {
					t.Fatalf("Generate: %v", err)
				}
				if rs, ok := resp.Output[0].(protocol.Reasoning); !ok || len(rs.Content) != 1 || rs.Content[0].Text != tc.reasoning {
					t.Errorf("Output[0] = %#v, want the reasoning %q as its content", resp.Output[0], tc.reasoning)
				}
				next := append(append([]protocol.Item{question}, resp.Output...), tc.follow)
				if _, err := c.Generate(t.Context(), ask(next...)); err != nil {
					t.Fatalf("Generate, next turn: %v", err)
				}
				reqs := srv.Requests()
				if len(reqs) != 2 {
					t.Fatalf("server received %d requests, want 2", len(reqs))
				}
				aitest.AssertJSON(t, reqs[1].Body, p.want)
			})
		}
	}
}
