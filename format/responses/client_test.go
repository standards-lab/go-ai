package responses_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// The tests here drive the format through ai.Client.Generate, for what it makes of the request
// on the wire and of a non-2xx reply, and drive each recorded gap over a loopback server that
// answers with the gap's recorded reply: under the zero profile the host's reply reaches the
// caller, and under the profile that declares the gap the format refuses or narrows the request,
// a refusal coming before the token source is called or anything is sent.

// newClient returns a Client of the server at base speaking the spec's form of the responses
// format, with token.
func newClient(t *testing.T, base string, token ai.TokenSource) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, responses.New(responses.Options{}), ai.Options{Token: token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestGeneratePostsToResponsesWithBearer(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "conversation.json")))
	c := newClient(t, srv.URL+"/v1", func(context.Context) (string, error) { return "secret", nil })
	resp, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi")))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Status != protocol.StatusCompleted || len(resp.Output) != 1 {
		t.Errorf("Generate = %+v, want the recorded reply's one completed message", resp)
	}
	r := srv.Only(t)
	if r.Path != "/v1/responses" {
		t.Errorf("path = %q, want /v1/responses", r.Path)
	}
	if got := r.Header.Values("Authorization"); len(got) != 1 || got[0] != "Bearer secret" {
		t.Errorf("Authorization = %q, want [%q]", got, "Bearer secret")
	}
	aitest.AssertJSON(t, r.Body, `{"model":"m",`+stateless+`,"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
}

// TestGenerateRefusesBeforeTheToken wants a request Validate refuses refused before the token
// source is called or anything is sent.
func TestGenerateRefusesBeforeTheToken(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "conversation.json")))
	called := false
	c := newClient(t, srv.URL, func(context.Context) (string, error) { called = true; return "secret", nil })
	req := ask(text(protocol.RoleUser, "hi"))
	req.MaxOutputTokens = -1
	if _, err := c.Generate(t.Context(), req); err == nil {
		t.Fatal("Generate = nil error, want the request refused")
	}
	if called {
		t.Error("the token source was called for a refused request")
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// TestGenerateAPIError covers the OpenAI-shaped error body the spec's endpoints answer with, an
// *APIError of its message, type, and numeric code.
func TestGenerateAPIError(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusBadRequest,
		`{"error":{"code":400,"message":"model 'no-such-model' not found","type":"invalid_request_error"}}`)
	c := newClient(t, srv.URL, nil)
	_, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi")))
	var apiErr *ai.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Generate error = %v, want an *APIError", err)
	}
	want := ai.APIError{
		Status:  http.StatusBadRequest,
		Message: "model 'no-such-model' not found",
		Type:    "invalid_request_error",
		Code:    "400",
	}
	if apiErr.Status != want.Status || apiErr.Message != want.Message || apiErr.Type != want.Type || apiErr.Code != want.Code {
		t.Errorf("APIError = %+v, want %+v", *apiErr, want)
	}
}

// tokenCounter is a token source that counts its calls.
type tokenCounter struct{ calls int }

func (c *tokenCounter) token(context.Context) (string, error) {
	c.calls++
	return "secret", nil
}

// newProfileClient returns a Client of the server at base speaking the responses format under
// opts, with the token source tokens.
func newProfileClient(t *testing.T, base string, opts responses.Options, tokens *tokenCounter) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, responses.New(opts), ai.Options{Token: tokens.token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// recordedMessage returns the error message of the recorded reply, read from its body apart from
// the client's own decoding.
func recordedMessage(t *testing.T, reply []byte) string {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(reply, &body); err != nil || body.Error.Message == "" {
		t.Fatalf("recorded reply %s has no error message: %v", reply, err)
	}
	return body.Error.Message
}

// The tools of the recorded allowed_tools request, as record.sh sends them, get_time on the wire
// as recordedTimeJSON.
var (
	recordedWeather = protocol.FunctionTool{
		Name:        "get_weather",
		Description: "Get the current temperature in a city, in degrees Celsius.",
		Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}
	recordedTime = protocol.FunctionTool{
		Name:        "get_time",
		Description: "Get the current local time in a city.",
		Parameters:  jsontext.Value(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
	}
)

const recordedTimeJSON = `{"type":"function","name":"get_time","description":"Get the current local time in a city.",
	"parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":false}`

// recordedEffort is the request of the recorded effort gaps, of effort e.
func recordedEffort(e protocol.ReasoningEffort) *protocol.Request {
	return &protocol.Request{Model: "agent",
		Input:     []protocol.Item{text(protocol.RoleUser, "Name three French cities, comma-separated, nothing else.")},
		Reasoning: protocol.ReasoningParam{Effort: e}}
}

// recordedAllowedTools is the request of the recorded allowed_tools gap: both tools, get_time
// alone allowed, and a call required.
func recordedAllowedTools() *protocol.Request {
	return &protocol.Request{Model: "agent",
		Input:      []protocol.Item{text(protocol.RoleUser, "What is the weather in Paris right now?")},
		Tools:      []protocol.FunctionTool{recordedWeather, recordedTime},
		ToolChoice: allowed(protocol.ToolChoiceRequired, "get_time"),
		Reasoning:  protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow}}
}

// recordedRefusedEfforts are the efforts vLLM 0.34.0 refused with a recorded 400.
var recordedRefusedEfforts = []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortXHigh}

// TestGenerateRecordedEffortUnderZeroProfile wants each recorded effort's 400, under the zero
// profile, to reach the caller as an *ai.APIError of the host's status and message, after the
// token was fetched and the request sent once.
func TestGenerateRecordedEffortUnderZeroProfile(t *testing.T) {
	for _, e := range recordedRefusedEfforts {
		t.Run(string(e), func(t *testing.T) {
			reply := aitest.ReadTestdata(t, "vllm-0.34.0/effort_"+string(e)+".json")
			srv := aitest.NewServer(t, http.StatusBadRequest, string(reply))
			tokens := &tokenCounter{}
			_, err := newProfileClient(t, srv.URL+"/v1", responses.Options{}, tokens).Generate(t.Context(), recordedEffort(e))
			apiErr, ok := errors.AsType[*ai.APIError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want an *ai.APIError", err)
			}
			if apiErr.Status != http.StatusBadRequest {
				t.Errorf("APIError.Status = %d, want %d", apiErr.Status, http.StatusBadRequest)
			}
			if want := recordedMessage(t, reply); apiErr.Message != want {
				t.Errorf("APIError.Message = %q, want %q", apiErr.Message, want)
			}
			if tokens.calls != 1 {
				t.Errorf("token source called %d times, want 1", tokens.calls)
			}
			srv.Only(t)
		})
	}
}

// TestGenerateRecordedEffortDeclared wants each recorded effort, under the profile that declares
// it refused, refused as declared before the token source is called or anything is sent.
func TestGenerateRecordedEffortDeclared(t *testing.T) {
	for _, e := range recordedRefusedEfforts {
		t.Run(string(e), func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusBadRequest,
				string(aitest.ReadTestdata(t, "vllm-0.34.0/effort_"+string(e)+".json")))
			tokens := &tokenCounter{}
			opts := responses.Options{RefusedEfforts: []protocol.ReasoningEffort{e}}
			_, err := newProfileClient(t, srv.URL+"/v1", opts, tokens).Generate(t.Context(), recordedEffort(e))
			got, ok := errors.AsType[*format.UnsupportedError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want a *format.UnsupportedError", err)
			}
			want := format.UnsupportedError{Format: "responses", Feature: "reasoning effort", Value: string(e), Declared: true}
			if got.Format != want.Format || got.Feature != want.Feature || got.Value != want.Value || !got.Declared {
				t.Errorf("UnsupportedError = %+v, want %+v", *got, want)
			}
			if tokens.calls != 0 {
				t.Errorf("token source called %d times for a refused request, want 0", tokens.calls)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

// TestGenerateAllowedToolsUnderZeroProfile wants the recorded allowed_tools request, under the
// zero profile, sent whole, its allowed_tools choice as the spec has it, and the recorded reply
// decoded as recorded: vLLM 0.34.0 ignored the choice and called get_weather, outside the allowed
// set.
func TestGenerateAllowedToolsUnderZeroProfile(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "vllm-0.34.0/allowed_tools.json")))
	resp, err := newProfileClient(t, srv.URL+"/v1", responses.Options{}, &tokenCounter{}).Generate(t.Context(), recordedAllowedTools())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	const reasoning = "We need to call get_weather."
	want := &protocol.Response{
		ID:    "resp_97c9cbb37a343cc1",
		Model: "agent",
		Output: []protocol.Item{
			protocol.Reasoning{ID: "rs_af73705db8e7b735", Content: []protocol.ReasoningText{{Text: reasoning}},
				EncryptedContent: seal(t, "", reasoning)},
			protocol.FunctionCall{
				ID:        "fc_8790c7badcce144b",
				Status:    protocol.StatusCompleted,
				CallID:    "call_8790c7badcce144b",
				Name:      "get_weather",
				Arguments: "{\n  \"city\": \"Paris\"\n}",
			},
		},
		Status: protocol.StatusCompleted,
		Usage: protocol.Usage{InputTokens: 158, OutputTokens: 35, TotalTokens: 193,
			OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 16},
			InputTokensDetails:  protocol.InputTokensDetails{CachedTokens: 128}},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("Generate = %#v\nwant       %#v", resp, want)
	}
	aitest.AssertJSON(t, toolMembers(t, srv.Only(t).Body), `{"tools":[
		{"type":"function","name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.",
			"parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":false},`+
		recordedTimeJSON+`],
		"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","name":"get_time"}],"mode":"required"}}`)
}

// TestGenerateAllowedToolsNarrowed wants the recorded allowed_tools request, under
// NoAllowedTools, sent narrowed: its allowed tool alone and its mode as a plain tool_choice, so
// vLLM 0.34.0 has no tool outside the set to call. The recorded reply stands in for the host's.
func TestGenerateAllowedToolsNarrowed(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "vllm-0.34.0/allowed_tools.json")))
	tokens := &tokenCounter{}
	c := newProfileClient(t, srv.URL+"/v1", responses.Options{NoAllowedTools: true}, tokens)
	if _, err := c.Generate(t.Context(), recordedAllowedTools()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	aitest.AssertJSON(t, toolMembers(t, srv.Only(t).Body), `{"tools":[`+recordedTimeJSON+`],"tool_choice":"required"}`)
}
