package aitest

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

const awaitBound = 10 * time.Second

// Gate parks requests inside route functions until the test lets them go, so a test can hold
// requests in flight and look at what the caller does meanwhile. A route function calls
// [Gate.Wait] with its context; the test calls [Gate.Await] to block until the requests it
// expects are parked, then [Gate.Release] or [Gate.Open] to let them go. A streamed
// [ChatReply] or [RawReply] whose Hold names a Gate parks on it the same way, mid-stream, so a
// held stream counts as a parked request.
//
// The zero Gate is ready to use and parks every request; a Gate must not be copied after first
// use. Its methods are safe for concurrent use. A parked request also goes when its context ends,
// which an [Endpoint] does when the client cancels or disconnects and when the test ends, so a
// test that ends with requests still parked doesn't hang.
type Gate struct {
	mu      sync.Mutex
	waiters []chan struct{}
	permits int
	open    bool
	parked  chan struct{}
}

// Wait parks the calling request until the test lets it go, by Release or Open, and returns nil;
// or until ctx ends, and returns ctx's error. A route function calls it with its context. A
// request whose ctx has already ended returns ctx's error at once and takes no release; one a
// release let go as its ctx ended returns nil, since it was let go.
func (g *Gate) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	if g.open {
		g.mu.Unlock()
		return nil
	}
	if g.permits > 0 {
		g.permits--
		g.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	g.waiters = append(g.waiters, ch)
	if g.parked != nil {
		close(g.parked)
		g.parked = nil
	}
	g.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, w := range g.waiters {
		if w == ch {
			g.waiters = slices.Delete(g.waiters, i, i+1)
			return ctx.Err()
		}
	}
	return nil
}

// Await blocks the test until at least n requests are parked in Wait at once. A test that sees
// fewer than n parked within ten seconds fails, naming how many were, rather than hang. It calls
// t.Fatalf, so it runs on the test's goroutine.
func (g *Gate) Await(t testing.TB, n int) {
	t.Helper()
	timer := time.NewTimer(awaitBound)
	defer timer.Stop()
	for {
		g.mu.Lock()
		got := len(g.waiters)
		if g.parked == nil {
			g.parked = make(chan struct{})
		}
		parked := g.parked
		g.mu.Unlock()
		if got >= n {
			return
		}
		select {
		case <-parked:
		case <-timer.C:
			t.Fatalf("aitest: Gate.Await: %d requests parked after %v, want %d", got, awaitBound, n)
		}
	}
}

// Parked returns how many requests are parked in Wait now.
func (g *Gate) Parked() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.waiters)
}

// Release lets n requests go: the parked ones first, longest parked first, and for any of n left
// over, as many later requests, which pass Wait at once. It panics if n is negative.
func (g *Gate) Release(n int) {
	if n < 0 {
		panic("aitest: Gate.Release of a negative count")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	k := min(n, len(g.waiters))
	for _, ch := range g.waiters[:k] {
		close(ch)
	}
	g.waiters = g.waiters[k:]
	g.permits += n - k
}

// Open lets every parked request go, and every later request pass Wait at once.
func (g *Gate) Open() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.open = true
	for _, ch := range g.waiters {
		close(ch)
	}
	g.waiters = nil
}
