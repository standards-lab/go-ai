package app_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// models writes each model ID the endpoint lists, one per line, in the
// endpoint's order, from one GET of the models route.
func TestModels_WritesEachID(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent", "vision", "embedding"))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "models")

	wantCode(t, r, process.ExitOK)
	if want := "agent\nvision\nembedding\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
	reqs := e.Requests()
	if len(reqs) != 1 {
		t.Fatalf("endpoint received %d requests, want 1", len(reqs))
	}
	if reqs[0].Method != http.MethodGet || reqs[0].Path != "/v1/models" {
		t.Errorf("request = %s %s, want GET /v1/models", reqs[0].Method, reqs[0].Path)
	}
}

// An endpoint that lists no model writes nothing and succeeds.
func TestModels_NoneListed(t *testing.T) {
	e := apptest.Serve(t, apptest.Models())

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "models")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "" || r.stderr != "" {
		t.Errorf("stdout, stderr = %q, %q, want both empty", r.stdout, r.stderr)
	}
}

// models builds the client and what it reads, not the model setting, which
// it never names.
func TestModels_BuildsTheClient(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "models")

	wantCode(t, r, process.ExitOK)
	for _, want := range []string{"client", "base url", "format", "lifecycle config"} {
		if !slices.Contains(r.built, want) {
			t.Errorf("nodes built = %q, want %q among them", r.built, want)
		}
	}
	if slices.Contains(r.built, "model") {
		t.Errorf("nodes built = %q, want no model", r.built)
	}
}

// INFER_TOKEN is sent as the bearer token; an empty or unset one sends no
// Authorization header at all.
func TestModels_Token(t *testing.T) {
	for _, tc := range []struct {
		name, token, want string
	}{
		{"set", "secret", "Bearer secret"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := apptest.Serve(t, apptest.Models("agent"))

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_TOKEN": tc.token}, "models")

			wantCode(t, r, process.ExitOK)
			reqs := e.Requests()
			if len(reqs) != 1 {
				t.Fatalf("endpoint received %d requests, want 1", len(reqs))
			}
			got, present := reqs[0].Header["Authorization"]
			switch {
			case tc.want == "" && present:
				t.Errorf("Authorization = %q, want no header", got)
			case tc.want != "" && reqs[0].Header.Get("Authorization") != tc.want:
				t.Errorf("Authorization = %q, want %q", reqs[0].Header.Get("Authorization"), tc.want)
			}
		})
	}
}

// No flag takes the token: --token is an unknown flag, a usage error that
// sends nothing.
func TestModels_NoTokenFlag(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "models", "--token", "secret")

	wantCode(t, r, process.ExitUsage)
	wantNoRequest(t, e)
}

// An endpoint's error reply fails the run with exit 1, reported once on
// stderr with the command's label, and nothing on stdout.
func TestModels_EndpointError(t *testing.T) {
	e := apptest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"engine down","type":"server_error"}}`))
	}))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "models")

	wantCode(t, r, process.ExitFailure)
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if !strings.HasPrefix(r.stderr, "infer models: ") || strings.Count(r.stderr, "engine down") != 1 {
		t.Errorf("stderr = %q, want the endpoint's error once, labelled infer models", r.stderr)
	}
	if strings.Contains(r.stderr, "Usage:") {
		t.Errorf("stderr = %q, want no usage for a failure that isn't one", r.stderr)
	}
}
