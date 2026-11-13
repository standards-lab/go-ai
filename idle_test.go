package ai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// idleLimit is the StreamIdleTimeout the idle tests' clients carry.
const idleLimit = 5 * time.Second

// part is one piece of a paced body: text, sent once after waits on the bubble's clock.
type part struct {
	after time.Duration
	text  string
}

// paced is a reply body that sends its parts, each after its wait, and then ends, or, when
// hang is set, waits for ctx, its request's, and then fails as the transport does once a
// request's context is done. A wait also ends when ctx does. It records whether it was closed.
type paced struct {
	parts  []part
	hang   bool
	ctx    context.Context
	closed atomic.Bool
}

func (b *paced) Read(p []byte) (int, error) {
	if len(b.parts) == 0 {
		if b.hang {
			<-b.ctx.Done()
			return 0, errRequestCanceled
		}
		return 0, io.EOF
	}
	next := &b.parts[0]
	if next.after > 0 {
		select {
		case <-time.After(next.after):
		case <-b.ctx.Done():
			return 0, errRequestCanceled
		}
		next.after = 0
	}
	n := copy(p, next.text)
	if next.text = next.text[n:]; next.text == "" {
		b.parts = b.parts[1:]
	}
	return n, nil
}

func (b *paced) Close() error {
	b.closed.Store(true)
	return nil
}

// pacedClient returns a Client whose transport answers with a paced body of parts, and the
// body it last answered with.
func pacedClient(t *testing.T, opts ai.Options, hang bool, parts ...part) (*ai.Client, func() *paced) {
	t.Helper()
	var b *paced
	c := newClientWith(t, "http://router/v1", &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		b = &paced{parts: append([]part(nil), parts...), hang: hang, ctx: r.Context()}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: b, Request: r}, nil
	})}, opts)
	return c, func() *paced { return b }
}

// TestStreamIdleTimeout has the stream stall, before its first byte and after its first events,
// and wants it ended once a read has waited the bound: one error, matching io.ErrUnexpectedEOF
// and os.ErrDeadlineExceeded and not the caller's context, after the events before it, which
// fold to the partial Response, and the body closed.
func TestStreamIdleTimeout(t *testing.T) {
	cases := []struct {
		name  string
		parts []part
		// stalled is when the stall began; want the partial Response, nil for no events.
		stalled time.Duration
		want    *protocol.Response
	}{
		{"before the first byte", nil, 0, nil},
		{"after the first events", []part{{after: 3 * time.Second, text: eventStream(streamed[:3]...)}}, 3 * time.Second, partial("Paris")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, body := pacedClient(t, ai.Options{StreamIdleTimeout: idleLimit}, true, tc.parts...)
				ctx := t.Context()
				start := time.Now()
				events, resp, errs := collect(t, c.GenerateStream(ctx, hello()))
				if waited := time.Since(start); waited != tc.stalled+idleLimit {
					t.Errorf("ended after %v, want %v", waited, tc.stalled+idleLimit)
				}
				if len(errs) != 1 {
					t.Fatalf("GenerateStream errors = %v, want one", errs)
				}
				err := errs[0]
				if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Errorf("error = %v, want it to match io.ErrUnexpectedEOF and os.ErrDeadlineExceeded", err)
				}
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
					t.Errorf("error = %v, ctx %v; want neither the caller's context", err, ctx.Err())
				}
				if !strings.Contains(err.Error(), "stream idle timeout (5s)") {
					t.Errorf("error = %q, want it to name the bound", err)
				}
				if tc.want == nil {
					if len(events) != 0 {
						t.Errorf("%d events, want none", len(events))
					}
				} else if !reflect.DeepEqual(resp, tc.want) {
					t.Errorf("Response = %#v\nwant       %#v", resp, tc.want)
				}
				if !body().closed.Load() {
					t.Error("the body is open after the sequence ended")
				}
			})
		})
	}
}

// TestStreamIdleTimeoutResetByBytes has every gap in the stream shorter than the bound, though
// together far longer, the gaps filled by llama.cpp's ping comments, and a caller that takes
// longer than the bound over each event, and wants the stream whole: any bytes reset the bound,
// and the clock runs only while a read waits.
func TestStreamIdleTimeoutResetByBytes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gap := idleLimit - time.Second
		parts := []part{{text: eventStream(streamed[:2]...)}}
		for range 5 {
			parts = append(parts, part{after: gap, text: ": ping\n\n"})
		}
		for _, chunk := range streamed[2:] {
			parts = append(parts, part{after: gap, text: eventStream(chunk)})
		}
		c, _ := pacedClient(t, ai.Options{StreamIdleTimeout: idleLimit}, false, parts...)
		var acc protocol.Accumulator
		for e, err := range c.GenerateStream(t.Context(), hello()) {
			if err != nil {
				t.Fatalf("GenerateStream error: %v", err)
			}
			if err := acc.Add(e); err != nil {
				t.Errorf("Accumulator.Add: %v", err)
			}
			time.Sleep(2 * idleLimit)
		}
		if got := acc.Response(); !reflect.DeepEqual(got, completed) {
			t.Errorf("Response = %#v\nwant       %#v", got, completed)
		}
	})
}

// TestStreamIdleTimeoutZero wants a Client of no bound to wait out a stall for the context:
// the stream ends at the context's deadline, with its error.
func TestStreamIdleTimeoutZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()
		c, _ := pacedClient(t, ai.Options{}, true, part{text: eventStream(streamed[:3]...)})
		start := time.Now()
		_, _, errs := collect(t, c.GenerateStream(ctx, hello()))
		if len(errs) != 1 || !errors.Is(errs[0], context.DeadlineExceeded) {
			t.Fatalf("GenerateStream errors = %v, want one context.DeadlineExceeded", errs)
		}
		if waited := time.Since(start); waited != time.Hour {
			t.Errorf("ended after %v, want the context's hour", waited)
		}
	})
}
