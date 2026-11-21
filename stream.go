package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"

	"github.com/standards-lab/go-ai/protocol"
)

// GenerateStream streams req's response as the spec's streaming events, which a
// protocol.Accumulator folds into the Response Generate would return. The sequence is lazy:
// nothing is validated, encoded, or sent until it is ranged, and each range sends a new request.
// It asks, in its Accept header, for the media type of the format's EventStream, such as
// format.SSE's text/event-stream, and reads the reply only through that EventStream's reader,
// whatever Content-Type the reply names, handing each event to the format's StreamDecoder. It
// reads the events as they arrive, inside the range loop, with no goroutine; breaking out of the
// loop closes the reply's body.
//
// Each pair holds an event or an error, never both. The events run from
// protocol.ResponseCreated to one terminal event, after which the sequence ends. A failure the
// endpoint reports in the stream, such as llama.cpp's error frame, data {"error": {...}}, is no
// error pair: the stream ends as the spec has it, with protocol.ErrorEvent, holding the error's
// type, code, message, and param, and then the terminal protocol.ResponseFailed, whose Response
// is in protocol.StatusFailed, its Error the failure, holding the output made before it. The
// caller checks the folded Response's Status for StatusFailed as it does for StatusIncomplete.
//
// Error pairs are for a stream that breaks, or never begins. An error ends the sequence as its
// last pair, and no event follows it; the events before it fold, in a protocol.Accumulator, to
// the partial Response, in protocol.StatusInProgress:
//
//   - a request Validate or the format refuses, a token source error, a transport failure, or a
//     non-2xx reply, the same *APIError as Generate, is the sequence's only pair
//   - a cancelled or expired ctx is ctx's error, wrapped, matching context.Canceled or
//     context.DeadlineExceeded; cancelling closes the connection, which stops the generation,
//     and no event is yielded once ctx is done
//   - a stream that ends before its terminal event matches io.ErrUnexpectedEOF, as does a read
//     that fails, such as on a connection dropped or reset, whose error is wrapped too
//   - with Options.StreamIdleTimeout set, a read that gets no bytes for that long closes the
//     connection, and its error matches both io.ErrUnexpectedEOF and os.ErrDeadlineExceeded
//   - an event the format can't decode is the format's error, wrapped
//
// With an Options.Retry policy, a request the policy retries is sent again before the stream
// starts: until a 2xx reply arrives, its first byte unread. A stream once started is never sent
// again; its error ends the sequence as above.
//
// What the EventStream reads as no event, such as llama.cpp's ping under SSE, a ":" comment line
// every 30 seconds, changes nothing, except that its bytes reset the idle bound.
func (c *Client) GenerateStream(ctx context.Context, req *protocol.Request) iter.Seq2[protocol.Event, error] {
	return func(yield func(protocol.Event, error) bool) {
		if err := c.stream(ctx, req, yield); err != nil {
			yield(nil, fmt.Errorf("ai: generate stream: %w", err))
		}
	}
}

// See docs/design.md, "Implementation notes".
func (c *Client) stream(ctx context.Context, req *protocol.Request, yield func(protocol.Event, error) bool) error {
	if err := req.Validate(); err != nil {
		return err
	}
	header := make(http.Header)
	body, err := c.format.EncodeStreamRequest(req, header)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream := c.format.EventStream()
	resp, err := c.send(reqCtx, http.MethodPost, c.format.Route(), fixedBody(header, body), "application/json",
		stream.MediaType(), c.format.DecodeError)
	if err != nil {
		return err
	}
	reply := io.ReadCloser(resp.Body)
	if c.idle > 0 {
		reply = newIdleReader(resp.Body, c.idle, cancel)
	}
	defer func() { _ = reply.Close() }()
	emit := func(made []protocol.Event) (bool, error) {
		for _, event := range made {
			if err := ctx.Err(); err != nil {
				return false, err
			}
			if !yield(event, nil) || terminal(event) {
				return true, nil
			}
		}
		return false, nil
	}
	events, decoder := stream.NewReader(reply), c.format.NewStreamDecoder()
	for {
		e, err := events.Next()
		if errors.Is(err, io.EOF) {
			made, err := decoder.End()
			switch over, ctxErr := emit(made); {
			case ctxErr != nil:
				return ctxErr
			case over:
				return nil
			case err != nil:
				return err
			}
			return fmt.Errorf("stream ended before its terminal event: %w", io.ErrUnexpectedEOF)
		}
		if err != nil {
			return readError(ctx, err)
		}
		made, err := decoder.Decode(e)
		switch over, ctxErr := emit(made); {
		case ctxErr != nil:
			return ctxErr
		case over:
			return nil
		case err != nil:
			return err
		}
	}
}

func readError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("read stream: %w", err)
	}
	return fmt.Errorf("read stream: %w: %w", io.ErrUnexpectedEOF, err)
}

func terminal(e protocol.Event) bool {
	switch e.(type) {
	case protocol.ResponseCompleted, protocol.ResponseIncomplete, protocol.ResponseFailed:
		return true
	}
	return false
}
