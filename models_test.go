package ai_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/internal/aitest"
)

// modelsReply is a model list as llama.cpp's router sends it, with members the client doesn't
// keep.
const modelsReply = `{"object":"list","data":[` +
	`{"id":"gemma-4-26b","object":"model","owned_by":"llamacpp","status":{"value":"loaded"}},` +
	`{"id":"qwen3-embedding","object":"model","owned_by":"llamacpp","status":{"value":"unloaded"}}]}`

// TestModelsWireRequest wants a GET of the models route, with no body or Content-Type, and the
// Client's query, token, and fixed headers.
func TestModelsWireRequest(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, modelsReply)
	c := newClient(t, srv.URL+"/v1/", ai.Options{
		Query:  url.Values{"autoload": {"false"}},
		Token:  func(context.Context) (string, error) { return "secret", nil },
		Header: http.Header{"X-Consumer-Key": {"k"}},
	})
	if _, err := c.Models(t.Context()); err != nil {
		t.Fatalf("Models: %v", err)
	}
	got := srv.Only(t)
	if got.Method != http.MethodGet || got.Path != "/v1/models" || got.RawQuery != "autoload=false" {
		t.Errorf("request = %s %s?%s, want GET /v1/models?autoload=false", got.Method, got.Path, got.RawQuery)
	}
	for k, want := range map[string]string{
		"Accept":         "application/json",
		"Authorization":  "Bearer secret",
		"X-Consumer-Key": "k",
		"Content-Type":   "",
	} {
		if v := got.Header.Get(k); v != want {
			t.Errorf("%s = %q, want %q", k, v, want)
		}
	}
	if len(got.Body) != 0 {
		t.Errorf("body = %q, want none", got.Body)
	}
}

func TestModelsDecodesList(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, modelsReply)
	c := newClient(t, srv.URL, ai.Options{})
	got, err := c.Models(t.Context())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	want := []ai.Model{{ID: "gemma-4-26b"}, {ID: "qwen3-embedding"}}
	if !slices.Equal(got, want) {
		t.Errorf("Models = %v, want %v", got, want)
	}
}

// TestModelsAnyFormat wants Models to GET the OpenAI-compatible route on a Client of a format
// whose own route is elsewhere.
func TestModelsAnyFormat(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, modelsReply)
	c, err := ai.New(srv.URL+"/v1", http.DefaultClient, messages.New(), ai.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Models(t.Context()); err != nil {
		t.Fatalf("Models: %v", err)
	}
	if got := srv.Only(t); got.Method != http.MethodGet || got.Path != "/v1/models" {
		t.Errorf("request = %s %s, want GET /v1/models", got.Method, got.Path)
	}
}

func TestModelsRefusesReply(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, `{"data":"none"}`)
	c := newClient(t, srv.URL, ai.Options{})
	got, err := c.Models(t.Context())
	if err == nil || !strings.Contains(err.Error(), "decode response") || !strings.HasPrefix(err.Error(), "ai: models: ") {
		t.Errorf("Models = %v, %v; want an ai: models: decode response error", got, err)
	}
}

// TestModelsAPIError covers a non-2xx reply: an *APIError with the OpenAI-style error body's
// details.
func TestModelsAPIError(t *testing.T) {
	reply := `{"error":{"code":401,"message":"Invalid API Key","type":"authentication_error"}}`
	srv := aitest.NewServer(t, http.StatusUnauthorized, reply)
	c := newClient(t, srv.URL, ai.Options{})
	got, err := c.Models(t.Context())
	apiErr, ok := errors.AsType[*ai.APIError](err)
	if !ok {
		t.Fatalf("Models = %v, %v; want an *APIError", got, err)
	}
	if apiErr.Status != http.StatusUnauthorized || apiErr.Message != "Invalid API Key" ||
		apiErr.Type != "authentication_error" || apiErr.Code != "401" {
		t.Errorf("APIError = %+v, want 401 with the body's message, type, and code", apiErr)
	}
}

func TestModelsTokenErrorSendsNothing(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, modelsReply)
	errExpired := errors.New("token expired")
	c := newClient(t, srv.URL, ai.Options{
		Token: func(context.Context) (string, error) { return "", errExpired },
	})
	if _, err := c.Models(t.Context()); !errors.Is(err, errExpired) {
		t.Errorf("Models error = %v, want it to wrap %v", err, errExpired)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}
