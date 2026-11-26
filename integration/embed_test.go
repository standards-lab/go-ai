//go:build integration

package integration_test

import (
	"math"
	"testing"

	"github.com/standards-lab/go-ai"
)

// See docs/design.md, "Implementation notes".
func TestEmbed(t *testing.T) {
	c, model := endpoint(t, embedding, completionsFormat, ai.Options{})
	inputs := []string{
		"The cat sat on the warm windowsill all afternoon.",
		"A cat dozed in the sunny window for hours.",
		"Quarterly revenue rose eight percent on strong cloud sales.",
	}
	batch, err := c.Embed(requestContext(t), &ai.EmbeddingRequest{Model: model, Input: inputs})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(batch.Vectors) != len(inputs) {
		t.Fatalf("Embed gave %d vectors for %d inputs", len(batch.Vectors), len(inputs))
	}
	dims := len(batch.Vectors[0])
	if dims < 64 {
		t.Fatalf("vector 0 has %d dimensions, want at least 64", dims)
	}
	for i, v := range batch.Vectors {
		if len(v) != dims {
			t.Fatalf("vector %d has %d dimensions, vector 0 %d", i, len(v), dims)
		}
		for j, x := range v {
			if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
				t.Fatalf("vector %d's component %d is %g, want a finite number", i, j, x)
			}
		}
	}
	cat, cats, revenue := batch.Vectors[0], batch.Vectors[1], batch.Vectors[2]
	if alike, apart := cosine(cat, cats), max(cosine(cat, revenue), cosine(cats, revenue)); alike <= apart {
		t.Errorf("the alike inputs have cosine %g, at most the %g of one with the unrelated input", alike, apart)
	}
	if u := batch.Usage; u.PromptTokens == 0 || u.TotalTokens < u.PromptTokens {
		t.Errorf("Usage = %+v, want prompt tokens counted, and total at least them", u)
	}
	alone, err := c.Embed(requestContext(t), &ai.EmbeddingRequest{Model: model, Input: inputs[2:]})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(alone.Vectors) != 1 || len(alone.Vectors[0]) != dims {
		t.Fatalf("Embed of one input gave %d vectors, want one of %d dimensions", len(alone.Vectors), dims)
	}
	same, other := cosine(alone.Vectors[0], revenue), cosine(alone.Vectors[0], cat)
	if same < 0.999 || other > 0.9 {
		t.Errorf("the third input alone has cosine %g with the batch's third vector and %g with its first; "+
			"want at least 0.999 and at most 0.9", same, other)
	}
}

var passagePrefix = ai.EmbeddingProfile{InputTypes: map[ai.InputType]ai.InputConvention{
	ai.InputDocument: {Prefix: "passage: "},
}}

const embeddingTolerance = 1e-3

func TestEmbedInputType(t *testing.T) {
	c, model := endpoint(t, embedding, completionsFormat, ai.Options{Embedding: passagePrefix})
	const text = "The lighthouse keeper logged every passing ship in a leather-bound book."
	embedOne := func(input string, typ ai.InputType) []float32 {
		t.Helper()
		batch, err := c.Embed(requestContext(t), &ai.EmbeddingRequest{Model: model, Input: []string{input}, InputType: typ})
		if err != nil {
			t.Fatalf("Embed(%q, %s): %v", input, typ, err)
		}
		if len(batch.Vectors) != 1 {
			t.Fatalf("Embed(%q, %s) gave %d vectors, want 1", input, typ, len(batch.Vectors))
		}
		return batch.Vectors[0]
	}
	document := embedOne(text, ai.InputDocument)
	prefixed := embedOne("passage: "+text, ai.InputNone)
	bare := embedOne(text, ai.InputNone)
	if len(document) != len(prefixed) || len(bare) != len(prefixed) {
		t.Fatalf("the vectors have %d, %d, and %d dimensions, want them equal", len(document), len(prefixed), len(bare))
	}
	if d := maxDiff(document, prefixed); d > embeddingTolerance {
		t.Errorf("the document embedding differs from the bare embedding of the prefixed text by up to %g, want at most %g",
			d, embeddingTolerance)
	}
	if d := maxDiff(bare, prefixed); d <= embeddingTolerance {
		t.Errorf("the bare text's embedding is within %g of the prefixed text's, at most %g: the model doesn't tell them apart",
			d, embeddingTolerance)
	}
}

func maxDiff(a, b []float32) float64 {
	var d float64
	for i := range a {
		d = max(d, math.Abs(float64(a[i])-float64(b[i])))
	}
	return d
}

func norm(v []float32) float64 {
	return math.Sqrt(dot(v, v))
}

func cosine(a, b []float32) float64 {
	return dot(a, b) / (norm(a) * norm(b))
}

func dot(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}
