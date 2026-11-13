package app_test

import (
	"fmt"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// The retry policy is zero, go-ai's default of no retry, when unset; each
// field is its variable's when set, and its root flag's when given,
// wherever on the command line, even given empty.
func TestRetryPolicy_FlagOverVariable(t *testing.T) {
	vars := map[string]string{"INFER_RETRIES": "3", "INFER_RETRY_BASE_DELAY": "100ms", "INFER_RETRY_MAX_DELAY": "2s"}
	fromVars := ai.RetryPolicy{MaxAttempts: 3, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second}
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
		want ai.RetryPolicy
	}{
		{"unset", nil, []string{"probe"}, ai.RetryPolicy{}},
		{"variables", vars, []string{"probe"}, fromVars},
		{"flags before the command", vars, []string{"--retries", "5", "--retry-base-delay", "1s", "--retry-max-delay", "1m", "probe"},
			ai.RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Minute}},
		{"flags after the command", vars, []string{"probe", "--retries=4", "--retry-base-delay=200ms", "--retry-max-delay=3s"},
			ai.RetryPolicy{MaxAttempts: 4, BaseDelay: 200 * time.Millisecond, MaxDelay: 3 * time.Second}},
		{"one flag over its variable", vars, []string{"probe", "--retries", "1"},
			ai.RetryPolicy{MaxAttempts: 1, BaseDelay: 100 * time.Millisecond, MaxDelay: 2 * time.Second}},
		{"flags given empty", vars, []string{"probe", "--retries=", "--retry-base-delay=", "--retry-max-delay="}, ai.RetryPolicy{}},
		{"flag over an unparsable variable", map[string]string{"INFER_RETRIES": "many"}, []string{"probe", "--retries", "2"},
			ai.RetryPolicy{MaxAttempts: 2}},
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

// retryEndpoint is an endpoint that records when each request arrived.
type retryEndpoint struct {
	*apptest.Endpoint
	mu       sync.Mutex
	arrivals []time.Time
}

// times returns when each request e received arrived, in order.
func (e *retryEndpoint) times() []time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]time.Time(nil), e.arrivals...)
}

// serveRetry serves a [retryEndpoint] whose GET /v1/models answers 503
// with Retry-After retryAfter to the first failures requests, then lists
// agent.
func serveRetry(t *testing.T, failures int, retryAfter string) *retryEndpoint {
	t.Helper()
	e := &retryEndpoint{}
	models := apptest.Models("agent")
	e.Endpoint = apptest.Serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.arrivals = append(e.arrivals, time.Now())
		n := len(e.arrivals)
		e.mu.Unlock()
		if n <= failures {
			w.Header().Set("Retry-After", retryAfter)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		models.ServeHTTP(w, r)
	}))
	return e
}

// With retries set, a 503 with Retry-After, then a 200, succeeds after the
// wait: the second attempt arrives no sooner than the Retry-After's second
// after the first, which a retry that ignored it, under a base delay of
// zero, would send at once.
func TestRetries_WaitForRetryAfter(t *testing.T) {
	e := serveRetry(t, 1, "1")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "models", "--retries", "2", "--retry-max-delay", "2s")

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

// With no retries set, one 503 is the run's failure, exit 1, after one
// request, whatever its Retry-After.
func TestRetries_NoneWhenUnset(t *testing.T) {
	e := serveRetry(t, 1, "0")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_RETRY_MAX_DELAY": "1s"}, "models")

	wantCode(t, r, process.ExitFailure)
	if !strings.HasPrefix(r.stderr, "infer models: ") || !strings.Contains(r.stderr, "503") {
		t.Errorf("stderr = %q, want one infer models error naming the 503", r.stderr)
	}
	if n := len(e.Requests()); n != 1 {
		t.Errorf("endpoint received %d requests, want 1", n)
	}
}

// The retry policy reaches the chat client too: an unstreamed chat whose
// first attempt is answered 503 is sent again.
func TestRetries_Chat(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	reply := apptest.Reply(completionsRoute, completionsReply("Paris", "", "stop"))
	e := chatEndpoint(t, nil, completionsRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		n := attempts
		mu.Unlock()
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		reply.ServeHTTP(w, r)
	}))

	r := runApp(t, agent(e), "chat", "--stream=false", "--retries", "2", "capital?")

	wantCode(t, r, process.ExitOK)
	if r.stdout != "Paris\n" {
		t.Errorf("stdout = %q, want %q", r.stdout, "Paris\n")
	}
	if n := len(chatBodies(t, e, completionsRoute)); n != 2 {
		t.Errorf("sent %d chat requests, want 2", n)
	}
}

// Each refused retry or idle setting exits 2 before any request, its
// error reported once on stderr, naming the flag and its variable: one
// that doesn't parse, by the node that reads it, and a negative or
// inverted policy, by go-ai's refusal in the client node of the command.
func TestRetryRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
		want string // the error, after the command's label
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
			name: "base delay without a most delay",
			args: []string{"models", "--retry-base-delay", "1s"},
			want: "client: --retry-base-delay (INFER_RETRY_BASE_DELAY) needs --retry-max-delay (INFER_RETRY_MAX_DELAY) at least as long",
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
			e := apptest.Serve(t, apptest.Models("agent"))
			env := map[string]string{"INFER_BASE_URL": e.URL, "INFER_MODEL": "agent"}
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

// stallingStream serves a streamed completions endpoint that sends the
// role chunk and a "Paris" delta, then sends nothing for stall, or until
// the request ends, then the rest of a completed reply.
func stallingStream(t *testing.T, stall time.Duration) *apptest.Endpoint {
	t.Helper()
	frames := completionsChunks("", "stop", "Paris")
	return chatEndpoint(t, nil, completionsRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		for i, f := range frames {
			if i == 2 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(stall):
				}
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", f); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}))
}

// The idle timeout is INFER_IDLE_TIMEOUT's when set, and chat's
// --idle-timeout's when given, even given empty, which sets no bound. A
// bound shorter than the stream's stall ends the stream, exit 1, the text
// already written kept and the error naming the idle bound; none, or a
// longer one, lets the stream finish.
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
			e := stallingStream(t, stall)
			env := agent(e)
			env["INFER_IDLE_TIMEOUT"] = tc.env

			r := runApp(t, env, append(append([]string{"chat"}, tc.args...), "capital?")...)

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

// Each command's own setting is read by that command alone: a variable
// that would refuse its command, an unknown transcription mode or an
// unparsable idle timeout, fails no other command.
func TestCommandSettings_ReadByTheirCommandAlone(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/models", apptest.Models("agent"))
	mux.Handle("POST /v1/embeddings", apptest.Embeddings(7, 9, []float32{1}))
	mux.Handle("POST /v1/"+completionsRoute, apptest.Stream(completionsRoute, completionsChunks("", "stop", "Paris")...))
	mux.Handle("POST /v1/"+transcriptionsRoute, apptest.Reply(transcriptionsRoute, routeReply("hello world")))
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
			e := apptest.Serve(t, mux)
			env := agent(e)
			env[tc.variable] = "bogus"

			r := runApp(t, env, tc.args...)

			wantCode(t, r, process.ExitOK)
		})
	}
}
