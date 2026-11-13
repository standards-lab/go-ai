package ai_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// okReply is a Chat Completions reply that decodes to a completed response.
const okReply = `{"choices":[{"message":{"role":"assistant","content":"Paris"},"finish_reason":"stop"}]}`

// hello is a minimal valid request.
func hello() *protocol.Request {
	return &protocol.Request{
		Model: "m",
		Input: []protocol.Item{protocol.Message{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{protocol.InputText{Text: "Capital of France?"}},
		}},
	}
}

// newClient returns a Client of base speaking Chat Completions through http.DefaultClient,
// failing t if New refuses it.
func newClient(t *testing.T, base string, opts ai.Options) *ai.Client {
	t.Helper()
	return newClientWith(t, base, http.DefaultClient, opts)
}

// newClientWith returns a Client of base speaking Chat Completions through hc, failing t if New
// refuses it.
func newClientWith(t *testing.T, base string, hc *http.Client, opts ai.Options) *ai.Client {
	t.Helper()
	c, err := ai.New(base, hc, completions.New(), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// TestNewPanicsOnWiringDefect wants a nil HTTP client or format to panic, naming the input.
func TestNewPanicsOnWiringDefect(t *testing.T) {
	cases := []struct {
		name string
		hc   *http.Client
		wf   bool
		want string
	}{
		{"no HTTP client", nil, true, "httpClient"},
		{"no format", http.DefaultClient, false, "wireFormat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if s, ok := r.(string); !ok || !strings.Contains(s, tc.want) || !strings.HasPrefix(s, "ai: ") {
					t.Errorf("New panicked with %v, want a string naming %s", r, tc.want)
				}
			}()
			if tc.wf {
				_, _ = ai.New("http://h/v1", tc.hc, completions.New(), ai.Options{})
			} else {
				_, _ = ai.New("http://h/v1", tc.hc, nil, ai.Options{})
			}
			t.Error("New returned, want a panic")
		})
	}
}

func TestNewRefusesBaseURL(t *testing.T) {
	cases := []struct {
		name string
		base string
		want string
	}{
		{"no base URL", "", "no base URL"},
		{"relative base URL", "/v1", "isn't absolute"},
		{"no host", "http:///v1", "isn't absolute"},
		{"unparsable", "http://h/%zz", "base URL"},
		{"query", "http://h/v1?autoload=false", "has a query"},
		{"empty query", "http://h/v1?", "has a query"},
		{"fragment", "http://h/v1#top", "has a fragment"},
		{"scheme", "ftp://h/v1", "isn't http or https"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ai.New(tc.base, http.DefaultClient, completions.New(), ai.Options{})
			if err == nil {
				t.Fatalf("New = %v, nil; want an error containing %q", c, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "ai: ") {
				t.Errorf("New error = %q, want it to start ai: and contain %q", err, tc.want)
			}
		})
	}
}

// TestNewRefusesHeader wants a fixed header naming one the Client sets refused, in any case.
func TestNewRefusesHeader(t *testing.T) {
	for _, name := range []string{"Content-Type", "accept", "AUTHORIZATION"} {
		t.Run(name, func(t *testing.T) {
			h := http.Header{name: {"x"}}
			c, err := ai.New("http://h/v1", http.DefaultClient, completions.New(), ai.Options{Header: h})
			want := http.CanonicalHeaderKey(name)
			if err == nil {
				t.Fatalf("New = %v, nil; want an error naming %s", c, want)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("New error = %q, want it to name %s", err, want)
			}
		})
	}
}

func TestNewMakesNoRequest(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, okReply)
	called := false
	newClient(t, srv.URL, ai.Options{
		Token: func(context.Context) (string, error) { called = true; return "", nil },
	})
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("New sent %d requests, want 0", n)
	}
	if called {
		t.Error("New called the token source")
	}
}

// TestZeroOptions wants a Client of the zero Options to send no query, no Authorization, and
// no header beyond the Client's and the transport's, and to send one request for a 503.
func TestZeroOptions(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusServiceUnavailable, "")
	c := newClient(t, srv.URL, ai.Options{})
	if _, err := c.Generate(t.Context(), hello()); err == nil {
		t.Fatal("Generate succeeded, want the 503's *APIError")
	}
	r := srv.Only(t)
	if r.RawQuery != "" {
		t.Errorf("query = %q, want none", r.RawQuery)
	}
	got := slices.Sorted(maps.Keys(r.Header))
	// Accept-Encoding, Content-Length, and User-Agent are the transport's.
	want := []string{"Accept", "Accept-Encoding", "Content-Length", "Content-Type", "User-Agent"}
	if !slices.Equal(got, want) {
		t.Errorf("headers = %q, want %q", got, want)
	}
}

func TestGenerateJoinsRouteWithOneSlash(t *testing.T) {
	for base, want := range map[string]string{
		"":     "/chat/completions",
		"/":    "/chat/completions",
		"/v1":  "/v1/chat/completions",
		"/v1/": "/v1/chat/completions",
	} {
		t.Run(base, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, okReply)
			c := newClient(t, srv.URL+base, ai.Options{})
			if _, err := c.Generate(t.Context(), hello()); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			got := srv.Only(t)
			if got.Method != http.MethodPost || got.Path != want {
				t.Errorf("request = %s %s, want POST %s", got.Method, got.Path, want)
			}
		})
	}
}

