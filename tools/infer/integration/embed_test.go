//go:build integration

package integration_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
)

// TestEmbed embeds three inputs, two alike and one unrelated, as embed's
// arguments, and wants one JSON array of numbers per input, one per line,
// with one dimension count of at least 64, and the two alike inputs closer
// to each other than either is to the unrelated one, which vectors written
// out of input order, or decoded wrong, fail. Their lengths are not
// asserted: vLLM 0.33.0 gives granite-embedding-english-r2's vectors
// unnormalized.
func TestEmbed(t *testing.T) {
	inputs := []string{
		"The cat sat on the warm windowsill all afternoon.",
		"A cat dozed in the sunny window for hours.",
		"Quarterly revenue rose eight percent on strong cloud sales.",
	}
	res := runOn(t, embedding, nil, append([]string{"embed"}, inputs...)...)

	wantCode(t, res, process.ExitOK)
	lines := strings.Split(strings.TrimSuffix(res.Stdout, "\n"), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("stdout has %d lines for %d inputs: %q", len(lines), len(inputs), res.Stdout)
	}
	vectors := make([][]float64, len(lines))
	for i, line := range lines {
		if err := json.Unmarshal([]byte(line), &vectors[i]); err != nil {
			t.Fatalf("line %d isn't a JSON array of numbers: %v", i, err)
		}
	}
	dims := len(vectors[0])
	if dims < 64 {
		t.Fatalf("vector 0 has %d dimensions, want at least 64", dims)
	}
	for i, v := range vectors {
		if len(v) != dims {
			t.Fatalf("vector %d has %d dimensions, vector 0 %d", i, len(v), dims)
		}
	}
	cat, cats, revenue := vectors[0], vectors[1], vectors[2]
	if alike, apart := cosine(cat, cats), max(cosine(cat, revenue), cosine(cats, revenue)); alike <= apart {
		t.Errorf("the alike inputs have cosine %g, at most the %g of one with the unrelated input", alike, apart)
	}
	if !strings.HasPrefix(res.Stderr, "usage: ") {
		t.Errorf("stderr = %q, want the usage line", res.Stderr)
	}
}

// cosine is the cosine similarity of a and b, which have the same length.
func cosine(a, b []float64) float64 {
	var ab, aa, bb float64
	for i := range a {
		ab += a[i] * b[i]
		aa += a[i] * a[i]
		bb += b[i] * b[i]
	}
	return ab / (math.Sqrt(aa) * math.Sqrt(bb))
}
