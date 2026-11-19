package aitest

import (
	"context"
	"encoding/json/v2"
	"testing"
)

// TokenCounter is a token source, its Token method, that counts its calls.
type TokenCounter struct {
	// Calls is the number of times Token has been called.
	Calls int
}

// Token returns a fixed token and counts the call.
func (c *TokenCounter) Token(context.Context) (string, error) {
	c.Calls++
	return "secret", nil
}

// ReplyMessage returns the error message of a recorded error reply, its error.message member,
// read from the body apart from the client's own decoding. It fails t if the reply has none.
func ReplyMessage(t testing.TB, reply []byte) string {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(reply, &body); err != nil || body.Error.Message == "" {
		t.Fatalf("recorded reply %s has no error message: %v", reply, err)
	}
	return body.Error.Message
}
