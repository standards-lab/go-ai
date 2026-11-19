package completions_test

import (
	"context"
	"testing"

	"github.com/standards-lab/go-ai/aitest"
)

// serve starts an endpoint whose Chat Completions route answers every request with status and
// body, as application/json.
func serve(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
		return aitest.RawReply{Status: status, Body: []byte(body)}
	}})
}
