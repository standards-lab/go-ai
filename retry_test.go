package ai_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/protocol"
)

// step is the scripted transport's answer to one attempt: err, or a reply of status with body
// and, when set, a Retry-After header.
type step struct {
	status     int
	body       string
	retryAfter string
	err        error
}

// sent is one request the scripted transport received: its body, its Authorization header, and
// when it arrived, on the bubble's clock.
type sent struct {
	body string
	auth string
	at   time.Time
}

// script is an in-memory transport that answers the nth request with steps[n], repeating the
// last step once they run out, and records each request.
type script struct {
	steps []step
	sent  []sent
}

func (s *script) RoundTrip(r *http.Request) (*http.Response, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	s.sent = append(s.sent, sent{body: string(b), auth: r.Header.Get("Authorization"), at: time.Now()})
	st := s.steps[min(len(s.sent), len(s.steps))-1]
	if st.err != nil {
		return nil, st.err
	}
	h := http.Header{}
	if st.retryAfter != "" {
		h.Set("Retry-After", st.retryAfter)
	}
	return &http.Response{StatusCode: st.status, Header: h, Body: io.NopCloser(strings.NewReader(st.body)), Request: r}, nil
}

// retrying returns a Client of policy over a script of steps, its token source numbering each
// call, and its jitter source jitter, or the Client's own when nil.
func retrying(t *testing.T, policy ai.RetryPolicy, jitter func(time.Duration) time.Duration, steps ...step) (*ai.Client, *script) {
	t.Helper()
	s := &script{steps: steps}
	var tokens int
	c := newClientWith(t, "http://router/v1", &http.Client{Transport: s}, ai.Options{
		Retry: policy,
		Token: func(context.Context) (string, error) {
			tokens++
			return fmt.Sprintf("t%d", tokens), nil
		},
	})
	if jitter != nil {
		ai.SetJitter(c, jitter)
	}
	return c, s
}

// whole is a jitter source that waits the backoff's whole cap.
func whole(d time.Duration) time.Duration { return d }

// policy is a retry policy of 3 attempts, base 1s and most 10s.
var policy = ai.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 10 * time.Second}

// operation is one of the retried operations, run on c, reporting its error.
type operation struct {
	name string
	// ok is the 2xx reply that ends it.
	ok  string
	run func(ctx context.Context, c *ai.Client) error
}

var operations = []operation{
	{"Generate", okReply, func(ctx context.Context, c *ai.Client) error {
		_, err := c.Generate(ctx, hello())
		return err
	}},
	{"GenerateStream", eventStream(streamed...), func(ctx context.Context, c *ai.Client) error {
		var errs []error
		for _, err := range c.GenerateStream(ctx, hello()) {
			if err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}},
}

func TestNewRefusesRetryPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy ai.RetryPolicy
		want   string
	}{
		{"negative attempts", ai.RetryPolicy{MaxAttempts: -1}, "negative attempt count"},
		{"negative base delay", ai.RetryPolicy{MaxAttempts: 2, BaseDelay: -1, MaxDelay: time.Second}, "negative delay"},
		{"negative most delay", ai.RetryPolicy{MaxAttempts: 2, MaxDelay: -1}, "negative delay"},
		{"base above most", ai.RetryPolicy{MaxAttempts: 2, BaseDelay: 2 * time.Second, MaxDelay: time.Second}, "base delay is above"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := ai.New("http://h/v1", http.DefaultClient, completions.New(), ai.Options{Retry: tc.policy})
			if err == nil {
				t.Fatalf("New = %v, nil; want an error containing %q", c, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "ai: ") {
				t.Errorf("New error = %q, want it to start ai: and contain %q", err, tc.want)
			}
		})
	}
	for _, p := range []ai.RetryPolicy{
		{},
		{MaxAttempts: 1},
		{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Second},
		{MaxAttempts: 3, MaxDelay: time.Minute},
	} {
		if _, err := ai.New("http://h/v1", http.DefaultClient, completions.New(), ai.Options{Retry: p}); err != nil {
			t.Errorf("New of policy %+v: %v", p, err)
		}
	}
}

// TestNewCopiesRetry wants the policy New was given, not a later change to the caller's value:
// Options is passed by value, so the copy is the language's, and this pins that no pointer
// crept in.
func TestNewCopiesRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		opts := ai.Options{Retry: ai.RetryPolicy{MaxAttempts: 2}}
		s := &script{steps: []step{{status: http.StatusServiceUnavailable}}}
		c := newClientWith(t, "http://router/v1", &http.Client{Transport: s}, opts)
		opts.Retry.MaxAttempts = 5
		if _, err := c.Generate(t.Context(), hello()); err == nil {
			t.Fatal("Generate succeeded, want the 503's *APIError")
		}
		if len(s.sent) != 2 {
			t.Errorf("%d requests sent, want 2", len(s.sent))
		}
	})
}

