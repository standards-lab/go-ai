package app_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// embedding is the part of an embeddings request body a test asserts.
type embedding struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedEndpoint serves an endpoint that lists ids as its models and answers
// an embeddings request with vectors and usage of 7 prompt tokens and 9
// total, so a test tells the two counts apart.
func embedEndpoint(t *testing.T, ids []string, vectors ...[]float32) *apptest.Endpoint {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /v1/models", apptest.Models(ids...))
	mux.Handle("POST /v1/embeddings", apptest.Embeddings(7, 9, vectors...))
	return apptest.Serve(t, mux)
}

// embedRequests returns the embeddings requests e received, decoded, and
// fails t on any other request but GET /v1/models, which it counts.
func embedRequests(t *testing.T, e *apptest.Endpoint) (sent []embedding, listed int) {
	t.Helper()
	for _, r := range e.Requests() {
		switch {
		case r.Method == http.MethodGet && r.Path == "/v1/models":
			listed++
		case r.Method == http.MethodPost && r.Path == "/v1/embeddings":
			var body embedding
			if err := json.Unmarshal(r.Body, &body); err != nil {
				t.Fatalf("embeddings request body %s: %v", r.Body, err)
			}
			sent = append(sent, body)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.Path)
		}
	}
	return sent, listed
}

// wantVectors fails t unless stdout is one line per vector, each a JSON
// array of numbers equal to the vector the endpoint sent, in order.
func wantVectors(t *testing.T, stdout string, vectors ...[]float32) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if !strings.HasSuffix(stdout, "\n") || len(lines) != len(vectors) {
		t.Fatalf("stdout = %q, want %d lines, each ending in a newline", stdout, len(vectors))
	}
	for i, line := range lines {
		var got []float32
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Errorf("line %d = %q, want a JSON array of numbers: %v", i, line, err)
			continue
		}
		if !slices.Equal(got, vectors[i]) {
			t.Errorf("line %d = %v, want %v", i, got, vectors[i])
		}
	}
}

var (
	vecA = []float32{0.1, -2.5, 3}
	vecB = []float32{1e-7, 0}
	vecC = []float32{42}
)

// With no model set, embed lists the endpoint's models and sends to the one
// listed.
func TestEmbed_FallsBackToTheOneModel(t *testing.T) {
	e := embedEndpoint(t, []string{"embedder"}, vecA)

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "embed", "hello")

	wantCode(t, r, process.ExitOK)
	wantVectors(t, r.stdout, vecA)
	sent, listed := embedRequests(t, e)
	if listed != 1 {
		t.Errorf("listed the models %d times, want 1", listed)
	}
	if len(sent) != 1 || sent[0].Model != "embedder" {
		t.Errorf("embeddings requests = %+v, want one naming model embedder", sent)
	}
	if reqs := e.Requests(); len(reqs) > 0 && reqs[0].Path != "/v1/models" {
		t.Errorf("first request = %s %s, want the model list first", reqs[0].Method, reqs[0].Path)
	}
}

// An endpoint that lists no model, or several, leaves the model open: embed
// exits 1, saying so or naming each in the endpoint's order, and sends no
// embeddings request.
func TestEmbed_FallbackRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []string
		want string
	}{
		{"none listed", nil, "infer embed: no model set, and the endpoint lists none; pass --model\n"},
		{"several listed", []string{"agent", "vision", "embedder"}, "infer embed: several models: agent, vision, embedder; pass --model\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := embedEndpoint(t, tc.ids, vecA)

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "embed", "hello")

			wantCode(t, r, process.ExitFailure)
			if r.stderr != tc.want {
				t.Errorf("stderr = %q, want %q", r.stderr, tc.want)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			sent, listed := embedRequests(t, e)
			if listed != 1 || len(sent) != 0 {
				t.Errorf("listed %d times and sent %d embeddings requests, want 1 and 0", listed, len(sent))
			}
		})
	}
}

