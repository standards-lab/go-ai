// Package sse is a decoder for a server-sent event stream, the text/event-stream body of a
// streamed reply, written to the WHATWG HTML standard's rules for parsing and interpreting an
// event stream. It reads the body as the bytes arrive and holds only the event being built. It is
// a decoder alone: it never reconnects, and it never sends Last-Event-ID, so it reads a retry
// field, the reconnection time, as it reads an unknown field, and ignores it.
//
// The stream is decoded as UTF-8 with replacement, as the standard requires: each invalid byte
// sequence becomes one U+FFFD per maximal subpart, as the Encoding Standard's UTF-8 decoder
// yields, so Event's fields are always valid UTF-8.
//
// Its exported identifiers:
//
//   - [Event] is one dispatched event: its type, data, and last event ID.
//   - [Decoder] reads events from a stream, one per call.
//   - [NewDecoder] returns a Decoder reading a stream.
//   - [Decoder.Next] returns the stream's next event, or io.EOF at its end.
package sse