func TestGenerateCarriesQueryCopiedAtNew(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, okReply)
	query := url.Values{"autoload": {"false"}}
	c := newClient(t, srv.URL+"/v1", ai.Options{Query: query})
	query.Set("autoload", "true")
	query.Add("extra", "1")
	if _, err := c.Generate(t.Context(), hello()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if got := srv.Only(t).RawQuery; got != "autoload=false" {
		t.Errorf("query = %q, want autoload=false", got)
	}
}

func TestGenerateSendsJSONHeaders(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, okReply)
	c := newClient(t, srv.URL, ai.Options{})
	if _, err := c.Generate(t.Context(), hello()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	h := srv.Only(t).Header
	for k, want := range map[string]string{"Content-Type": "application/json", "Accept": "application/json"} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// TestGenerateSendsFixedHeaders wants Options.Header on every request under its canonical
// name, names differing in case joined, copied at New, so a change after it sends nothing new.
func TestGenerateSendsFixedHeaders(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, okReply)
	key := []string{"k1"}
	h := http.Header{"x-api-key": key, "X-Consumer": {"c"}, "x-consumer": {"d"}}
	c := newClient(t, srv.URL, ai.Options{Header: h})
	key[0] = "changed"
	h.Add("X-Late", "1")
	for range 2 {
		if _, err := c.Generate(t.Context(), hello()); err != nil {
			t.Fatalf("Generate: %v", err)
		}
	}
	for i, r := range srv.Requests() {
		if got := r.Header.Values("X-Api-Key"); !slices.Equal(got, []string{"k1"}) {
			t.Errorf("request %d X-Api-Key = %q, want [k1]", i, got)
		}
		if got := r.Header.Values("X-Consumer"); !slices.Equal(got, []string{"c", "d"}) {
			t.Errorf("request %d X-Consumer = %q, want [c d]", i, got)
		}
		if got := r.Header.Values("X-Late"); len(got) != 0 {
			t.Errorf("request %d X-Late = %q, want none", i, got)
		}
	}
}

