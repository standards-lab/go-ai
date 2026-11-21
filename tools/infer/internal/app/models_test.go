package app_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"
)

func TestModels_WritesEachID(t *testing.T) {
	e := serveModels(t, "agent", "vision", "embedding")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models")

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

func TestModels_WritesEachRoot(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: listingOf(
		`{"id":"agent","object":"model","owned_by":"vllm","root":"openai/gpt-oss-120b","parent":null,"max_model_len":131072}`,
		`{"id":"vision","object":"model"}`,
	)})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models")

	wantCode(t, r, process.ExitOK)
	if want := "agent\topenai/gpt-oss-120b\nvision\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
}

func TestModels_NoStringRoot(t *testing.T) {
	for _, tc := range []struct{ name, root string }{
		{"null", `null`},
		{"empty", `""`},
		{"number", `7`},
		{"object", `{"name":"openai/gpt-oss-120b"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{Models: listingOf(`{"id":"agent","object":"model","root":` + tc.root + `}`)})

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models")

			wantCode(t, r, process.ExitOK)
			if want := "agent\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
		})
	}
}

func TestModels_NoneListed(t *testing.T) {
	e := serveModels(t)

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "" || r.stderr != "" {
		t.Errorf("stdout, stderr = %q, %q, want both empty", r.stdout, r.stderr)
	}
}

func TestModels_BuildsTheClient(t *testing.T) {
	e := serveModels(t, "agent")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models")

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

func TestModels_Token(t *testing.T) {
	for _, tc := range []struct {
		name, token, want string
	}{
		{"set", "secret", "Bearer secret"},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := serveModels(t, "agent")

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_TOKEN": tc.token}, "models")

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

func TestModels_NoTokenFlag(t *testing.T) {
	e := serveModels(t, "agent")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models", "--token", "secret")

	wantCode(t, r, process.ExitUsage)
	wantNoRequest(t, e)
}

func TestModels_EndpointError(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: always[aitest.ModelsRequest](aitest.ErrorReply{Status: http.StatusInternalServerError, Message: "engine down"})})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models")

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