// TestRetryStatuses has each operation answered first by a status the policy retries, and then
// by its 2xx reply, and wants it to succeed on the second attempt, which called the token source
// again and sent the same body.
func TestRetryStatuses(t *testing.T) {
	statuses := []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout}
	for _, op := range operations {
		for _, status := range statuses {
			t.Run(fmt.Sprintf("%s/%d", op.name, status), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, s := retrying(t, policy, nil, step{status: status}, step{status: http.StatusOK, body: op.ok})
					if err := op.run(t.Context(), c); err != nil {
						t.Fatalf("%s: %v", op.name, err)
					}
					if len(s.sent) != 2 {
						t.Fatalf("%d requests sent, want 2", len(s.sent))
					}
					if s.sent[0].body != s.sent[1].body || s.sent[0].body == "" {
						t.Errorf("bodies = %q and %q, want the same body twice", s.sent[0].body, s.sent[1].body)
					}
					if s.sent[0].auth != "Bearer t1" || s.sent[1].auth != "Bearer t2" {
						t.Errorf("Authorization = %q, %q; want a token per attempt, Bearer t1 and Bearer t2", s.sent[0].auth, s.sent[1].auth)
					}
				})
			})
		}
	}
}

// TestRetryNotRetried wants a status the policy doesn't name returned at once, as the *APIError
// of the one request sent.
func TestRetryNotRetried(t *testing.T) {
	for _, op := range operations {
		for _, status := range []int{http.StatusInternalServerError, http.StatusBadRequest, http.StatusNotFound, http.StatusNotImplemented} {
			t.Run(fmt.Sprintf("%s/%d", op.name, status), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, s := retrying(t, policy, nil, step{status: status}, step{status: http.StatusOK, body: op.ok})
					err := op.run(t.Context(), c)
					if apiErr, ok := errors.AsType[*ai.APIError](err); !ok || apiErr.Status != status {
						t.Errorf("%s error = %v, want the *APIError of %d", op.name, err, status)
					}
					if len(s.sent) != 1 {
						t.Errorf("%d requests sent, want 1", len(s.sent))
					}
				})
			})
		}
	}
}

// TestRetryUnsetRetriesNothing wants a Client of the zero policy, or of one attempt, to send one
// request for a 503.
func TestRetryUnsetRetriesNothing(t *testing.T) {
	for _, p := range []ai.RetryPolicy{{}, {MaxAttempts: 1, BaseDelay: time.Second, MaxDelay: time.Second}} {
		t.Run(fmt.Sprintf("%d attempts", p.MaxAttempts), func(t *testing.T) {
			s := &script{steps: []step{{status: http.StatusServiceUnavailable}, {status: http.StatusOK, body: okReply}}}
			c := newClientWith(t, "http://router/v1", &http.Client{Transport: s}, ai.Options{Retry: p})
			_, err := c.Generate(t.Context(), hello())
			if apiErr, ok := errors.AsType[*ai.APIError](err); !ok || apiErr.Status != http.StatusServiceUnavailable {
				t.Errorf("Generate error = %v, want the 503's *APIError", err)
			}
			if len(s.sent) != 1 {
				t.Errorf("%d requests sent, want 1", len(s.sent))
			}
		})
	}
}

// TestRetryStartedStream has a stream break mid-body under a policy, and wants its one error
// pair after the events before it, from one request: a started stream is never sent again.
func TestRetryStartedStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, s := retrying(t, policy, nil, step{status: http.StatusOK, body: eventStream(streamed[:3]...)},
			step{status: http.StatusOK, body: eventStream(streamed...)})
		events, resp, errs := collect(t, c.GenerateStream(t.Context(), hello()))
		if len(errs) != 1 || !errors.Is(errs[0], io.ErrUnexpectedEOF) {
			t.Fatalf("GenerateStream errors = %v, want one io.ErrUnexpectedEOF", errs)
		}
		if len(events) == 0 || resp.Status != protocol.StatusInProgress {
			t.Errorf("%d events folding to status %q, want the events before the break, in progress", len(events), resp.Status)
		}
		if len(s.sent) != 1 {
			t.Errorf("%d requests sent, want 1", len(s.sent))
		}
	})
}

