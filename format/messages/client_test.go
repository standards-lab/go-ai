package messages_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// The tests here drive the format through ai.Client.Generate, for what it makes of the request
// on the wire and of a non-2xx reply, and drive each recorded gap over a loopback server that
// answers with the gap's recorded reply: under the zero profile the host's reply reaches the
// caller, and under the profile that declares the gap the format refuses the request before the
// token source is called or anything is sent.

// newClient returns a Client of the server at base speaking the messages format, with opts.
func newClient(t *testing.T, base string, opts ai.Options) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, messages.New(messages.Options{}), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestGeneratePostsToMessagesWithVersionAndBearer(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "conversation.json")))
	c := newClient(t, srv.URL+"/v1", ai.Options{Token: func(context.Context) (string, error) { return "secret", nil }})
	if _, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi"))); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	r := srv.Only(t)
	if r.Path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", r.Path)
	}
	for k, want := range map[string]string{"Anthropic-Version": "2026-10-21", "Authorization": "Bearer secret"} {
		if got := r.Header.Values(k); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want [%q]", k, got, want)
		}
	}
	aitest.AssertJSON(t, r.Body, `{"model":"m","max_tokens":64,"stream":false,"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
}

// TestGenerateSendsAPIKey wants Anthropic's x-api-key, set as a fixed header, on the request
// beside the format's anthropic-version.
func TestGenerateSendsAPIKey(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "conversation.json")))
	c := newClient(t, srv.URL, ai.Options{Header: http.Header{"x-api-key": {"key"}}})
	if _, err := c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi"))); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	h := srv.Only(t).Header
	if got := h.Values("X-Api-Key"); len(got) != 1 || got[0] != "key" {
		t.Errorf("X-Api-Key = %q, want [key]", got)
	}
	if got := h.Get("Anthropic-Version"); got != "2026-10-21" {
		t.Errorf("Anthropic-Version = %q, want 2026-10-21", got)
	}
}

// TestGenerateRefusesBeforeTheToken wants a request the format refuses, with no
// MaxOutputTokens or with audio input, refused before the token source is called or anything
// is sent.
func TestGenerateRefusesBeforeTheToken(t *testing.T) {
	noMax := ask(text(protocol.RoleUser, "hi"))
	noMax.MaxOutputTokens = 0
	audio := ask(protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
		protocol.InputAudio{Data: []byte("RIFF"), Format: protocol.AudioFormatWAV},
	}})
	for name, req := range map[string]*protocol.Request{"no MaxOutputTokens": noMax, "audio input": audio} {
		t.Run(name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "conversation.json")))
			called := false
			c := newClient(t, srv.URL, ai.Options{Token: func(context.Context) (string, error) { called = true; return "secret", nil }})
			if _, err := c.Generate(t.Context(), req); err == nil {
				t.Fatal("Generate = nil error, want the request refused")
			}
			var errs []error
			for _, err := range c.GenerateStream(t.Context(), req) {
				errs = append(errs, err)
			}
			if len(errs) != 1 || errs[0] == nil {
				t.Errorf("GenerateStream = %v, want one error pair", errs)
			}
			if called {
				t.Error("the token source was called for a refused request")
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

// TestGenerateAPIError covers the error bodies of both shapes a Messages endpoint answers with,
// each an *APIError of the body's message and type, and the reply's Retry-After.
func TestGenerateAPIError(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		message     string
		typ         string
		code        string
		errorString string
	}{
		{"OpenAI-shaped, as llama.cpp's router answers", http.StatusBadRequest,
			`{"error":{"code":400,"message":"model 'no-such-model' not found","type":"invalid_request_error"}}`,
			"model 'no-such-model' not found", "invalid_request_error", "400",
			"400 Bad Request: model 'no-such-model' not found (invalid_request_error, 400)"},
		{"Anthropic's", http.StatusTooManyRequests,
			`{"type":"error","error":{"type":"rate_limit_error","message":"Number of request tokens has exceeded your per-minute rate limit"}}`,
			"Number of request tokens has exceeded your per-minute rate limit", "rate_limit_error", "",
			"429 Too Many Requests: Number of request tokens has exceeded your per-minute rate limit (rate_limit_error)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hc := &http.Client{Transport: reply(tc.status, tc.body, "30")}
			c, err := ai.New("http://router/v1", hc, messages.New(messages.Options{}), ai.Options{})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			_, err = c.Generate(t.Context(), ask(text(protocol.RoleUser, "hi")))
			var apiErr *ai.APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Generate error = %v, want an *APIError", err)
			}
			if apiErr.Status != tc.status || apiErr.Message != tc.message || apiErr.Type != tc.typ || apiErr.Code != tc.code {
				t.Errorf("APIError = {%d %q %q %q}, want {%d %q %q %q}",
					apiErr.Status, apiErr.Message, apiErr.Type, apiErr.Code, tc.status, tc.message, tc.typ, tc.code)
			}
			if apiErr.RetryAfter != 30*time.Second {
				t.Errorf("APIError.RetryAfter = %v, want 30s", apiErr.RetryAfter)
			}
			if got := apiErr.Error(); got != tc.errorString {
				t.Errorf("Error() = %q, want %q", got, tc.errorString)
			}
		})
	}
}

// reply is a transport that answers every request with status, body, and a Retry-After of
// retryAfter.
func reply(status int, body, retryAfter string) roundTripper {
	return func(r *http.Request) (*http.Response, error) {
		h := http.Header{"Content-Type": {"application/json"}, "Retry-After": {retryAfter}}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	}
}

// roundTripper is an http.RoundTripper made of a function.
type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// tokenCounter is a token source that counts its calls.
type tokenCounter struct{ calls int }

func (c *tokenCounter) token(context.Context) (string, error) {
	c.calls++
	return "secret", nil
}

// newProfileClient returns a Client of the server at base speaking the messages format under
// opts, with the token source tokens.
func newProfileClient(t *testing.T, base string, opts messages.Options, tokens *tokenCounter) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, messages.New(opts), ai.Options{Token: tokens.token})
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

// recordedEffort is the request of the recorded effort gaps, of effort e, as record.sh sends it.
func recordedEffort(e protocol.ReasoningEffort) *protocol.Request {
	return &protocol.Request{Model: "agent",
		Input:           []protocol.Item{text(protocol.RoleUser, "Name three French cities, comma-separated, nothing else.")},
		MaxOutputTokens: 256,
		Reasoning:       protocol.ReasoningParam{Effort: e}}
}

// recordedRefusedEfforts are the efforts vLLM 0.34.0 refused with a recorded 400.
var recordedRefusedEfforts = []protocol.ReasoningEffort{protocol.ReasoningEffortXHigh, protocol.ReasoningEffortMax}

// TestGenerateRecordedEffortUnderZeroProfile wants each recorded effort's 400, under the zero
// profile, to reach the caller as an *ai.APIError of the host's status and message, after the
// token was fetched and the request sent once.
func TestGenerateRecordedEffortUnderZeroProfile(t *testing.T) {
	for _, e := range recordedRefusedEfforts {
		t.Run(string(e), func(t *testing.T) {
			reply := aitest.ReadTestdata(t, "vllm-0.34.0/effort_"+string(e)+".json")
			srv := aitest.NewServer(t, http.StatusBadRequest, string(reply))
			tokens := &tokenCounter{}
			_, err := newProfileClient(t, srv.URL+"/v1", messages.Options{}, tokens).Generate(t.Context(), recordedEffort(e))
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

// TestGenerateRecordedEffortNoneUnderZeroProfile wants effort none, under the zero profile, sent
// as thinking disabled, and the recorded reply decoded as recorded: vLLM 0.34.0 ignored it and
// reasoned anyway, so a reasoning item leads the message. The expected response is built from the
// recording, read apart from the format's own decoding.
func TestGenerateRecordedEffortNoneUnderZeroProfile(t *testing.T) {
	reply := aitest.ReadTestdata(t, "vllm-0.34.0/effort_none.json")
	srv := aitest.NewServer(t, http.StatusOK, string(reply))
	tokens := &tokenCounter{}
	resp, err := newProfileClient(t, srv.URL+"/v1", messages.Options{}, tokens).
		Generate(t.Context(), recordedEffort(protocol.ReasoningEffortNone))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var rec struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type      string `json:"type"`
			Thinking  string `json:"thinking"`
			Signature string `json:"signature"`
			Text      string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(reply, &rec); err != nil {
		t.Fatalf("recording: %v", err)
	}
	if len(rec.Content) != 2 || rec.Content[0].Type != "thinking" || rec.Content[1].Type != "text" {
		t.Fatalf("recording's content = %+v, want a thinking block then a text block", rec.Content)
	}
	thinking, answer := rec.Content[0], rec.Content[1]
	want := &protocol.Response{
		ID:    rec.ID,
		Model: rec.Model,
		Output: []protocol.Item{
			protocol.Reasoning{
				ID:               rec.ID + "/0",
				Status:           protocol.StatusCompleted,
				Content:          []protocol.ReasoningText{{Text: thinking.Thinking}},
				EncryptedContent: seal(`{"thinking":` + quote(thinking.Thinking) + `,"signature":` + quote(thinking.Signature) + `}`),
			},
			protocol.Message{
				ID:      rec.ID + "/1",
				Status:  protocol.StatusCompleted,
				Role:    protocol.RoleAssistant,
				Content: []protocol.ContentPart{protocol.OutputText{Text: answer.Text}},
			},
		},
		Status: protocol.StatusCompleted,
		Usage: protocol.Usage{InputTokens: rec.Usage.InputTokens, OutputTokens: rec.Usage.OutputTokens,
			TotalTokens: rec.Usage.InputTokens + rec.Usage.OutputTokens},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("Generate = %#v\nwant       %#v", resp, want)
	}
	if tokens.calls != 1 {
		t.Errorf("token source called %d times, want 1", tokens.calls)
	}
	var sent struct {
		Thinking map[string]any `json:"thinking"`
	}
	if err := json.Unmarshal(srv.Only(t).Body, &sent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sent.Thinking, map[string]any{"type": "disabled"}) {
		t.Errorf("sent thinking = %v, want {\"type\":\"disabled\"}", sent.Thinking)
	}
}

// TestGenerateRecordedEffortDeclared wants each recorded effort, under the profile that declares
// it refused, refused as declared before the token source is called or anything is sent: none
// among them, which vLLM 0.34.0 ignores rather than refuses.
func TestGenerateRecordedEffortDeclared(t *testing.T) {
	cases := []struct {
		effort protocol.ReasoningEffort
		status int
	}{
		{protocol.ReasoningEffortNone, http.StatusOK},
		{protocol.ReasoningEffortXHigh, http.StatusBadRequest},
		{protocol.ReasoningEffortMax, http.StatusBadRequest},
	}
	for _, tc := range cases {
		e := tc.effort
		t.Run(string(e), func(t *testing.T) {
			srv := aitest.NewServer(t, tc.status, string(aitest.ReadTestdata(t, "vllm-0.34.0/effort_"+string(e)+".json")))
			tokens := &tokenCounter{}
			opts := messages.Options{RefusedEfforts: []protocol.ReasoningEffort{e}}
			_, err := newProfileClient(t, srv.URL+"/v1", opts, tokens).Generate(t.Context(), recordedEffort(e))
			got, ok := errors.AsType[*format.UnsupportedError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want a *format.UnsupportedError", err)
			}
			want := format.UnsupportedError{Format: "messages", Feature: "reasoning effort", Value: string(e), Declared: true}
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
