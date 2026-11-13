package app_test

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// wantHeaders fails t unless e received at least one request and every
// request carried each of want's headers with exactly its values, in
// order.
func wantHeaders(t *testing.T, e *apptest.Endpoint, want http.Header) {
	t.Helper()
	reqs := e.Requests()
	if len(reqs) == 0 {
		t.Fatal("endpoint received no request")
	}
	for _, r := range reqs {
		for _, name := range slices.Sorted(maps.Keys(want)) {
			if got := r.Header.Values(name); !slices.Equal(got, want[name]) {
				t.Errorf("%s %s: %s = %q, want %q", r.Method, r.Path, name, got, want[name])
			}
		}
	}
}

// INFER_HEADER's lines each reach the request: one line or several, each
// trimmed of surrounding space, a blank line skipped, a value kept whole
// after its first colon, and a name given twice sending both values.
func TestHeader_ReachesTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		want         http.Header
	}{
		{"one", "api-key: secret", http.Header{"Api-Key": {"secret"}}},
		{
			name:   "several",
			header: "api-key: secret\nX-Tenant: docs\nx-route: a:b",
			want:   http.Header{"Api-Key": {"secret"}, "X-Tenant": {"docs"}, "X-Route": {"a:b"}},
		},
		{
			name:   "blank lines and surrounding space",
			header: "\n  api-key :  secret  \r\n\n\t\nX-Tenant:docs\n\n",
			want:   http.Header{"Api-Key": {"secret"}, "X-Tenant": {"docs"}},
		},
		{"a name twice", "x-tag: one\nX-Tag: two", http.Header{"X-Tag": {"one", "two"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := apptest.Serve(t, apptest.Models("agent"))

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_HEADER": tc.header}, "models")

			wantCode(t, r, process.ExitOK)
			wantHeaders(t, e, tc.want)
		})
	}
}

// An INFER_HEADER of blank lines alone sends no header of its own: the
// request carries only what it would without one.
func TestHeader_BlankSendsNone(t *testing.T) {
	without := apptest.Serve(t, apptest.Models("agent"))
	blank := apptest.Serve(t, apptest.Models("agent"))

	wantCode(t, runApp(t, map[string]string{"INFER_BASE_URL": without.URL}, "models"), process.ExitOK)
	wantCode(t, runApp(t, map[string]string{"INFER_BASE_URL": blank.URL, "INFER_HEADER": "\n  \n"}, "models"), process.ExitOK)

	got, want := blank.Requests()[0].Header, without.Requests()[0].Header
	if !slices.Equal(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want))) {
		t.Errorf("header names = %q, want %q", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
}

// secretValue is the value each refused INFER_HEADER carries, which no
// refusal may echo.
const secretValue = "s3cret-value"

// Each refused INFER_HEADER exits 2 before any request, its error reported
// once on stderr naming INFER_HEADER and never the value: a line with no
// colon, a line with no name, a name the client sets in any case, and a
// name or a value net/http can't send.
func TestHeader_Refusals(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		want         string // after "infer models: client: "
	}{
		{"no colon", "api-key: ok\n" + secretValue, `INFER_HEADER line 2 has no colon: each line is "Name: value"`},
		{"no name", "  : " + secretValue, `INFER_HEADER line 1 has no name before its colon`},
		{"authorization", "Authorization: Bearer " + secretValue, "INFER_HEADER: ai: Options.Header names Authorization, which the Client sets"},
		{"content type", "content-type: " + secretValue, "INFER_HEADER: ai: Options.Header names Content-Type, which the Client sets"},
		{"accept", "ACCEPT: " + secretValue, "INFER_HEADER: ai: Options.Header names Accept, which the Client sets"},
		{"unsendable name", "api key: " + secretValue, `INFER_HEADER: ai: Options.Header name "api key" isn't a header name`},
		{"unsendable value", "api-key: " + secretValue + "\x7f", "INFER_HEADER: ai: Options.Header Api-Key has a value with a control character"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := apptest.Serve(t, apptest.Models("agent"))

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_HEADER": tc.header}, "models")

			wantCode(t, r, process.ExitUsage)
			if want := "infer models: client: " + tc.want; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			if n := strings.Count(r.stderr, "infer models: "); n != 1 {
				t.Errorf("stderr = %q, want the error reported once", r.stderr)
			}
			if strings.Contains(r.stderr, secretValue) {
				t.Errorf("stderr = %q, want it never to echo the header's value", r.stderr)
			}
			wantNoRequest(t, e)
		})
	}
}

// INFER_TOKEN and INFER_HEADER together each reach the request: the token
// as the bearer token, the header beside it.
func TestHeader_WithToken(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_TOKEN": "tok", "INFER_HEADER": "api-key: key"}, "models")

	wantCode(t, r, process.ExitOK)
	wantHeaders(t, e, http.Header{"Authorization": {"Bearer tok"}, "Api-Key": {"key"}})
}

// INFER_HEADER reaches every client: the client node, through models and
// embed, the model listing of embed's fallback included; the chat client,
// through chat; and the transcriber, through transcribe in both modes.
func TestHeader_EveryClient(t *testing.T) {
	want := http.Header{"Api-Key": {"key"}}
	header := map[string]string{"INFER_HEADER": "api-key: key"}
	with := func(env map[string]string) map[string]string {
		env = maps.Clone(env)
		maps.Copy(env, header)
		return env
	}
	t.Run("models", func(t *testing.T) {
		e := apptest.Serve(t, apptest.Models("agent"))
		wantCode(t, runApp(t, with(map[string]string{"INFER_BASE_URL": e.URL}), "models"), process.ExitOK)
		wantHeaders(t, e, want)
	})
	t.Run("embed", func(t *testing.T) {
		e := embedEndpoint(t, []string{"embedder"}, vecA)
		wantCode(t, runApp(t, with(map[string]string{"INFER_BASE_URL": e.URL}), "embed", "one"), process.ExitOK)
		if _, listed := embedRequests(t, e); listed != 1 {
			t.Errorf("listed models %d times, want 1", listed)
		}
		wantHeaders(t, e, want)
	})
	for _, stream := range []string{"--stream=true", "--stream=false"} {
		t.Run("chat "+stream, func(t *testing.T) {
			handler := apptest.Reply(completionsRoute, completionsReply("Paris", "", "stop"))
			if stream == "--stream=true" {
				handler = apptest.Stream(completionsRoute, completionsChunks("", "stop", "Paris")...)
			}
			e := chatEndpoint(t, nil, completionsRoute, handler)
			wantCode(t, runApp(t, with(agent(e)), "chat", stream, "hi"), process.ExitOK)
			wantHeaders(t, e, want)
		})
	}
	for _, mode := range []string{"route", "chat"} {
		t.Run("transcribe --mode "+mode, func(t *testing.T) {
			e := transcribeEndpoint(t, nil, completedChat())
			wantCode(t, runApp(t, with(whisper(e)), "transcribe", "--mode", mode, writeClip(t, "clip.wav")), process.ExitOK)
			wantHeaders(t, e, want)
		})
	}
}
