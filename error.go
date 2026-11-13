package ai

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// maxErrorText bounds how much of an error body without a message Error prints. It bounds the
// text of the error, not what is read: Body holds the whole reply.
const maxErrorText = 512

// APIError is a non-2xx reply from the endpoint. Requests return it wrapped, so callers reach it
// with errors.As.
type APIError struct {
	// Status is the HTTP status code.
	Status int
	// Message is the endpoint's error message, when Body gave one, decoded by the Client's
	// format.
	Message string
	// Type is the endpoint's error type, such as "invalid_request_error", when it gave one.
	Type string
	// Code is the endpoint's error code, when it gave one. A numeric code is kept as its
	// decimal text.
	Code string
	// Body is the reply's body.
	Body []byte
	// RetryAfter is the wait the reply's Retry-After header asked for, in seconds or as an HTTP
	// date, measured from the reply's arrival. It is zero when the reply had none, or one that
	// can't be read or is negative, and zero for a date already passed. Seconds too many to
	// hold are the longest duration there is. A caller that redelivers the request later takes
	// its delay from here.
	RetryAfter time.Duration
}

// Error reports the status and the endpoint's message, with its type and code when it gave
// them, or the start of the body when it gave no message.
func (e *APIError) Error() string {
	s := fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
	if e.Message != "" {
		s += ": " + e.Message
		var detail []string
		for _, d := range []string{e.Type, e.Code} {
			if d != "" {
				detail = append(detail, d)
			}
		}
		if len(detail) > 0 {
			s += " (" + strings.Join(detail, ", ") + ")"
		}
		return s
	}
	body := bytes.TrimSpace(e.Body)
	switch {
	case len(body) == 0:
		return s
	case len(body) > maxErrorText:
		return fmt.Sprintf("%s: %s...", s, body[:maxErrorText])
	}
	return fmt.Sprintf("%s: %s", s, body)
}
