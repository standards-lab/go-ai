package ai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"
)

// idleReader bounds each read of a started stream's body by limit, the Options'
// StreamIdleTimeout. A timer runs only while a read waits, so the time the caller spends between
// events isn't counted; when it fires, it cancels the stream's request, which unblocks the
// transport's read, and the read reports the stall. Any bytes a read returns, a comment's
// included, end that read and so reset the bound. The timer is time.AfterFunc's, so the bound
// starts no goroutine of its own.
type idleReader struct {
	body   io.ReadCloser
	limit  time.Duration
	cancel context.CancelFunc
	timer  *time.Timer
	fired  atomic.Bool
}

// newIdleReader returns body bounded by limit, cancel ending the request it is the reply of.
func newIdleReader(body io.ReadCloser, limit time.Duration, cancel context.CancelFunc) *idleReader {
	r := &idleReader{body: body, limit: limit, cancel: cancel}
	r.timer = time.AfterFunc(limit, func() {
		r.fired.Store(true)
		cancel()
	})
	r.timer.Stop()
	return r
}

// Read reads body with the timer running. A read the timer cut off, or any read after it, is
// the stall's error, whatever the transport returned for the cancelled request: its error is the
// cancellation's, which the caller must not mistake for its own context's. A read that reached
// the body's end as the timer fired is the end, since the stream was whole.
func (r *idleReader) Read(p []byte) (int, error) {
	if r.fired.Load() {
		return 0, r.stalled()
	}
	r.timer.Reset(r.limit)
	n, err := r.body.Read(p)
	if !r.timer.Stop() && r.fired.Load() && !errors.Is(err, io.EOF) {
		return n, r.stalled()
	}
	return n, err
}

// stalled is the error of a read the timer cut off: the stream ended before its terminal event,
// so it matches io.ErrUnexpectedEOF, and it ended on a deadline, so it matches
// os.ErrDeadlineExceeded, as a read past a net.Conn's deadline does.
func (r *idleReader) stalled() error {
	return fmt.Errorf("%w: no bytes within the stream idle timeout (%s): %w",
		io.ErrUnexpectedEOF, r.limit, os.ErrDeadlineExceeded)
}

// Close stops the timer and cancels the request before it closes the body, so a transport whose
// body resumes after a failed read sees the request is done.
func (r *idleReader) Close() error {
	r.timer.Stop()
	r.cancel()
	return r.body.Close()
}
