package app_test

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

func TestRetryPolicy_FlagOverVariable(t *testing.T) {
	vars := map[string]string{"INFER_RETRIES": "4", "INFER_RETRY_BASE_DELAY": "100ms", "INFER_RETRY_MAX_DELAY": "2s"}
	fromVars := ai.RetryPolicy{MaxAttempts: 4, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second}
	defaults := ai.RetryPolicy{MaxAttempts: 3, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
		want ai.RetryPolicy
	}{
		{"unset", nil, []string{"probe"}, defaults},
		{"variables given empty", map[string]string{"INFER_RETRIES": "", "INFER_RETRY_BASE_DELAY": "", "INFER_RETRY_MAX_DELAY": ""},
			[]string{"probe"}, defaults},
		{"variables", vars, []string{"probe"}, fromVars},
		{"flags before the command", vars, []string{"--retries", "5", "--retry-base-delay", "1s", "--retry-max-delay", "1m", "probe"},
			ai.RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Minute}},
		{"flags after the command", vars, []string{"probe", "--retries=4", "--retry-base-delay=200ms", "--retry-max-delay=3s"},
			ai.RetryPolicy{MaxAttempts: 4, BaseDelay: 200 * time.Millisecond, MaxDelay: 3 * time.Second}},
		{"one flag over its variable", vars, []string{"probe", "--retries", "1"},
			ai.RetryPolicy{MaxAttempts: 1, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second}},
		{"flags given empty", vars, []string{"probe", "--retries=", "--retry-base-delay=", "--retry-max-delay="}, defaults},
		{"flag over an unparsable variable", map[string]string{"INFER_RETRIES": "many"}, []string{"probe", "--retries", "2"},
			ai.RetryPolicy{MaxAttempts: 2, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}},
		{"one attempt", nil, []string{"probe", "--retries", "1"},
			ai.RetryPolicy{MaxAttempts: 1, BaseDelay: 500 * time.Millisecond, MaxDelay: 30 * time.Second}},
		{"most delay below the default base delay", nil, []string{"probe", "--retry-max-delay", "100ms"},
			ai.RetryPolicy{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: 100 * time.Millisecond}},
		{"zero most delay", map[string]string{"INFER_RETRY_MAX_DELAY": "0s"}, []string{"probe"},
			ai.RetryPolicy{MaxAttempts: 3, MaxDelay: 0}},
		{"base delay given above a most delay", nil, []string{"probe", "--retry-base-delay", "1s", "--retry-max-delay", "100ms"},
			ai.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 100 * time.Millisecond}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.env)
			probe(h.app, h.app.Nodes().RetryPolicy, "%+v")

			r := h.run(tc.args...)

			wantCode(t, r, process.ExitOK)
			if want := fmt.Sprintf("%+v", tc.want); r.stdout != want {
				t.Errorf("retry policy = %s, want %s", r.stdout, want)
			}
		})
	}
}

type retryEndpoint struct {
	*aitest.Endpoint
	mu       sync.Mutex
	arrivals []time.Time
}

func (e *retryEndpoint) times() []time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]time.Time(nil), e.arrivals...)
}

func serveRetry(t *testing.T, failures int, retryAfter string) *retryEndpoint {
	t.Helper()
	e := &retryEndpoint{}
	models := listing("agent")
	e.Endpoint = aitest.Serve(t, aitest.Routes{Models: func(ctx context.Context, req aitest.ModelsRequest) aitest.Reply {
		e.mu.Lock()
		e.arrivals = append(e.arrivals, time.Now())
		e.mu.Unlock()
		if req.Arrival <= failures {
			return aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "overloaded", RetryAfter: retryAfter}
		}
		return models(ctx, req)
	}})
	return e
}

func TestRetries_WaitForRetryAfter(t *testing.T) {
	e := serveRetry(t, 1, "1")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models", "--retries", "2", "--retry-max-delay", "2s")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "agent\n" {
		t.Errorf("stdout = %q, want %q", r.stdout, "agent\n")
	}
	arrivals := e.times()
	if len(arrivals) != 2 {
		t.Fatalf("endpoint received %d requests, want 2", len(arrivals))
	}
	if gap := arrivals[1].Sub(arrivals[0]); gap < time.Second {
		t.Errorf("second attempt %s after the first, want at least the Retry-After's 1s", gap)
	}
}

func TestRetries_Default(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
		code     int
		requests int
	}{
		{"one 503", 1, process.ExitOK, 2},
		{"three 503s", 3, process.ExitFailure, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := serveRetry(t, tc.failures, "0")

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "models")

			wantCode(t, r, tc.code)
			if tc.code == process.ExitOK && r.stdout != "agent\n" {
				t.Errorf("stdout = %q, want %q", r.stdout, "agent\n")
			}
			if tc.code == process.ExitFailure && (!strings.HasPrefix(r.stderr, "infer models: ") || !strings.Contains(r.stderr, "503")) {
				t.Errorf("stderr = %q, want one infer models error naming the 503", r.stderr)
			}
			if n := len(e.Requests()); n != tc.requests {
				t.Errorf("endpoint received %d requests, want %d", n, tc.requests)
			}
		})
	}
}

