package format_test

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/standards-lab/go-ai/format"
)

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

func readAll(t *testing.T, body io.Reader) []format.WireEvent {
	t.Helper()
	r := format.SSE.NewReader(body)
	var got []format.WireEvent
	for {
		e, err := r.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		got = append(got, e)
	}
}

type splitReader struct{ chunks []string }

func (r *splitReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks[0] = r.chunks[0][n:]
	if r.chunks[0] == "" {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

func ev(typ, data string) format.WireEvent {
	return format.WireEvent{Type: typ, Data: []byte(data)}
}

var sseTests = []struct {
	name   string
	stream string
	want   []format.WireEvent
}{
	{"LF endings", "data: a\n\ndata: b\n\n", []format.WireEvent{ev("", "a"), ev("", "b")}},
	{"CR endings", "data: a\r\rdata: b\r\r", []format.WireEvent{ev("", "a"), ev("", "b")}},
	{"CRLF endings", "data: a\r\n\r\ndata: b\r\n\r\n", []format.WireEvent{ev("", "a"), ev("", "b")}},
	{"mixed endings", "data: a\r\n\ndata: b\r\r\ndata: c\n\r", []format.WireEvent{ev("", "a"), ev("", "b"), ev("", "c")}},
	{"CR CR is two line endings", "data: a\r\rdata: b\n\n", []format.WireEvent{ev("", "a"), ev("", "b")}},
	{"leading BOM stripped", "\uFEFFdata: a\n\n", []format.WireEvent{ev("", "a")}},
	{"leading BOM before a blank line", "\uFEFF\ndata: a\n\n", []format.WireEvent{ev("", "a")}},
	{"only one BOM stripped", "\uFEFF\uFEFFdata: a\n\n", nil},
	{"BOM after the start kept", "data: a\n\n\uFEFFdata: b\n\n", []format.WireEvent{ev("", "a")}},
	{"comment ignored", ": ping\ndata: a\n: more\n\n", []format.WireEvent{ev("", "a")}},
	{"comment-only block dispatches nothing", ": ping\n\n: ping\n\n", nil},
	{"no colon is a name with empty value", "data\n\n", []format.WireEvent{ev("", "")}},
	{"no colon data lines joined", "data\ndata\n\n", []format.WireEvent{ev("", "\n")}},
	{"one leading space removed", "data:  a\n\n", []format.WireEvent{ev("", " a")}},
	{"no space after the colon", "data:a\n\n", []format.WireEvent{ev("", "a")}},
	{"value split at the first colon", "data: a: b\n\n", []format.WireEvent{ev("", "a: b")}},
	{"trailing space kept", "data: a \n\n", []format.WireEvent{ev("", "a ")}},
	{"data lines joined", "data: a\ndata: b\ndata\ndata: c\n\n", []format.WireEvent{ev("", "a\nb\n\nc")}},
	{"empty data dispatched", "data:\n\n", []format.WireEvent{ev("", "")}},
	{"event names the type", "event: response.created\ndata: {}\n\n", []format.WireEvent{ev("response.created", "{}")}},
	{"last event field wins", "event: a\nevent: b\ndata: x\n\n", []format.WireEvent{ev("b", "x")}},
	{"type resets after dispatch", "event: a\ndata: x\n\ndata: y\n\n", []format.WireEvent{ev("a", "x"), ev("", "y")}},
	{"type resets after a block without data", "event: a\n\ndata: y\n\n", []format.WireEvent{ev("", "y")}},
	{"id field makes no event data", "id: 7\ndata: x\n\n", []format.WireEvent{ev("", "x")}},
	{"id-only block dispatches nothing", "id: 7\n\ndata: y\n\n", []format.WireEvent{ev("", "y")}},
	{"id with NUL makes no event data", "id: 8\x009\ndata: y\n\n", []format.WireEvent{ev("", "y")}},
	{"unknown fields ignored", "foo: bar\nData: no\ndata: x\nretry: 5\n\n", []format.WireEvent{ev("", "x")}},
	{"retry field ignored", "retry: 1500\ndata: x\n\n", []format.WireEvent{ev("", "x")}},
	{"retry-only block dispatches nothing", "retry: 1500\n\ndata: y\n\n", []format.WireEvent{ev("", "y")}},
	{"blank line without data dispatches nothing", "\n\n\nevent: a\n\n", nil},
	{"pending event at end discarded", "data: a\n\ndata: b\n", []format.WireEvent{ev("", "a")}},
	{"pending line at end discarded", "data: a\n\ndata: b", []format.WireEvent{ev("", "a")}},
	{"empty stream", "", nil},
	{"multi-byte text kept", "event: é\nid: €\ndata: h\u00e9llo \u20ac \U0001D11E\n\n", []format.WireEvent{ev("é", "héllo € \U0001D11E")}},
	{"invalid byte in data replaced", "data: a\xffb\n\n", []format.WireEvent{ev("", "a\uFFFDb")}},
	{"invalid byte in event type replaced", "event: a\x80\ndata: x\n\n", []format.WireEvent{ev("a\uFFFD", "x")}},
	{"truncated sequence is one replacement", "data: a\xe2\x82b\n\n", []format.WireEvent{ev("", "a\uFFFDb")}},
	{"truncated sequence at line end", "data: a\xf0\x9f\x98\ndata: b\xe2\r\n\r\n", []format.WireEvent{ev("", "a\uFFFD\nb\uFFFD")}},
	{"overlong and surrogate forms replaced per byte", "data: \xc0\x80\xed\xa0\x80\n\n", []format.WireEvent{ev("", "\uFFFD\uFFFD\uFFFD\uFFFD\uFFFD")}},
	{"maximal subparts replaced as Unicode Table 3-8 does", "data: a\xf1\x80\x80\xe1\x80\xc2b\x80c\x80\xbfd\n\n", []format.WireEvent{ev("", "a\uFFFD\uFFFD\uFFFDb\uFFFDc\uFFFD\uFFFDd")}},
}

func TestSSEEvents(t *testing.T) {
	readers := []struct {
		name string
		wrap func(string) io.Reader
	}{
		{"whole", func(s string) io.Reader { return strings.NewReader(s) }},
		{"one byte", func(s string) io.Reader { return iotest.OneByteReader(strings.NewReader(s)) }},
		{"split after CR, every CRLF across two reads", func(s string) io.Reader {
			var chunks []string
			for s != "" {
				i := strings.IndexByte(s, '\r')
				if i < 0 {
					i = len(s) - 1
				}
				chunks = append(chunks, s[:i+1])
				s = s[i+1:]
			}
			return &splitReader{chunks: chunks}
		}},
	}
	for _, tt := range sseTests {
		for _, r := range readers {
			t.Run(tt.name+"/"+r.name, func(t *testing.T) {
				got := readAll(t, r.wrap(tt.stream))
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("events = %q\nwant     %q", got, tt.want)
				}
			})
		}
	}
}

func TestSSEDataIsEachEventsOwn(t *testing.T) {
	r := format.SSE.NewReader(strings.NewReader("data: abc\n\ndata: xyz\n\n"))
	first, err := r.Next()
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if _, err := r.Next(); err != nil {
		t.Fatalf("Next: %v", err)
	}
	if string(first.Data) != "abc" {
		t.Errorf("first event's Data after the second Next = %q, want abc", first.Data)
	}
}

func TestSSEDispatchesAsBytesArrive(t *testing.T) {
	pr, pw := io.Pipe()
	r := format.SSE.NewReader(pr)
	writes := []struct {
		chunk string
		want  format.WireEvent
	}{
		{"data: a\r\r", ev("", "a")},
		{"\ndata: b\r\n\r", ev("", "b")},
		{"\nevent: c\ndata: c\n\n", ev("c", "c")},
	}
	type result struct {
		e   format.WireEvent
		err error
	}
	for _, w := range writes {
		go func() { _, _ = io.WriteString(pw, w.chunk) }()
		done := make(chan result, 1)
		go func() {
			e, err := r.Next()
			done <- result{e, err}
		}()
		select {
		case got := <-done:
			if got.err != nil {
				t.Fatalf("Next: %v", got.err)
			}
			if !reflect.DeepEqual(got.e, w.want) {
				t.Errorf("Next() = %q, want %q", got.e, w.want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Next waited past the blank line ending %q", w.chunk)
		}
	}
	_ = pw.Close()
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("Next at end = %v, want io.EOF", err)
	}
}
