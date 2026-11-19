package ai_test

import (
	"context"
	"testing"

	"github.com/standards-lab/go-ai/aitest"
)

// fixed returns a route function that answers every request with status and body, as
// application/json.
func fixed[R any](status int, body string) func(context.Context, R) aitest.Reply {
	return func(context.Context, R) aitest.Reply {
		return aitest.RawReply{Status: status, Body: []byte(body)}
	}
}

// serveChat starts an endpoint whose Chat Completions route answers every request with status
// and body.
func serveChat(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{ChatCompletions: fixed[aitest.ChatRequest](status, body)})
}

// serveEmbeddings starts an endpoint whose embeddings route answers every request with status
// and body.
func serveEmbeddings(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Embeddings: fixed[aitest.EmbeddingRequest](status, body)})
}

// serveTranscriptions starts an endpoint whose transcription route answers every request with
// status and body.
func serveTranscriptions(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Transcriptions: fixed[aitest.TranscriptionRequest](status, body)})
}

// serveModels starts an endpoint whose models route answers every request with status and body.
func serveModels(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Models: fixed[aitest.ModelsRequest](status, body)})
}

// serveRaw starts an endpoint whose raw route, the path after /v1/ such as "messages", answers
// every request with status and body.
func serveRaw(t *testing.T, route string, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		route: fixed[aitest.RawRequest](status, body),
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