// TestRetryConnectionFailure has the first attempt fail in the transport, and wants a connection
// failure before any reply retried, and any other failure returned at once.
func TestRetryConnectionFailure(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	reset := &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}
	cases := []struct {
		name  string
		err   error
		retry bool
	}{
		{"dial refused", refused, true},
		{"connection reset", reset, true},
		{"closed before the reply", fmt.Errorf("transport: %w", io.EOF), true},
		{"closed mid-header", io.ErrUnexpectedEOF, true},
		{"temporary DNS failure", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "router", IsTemporary: true}}, true},
		{"unknown host", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Name: "router", IsNotFound: true}}, false},
		{"another failure", errors.New("certificate refused"), false},
		// net/http's error for a server closing an idle connection is unexported; its text is
		// all a client could match.
		{"idle connection closed", errors.New("http: server closed idle connection"), false},
		{"context error from the transport", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, s := retrying(t, policy, nil, step{err: tc.err}, step{status: http.StatusOK, body: okReply})
				_, err := c.Generate(t.Context(), hello())
				if tc.retry {
					if err != nil {
						t.Errorf("Generate: %v", err)
					}
					if len(s.sent) != 2 {
						t.Errorf("%d requests sent, want 2", len(s.sent))
					}
					return
				}
				if !errors.Is(err, tc.err) {
					t.Errorf("Generate error = %v, want it to wrap %v", err, tc.err)
				}
				if len(s.sent) != 1 {
					t.Errorf("%d requests sent, want 1", len(s.sent))
				}
			})
		})
	}
}

// TestRetryHeaderTimeout has a real server hold its reply past the transport's
// ResponseHeaderTimeout, and wants the timeout returned from one request: the endpoint may
// still be running it.
func TestRetryHeaderTimeout(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// The server notices the client gone only once the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(srv.Close)
	hc := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 20 * time.Millisecond}}
	c := newClientWith(t, srv.URL, hc, ai.Options{Retry: ai.RetryPolicy{MaxAttempts: 3}})
	_, err := c.Generate(t.Context(), hello())
	if err == nil || !strings.Contains(err.Error(), "timeout awaiting response headers") {
		t.Errorf("Generate error = %v, want the header timeout", err)
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("server received %d requests, want 1", n)
	}
}

// TestRetryTokenSourceError has the token source fail under a policy, wrapping what the
// policy would retry from the transport, and wants the error returned at once: the source
// called once, one error, wrapping the source's, and no request sent. Its failure is the
// source's own, before anything is sent, which a later attempt doesn't mend.
func TestRetryTokenSourceError(t *testing.T) {
	causes := []struct {
		name string
		err  error
	}{
		{"network operation", fmt.Errorf("fetch token: %w", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED})},
		{"connection closed", io.ErrUnexpectedEOF},
	}
	for _, op := range operations {
		for _, cause := range causes {
			t.Run(op.name+"/"+cause.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					s := &script{steps: []step{{status: http.StatusOK, body: op.ok}}}
					var calls int
					c := newClientWith(t, "http://router/v1", &http.Client{Transport: s}, ai.Options{
						Retry: policy,
						Token: func(context.Context) (string, error) {
							calls++
							return "", cause.err
						},
					})
					var errs []error
					if op.name == "GenerateStream" {
						_, _, errs = collect(t, c.GenerateStream(t.Context(), hello()))
					} else if err := op.run(t.Context(), c); err != nil {
						errs = []error{err}
					}
					if len(errs) != 1 || !errors.Is(errs[0], cause.err) || !strings.Contains(errs[0].Error(), "token: ") {
						t.Errorf("%s errors = %v, want one token error wrapping %v", op.name, errs, cause.err)
					}
					if calls != 1 {
						t.Errorf("token source called %d times, want 1", calls)
					}
					if len(s.sent) != 0 {
						t.Errorf("%d requests sent, want 0", len(s.sent))
					}
				})
			})
		}
	}
}

// TestRetryConnectionClosed has a real server close the first connection before replying, and
// wants the request sent again and answered.
func TestRetryConnectionClosed(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			_ = conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, okReply)
	}))
	t.Cleanup(srv.Close)
	c := newClient(t, srv.URL, ai.Options{Retry: ai.RetryPolicy{MaxAttempts: 2}})
	if _, err := c.Generate(t.Context(), hello()); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("server received %d requests, want 2", n)
	}
}

