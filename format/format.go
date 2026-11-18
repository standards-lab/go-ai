package format

import (
	"net/http"

	"github.com/standards-lab/go-ai/protocol"
)

// Format is a wire format: the adapter between the protocol's request and response model and one
// API's shape, such as Chat Completions. An ai.Client speaks one Format, chosen at the caller's
// composition root by the implementing package's constructor; there is no registry. Each
// implementation lives in a package of its own below this one, named for the endpoint it
// targets, such as completions.
//
// The client validates a request with protocol.Request.Validate before calling EncodeRequest
// or EncodeStreamRequest, and sends nothing when either fails, so a Format refuses there what
// its API can't express. A Format is safe for concurrent use: the state of a stream is its
// StreamDecoder's, and a new one serves each stream.
type Format interface {
	// Route is the path, relative to the client's base URL, that a request is posted to, such
	// as "chat/completions".
	Route() string
	// EncodeRequest encodes req as the JSON body of a request, merging req.Native into its top
	// level. It may add the format's own headers to header, such as a version header its API
	// requires; the client sets Content-Type, Accept, and Authorization itself. It refuses an
	// item the format can't express and a Native member that clashes with a member req's
	// typed fields set.
	EncodeRequest(req *protocol.Request, header http.Header) ([]byte, error)
	// DecodeResponse decodes the body of a 2xx reply into a Response.
	DecodeResponse(body []byte) (*protocol.Response, error)
	// EncodeStreamRequest encodes req as the JSON body of a streamed request, as EncodeRequest
	// encodes it for an unstreamed one, with the members that ask its API to stream the reply.
	// It refuses what EncodeRequest refuses, and a Native member that clashes with those.
	EncodeStreamRequest(req *protocol.Request, header http.Header) ([]byte, error)
	// EventStream is the framing the format's API streams a reply in, such as SSE. The client
	// asks for its media type in a streamed request's Accept header, and reads the reply only
	// through its reader, handing each event to the StreamDecoder.
	EventStream() EventStream
	// NewStreamDecoder returns a decoder for the reply to one streamed request. The client
	// calls it once per stream, so the decoder's state is that stream's alone.
	NewStreamDecoder() StreamDecoder
	// DecodeError returns the error message, type, and code the body of a non-2xx reply holds,
	// each empty when the body doesn't give it. The client puts them in its APIError.
	DecodeError(body []byte) (message, typ, code string)
}

// StreamDecoder turns the events of one streamed reply into the protocol's streaming events: a
// stream opens with protocol.ResponseCreated and closes with one terminal event, whose Response
// holds the finished output and the usage. It is framing-neutral: the client reads the reply
// through the Format's EventStream and hands the decoder each WireEvent, the framing's name for
// the event and its payload. A format that names its events in the payload reads the name from
// there, as Open Responses requires its event field to match the type in the event's body and
// Anthropic Messages puts a type in every data body, and Chat Completions names none; such a
// decoder needn't read the WireEvent's Type. It serves one stream, and isn't safe for
// concurrent use.
//
// An exception the endpoint reports in the stream, such as an error frame, reaches the decoder
// as an event like any other, never as an error from the WireEventReader, and the decoder
// closes the stream with protocol.ErrorEvent and then protocol.ResponseFailed.
type StreamDecoder interface {
	// Decode decodes e, the stream's next event, and returns the protocol events it makes,
	// which may be none. An error frame, the endpoint's report in the stream that the response
	// failed, is no error: it closes the stream as the spec has it, with the error event,
	// protocol.ErrorEvent, and then the terminal protocol.ResponseFailed, its Response failed,
	// holding the error and the output made before the frame. A chunk the format can't decode
	// is an error, after which the stream is broken; with it, Decode returns the events it made
	// before the error, which the client yields ahead of it.
	Decode(e WireEvent) ([]protocol.Event, error)
	// End is told the stream has ended, and returns the events that close it. A stream that
	// ended before it was complete is an error matching io.ErrUnexpectedEOF.
	End() ([]protocol.Event, error)
}
