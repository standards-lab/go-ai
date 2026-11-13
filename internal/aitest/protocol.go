package aitest

import (
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/standards-lab/go-ai/protocol"
)

// ReadTestdata returns the file name under the test's testdata directory, failing t if it can't
// be read.
func ReadTestdata(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Fold folds events with an Accumulator, failing t if it refuses one, and returns its Response.
func Fold(t testing.TB, events []protocol.Event) *protocol.Response {
	t.Helper()
	var acc protocol.Accumulator
	for _, e := range events {
		if err := acc.Add(e); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	return acc.Response()
}

// AssertResponse fails t unless got and want are the same response.
func AssertResponse(t testing.TB, got, want *protocol.Response) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Response = %#v\nwant       %#v", got, want)
	}
}

// AssertJSON fails t unless got and want are the same JSON, ignoring member order and spacing.
func AssertJSON(t testing.TB, got []byte, want string) {
	t.Helper()
	g, w := jsontext.Value(append([]byte(nil), got...)), jsontext.Value(want)
	if err := g.Canonicalize(); err != nil {
		t.Fatalf("body %s isn't JSON: %v", got, err)
	}
	if err := w.Canonicalize(); err != nil {
		t.Fatalf("want %s isn't JSON: %v", want, err)
	}
	if string(g) != string(w) {
		t.Errorf("body = %s\nwant   %s", g, w)
	}
}

// SameArguments sets each function call of want to the arguments of got's in its place, failing
// t unless the two are the same JSON value, so a comparison of the two responses that follows
// holds the calls' arguments to their JSON value rather than their text. An endpoint may write a
// call's arguments with other spacing unstreamed than streamed: vLLM parses them and writes them
// back in an unstreamed reply, and streams the model's own text. Outputs of different lengths,
// or a place whose items aren't both calls, are left for that comparison to report.
func SameArguments(t testing.TB, got, want *protocol.Response) {
	t.Helper()
	if len(got.Output) != len(want.Output) {
		return
	}
	for i, it := range want.Output {
		w, ok := it.(protocol.FunctionCall)
		g, ok2 := got.Output[i].(protocol.FunctionCall)
		if !ok || !ok2 {
			continue
		}
		gv, wv := jsontext.Value(g.Arguments), jsontext.Value(w.Arguments)
		if gv.Canonicalize() != nil || wv.Canonicalize() != nil || string(gv) != string(wv) {
			t.Errorf("output %d's arguments %q, want the JSON of %q", i, g.Arguments, w.Arguments)
			continue
		}
		w.Arguments = g.Arguments
		want.Output[i] = w
	}
}
