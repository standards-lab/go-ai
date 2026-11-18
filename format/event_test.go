package format_test

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/standards-lab/go-ai/format"
)

// TestSSE wants SSE to name text/event-stream and to read each event as a WireEvent: its Type
// the event field, empty when the event has none, and its Data the data, with comments and
// blocks without data making no event, and io.EOF at the body's end.
func TestSSE(t *testing.T) {
	if got := format.SSE.MediaType(); got != "text/event-stream" {
		t.Errorf("MediaType = %q, want text/event-stream", got)
	}
	body := ": ping\n\nevent: response.created\ndata: {\"a\":1}\n\ndata: [DONE]\n\nevent: x\n\n"
	r := format.SSE.NewReader(strings.NewReader(body))
	var got []format.WireEvent
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		got = append(got, e)
	}
	want := []format.WireEvent{
		{Type: "response.created", Data: []byte(`{"a":1}`)},
		{Type: "", Data: []byte("[DONE]")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events = %q\nwant     %q", got, want)
	}
}

// TestSSEReadError wants a failed read of the body returned as it is, for the client to
// classify.
func TestSSEReadError(t *testing.T) {
	boom := errors.New("boom")
	r := format.SSE.NewReader(io.MultiReader(strings.NewReader("data: a\n\ndata: b\n"), iotest.ErrReader(boom)))
	if e, err := r.Next(); err != nil || string(e.Data) != "a" {
		t.Fatalf("first Next = %q, %v; want data a", e.Data, err)
	}
	if _, err := r.Next(); !errors.Is(err, boom) {
		t.Errorf("second Next error = %v, want the read's", err)
	}
}