func TestRetries_GivenOverDefault(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      map[string]string
		args     []string
		requests int
	}{
		{"--retries 1", nil, []string{"--retries", "1"}, 1},
		{"--retries 0", nil, []string{"--retries", "0"}, 1},
		{"INFER_RETRIES 2", map[string]string{"INFER_RETRIES": "2"}, nil, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := serveRetry(t, 3, "0")
			env := map[string]string{"INFER_BASE_URL": e.URL()}
			maps.Copy(env, tc.env)

			r := runApp(t, env, append([]string{"models"}, tc.args...)...)

			wantCode(t, r, process.ExitFailure)
			if !strings.HasPrefix(r.stderr, "infer models: ") || !strings.Contains(r.stderr, "503") {
				t.Errorf("stderr = %q, want one infer models error naming the 503", r.stderr)
			}
			if n := len(e.Requests()); n != tc.requests {
				t.Errorf("endpoint received %d requests, want %d", n, tc.requests)
			}
		})
	}
}

func TestRetries_Chat(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(_ context.Context, req aitest.ChatRequest) aitest.Reply {
		if req.Arrival == 1 {
			return aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "overloaded", RetryAfter: "0"}
		}
		return chatReply("", "stop", "Paris")
	}})

	r := runApp(t, agent(e), "chat", "--stream=false", "--retries", "2", "capital?")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "Paris\n" {
		t.Errorf("stdout = %q, want %q", r.stdout, "Paris\n")
	}
	if n := len(chatBodies(t, e, completionsRoute)); n != 2 {
		t.Errorf("sent %d chat requests, want 2", n)
	}
}

func TestRetryRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
		want string
	}{
		{
			name: "unparsable retries variable",
			env:  map[string]string{"INFER_RETRIES": "many"},
			args: []string{"models"},
			want: `retry policy: --retries (INFER_RETRIES): "many" isn't a whole number`,
		},
		{
			name: "unparsable retries flag",
			args: []string{"models", "--retries", "2.5"},
			want: `retry policy: --retries (INFER_RETRIES): "2.5" isn't a whole number`,
		},
		{
			name: "unparsable base delay",
			env:  map[string]string{"INFER_RETRY_BASE_DELAY": "soon"},
			args: []string{"models"},
			want: `retry policy: --retry-base-delay (INFER_RETRY_BASE_DELAY): time: invalid duration "soon"`,
		},
		{
			name: "unparsable most delay",
			args: []string{"models", "--retry-max-delay", "5"},
			want: `retry policy: --retry-max-delay (INFER_RETRY_MAX_DELAY): time: missing unit in duration "5"`,
		},
		{
			name: "negative retries",
			env:  map[string]string{"INFER_RETRIES": "-1"},
			args: []string{"models"},
			want: "client: --retries (INFER_RETRIES): ai: retry policy has a negative attempt count",
		},
		{
			name: "negative base delay",
			args: []string{"models", "--retry-base-delay", "-1s", "--retry-max-delay", "1s"},
			want: "client: --retry-base-delay (INFER_RETRY_BASE_DELAY): ai: retry policy has a negative delay",
		},
		{
			name: "negative most delay",
			env:  map[string]string{"INFER_RETRY_MAX_DELAY": "-1s"},
			args: []string{"models"},
			want: "client: --retry-max-delay (INFER_RETRY_MAX_DELAY): ai: retry policy has a negative delay",
		},
		{
			name: "inverted delays",
			env:  map[string]string{"INFER_RETRY_BASE_DELAY": "2s", "INFER_RETRY_MAX_DELAY": "1s"},
			args: []string{"models"},
			want: "client: --retry-base-delay (INFER_RETRY_BASE_DELAY) and --retry-max-delay (INFER_RETRY_MAX_DELAY): ai: retry policy's base delay is above its most delay",
		},
		{
			name: "base delay above the default most delay",
			args: []string{"models", "--retry-base-delay", "1m"},
			want: "client: --retry-base-delay (INFER_RETRY_BASE_DELAY) and --retry-max-delay (INFER_RETRY_MAX_DELAY): ai: retry policy's base delay is above its most delay",
		},
		{
			name: "base delay with a zero most delay",
			env:  map[string]string{"INFER_RETRY_BASE_DELAY": "500ms"},
			args: []string{"models", "--retry-max-delay", "0s"},
			want: "client: --retry-base-delay (INFER_RETRY_BASE_DELAY) needs --retry-max-delay (INFER_RETRY_MAX_DELAY) at least as long",
		},
		{
			name: "negative retries through the chat client",
			args: []string{"chat", "--retries", "-2", "hello"},
			want: "chat client: --retries (INFER_RETRIES): ai: retry policy has a negative attempt count",
		},
		{
			name: "negative retries through the transcriber",
			args: []string{"transcribe", "--retries", "-2", "clip.wav"},
			want: "transcriber: --retries (INFER_RETRIES): ai: retry policy has a negative attempt count",
		},
		{
			name: "unparsable idle timeout variable",
			env:  map[string]string{"INFER_IDLE_TIMEOUT": "soon"},
			args: []string{"chat", "hello"},
			want: `idle timeout: --idle-timeout (INFER_IDLE_TIMEOUT): time: invalid duration "soon"`,
		},
		{
			name: "negative idle timeout flag",
			args: []string{"chat", "--idle-timeout", "-1s", "hello"},
			want: "idle timeout: --idle-timeout (INFER_IDLE_TIMEOUT): negative duration -1s; 0 sets no bound",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := serveModels(t, "agent")
			env := map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "agent"}
			maps.Copy(env, tc.env)

			r := runApp(t, env, tc.args...)

			wantCode(t, r, process.ExitUsage)
			label := "infer " + tc.args[0] + ": "
			if first, _, _ := strings.Cut(r.stderr, "\n"); first != label+tc.want {
				t.Errorf("stderr = %q, want its first line %q", r.stderr, label+tc.want)
			}
			if n := strings.Count(r.stderr, label); n != 1 {
				t.Errorf("stderr = %q, want the error reported once", r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			wantNoRequest(t, e)
		})
	}
}

