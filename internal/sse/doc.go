// Package sse is a decoder for a server-sent event stream, the text/event-stream body of a
// streamed reply, written to the WHATWG HTML standard's rules for parsing and interpreting an
// event stream. It reads the body as the bytes arrive and holds only the event being built. It is
// a decoder alone: it never reconnects, and it never sends Last-Event-ID, so it reads a retry
// field, the reconnection time, as it reads an unknown field, and ignores it.
//
// The stream is decoded as UTF-8 with replacement, as the standard requires: each invalid byte
// sequence becomes one U+FFFD per maximal subpart, as the Encoding Standard's UTF-8 decoder
// yields, so Event's fields are always valid UTF-8. That is neither utf8.DecodeRune's
// replacement, which gives a truncated sequence a U+FFFD per byte, nor bytes.ToValidUTF8's, which
// collapses a run of invalid bytes into one. The decoder's bounds on a lead byte's first
// continuation byte bar overlong forms (E0, F0), the surrogates (ED), and code points past
// U+10FFFF (F4), as the Encoding Standard's do. A stream is decoded a line at a time, which
// decodes it whole: a line ends at CR or LF, and an ASCII byte ends any pending sequence in the
// whole-stream decoder too, so a character split across reads is whole by its line's end.
//
// A line ends at CRLF, LF, or CR. A CR is remembered rather than settled by peeking past it, so
// an LF that starts the next read completes that line's ending, and the decoder never waits for a
// byte the server may not send until its next event. A line the stream's end cuts off is
// discarded with the event it belongs to.
//
// Its exported identifiers:
//
//   - [Event] is one dispatched event: its type, data, and last event ID.
//   - [Decoder] reads events from a stream, one per call.
//   - [NewDecoder] returns a Decoder reading a stream.
//   - [Decoder.Next] returns the stream's next event, or io.EOF at its end.
package sse
