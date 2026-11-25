package app_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"
)

type tokenizeEndpoint struct {
	*aitest.Endpoint
	mu   sync.Mutex
	sent []aitest.TokenizeRequest
}

func serveTokenize(t *testing.T, ids []string, replies ...aitest.Reply) *tokenizeEndpoint {
	t.Helper()
	te := &tokenizeEndpoint{}
	te.Endpoint = aitest.Serve(t, aitest.Routes{
		Models: listing(ids...),
		Tokenize: func(_ context.Context, req aitest.TokenizeRequest) aitest.Reply {
			te.mu.Lock()
			defer te.mu.Unlock()
			te.sent = append(te.sent, req)
			return replies[min(len(te.sent), len(replies))-1]
		},
	})
	return te
}

func (te *tokenizeEndpoint) requests() []aitest.TokenizeRequest {
	te.mu.Lock()
	defer te.mu.Unlock()
	return slices.Clone(te.sent)
}

func prompts(reqs []aitest.TokenizeRequest) []string {
	out := make([]string, len(reqs))
	for i, r := range reqs {
		out[i] = r.Prompt
	}
	return out
}

func TestTokenize_PrintsTheCount(t *testing.T) {
	e := serveTokenize(t, nil, aitest.TokenizeReply{Count: 5, MaxModelLen: 512, Tokens: []int{0, 11, 12, 13, 2}})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder"}, "tokenize", "hello world")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "5\n" {
		t.Errorf("stdout = %q, want %q", r.stdout, "5\n")
	}
	if want := "max model length: 512 tokens\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	reqs := e.Requests()
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Path != "/v1/tokenize" {
		t.Fatalf("requests = %+v, want one POST /v1/tokenize", reqs)
	}
	if want := `{"model":"embedder","prompt":"hello world","add_special_tokens":true}`; string(reqs[0].Body) != want {
		t.Errorf("body = %s, want %s", reqs[0].Body, want)
	}
}

func TestTokenize_NoMaxModelLength(t *testing.T) {
	e := serveTokenize(t, nil, aitest.TokenizeReply{Count: 3})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder"}, "tokenize", "hello")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "3\n" || r.stderr != "" {
		t.Errorf("stdout = %q and stderr = %q, want %q and nothing", r.stdout, r.stderr, "3\n")
	}
}

func TestTokenize_Inputs(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  []string
	}{
		{"arguments", []string{"tokenize", "first", "second input", "third"}, "ignored\n", []string{"first", "second input", "third"}},
		{"stdin", []string{"tokenize"}, "first\r\n\r\nthird\n", []string{"first", "", "third"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := serveTokenize(t, nil, aitest.TokenizeReply{Count: 4}, aitest.TokenizeReply{Count: 2}, aitest.TokenizeReply{Count: 9})

			r := newHarnessIn(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder"}, tc.stdin).run(tc.args...)

			wantCode(t, r, process.ExitOK)
			if got := prompts(e.requests()); !slices.Equal(got, tc.want) {
				t.Errorf("prompts = %q, want %q, one request each", got, tc.want)
			}
			if r.stdout != "4\n2\n9\n" {
				t.Errorf("stdout = %q, want one count per input, in order", r.stdout)
			}
		})
	}
}

func TestTokenize_InputType(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile string
		args    []string
		want    string
	}{
		{"document under a profile", passages, []string{"tokenize", "--input-type", "document", "hello"}, "passage: hello"},
		{"document with no profile", "", []string{"tokenize", "--input-type", "document", "hello"}, "hello"},
		{"no type under a profile", passages, []string{"tokenize", "hello"}, "hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := serveTokenize(t, nil, aitest.TokenizeReply{Count: 4})

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder", "INFER_EMBEDDING_PROFILE": tc.profile}, tc.args...)

			wantCode(t, r, process.ExitOK)
			if got := prompts(e.requests()); !slices.Equal(got, []string{tc.want}) {
				t.Errorf("prompts = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTokenize_FallsBackToTheOneModel(t *testing.T) {
	e := serveTokenize(t, []string{"embedder"}, aitest.TokenizeReply{Count: 1})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "tokenize", "hello")

	wantCode(t, r, process.ExitOK)
	if reqs := e.requests(); len(reqs) != 1 || reqs[0].Model != "embedder" || !reqs[0].AddSpecialTokens {
		t.Errorf("tokenize requests = %+v, want one naming model embedder with special tokens", reqs)
	}
}

func TestTokenize_InputTypeRefused(t *testing.T) {
	e := serveTokenize(t, []string{"embedder"}, aitest.TokenizeReply{Count: 1})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "tokenize", "--input-type", "passage", "hello")

	wantCode(t, r, process.ExitUsage)
	if want := `infer tokenize: invalid value "passage" for flag -input-type: `; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e.Endpoint)
}

func TestTokenize_NoInput(t *testing.T) {
	e := serveTokenize(t, []string{"embedder"}, aitest.TokenizeReply{Count: 1})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "tokenize")

	wantCode(t, r, process.ExitUsage)
	if want := "infer tokenize: no input"; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	wantNoRequest(t, e.Endpoint)
}

func TestTokenize_EndpointError(t *testing.T) {
	e := serveTokenize(t, nil, aitest.ErrorReply{Status: http.StatusNotFound, Message: "no tokenize route"})

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "embedder"}, "tokenize", "hello")

	wantCode(t, r, process.ExitFailure)
	if !strings.HasPrefix(r.stderr, "infer tokenize: ") || !strings.Contains(r.stderr, "no tokenize route") {
		t.Errorf("stderr = %q, want infer tokenize's error carrying the endpoint's", r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
}
