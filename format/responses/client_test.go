package responses_test

import (
	"context"
	"encoding/json/jsontext"
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

func newClient(t *testing.T, base string, token ai.TokenSource) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, responses.New(responses.Options{}), ai.Options{Token: token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestGeneratePostsToResponsesWithBearer(t *testing.T) {
	srv := serve(t, http.StatusOK, string(aitest.ReadTestdata(t, "conversation.json")))
	c := newClient(t, srv.URL(), func(context.Context) (string, error) { return "secret", nil })
	resp, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi")))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Status != protocol.StatusCompleted || len(resp.Output) != 1 {
		t.Errorf("Generate = %+v, want the recorded reply's one completed message", resp)
	}
	r := aitest.Only(t, srv)
	if r.Path != "/v1/responses" {
		t.Errorf("path = %q, want /v1/responses", r.Path)
	}
	if got := r.Header.Values("Authorization"); len(got) != 1 || got[0] != "Bearer secret" {
		t.Errorf("Authorization = %q, want [%q]", got, "Bearer secret")
	}
	aitest.AssertJSON(t, r.Body, `{"model":"m",`+stateless+`,"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
}

func TestGenerateRefusesBeforeTheToken(t *testing.T) {
	srv := serve(t, http.StatusOK, string(aitest.ReadTestdata(t, "conversation.json")))
	called := false
	c := newClient(t, srv.URL(), func(context.Context) (string, error) { called = true; return "secret", nil })
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

func TestGenerateAPIError(t *testing.T) {
	srv := serve(t, http.StatusBadRequest,
		`{"error":{"code":400,"message":"model 'no-such-model' not found","type":"invalid_request_error"}}`)
	c := newClient(t, srv.URL(), nil)
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

func newProfileClient(t *testing.T, base string, opts responses.Options, tokens *aitest.TokenCounter) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, responses.New(opts), ai.Options{Token: tokens.Token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

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

func recordedEffort(e protocol.ReasoningEffort) *protocol.Request {
	return &protocol.Request{Model: "agent",
		Input:     []protocol.Item{text(protocol.RoleUser, "Name three French cities, comma-separated, nothing else.")},
		Reasoning: protocol.ReasoningParam{Effort: e}}
}

func recordedAllowedTools() *protocol.Request {
	return &protocol.Request{Model: "agent",
		Input:      []protocol.Item{text(protocol.RoleUser, "What is the weather in Paris right now?")},
		Tools:      []protocol.FunctionTool{recordedWeather, recordedTime},
		ToolChoice: allowed(protocol.ToolChoiceRequired, "get_time"),
		Reasoning:  protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow}}
}

var recordedRefusedEfforts = []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortXHigh}

func TestGenerateRecordedEffortUnderZeroProfile(t *testing.T) {
	for _, e := range recordedRefusedEfforts {
		t.Run(string(e), func(t *testing.T) {
			reply := aitest.ReadTestdata(t, "vllm-0.34.0/effort_"+string(e)+".json")
			srv := serve(t, http.StatusBadRequest, string(reply))
			tokens := &aitest.TokenCounter{}
			_, err := newProfileClient(t, srv.URL(), responses.Options{}, tokens).Generate(t.Context(), recordedEffort(e))
			apiErr, ok := errors.AsType[*ai.APIError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want an *ai.APIError", err)
			}
			if apiErr.Status != http.StatusBadRequest {
				t.Errorf("APIError.Status = %d, want %d", apiErr.Status, http.StatusBadRequest)
			}
			if want := aitest.ReplyMessage(t, reply); apiErr.Message != want {
				t.Errorf("APIError.Message = %q, want %q", apiErr.Message, want)
			}
			if tokens.Calls != 1 {
				t.Errorf("token source called %d times, want 1", tokens.Calls)
			}
			aitest.Only(t, srv)
		})
	}
}

func TestGenerateRecordedEffortDeclared(t *testing.T) {
	for _, e := range recordedRefusedEfforts {
		t.Run(string(e), func(t *testing.T) {
			srv := serve(t, http.StatusBadRequest,
				string(aitest.ReadTestdata(t, "vllm-0.34.0/effort_"+string(e)+".json")))
			tokens := &aitest.TokenCounter{}
			opts := responses.Options{RefusedEfforts: []protocol.ReasoningEffort{e}}
			_, err := newProfileClient(t, srv.URL(), opts, tokens).Generate(t.Context(), recordedEffort(e))
			got, ok := errors.AsType[*format.UnsupportedError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want a *format.UnsupportedError", err)
			}
			want := format.UnsupportedError{Format: "responses", Feature: "reasoning effort", Value: string(e), Declared: true}
			if got.Format != want.Format || got.Feature != want.Feature || got.Value != want.Value || !got.Declared {
				t.Errorf("UnsupportedError = %+v, want %+v", *got, want)
			}
			if tokens.Calls != 0 {
				t.Errorf("token source called %d times for a refused request, want 0", tokens.Calls)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

func TestGenerateAllowedToolsUnderZeroProfile(t *testing.T) {
	srv := serve(t, http.StatusOK, string(aitest.ReadTestdata(t, "vllm-0.34.0/allowed_tools.json")))
	resp, err := newProfileClient(t, srv.URL(), responses.Options{}, &aitest.TokenCounter{}).Generate(t.Context(), recordedAllowedTools())
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
	aitest.AssertJSON(t, toolMembers(t, aitest.Only(t, srv).Body), `{"tools":[
		{"type":"function","name":"get_weather","description":"Get the current temperature in a city, in degrees Celsius.",
			"parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":false},`+
		recordedTimeJSON+`],
		"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","name":"get_time"}],"mode":"required"}}`)
}

func TestGenerateAllowedToolsNarrowed(t *testing.T) {
	srv := serve(t, http.StatusOK, string(aitest.ReadTestdata(t, "vllm-0.34.0/allowed_tools.json")))
	tokens := &aitest.TokenCounter{}
	c := newProfileClient(t, srv.URL(), responses.Options{NoAllowedTools: true}, tokens)
	if _, err := c.Generate(t.Context(), recordedAllowedTools()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	aitest.AssertJSON(t, toolMembers(t, aitest.Only(t, srv).Body), `{"tools":[`+recordedTimeJSON+`],"tool_choice":"required"}`)
}