// TestRetryAfter wants a Retry-After, in seconds or as an HTTP date, to replace the backoff, and
// one that is negative or unreadable ignored, leaving the backoff, here its whole cap of 1s.
func TestRetryAfter(t *testing.T) {
	cases := []struct {
		name string
		// retryAfter is the header, its date's offset from now formatted in.
		retryAfter func(now time.Time) string
		wait       time.Duration
	}{
		{"seconds", func(time.Time) string { return "3" }, 3 * time.Second},
		{"zero seconds", func(time.Time) string { return "0" }, 0},
		{"HTTP date", func(now time.Time) string { return now.Add(5 * time.Second).UTC().Format(http.TimeFormat) }, 5 * time.Second},
		{"HTTP date passed", func(now time.Time) string { return now.Add(-time.Minute).UTC().Format(http.TimeFormat) }, 0},
		{"negative", func(time.Time) string { return "-3" }, time.Second},
		{"unreadable", func(time.Time) string { return "soon" }, time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, s := retrying(t, policy, whole,
					step{status: http.StatusTooManyRequests, retryAfter: tc.retryAfter(time.Now())},
					step{status: http.StatusOK, body: okReply})
				if _, err := c.Generate(t.Context(), hello()); err != nil {
					t.Fatalf("Generate: %v", err)
				}
				if len(s.sent) != 2 {
					t.Fatalf("%d requests sent, want 2", len(s.sent))
				}
				if got := s.sent[1].at.Sub(s.sent[0].at); got != tc.wait {
					t.Errorf("waited %v, want %v", got, tc.wait)
				}
			})
		})
	}
}

// TestRetryReturnsAtOnce wants the last error returned without a wait when the wait is longer
// than the policy's most delay, or would end past the context's deadline, its RetryAfter the
// reply's wait for a caller that redelivers.
func TestRetryReturnsAtOnce(t *testing.T) {
	cases := []struct {
		name       string
		retryAfter string
		timeout    time.Duration
		want       time.Duration
	}{
		{"Retry-After over the most delay", "11", 0, 11 * time.Second},
		{"Retry-After too long to hold", "99999999999999999999", 0, time.Duration(math.MaxInt64)},
		{"Retry-After past the deadline", "5", 4 * time.Second, 5 * time.Second},
		{"backoff past the deadline", "", 500 * time.Millisecond, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				if tc.timeout > 0 {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, tc.timeout)
					defer cancel()
				}
				c, s := retrying(t, policy, whole,
					step{status: http.StatusServiceUnavailable, body: "busy", retryAfter: tc.retryAfter},
					step{status: http.StatusOK, body: okReply})
				start := time.Now()
				_, err := c.Generate(ctx, hello())
				apiErr, ok := errors.AsType[*ai.APIError](err)
				if !ok || apiErr.Status != http.StatusServiceUnavailable || string(apiErr.Body) != "busy" {
					t.Fatalf("Generate error = %v, want the 503's *APIError", err)
				}
				if apiErr.RetryAfter != tc.want {
					t.Errorf("APIError.RetryAfter = %v, want %v", apiErr.RetryAfter, tc.want)
				}
				if waited := time.Since(start); waited != 0 {
					t.Errorf("waited %v, want none", waited)
				}
				if len(s.sent) != 1 {
					t.Errorf("%d requests sent, want 1", len(s.sent))
				}
			})
		})
	}
}

// TestRetryAfterExhausted wants the last attempt's Retry-After on the error returned once the
// attempts run out, HTTP date included, measured from the reply's arrival.
func TestRetryAfterExhausted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		date := time.Now().Add(2*time.Second + 8*time.Second).UTC().Format(http.TimeFormat)
		c, s := retrying(t, ai.RetryPolicy{MaxAttempts: 2, MaxDelay: 10 * time.Second}, whole,
			step{status: http.StatusTooManyRequests, retryAfter: "2"},
			step{status: http.StatusTooManyRequests, retryAfter: date})
		_, err := c.Generate(t.Context(), hello())
		apiErr, ok := errors.AsType[*ai.APIError](err)
		if !ok || apiErr.Status != http.StatusTooManyRequests {
			t.Fatalf("Generate error = %v, want the 429's *APIError", err)
		}
		if apiErr.RetryAfter != 8*time.Second {
			t.Errorf("APIError.RetryAfter = %v, want 8s, the date's wait from the second reply", apiErr.RetryAfter)
		}
		if len(s.sent) != 2 {
			t.Errorf("%d requests sent, want 2", len(s.sent))
		}
	})
}

