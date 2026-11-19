package ai_test

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/internal/aitest"
)

// ndjsonType is the media type of ndjsonStream, an event stream defined only by these tests.
const ndjsonType = "application/x-ndjson"

// ndjsonStream is an event stream the client knows nothing of: newline-delimited JSON, each line
// one event, an object of its type and its data as a string. A blank line makes no event, and a
// line the body's end cuts off is discarded, as SSE discards an event no blank line ended.
type ndjsonStream struct{}

func (ndjsonStream) MediaType() string { return ndjsonType }

func (ndjsonStream) NewReader(body io.Reader) format.WireEventReader {
	return &ndjsonReader{r: bufio.NewReader(body)}
}

// ndjsonReader is ndjsonStream's WireEventReader.
type ndjsonReader struct{ r *bufio.Reader }

func (r *ndjsonReader) Next() (format.WireEvent, error) {
	for {
		line, err := r.r.ReadBytes('\n')
		if err != nil {
			return format.WireEvent{}, err
		}
		if line = bytes.TrimSpace(line); len(line) == 0 {
			continue
		}
		var e struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(line, &e); err != nil {
			return format.WireEvent{}, fmt.Errorf("ndjson: %w", err)
		}
		return format.WireEvent{Type: e.Type, Data: []byte(e.Data)}, nil
	}
}

// ndjsonFormat is Chat Completions streamed in ndjsonStream rather than SSE: a test Format
// wrapping the real one, its event stream the only difference.
type ndjsonFormat struct{ *completions.Format }

func (ndjsonFormat) EventStream() format.EventStream { return ndjsonStream{} }

// ndjsonEvents is an ndjsonStream body of chunks, one event each, named chunk.
func ndjsonEvents(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		line, err := json.Marshal(struct {
			Type string `json:"type"`
			Data string `json:"data"`
		}{"chunk", c})
		if err != nil {
			panic(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// framing is an event stream the stream tests run under: a Format streaming in it, and the
// encoding of chunks as a body framed so.
type framing struct {
	name   string
	format func() format.Format
	encode func(chunks ...string) string
}

// framings are SSE, the formats' own, and ndjsonStream, one the client was never told of.
var framings = []framing{
	{"SSE", func() format.Format { return completions.New(completions.Options{}) }, eventStream},
	{"NDJSON", func() format.Format { return ndjsonFormat{completions.New(completions.Options{})} }, ndjsonEvents},
}

// TestGenerateStreamDeclaredEventStream streams a reply in ndjsonStream, which only this test
// defines, through a Format that declares it, and wants the client to ask for its media type and
// fold the reply to the same Response as the same reply over SSE: the client reads a reply only
// through its Format's event stream.
func TestGenerateStreamDeclaredEventStream(t *testing.T) {
	srv := serveChat(t, http.StatusOK, ndjsonEvents(streamed...))
	c := newClientFormat(t, srv.URL(), http.DefaultClient, ndjsonFormat{completions.New(completions.Options{})}, ai.Options{})
	events, got, errs := collect(t, c.GenerateStream(t.Context(), hello()))
	if len(errs) > 0 {
		t.Fatalf("GenerateStream errors: %v", errs)
	}
	sse := serveChat(t, http.StatusOK, eventStream(streamed...))
	wantEvents, want, _ := collect(t, newClient(t, sse.URL(), ai.Options{}).GenerateStream(t.Context(), hello()))
	if !reflect.DeepEqual(events, wantEvents) || !reflect.DeepEqual(got, want) {
		t.Errorf("over NDJSON: %d events, Response %#v\nover SSE: %d events, Response %#v", len(events), got, len(wantEvents), want)
	}
	if !reflect.DeepEqual(got, completed) {
		t.Errorf("Response = %#v\nwant       %#v", got, completed)
	}
	if got := aitest.Only(t, srv).Header.Get("Accept"); got != ndjsonType {
		t.Errorf("Accept = %q, want %s", got, ndjsonType)
	}
}

// TestGenerateStreamEventStreamReadError wants an error the event stream's reader makes of the
// body, beside a failed read, to end the stream as a failed read does: matching
// io.ErrUnexpectedEOF and wrapping the reader's error.
func TestGenerateStreamEventStreamReadError(t *testing.T) {
	srv := serveChat(t, http.StatusOK, ndjsonEvents(streamed[:3]...)+"not JSON\n")
	c := newClientFormat(t, srv.URL(), http.DefaultClient, ndjsonFormat{completions.New(completions.Options{})}, ai.Options{})
	_, resp, errs := collect(t, c.GenerateStream(t.Context(), hello()))
	if len(errs) != 1 || !errors.Is(errs[0], io.ErrUnexpectedEOF) || !strings.Contains(errs[0].Error(), "ndjson:") {
		t.Fatalf("GenerateStream errors = %v, want one io.ErrUnexpectedEOF wrapping the reader's", errs)
	}
	if want := partial("Paris"); !reflect.DeepEqual(resp, want) {
		t.Errorf("Response = %#v\nwant       %#v", resp, want)
	}
}
