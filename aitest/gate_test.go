package aitest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

// gatedChat serves a chat route that parks each request on g, then answers ok, or, when the
// request's context ends first, an error reply. waited, when not nil, gets each Wait's result.
// It returns the endpoint and a Chat Completions client of it, which retries nothing.
func gatedChat(t *testing.T, g *aitest.Gate, waited chan<- error) (*aitest.Endpoint, *ai.Client) {
	t.Helper()
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(ctx context.Context, _ aitest.ChatRequest) aitest.Reply {
		err := g.Wait(ctx)
		if waited != nil {
			waited <- err
		}
		if err != nil {
			return aitest.ErrorReply{Message: err.Error()}
		}
		return aitest.ChatReply{Text: "ok"}
	}})
	return e, chatClient(t, e, ai.Options{})
}

// generate sends c one chat request with ctx, giving its error to errs.
func generate(ctx context.Context, c *ai.Client, errs chan<- error) {
	_, err := c.Generate(ctx, userText("m", "hi"))
	errs <- err
}

// receive returns the next value of ch, failing t if none comes within five seconds, which
// names what.
func receive(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: nothing within 5s", what)
		return nil
	}
}

// TestGateBoundsInFlight wants a caller that bounds its own concurrency at 4 to have exactly 4
// requests in flight of the 5 it sends, the fifth unsent, while the gate holds them; then, once
// released, all 5 to succeed.
func TestGateBoundsInFlight(t *testing.T) {
	var g aitest.Gate
	e, c := gatedChat(t, &g, nil)
	sem := make(chan struct{}, 4)
	errs := make(chan error, 5)
	for range 5 {
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			generate(t.Context(), c, errs)
		}()
	}
	g.Await(t, 4)
	// Every parked request was recorded before it parked, and the fifth can't be sent while the
	// four hold the semaphore, so the count is exact without waiting.
	if n := len(e.Requests()); n != 4 {
		t.Errorf("with 4 parked, endpoint got %d requests, want 4", n)
	}
	if n := g.Parked(); n != 4 {
		t.Errorf("Parked = %d, want 4", n)
	}
	// Four for the parked requests and one for the fifth, which passes at once when it comes.
	g.Release(5)
	for i := range 5 {
		if err := receive(t, errs, "Generate"); err != nil {
			t.Errorf("Generate %d: %v", i, err)
		}
	}
	if n := len(e.Requests()); n != 5 {
		t.Errorf("endpoint got %d requests, want 5", n)
	}
}

// TestGateWaitEndsWithClientContext wants a parked request whose client cancels to end at once:
// Wait returns context.Canceled, and the client sees context.Canceled.
func TestGateWaitEndsWithClientContext(t *testing.T) {
	var g aitest.Gate
	waited := make(chan error, 1)
	_, c := gatedChat(t, &g, waited)
	ctx, cancel := context.WithCancel(t.Context())
	errs := make(chan error, 1)
	go generate(ctx, c, errs)
	g.Await(t, 1)
	cancel()
	if err := receive(t, errs, "Generate after cancel"); !errors.Is(err, context.Canceled) {
		t.Errorf("Generate error = %v, want context.Canceled", err)
	}
	if err := receive(t, waited, "Wait after cancel"); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait = %v, want context.Canceled", err)
	}
	if n := g.Parked(); n != 0 {
		t.Errorf("Parked = %d after the request ended, want 0", n)
	}
}

// TestGateTestEndsWhileParked wants a test that ends with a request still parked, its client's
// context never ending, to end rather than hang: the endpoint's cleanup ends the request's
// context, and Wait returns context.Canceled.
func TestGateTestEndsWhileParked(t *testing.T) {
	waited := make(chan error, 1)
	errs := make(chan error, 1)
	t.Run("parked", func(t *testing.T) {
		var g aitest.Gate
		_, c := gatedChat(t, &g, waited)
		// The client's context is Background, not t's, so only the endpoint can end the request.
		go generate(context.Background(), c, errs)
		g.Await(t, 1)
	})
	if err := receive(t, waited, "Wait after the test ended"); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait = %v, want context.Canceled", err)
	}
	// The client ends too, with whatever the closing endpoint gave it.
	_ = receive(t, errs, "Generate after the test ended")
}

// TestGateReleaseAndOpen wants Release(n) to let exactly n requests go, a release with none
// parked to let the next request through at once, and Open to let the rest go and every later
// request through.
func TestGateReleaseAndOpen(t *testing.T) {
	var g aitest.Gate
	_, c := gatedChat(t, &g, nil)

	g.Release(1)
	if _, err := c.Generate(t.Context(), userText("m", "hi")); err != nil {
		t.Fatalf("Generate after Release(1) with none parked: %v", err)
	}

	errs := make(chan error, 5)
	for range 5 {
		go generate(t.Context(), c, errs)
	}
	g.Await(t, 5)
	g.Release(2)
	if n := g.Parked(); n != 3 {
		t.Errorf("Parked = %d after Release(2) of 5, want 3", n)
	}
	for i := range 2 {
		if err := receive(t, errs, "Generate after Release(2)"); err != nil {
			t.Errorf("released Generate %d: %v", i, err)
		}
	}
	if n := g.Parked(); n != 3 {
		t.Errorf("Parked = %d after two released requests ended, want 3", n)
	}

	g.Open()
	for i := range 3 {
		if err := receive(t, errs, "Generate after Open"); err != nil {
			t.Errorf("opened Generate %d: %v", i, err)
		}
	}
	if _, err := c.Generate(t.Context(), userText("m", "hi")); err != nil {
		t.Errorf("Generate after Open: %v", err)
	}
	if n := g.Parked(); n != 0 {
		t.Errorf("Parked = %d after Open, want 0", n)
	}
}
