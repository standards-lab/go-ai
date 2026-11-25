package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"
)

type embedding struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

func embedEndpoint(t *testing.T, ids []string, vectors ...[]float32) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{
		Models:     listing(ids...),
		Embeddings: always[aitest.EmbeddingRequest](aitest.EmbeddingsReply{Vectors: vectors, PromptTokens: 7, TotalTokens: 9}),
	})
}

func embedRequests(t *testing.T, e *aitest.Endpoint) (sent []embedding, listed int) {
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

func TestEmbed_FallsBackToTheOneModel(t *testing.T) {
	e := embedEndpoint(t, []string{"embedder"}, vecA)

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "embed", "hello")

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

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "embed", "hello")

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

func TestEmbed_FallbackListingFails(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Models: always[aitest.ModelsRequest](aitest.ErrorReply{Status: http.StatusInternalServerError, Message: "engine down"})})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "embed", "hello")

	wantCode(t, r, process.ExitFailure)
	if want := "infer embed: no model set, and listing the endpoint's: "; !strings.HasPrefix(r.stderr, want) || !strings.Contains(r.stderr, "engine down") {
		t.Errorf("stderr = %q, want it to start with %q and carry the endpoint's error", r.stderr, want)
	}
	if n := len(e.Requests()); n != 1 {
		t.Errorf("endpoint received %d requests, want the one listing", n)
	}
}

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

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": tc.env}, tc.args...)

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

			r := newHarnessIn(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder"}, tc.stdin).run(tc.args...)

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

func TestEmbed_UsageOnStderr(t *testing.T) {
	e := embedEndpoint(t, nil, vecA)

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder"}, "embed", "hello")

	wantCode(t, r, process.ExitOK)
	if want := "usage: 7 input tokens, 9 total tokens\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if strings.Contains(r.stdout, "token") {
		t.Errorf("stdout = %q, want the vectors alone", r.stdout)
	}
	wantVectors(t, r.stdout, vecA)
}

func TestEmbed_NoInput(t *testing.T) {
	e := embedEndpoint(t, []string{"embedder"}, vecA)

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "embed")

	wantCode(t, r, process.ExitUsage)
	if want := "infer embed: no input"; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	wantNoRequest(t, e)
}

const passages = `{"input_types":{"document":{"prefix":"passage: "}}}`

func TestEmbed_InputType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		args    []string
		want    []string
	}{
		{"document under the variable's profile", passages, []string{"embed", "--input-type", "document", "first", "second"}, []string{"passage: first", "passage: second"}},
		{"document under the flag's profile", "", []string{"--embedding-profile", passages, "embed", "--input-type=document", "first"}, []string{"passage: first"}},
		{"the flag's profile over the variable's", `{"input_types":{"document":{"prefix":"doc: "}}}`, []string{"embed", "--embedding-profile", passages, "--input-type", "document", "first"}, []string{"passage: first"}},
		{"document with no profile", "", []string{"embed", "--input-type", "document", "first"}, []string{"first"}},
		{"a type the profile declares nothing for", passages, []string{"embed", "--input-type", "query", "first"}, []string{"first"}},
		{"no type under a profile", passages, []string{"embed", "first"}, []string{"first"}},
		{"none under a profile", passages, []string{"embed", "--input-type", "none", "first"}, []string{"first"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := embedEndpoint(t, nil, [][]float32{vecA, vecB}[:len(tc.want)]...)

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder", "INFER_EMBEDDING_PROFILE": tc.profile}, tc.args...)

			wantCode(t, r, process.ExitOK)
			sent, _ := embedRequests(t, e)
			if len(sent) != 1 || !slices.Equal(sent[0].Input, tc.want) {
				t.Errorf("embeddings requests = %+v, want one sending %q", sent, tc.want)
			}
		})
	}
}

func TestEmbed_InputTypeNative(t *testing.T) {
	var got []aitest.EmbeddingRequest
	e := aitest.Serve(t, aitest.Routes{Embeddings: func(_ context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		got = append(got, req)
		return aitest.EmbeddingsReply{Vectors: [][]float32{vecA}}
	}})
	env := map[string]string{
		"INFER_BASE_URL":          e.URL(),
		"INFER_MODEL":             "embedder",
		"INFER_EMBEDDING_PROFILE": `{"input_types":{"query":{"native":{"input_type":"search_query"}}}}`,
	}

	r := runApp(t, env, "embed", "--input-type", "query", "first")

	wantCode(t, r, process.ExitOK)
	if len(got) != 1 || !slices.Equal(got[0].Input, []string{"first"}) || string(got[0].Native) != `{"input_type":"search_query"}` {
		t.Errorf("embeddings requests = %+v, want one sending first bare with input_type search_query", got)
	}
}

func TestEmbed_InputTypeRefused(t *testing.T) {
	for _, value := range []string{"passage", "Document", ""} {
		t.Run(value, func(t *testing.T) {
			e := embedEndpoint(t, []string{"embedder"}, vecA)

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_EMBEDDING_PROFILE": passages}, "embed", "--input-type="+value, "hello")

			wantCode(t, r, process.ExitUsage)
			if want := `infer embed: invalid value "` + value + `" for flag -input-type: `; !strings.HasPrefix(r.stderr, want) || !strings.Contains(r.stderr, "none, query, document") {
				t.Errorf("stderr = %q, want it to start with %q and name the types", r.stderr, want)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			wantNothingBuilt(t, r)
			wantNoRequest(t, e)
		})
	}
}

func TestEmbeddingProfile_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		want    string
	}{
		{"not JSON", `{"input_types":`, "embedding profile: --embedding-profile (INFER_EMBEDDING_PROFILE): "},
		{"unknown member", `{"input_type":{}}`, "embedding profile: --embedding-profile (INFER_EMBEDDING_PROFILE): "},
		{"Go field name", `{"InputTypes":{}}`, "embedding profile: --embedding-profile (INFER_EMBEDDING_PROFILE): "},
		{"unknown input type", `{"input_types":{"passage":{"prefix":"passage: "}}}`, "embedding profile: --embedding-profile (INFER_EMBEDDING_PROFILE): "},
		{"a convention for none", `{"input_types":{"none":{"prefix":"x: "}}}`, "embedder: --embedding-profile (INFER_EMBEDDING_PROFILE): "},
		{"a native clashing with input", `{"input_types":{"query":{"native":{"input":"x"}}}}`, "embedder: --embedding-profile (INFER_EMBEDDING_PROFILE): "},
	} {
		for _, command := range []string{"embed", "tokenize"} {
			t.Run(tc.name+" "+command, func(t *testing.T) {
				e := embedEndpoint(t, []string{"embedder"}, vecA)

				r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_EMBEDDING_PROFILE": tc.profile}, command, "hello")

				wantCode(t, r, process.ExitUsage)
				label := "infer " + command + ": "
				if !strings.HasPrefix(r.stderr, label+tc.want) || strings.Count(r.stderr, label) != 1 {
					t.Errorf("stderr = %q, want one error starting %q", r.stderr, label+tc.want)
				}
				wantNoRequest(t, e)
			})
		}
	}
}

func TestEmbeddingProfile_IgnoredByOtherCommands(t *testing.T) {
	e := serveModels(t, "agent")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_EMBEDDING_PROFILE": "{"}, "models")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "agent\n" {
		t.Errorf("stdout = %q, want %q", r.stdout, "agent\n")
	}
}