func stallingStream(t *testing.T, hold *aitest.Gate) *aitest.Endpoint {
	t.Helper()
	reply := chatReply("", "stop", "Paris")
	reply.Hold, reply.HoldAfter = hold, 1
	return chatEndpoint(t, nil, completionsRoute, reply)
}

func TestChat_IdleTimeout(t *testing.T) {
	const stall = 500 * time.Millisecond
	for _, tc := range []struct {
		name  string
		env   string
		args  []string
		ended bool
	}{
		{"unset", "", nil, false},
		{"variable", "50ms", nil, true},
		{"flag", "", []string{"--idle-timeout", "50ms"}, true},
		{"flag over variable", "10s", []string{"--idle-timeout=50ms"}, true},
		{"longer flag over variable", "50ms", []string{"--idle-timeout", "10s"}, false},
		{"flag given empty over variable", "50ms", []string{"--idle-timeout="}, false},
		{"flag zero over variable", "50ms", []string{"--idle-timeout", "0"}, false},
		{"flag over an unparsable variable", "soon", []string{"--idle-timeout", "10s"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hold := &aitest.Gate{}
			e := stallingStream(t, hold)
			env := agent(e)
			env["INFER_IDLE_TIMEOUT"] = tc.env

			done := newHarness(t, env).start(append(append([]string{"chat"}, tc.args...), "capital?")...)
			if !tc.ended {
				hold.Await(t, 1)
				release := time.AfterFunc(stall, hold.Open)
				t.Cleanup(func() { release.Stop() })
			}
			r := finish(t, done)

			if !tc.ended {
				wantCode(t, r, process.ExitOK)
				if r.stdout != "Paris\n" {
					t.Errorf("stdout = %q, want %q", r.stdout, "Paris\n")
				}
				return
			}
			wantCode(t, r, process.ExitFailure)
			if r.stdout != "Paris\n" {
				t.Errorf("stdout = %q, want the text before the stall kept, %q", r.stdout, "Paris\n")
			}
			if want := "stream idle timeout (50ms)"; !strings.HasPrefix(r.stderr, "infer chat: ") || !strings.Contains(r.stderr, want) {
				t.Errorf("stderr = %q, want one infer chat error naming %q", r.stderr, want)
			}
		})
	}
}

func TestCommandSettings_ReadByTheirCommandAlone(t *testing.T) {
	routes := aitest.Routes{
		Models:          listing("agent"),
		Embeddings:      always[aitest.EmbeddingRequest](aitest.EmbeddingsReply{Vectors: [][]float32{{1}}, PromptTokens: 7, TotalTokens: 9}),
		ChatCompletions: always[aitest.ChatRequest](chatReply("", "stop", "Paris")),
		Transcriptions:  always[aitest.TranscriptionRequest](routeReply("hello world")),
	}
	clip := writeClip(t, "clip.wav")
	for _, tc := range []struct {
		variable string
		args     []string
	}{
		{"INFER_TRANSCRIPTION_MODE", []string{"models"}},
		{"INFER_TRANSCRIPTION_MODE", []string{"embed", "hello"}},
		{"INFER_TRANSCRIPTION_MODE", []string{"chat", "hello"}},
		{"INFER_IDLE_TIMEOUT", []string{"models"}},
		{"INFER_IDLE_TIMEOUT", []string{"embed", "hello"}},
		{"INFER_IDLE_TIMEOUT", []string{"transcribe", clip}},
	} {
		t.Run(tc.variable+" "+tc.args[0], func(t *testing.T) {
			e := aitest.Serve(t, routes)
			env := agent(e)
			env[tc.variable] = "bogus"

			r := runApp(t, env, tc.args...)

			wantCode(t, r, process.ExitOK)
		})
	}
}
