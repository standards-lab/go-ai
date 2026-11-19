package messages_test

import (
	"context"
	"testing"

	"github.com/standards-lab/go-ai/aitest"
)

// serve starts an endpoint whose messages route answers every request with status and body, as
// application/json.
func serve(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"messages": func(context.Context, aitest.RawRequest) aitest.Reply {
			return aitest.RawReply{Status: status, Body: []byte(body)}
		},
	}})
}

// only returns the one request e received, failing t unless it received exactly one.
func only(t *testing.T, e *aitest.Endpoint) aitest.Request {
	t.Helper()
	reqs := e.Requests()
	if len(reqs) != 1 {
		t.Fatalf("endpoint received %d requests, want 1", len(reqs))
	}
	return reqs[0]
}
