package aitest_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/protocol"
)

func textDeltas(ctx context.Context, c *ai.Client, errs chan<- error) <-chan string {
	deltas := make(chan string, 16)
	go func() {
		defer close(deltas)
		for ev, err := range c.GenerateStream(ctx, userText("m", "hi")) {
			if err != nil {
				errs <- err
				return
			}
			if d, ok := ev.(protocol.OutputTextDelta); ok {
				deltas <- d.Delta
			}
		}
		errs <- nil
	}()
	return deltas
}

func nextDelta(t *testing.T, deltas <-chan string) string {
	t.Helper()
	select {
	case d, ok := <-deltas:
		if !ok {
			t.Fatal("stream ended before the next text delta")
		}
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("no text delta within 5s")
		return ""
	}
}

func rest(t *testing.T, deltas <-chan string) string {
	t.Helper()
	var b strings.Builder
	timeout := time.After(5 * time.Second)
	for {
		select {
		case d, ok := <-deltas:
			if !ok {
				return b.String()
			}
			b.WriteString(d)
		case <-timeout:
			t.Fatal("stream didn't end within 5s")
		}
	}
}

func checkHeld(t *testing.T, deltas <-chan string) {
	t.Helper()
	select {
	case d := <-deltas:
		t.Errorf("text delta %q arrived while the stream was held", d)
	default:
	}
}

func awaitUnparked(t *testing.T, g *aitest.Gate) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for g.Parked() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("Gate still has %d parked after 5s, want 0", g.Parked())
		}
		time.Sleep(time.Millisecond)
	}
}

func TestChatReplyHold(t *testing.T) {
	var g aitest.Gate
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
		return aitest.ChatReply{Chunks: []string{"Hel", "lo", ", world"}, Hold: &g, HoldAfter: 1}
	}})
	errs := make(chan error, 1)
	deltas := textDeltas(t.Context(), chatClient(t, e, ai.Options{}), errs)
	if d := nextDelta(t, deltas); d != "Hel" {
		t.Fatalf("first text delta = %q, want Hel", d)
	}
	g.Await(t, 1)
	checkHeld(t, deltas)
	g.Release(1)
	if got := rest(t, deltas); got != "lo, world" {
		t.Errorf("text after the release = %q, want \"lo, world\"", got)
	}
	if err := receive(t, errs, "GenerateStream"); err != nil {
		t.Errorf("GenerateStream: %v", err)
	}
}

func TestChatReplyHoldIdle(t *testing.T) {
	var g aitest.Gate
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
		return aitest.ChatReply{Chunks: []string{"Hel", "lo"}, Hold: &g, HoldAfter: 1}
	}})
	c := chatClient(t, e, ai.Options{StreamIdleTimeout: 50 * time.Millisecond})
	var text strings.Builder
	var err error
	for ev, evErr := range c.GenerateStream(t.Context(), userText("m", "hi")) {
		if evErr != nil {
			err = evErr
			break
		}
		if d, ok := ev.(protocol.OutputTextDelta); ok {
			text.WriteString(d.Delta)
			g.Await(t, 1)
		}
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("GenerateStream error = %v, want go-ai's idle error, io.ErrUnexpectedEOF and os.ErrDeadlineExceeded", err)
	}
	if text.String() != "Hel" {
		t.Errorf("text before the idle error = %q, want Hel", text.String())
	}
	awaitUnparked(t, &g)
}

func TestRawReplyHold(t *testing.T) {
	frames := []aitest.Frame{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}`},
		{Event: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
	}
	var g aitest.Gate
	e := aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"messages": func(context.Context, aitest.RawRequest) aitest.Reply {
			return aitest.RawReply{Frames: frames, Hold: &g, HoldAfter: 3}
		},
	}})
	errs := make(chan error, 1)
	deltas := textDeltas(t.Context(), newClient(t, e, messages.New(messages.Options{})), errs)
	if d := nextDelta(t, deltas); d != "Hello" {
		t.Fatalf("first text delta = %q, want Hello", d)
	}
	g.Await(t, 1)
	checkHeld(t, deltas)
	g.Open()
	if got := rest(t, deltas); got != ", world" {
		t.Errorf("text after the release = %q, want \", world\"", got)
	}
	if err := receive(t, errs, "GenerateStream"); err != nil {
		t.Errorf("GenerateStream: %v", err)
	}
}

func TestHoldEndsWithTest(t *testing.T) {
	var g aitest.Gate
	errs := make(chan error, 2)
	t.Run("held", func(t *testing.T) {
		e := aitest.Serve(t, aitest.Routes{
			ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
				return aitest.ChatReply{Chunks: []string{"Hel", "lo"}, Hold: &g, HoldAfter: 1}
			},
			Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
				"messages": func(context.Context, aitest.RawRequest) aitest.Reply {
					return aitest.RawReply{Frames: []aitest.Frame{{Event: "ping", Data: `{"type":"ping"}`}}, Hold: &g}
				},
			},
		})
		chat := textDeltas(context.Background(), chatClient(t, e, ai.Options{}), errs)
		raw := textDeltas(context.Background(), newClient(t, e, messages.New(messages.Options{})), errs)
		nextDelta(t, chat)
		g.Await(t, 2)
		go func() {
			for range raw {
			}
		}()
	})
	for range 2 {
		if err := receive(t, errs, "GenerateStream"); !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("GenerateStream error = %v, want io.ErrUnexpectedEOF", err)
		}
	}
	if n := g.Parked(); n != 0 {
		t.Errorf("Parked = %d after the test ended, want 0", n)
	}
}
