package ai

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryPolicy is when and how long the Client waits to send a request again. It is the caller's
// policy, set in Options.Retry; its zero value retries nothing.
//
// A request is sent again only when its attempt ends without a 2xx reply in a way that says the
// endpoint may answer later: a 408 Request Timeout, 429 Too Many Requests, 502 Bad Gateway, 503
// Service Unavailable, or 504 Gateway Timeout reply, or a connection failure before any reply,
// such as a refused dial or a connection reset or closed before the reply's header. A gateway
// answers 502 and 504 for an upstream that failed or stalled, which a later attempt can mend.
// Nothing else is: not a 500 or any other status, not a cancelled or expired context, not a
// failure of the token source, not the transport's ResponseHeaderTimeout, after which the
// endpoint may still be running the request, not net/http's error for a server closing an idle
// connection, which is unexported and could be matched only by its text, and never a stream
// once its 2xx reply has arrived.
//
// A connection that fails before the reply may fail after the endpoint has read the request,
// and the requests the Client sends, but for Models' GET, are POSTs, which nothing marks as safe
// to repeat: a retried generation, embedding, or transcription may run twice on the endpoint,
// and be counted twice. Models' GET is safe to repeat, so its retry runs nothing twice. The Client sends no
// idempotency key, which would be one endpoint's native header, not the standard's. The caller
// who sets a policy accepts that.
//
// Each attempt calls the token source again and sends the same body: the same encoded bytes,
// or for Transcribe, the clip opened again. Between attempts
// the Client waits an exponential backoff with full jitter: before attempt n+1, a random wait of
// up to BaseDelay doubled n-1 times, capped at MaxDelay. A reply's Retry-After, in seconds or as
// an HTTP date, replaces the backoff; one that can't be read, or is negative, is ignored. When
// the wait is longer than MaxDelay, which only a Retry-After makes it, or would end past ctx's
// deadline, the Client doesn't wait: it returns the last attempt's error at once, whose
// APIError.RetryAfter holds the wait for a caller that redelivers. A ctx cancelled while
// waiting returns ctx's error. Once MaxAttempts are made, the last attempt's error is returned,
// as it is without a policy, such as the *APIError of the last reply.
type RetryPolicy struct {
	// MaxAttempts is the most requests sent for one call, the first included. 0 and 1 each send
	// one and retry nothing; a negative count is refused by New.
	MaxAttempts int
	// BaseDelay is the cap on the wait before the second attempt, which doubles for each attempt
	// after it. It is at least 0 and at most MaxDelay.
	BaseDelay time.Duration
	// MaxDelay is the cap on any wait, at least 0, and the longest Retry-After the Client
	// honors.
	MaxDelay time.Duration
}

func (p RetryPolicy) check() error {
	switch {
	case p.MaxAttempts < 0:
		return errors.New("ai: retry policy has a negative attempt count")
	case p.BaseDelay < 0 || p.MaxDelay < 0:
		return errors.New("ai: retry policy has a negative delay")
	case p.BaseDelay > p.MaxDelay:
		return errors.New("ai: retry policy's base delay is above its most delay")
	}
	return nil
}

func (p RetryPolicy) backoff(n int) time.Duration {
	d := p.BaseDelay
	for i := 1; i < n && d < p.MaxDelay; i++ {
		if d > p.MaxDelay/2 {
			return p.MaxDelay
		}
		d *= 2
	}
	return min(d, p.MaxDelay)
}

func fullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d == time.Duration(math.MaxInt64) {
		return rand.N(d)
	}
	return rand.N(d + 1)
}

// See docs/design.md, "Implementation notes".
func (c *Client) send(ctx context.Context, method, route string, makeBody body, contentType, accept string,
	decodeError func(body []byte) (message, typ, code string)) (*http.Response, error) {
	attempts := max(c.retry.MaxAttempts, 1)
	for n := 1; ; n++ {
		req, end, err := c.request(ctx, method, route, makeBody, contentType, accept)
		if err != nil {
			return nil, err
		}
		resp, wait, ok, err := c.do(req, decodeError)
		if err == nil {
			resp.Body = &endedBody{ReadCloser: resp.Body, end: end}
			return resp, nil
		}
		if bodyErr := end(); bodyErr != nil {
			return nil, bodyErr
		}
		if n == attempts || !retryable(ctx, err) {
			return nil, err
		}
		if !ok {
			wait = c.jitter(c.retry.backoff(n))
		}
		if wait > c.retry.MaxDelay {
			return nil, err
		}
		if deadline, ok := ctx.Deadline(); ok && time.Now().Add(wait).After(deadline) {
			return nil, err
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func retryable(ctx context.Context, err error) bool {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		switch apiErr.Status {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway,
			http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return true
		}
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	return connectionFailure(err)
}

// See docs/design.md, "Implementation notes".
func connectionFailure(err error) bool {
	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		return dnsErr.IsTemporary
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func parseRetryAfter(h string, now time.Time) (time.Duration, bool) {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0, false
	}
	if strings.Trim(h, "0123456789") == "" {
		secs, err := strconv.ParseInt(h, 10, 64)
		if err != nil || secs > int64(time.Duration(math.MaxInt64)/time.Second) {
			return time.Duration(math.MaxInt64), true
		}
		return time.Duration(secs) * time.Second, true
	}
	t, err := http.ParseTime(h)
	if err != nil {
		return 0, false
	}
	return max(t.Sub(now), 0), true
}
