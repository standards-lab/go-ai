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

type step struct {
	status     int
	body       string
	retryAfter string
	err        error
}

type sent struct {
	body string
	auth string
	at   time.Time
}

type script struct {
	steps []step
	sent  []sent
}

func (s *script) RoundTrip(r *http.Request) (*http.Response, error) {
	var b []byte
	if r.Body != nil {
		var err error
		if b, err = io.ReadAll(r.Body); err != nil {
			return nil, err
		}
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

func retrying(t *testing.T, policy ai.RetryPolicy, jitter func(time.Duration) time.Duration, steps ...step) (*ai.Client, *script) {
	t.Helper()
	return retryingIn(t, ai.TranscribeRoute, policy, jitter, steps...)
}

func retryingIn(t *testing.T, mode ai.TranscriptionMode, policy ai.RetryPolicy, jitter func(time.Duration) time.Duration,
	steps ...step) (*ai.Client, *script) {
	t.Helper()
	s := &script{steps: steps}
	var tokens int
	c := newClientWith(t, "http://router/v1", &http.Client{Transport: s}, ai.Options{
		TranscriptionMode: mode,
		Retry:             policy,
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

func whole(d time.Duration) time.Duration { return d }

var policy = ai.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 10 * time.Second}

type operation struct {
	name     string
	mode     ai.TranscriptionMode
	bodyless bool
	ok       string
	run      func(ctx context.Context, c *ai.Client) error
}

var operations = []operation{
	{name: "Generate", ok: okReply, run: func(ctx context.Context, c *ai.Client) error {
		_, err := c.Generate(ctx, hello())
		return err
	}},
	{name: "GenerateStream", ok: eventStream(streamed...), run: func(ctx context.Context, c *ai.Client) error {
		var errs []error
		for _, err := range c.GenerateStream(ctx, hello()) {
			if err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}},
	{name: "Embed", ok: embedReply, run: func(ctx context.Context, c *ai.Client) error {
		_, err := c.Embed(ctx, two())
		return err
	}},
	{name: "Models", bodyless: true, ok: modelsReply, run: func(ctx context.Context, c *ai.Client) error {
		_, err := c.Models(ctx)
		return err
	}},
	{name: "TranscribeRoute", ok: transcribeReply, run: func(ctx context.Context, c *ai.Client) error {
		_, err := c.Transcribe(ctx, &ai.TranscriptionRequest{Model: "m", Open: reopen(clip(64 << 10)), FileName: "clip.wav"})
		return err
	}},
	{name: "TranscribeChat", mode: ai.TranscribeChat, ok: audioReply, run: func(ctx context.Context, c *ai.Client) error {
		_, err := c.Transcribe(ctx, &ai.TranscriptionRequest{Model: "m", Open: reopen(clip(64 << 10)), FileName: "clip.wav"})
		return err
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
			c, err := ai.New("http://h/v1", http.DefaultClient, completions.New(completions.Options{}), ai.Options{Retry: tc.policy})
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
		if _, err := ai.New("http://h/v1", http.DefaultClient, completions.New(completions.Options{}), ai.Options{Retry: p}); err != nil {
			t.Errorf("New of policy %+v: %v", p, err)
		}
	}
}

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

func TestRetryStatuses(t *testing.T) {
	statuses := []int{http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout}
	for _, op := range operations {
		for _, status := range statuses {
			t.Run(fmt.Sprintf("%s/%d", op.name, status), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, s := retryingIn(t, op.mode, policy, nil, step{status: status}, step{status: http.StatusOK, body: op.ok})
					if err := op.run(t.Context(), c); err != nil {
						t.Fatalf("%s: %v", op.name, err)
					}
					if len(s.sent) != 2 {
						t.Fatalf("%d requests sent, want 2", len(s.sent))
					}
					if s.sent[0].body != s.sent[1].body || (s.sent[0].body == "") != op.bodyless {
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

func TestRetryNotRetried(t *testing.T) {
	for _, op := range operations {
		for _, status := range []int{http.StatusInternalServerError, http.StatusBadRequest, http.StatusNotFound, http.StatusNotImplemented} {
			t.Run(fmt.Sprintf("%s/%d", op.name, status), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c, s := retryingIn(t, op.mode, policy, nil, step{status: status}, step{status: http.StatusOK, body: op.ok})
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

func TestRetryHeaderTimeout(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
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
						TranscriptionMode: op.mode,
						Retry:             policy,
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

func TestRetryAfter(t *testing.T) {
	cases := []struct {
		name       string
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

func TestFullJitter(t *testing.T) {
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
