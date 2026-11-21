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

// See docs/design.md, "Implementation notes".
type idleReader struct {
	body   io.ReadCloser
	limit  time.Duration
	cancel context.CancelFunc
	timer  *time.Timer
	fired  atomic.Bool
}

func newIdleReader(body io.ReadCloser, limit time.Duration, cancel context.CancelFunc) *idleReader {
	r := &idleReader{body: body, limit: limit, cancel: cancel}
	r.timer = time.AfterFunc(limit, func() {
		r.fired.Store(true)
		cancel()
	})
	r.timer.Stop()
	return r
}

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

func (r *idleReader) stalled() error {
	return fmt.Errorf("%w: no bytes within the stream idle timeout (%s): %w",
		io.ErrUnexpectedEOF, r.limit, os.ErrDeadlineExceeded)
}

func (r *idleReader) Close() error {
	r.timer.Stop()
	r.cancel()
	return r.body.Close()
}
