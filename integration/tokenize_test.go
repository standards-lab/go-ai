//go:build integration

package integration_test

import (
	"os"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

const tokenizeRoot = "AI_TOKENIZE_BASE_URL"

func tokenizeClient(t *testing.T, opts ai.Options) *ai.Client {
	t.Helper()
	base := os.Getenv(tokenizeRoot)
	if base == "" {
		t.Skipf("%s is unset: set it to the root the embedding endpoint serves tokenize at, such as http://localhost:8000", tokenizeRoot)
	}
	c, err := newClient(tokenizeRoot, base, completions.New(completions.Options{}), opts, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestTokenize(t *testing.T) {
	tc := tokenizeClient(t, ai.Options{Embedding: passagePrefix})
	c, model := endpoint(t, embedding, completionsFormat, ai.Options{Embedding: passagePrefix})
	const text = "Tide tables for the northern harbour are posted at the ferry office each Monday."
	counts := make(map[ai.InputType]int)
	for _, typ := range []ai.InputType{ai.InputNone, ai.InputDocument} {
		tokens, err := tc.Tokenize(requestContext(t), &ai.TokenizeRequest{Model: model, Text: text, InputType: typ})
		if err != nil {
			t.Fatalf("Tokenize(%s): %v", typ, err)
		}
		batch, err := c.Embed(requestContext(t), &ai.EmbeddingRequest{Model: model, Input: []string{text}, InputType: typ})
		if err != nil {
			t.Fatalf("Embed(%s): %v", typ, err)
		}
		if tokens.Count != batch.Usage.PromptTokens {
			t.Errorf("Tokenize(%s) counts %d tokens, but Embed(%s) reports %d prompt tokens",
				typ, tokens.Count, typ, batch.Usage.PromptTokens)
		}
		counts[typ] = tokens.Count
	}
	if counts[ai.InputDocument] <= counts[ai.InputNone] {
		t.Errorf("Tokenize counts %d tokens as a document and %d bare, want the document's prefix counted",
			counts[ai.InputDocument], counts[ai.InputNone])
	}
}
