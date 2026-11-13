package responses_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// The tests here drive the format through ai.Client.Generate, for what it makes of the request
// on the wire and of a non-2xx reply.

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
	srv := aitest.NewServer(t, http.StatusOK, string(readTestdata(t, "conversation.json")))
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
	assertJSON(t, r.Body, `{"model":"m",`+stateless+`,"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)
}

// TestGenerateRefusesBeforeTheToken wants a request Validate refuses refused before the token
// source is called or anything is sent.
func TestGenerateRefusesBeforeTheToken(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, string(readTestdata(t, "conversation.json")))
	called := false
	c := newClient(t, srv.URL, func(context.Context) (string, error) { called = true; return "secret", nil })
	req := ask(text(protocol.RoleUser, "hi"))
	req.MaxOutputTokens = protocol.MinMaxOutputTokens - 1
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