// A model the endpoint can't list is reported with exit 1, the listing's
// error named.
func TestEmbed_FallbackListingFails(t *testing.T) {
	e := apptest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"engine down","type":"server_error"}}`))
	}))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "embed", "hello")

	wantCode(t, r, process.ExitFailure)
	if want := "infer embed: no model set, and listing the endpoint's: "; !strings.HasPrefix(r.stderr, want) || !strings.Contains(r.stderr, "engine down") {
		t.Errorf("stderr = %q, want it to start with %q and carry the endpoint's error", r.stderr, want)
	}
	if n := len(e.Requests()); n != 1 {
		t.Errorf("endpoint received %d requests, want the one listing", n)
	}
}

// A set model, from the variable or the flag, is sent as it is, and the
// models are never listed, even on an endpoint that lists several.
func TestEmbed_SetModelListsNothing(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		args []string
		want string
	}{
		{"variable", "vision", []string{"embed", "hello"}, "vision"},
		{"flag", "vision", []string{"embed", "--model", "agent", "hello"}, "agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := embedEndpoint(t, []string{"agent", "vision"}, vecA)

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_MODEL": tc.env}, tc.args...)

			wantCode(t, r, process.ExitOK)
			for _, req := range e.Requests() {
				if req.Method == http.MethodGet {
					t.Errorf("request %s %s, want no GET", req.Method, req.Path)
				}
			}
			sent, _ := embedRequests(t, e)
			if len(sent) != 1 || sent[0].Model != tc.want {
				t.Errorf("embeddings requests = %+v, want one naming model %s", sent, tc.want)
			}
		})
	}
}

// The inputs are the positional arguments, or with none each line of
// stdin; either way they go out in order as one request, and each vector
// is written as one JSON array per line, in input order. A final newline
// ends the last line, and a blank line is an empty input.
func TestEmbed_Inputs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  []string
	}{
		{"arguments", []string{"embed", "first", "second input", "third"}, "ignored\n", []string{"first", "second input", "third"}},
		{"stdin with a final newline", []string{"embed"}, "first\nsecond input\nthird\n", []string{"first", "second input", "third"}},
		{"stdin without a final newline", []string{"embed"}, "first\nsecond input\nthird", []string{"first", "second input", "third"}},
		{"stdin with CRLF and a blank line", []string{"embed"}, "first\r\n\r\nthird\r\n", []string{"first", "", "third"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := embedEndpoint(t, nil, vecA, vecB, vecC)

			r := newHarnessIn(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_MODEL": "embedder"}, tc.stdin).run(tc.args...)

			wantCode(t, r, process.ExitOK)
			sent, _ := embedRequests(t, e)
			if len(sent) != 1 {
				t.Fatalf("sent %d embeddings requests, want one for the whole batch", len(sent))
			}
			if !slices.Equal(sent[0].Input, tc.want) {
				t.Errorf("input = %q, want %q", sent[0].Input, tc.want)
			}
			wantVectors(t, r.stdout, vecA, vecB, vecC)
		})
	}
}

// The usage goes to stderr, both counts named, and stdout holds the vectors
// alone.
func TestEmbed_UsageOnStderr(t *testing.T) {
	e := embedEndpoint(t, nil, vecA)

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_MODEL": "embedder"}, "embed", "hello")

	wantCode(t, r, process.ExitOK)
	if want := "usage: 7 prompt tokens, 9 total tokens\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if strings.Contains(r.stdout, "token") {
		t.Errorf("stdout = %q, want the vectors alone", r.stdout)
	}
	wantVectors(t, r.stdout, vecA)
}

// No input at all, neither an argument nor a line of stdin, is a usage
// error, exit 2, before any request.
func TestEmbed_NoInput(t *testing.T) {
	e := embedEndpoint(t, []string{"embedder"}, vecA)

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "embed")

	wantCode(t, r, process.ExitUsage)
	if want := "infer embed: no input"; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	wantNoRequest(t, e)
}
