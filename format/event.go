package format

import (
	"io"

	"github.com/standards-lab/go-ai/internal/sse"
)

// WireEvent is one event of a streamed reply as its framing delivers it, before a
// StreamDecoder reads it: the framing's name for the event, if it gives one, and its payload.
type WireEvent struct {
	// Type is the framing's name for the event, empty when it gives none. Under SSE it is the
	// event's event field.
	Type string
	// Data is the event's payload. Under SSE it is the event's data.
	Data []byte
}

// WireEventReader reads the events of one streamed reply's body, one per call. It isn't safe
// for concurrent use.
type WireEventReader interface {
	// Next returns the body's next event. At the body's clean end it returns io.EOF; an error
	// reading the body is returned as it is, so the caller can classify it.
	Next() (WireEvent, error)
}

// EventStream is the framing a streamed reply's events arrive in, such as SSE: the media type a
// streamed request asks for, and the reader of a reply framed so. An EventStream is safe for
// concurrent use; the state of one reply's reading is its WireEventReader's.
type EventStream interface {
	// MediaType is the framing's media type, which a streamed request's Accept header names,
	// such as "text/event-stream".
	MediaType() string
	// NewReader returns a reader of the events of body, one streamed reply's.
	NewReader(body io.Reader) WireEventReader
}

// SSE is the server-sent event stream, text/event-stream, read to the WHATWG HTML standard's
// rules: comments, blocks without data, and the id and retry fields make no event, and an event
// that no blank line ended at the body's end is discarded. A WireEvent's Type is its event
// field, empty when the event has none, and its Data is its data fields' values joined with
// "\n", decoded as UTF-8 with replacement.
var SSE EventStream = sseStream{}

// sseStream is SSE's EventStream, over the internal sse decoder.
type sseStream struct{}

func (sseStream) MediaType() string { return "text/event-stream" }

func (sseStream) NewReader(body io.Reader) WireEventReader {
	return sseReader{sse.NewDecoder(body)}
}

// sseReader is SSE's WireEventReader.
type sseReader struct{ d *sse.Decoder }

func (r sseReader) Next() (WireEvent, error) {
	e, err := r.d.Next()
	if err != nil {
		return WireEvent{}, err
	}
	return WireEvent{Type: e.Type, Data: []byte(e.Data)}, nil
}
