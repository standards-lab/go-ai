package messages_test

import (
	"context"
	"testing"

	"github.com/standards-lab/go-ai/aitest"
)

func serve(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"messages": func(context.Context, aitest.RawRequest) aitest.Reply {
			return aitest.RawReply{Status: status, Body: []byte(body)}
		},
	}})
}
