package ai_test

import (
	"context"
	"testing"

	"github.com/standards-lab/go-ai/aitest"
)

func fixed[R any](status int, body string) func(context.Context, R) aitest.Reply {
	return func(context.Context, R) aitest.Reply {
		return aitest.RawReply{Status: status, Body: []byte(body)}
	}
}

func serveChat(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{ChatCompletions: fixed[aitest.ChatRequest](status, body)})
}

func serveEmbeddings(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Embeddings: fixed[aitest.EmbeddingRequest](status, body)})
}

func serveTranscriptions(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Transcriptions: fixed[aitest.TranscriptionRequest](status, body)})
}

func serveModels(t *testing.T, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Models: fixed[aitest.ModelsRequest](status, body)})
}

func serveRaw(t *testing.T, route string, status int, body string) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		route: fixed[aitest.RawRequest](status, body),
	}})
}
