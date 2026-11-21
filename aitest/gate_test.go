package aitest_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

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

func generate(ctx context.Context, c *ai.Client, errs chan<- error) {
	_, err := c.Generate(ctx, userText("m", "hi"))
	errs <- err
}

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
	if n := len(e.Requests()); n != 4 {
		t.Errorf("with 4 parked, endpoint got %d requests, want 4", n)
	}
	if n := g.Parked(); n != 4 {
		t.Errorf("Parked = %d, want 4", n)
	}
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

func TestGateTestEndsWhileParked(t *testing.T) {
	waited := make(chan error, 1)
	errs := make(chan error, 1)
	t.Run("parked", func(t *testing.T) {
		var g aitest.Gate
		_, c := gatedChat(t, &g, waited)
		go generate(context.Background(), c, errs)
		g.Await(t, 1)
	})
	if err := receive(t, waited, "Wait after the test ended"); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait = %v, want context.Canceled", err)
	}
	_ = receive(t, errs, "Generate after the test ended")
}

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
