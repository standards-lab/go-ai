package messages_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// The tests here drive the format through ai.Client.Generate, for what it makes of the request
// on the wire and of a non-2xx reply.

// newClient returns a Client of the server at base speaking the messages format, with opts.
func newClient(t *testing.T, base string, opts ai.Options) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, messages.New(), opts)
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
			c, err := ai.New("http://router/v1", hc, messages.New(), ai.Options{})
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