// TestRetryCancelWhileWaiting cancels the context during the wait, and wants the context's
// error, not the attempt's.
func TestRetryCancelWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		c, s := retrying(t, policy, nil, step{status: http.StatusServiceUnavailable, retryAfter: "10"},
			step{status: http.StatusOK, body: okReply})
		go func() {
			time.Sleep(time.Second)
			cancel()
		}()
		start := time.Now()
		_, err := c.Generate(ctx, hello())
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Generate error = %v, want context.Canceled", err)
		}
		if _, ok := errors.AsType[*ai.APIError](err); ok {
			t.Errorf("Generate error = %v, an *APIError; want the context's", err)
		}
		if waited := time.Since(start); waited != time.Second {
			t.Errorf("waited %v, want 1s, until the cancel", waited)
		}
		if len(s.sent) != 1 {
			t.Errorf("%d requests sent, want 1", len(s.sent))
		}
	})
}

// TestRetryExhausted has every attempt answered with a retried status, and wants MaxAttempts
// requests and the last attempt's *APIError.
func TestRetryExhausted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c, s := retrying(t, policy, nil,
			step{status: http.StatusServiceUnavailable, body: "attempt 1"},
			step{status: http.StatusTooManyRequests, body: "attempt 2"},
			step{status: http.StatusGatewayTimeout, body: "attempt 3"},
			step{status: http.StatusOK, body: okReply})
		_, err := c.Generate(t.Context(), hello())
		apiErr, ok := errors.AsType[*ai.APIError](err)
		if !ok || apiErr.Status != http.StatusGatewayTimeout || string(apiErr.Body) != "attempt 3" {
			t.Errorf("Generate error = %v, want the third attempt's *APIError", err)
		}
		if len(s.sent) != 3 {
			t.Errorf("%d requests sent, want 3", len(s.sent))
		}
	})
}

// TestRetryBackoff has every attempt answered 503, with a jitter source that records the cap it
// is given and waits half of it, and wants the caps to double from the base delay to the most
// delay, and each wait the jitter's.
func TestRetryBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var caps []time.Duration
		half := func(d time.Duration) time.Duration {
			caps = append(caps, d)
			return d / 2
		}
		p := ai.RetryPolicy{MaxAttempts: 6, BaseDelay: time.Second, MaxDelay: 5 * time.Second}
		c, s := retrying(t, p, half, step{status: http.StatusServiceUnavailable})
		if _, err := c.Generate(t.Context(), hello()); err == nil {
			t.Fatal("Generate succeeded, want the 503's *APIError")
		}
		want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
		if !slices.Equal(caps, want) {
			t.Errorf("backoff caps = %v, want %v", caps, want)
		}
		if len(s.sent) != len(want)+1 {
			t.Fatalf("%d requests sent, want %d", len(s.sent), len(want)+1)
		}
		for i, d := range want {
			if got := s.sent[i+1].at.Sub(s.sent[i].at); got != d/2 {
				t.Errorf("wait %d = %v, want %v", i+1, got, d/2)
			}
		}
	})
}

// TestFullJitter wants the Client's own jitter to wait from 0 up to the backoff's cap, each
// wait in that range taken: a policy whose cap is 4ns retries a 503 a thousand times, and every
// wait from 0 to 4ns is seen. A cap of the longest duration waits too, rather than overflow.
func TestFullJitter(t *testing.T) {
	// wait returns how long a Client of the policy, with its own jitter, waited between a 503
	// and the attempt after it.
	wait := func(t *testing.T, p ai.RetryPolicy) time.Duration {
		t.Helper()
		var d time.Duration
		synctest.Test(t, func(t *testing.T) {
			c, s := retrying(t, p, nil, step{status: http.StatusServiceUnavailable}, step{status: http.StatusOK, body: okReply})
			if _, err := c.Generate(t.Context(), hello()); err != nil {
				t.Fatalf("Generate: %v", err)
			}
			if len(s.sent) != 2 {
				t.Fatalf("%d requests sent, want 2", len(s.sent))
			}
			d = s.sent[1].at.Sub(s.sent[0].at)
		})
		return d
	}
	const d = 4 * time.Nanosecond
	seen := map[time.Duration]bool{}
	for range 1000 {
		w := wait(t, ai.RetryPolicy{MaxAttempts: 2, BaseDelay: d, MaxDelay: d})
		if w < 0 || w > d {
			t.Fatalf("waited %v, want 0 to %v", w, d)
		}
		seen[w] = true
	}
	if len(seen) != 5 {
		t.Errorf("%d distinct waits in 1000, want all 5 from 0 to %v", len(seen), d)
	}
	longest := time.Duration(math.MaxInt64)
	if w := wait(t, ai.RetryPolicy{MaxAttempts: 2, BaseDelay: longest, MaxDelay: longest}); w < 0 {
		t.Errorf("waited %v with a cap of the longest duration, want at least 0", w)
	}
}