// TestGenerateFormatHeaderReplacesFixed wants a header the format sets, the messages format's
// anthropic-version, to replace a fixed one of the same name, whatever its case.
func TestGenerateFormatHeaderReplacesFixed(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, `{"type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
	c, err := ai.New(srv.URL, http.DefaultClient, messages.New(), ai.Options{
		Header: http.Header{"anthropic-version": {"1999-01-01"}, "x-api-key": {"k"}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := hello()
	req.MaxOutputTokens = 64
	if _, err := c.Generate(t.Context(), req); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	h := srv.Only(t).Header
	if got := h.Values("Anthropic-Version"); !slices.Equal(got, []string{"2026-10-21"}) {
		t.Errorf("Anthropic-Version = %q, want the format's alone, [2026-10-21]", got)
	}
	if got := h.Get("X-Api-Key"); got != "k" {
		t.Errorf("X-Api-Key = %q, want k", got)
	}
}

func TestGenerateToken(t *testing.T) {
	cases := []struct {
		name  string
		token ai.TokenSource
		want  []string
	}{
		{"bearer", func(context.Context) (string, error) { return "secret", nil }, []string{"Bearer secret"}},
		{"empty token", func(context.Context) (string, error) { return "", nil }, nil},
		{"no source", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, okReply)
			c := newClient(t, srv.URL, ai.Options{Token: tc.token})
			if _, err := c.Generate(t.Context(), hello()); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			got := srv.Only(t).Header.Values("Authorization")
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("Authorization = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGenerateCallsTokenSourceOncePerRequest(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, okReply)
	calls := 0
	c := newClient(t, srv.URL, ai.Options{
		Token: func(context.Context) (string, error) {
			calls++
			return fmt.Sprintf("t%d", calls), nil
		},
	})
	for range 2 {
		if _, err := c.Generate(t.Context(), hello()); err != nil {
			t.Fatalf("Generate: %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("token source called %d times for 2 requests, want 2", calls)
	}
	reqs := srv.Requests()
	for i, want := range []string{"Bearer t1", "Bearer t2"} {
		if got := reqs[i].Header.Get("Authorization"); got != want {
			t.Errorf("request %d Authorization = %q, want %q", i, got, want)
		}
	}
}

func TestGenerateTokenErrorSendsNothing(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, okReply)
	errExpired := errors.New("token expired")
	c := newClient(t, srv.URL, ai.Options{
		Token: func(context.Context) (string, error) { return "", errExpired },
	})
	_, err := c.Generate(t.Context(), hello())
	if !errors.Is(err, errExpired) {
		t.Errorf("Generate error = %v, want it to wrap %v", err, errExpired)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// TestGenerateAPIError covers what the Client makes of a non-2xx reply: an *APIError with the
// status and the whole body, its text built from the details the format decoded or, without a
// message, from the start of the body, and its Retry-After. What the OpenAI-style body gives is
// format's test.
func TestGenerateAPIError(t *testing.T) {
	long := strings.Repeat("x", 600)
	cases := []struct {
		name       string
		status     int
		reply      string
		retryAfter string
		text       string
		wait       string
	}{
		{
			name:   "message, type, and code",
			status: http.StatusBadRequest,
			reply:  `{"error":{"code":400,"message":"model 'x' not found","type":"invalid_request_error"}}`,
			text:   "400 Bad Request: model 'x' not found (invalid_request_error, 400)",
			wait:   "0s",
		},
		{
			name:   "message only",
			status: http.StatusInternalServerError,
			reply:  `{"error":{"message":"boom"}}`,
			text:   "500 Internal Server Error: boom",
			wait:   "0s",
		},
		{
			name:   "no message",
			status: http.StatusBadGateway,
			reply:  "upstream down\n",
			text:   "502 Bad Gateway: upstream down",
			wait:   "0s",
		},
		{
			name:   "no message, long body",
			status: http.StatusBadGateway,
			reply:  long,
			text:   "502 Bad Gateway: " + long[:512] + "...",
			wait:   "0s",
		},
		{
			name:       "empty body, Retry-After in seconds",
			status:     http.StatusServiceUnavailable,
			retryAfter: "7",
			text:       "503 Service Unavailable",
			wait:       "7s",
		},
		{
			name:       "unreadable Retry-After",
			status:     http.StatusTooManyRequests,
			retryAfter: "soon",
			text:       "429 Too Many Requests",
			wait:       "0s",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newClientWith(t, "http://h/v1", &http.Client{Transport: &script{steps: []step{
				{status: tc.status, body: tc.reply, retryAfter: tc.retryAfter},
			}}}, ai.Options{})
			resp, err := c.Generate(t.Context(), hello())
			apiErr, ok := errors.AsType[*ai.APIError](err)
			if !ok {
				t.Fatalf("Generate = %v, %v; want an *APIError", resp, err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("APIError.Status = %d, want %d", apiErr.Status, tc.status)
			}
			if string(apiErr.Body) != tc.reply {
				t.Errorf("APIError.Body = %q, want %q", apiErr.Body, tc.reply)
			}
			if got := apiErr.Error(); got != tc.text {
				t.Errorf("APIError.Error() = %q, want %q", got, tc.text)
			}
			if got := apiErr.RetryAfter.String(); got != tc.wait {
				t.Errorf("APIError.RetryAfter = %s, want %s", got, tc.wait)
			}
			if !strings.HasPrefix(err.Error(), "ai: generate: ") {
				t.Errorf("Generate error = %q, want it prefixed ai: generate: ", err)
			}
		})
	}
}

// roundTripper is an http.RoundTripper made of a function.
type roundTripper func(*http.Request) (*http.Response, error)

func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGenerateWrapsTransportError(t *testing.T) {
	errDown := errors.New("network down")
	c := newClientWith(t, "http://h/v1", &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errDown
	})}, ai.Options{})
	_, err := c.Generate(t.Context(), hello())
	if !errors.Is(err, errDown) {
		t.Errorf("Generate error = %v, want it to wrap %v", err, errDown)
	}
	if _, ok := errors.AsType[*ai.APIError](err); ok {
		t.Errorf("Generate error = %v, an *APIError; want a transport error", err)
	}
}

func TestGenerateWrapsCancelledContext(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, okReply)
	c := newClient(t, srv.URL, ai.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := c.Generate(ctx, hello())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Generate error = %v, want it to wrap context.Canceled", err)
	}
}

// TestGenerateRefusesInvalidRequest covers when Generate refuses a request, whether Validate
// refuses it or the format can't encode it: before the token source is called or anything is
// sent. Which requests Validate refuses is protocol's test, and which the format refuses its.
func TestGenerateRefusesInvalidRequest(t *testing.T) {
	noModel := hello()
	noModel.Model = ""
	clash := hello()
	clash.Native = []byte(`{"model":"other"}`)
	modeNone := hello()
	modeNone.Tools = []protocol.FunctionTool{{Name: "get_time"}}
	modeNone.ToolChoice = protocol.AllowedToolChoice{
		Tools: []protocol.FunctionToolChoice{{Name: "get_time"}},
		Mode:  protocol.ToolChoiceNone,
	}
	cases := []struct {
		name string
		req  *protocol.Request
		want string
	}{
		{"nil", nil, "no request"},
		{"no model", noModel, "no model"},
		{"native clash, refused at encode", clash, "native member model clashes"},
		{"allowed_tools of mode none, refused at encode", modeNone, "allowed_tools of mode none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, okReply)
			called := false
			c := newClient(t, srv.URL, ai.Options{
				Token: func(context.Context) (string, error) { called = true; return "", nil },
			})
			resp, err := c.Generate(t.Context(), tc.req)
			if err == nil {
				t.Fatalf("Generate = %v, nil; want an error containing %q", resp, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Generate error = %q, want it to contain %q", err, tc.want)
			}
			if called {
				t.Error("the token source was called")
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}
