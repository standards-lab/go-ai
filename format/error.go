package format

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
)

// UnsupportedError is a format's refusal of a request it can't send: a feature of the request
// that its wire format, or the deployment it targets, has no form for. The format refuses it
// while it encodes the request, so a refusal happens before anything is sent or any token is
// fetched. It is returned as a pointer, wrapped in the format's and the client's context, so
// errors.As finds it through every layer.
type UnsupportedError struct {
	// Format names the wire format that refused the request, as its errors are prefixed:
	// completions, messages, or responses.
	Format string
	// Feature names the request feature refused, such as "tool choice", "text format",
	// "input item", "content part", or "native member".
	Feature string
	// Value is the refused value, such as the Go type of a tool choice or the name of a native
	// member, and empty when the refusal is of a feature's absence or there is no value to name.
	Value string
	// Declared is true when the refusal is of a gap the deployment's profile declares, and false
	// when it is of a feature the wire format's spec has no form for.
	Declared bool
	// Message is the refusal's account, which Error returns. When it is empty, Error builds one
	// from the other fields.
	Message string
}

// Error returns the refusal's Message or, when it has none, an account of its Format, Feature,
// and Value. It names no position in the request; the format's wrapping does.
func (e *UnsupportedError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Value == "" {
		return fmt.Sprintf("%s has no %s form", e.Feature, e.Format)
	}
	return fmt.Sprintf("%s %s has no %s form", e.Feature, e.Value, e.Format)
}

// DecodeOpenAIError decodes an OpenAI-style error body, {"error": {"message", "type", "code"}},
// where code is a string or a number, and returns its message, type, and code. An error that is
// a bare string is the message. A body of any other shape gives nothing; an error object with a
// member of an unexpected type gives the members decoded before it. It is the error body of
// every OpenAI-compatible endpoint, so the formats that target one use it for DecodeError, and
// the client uses it for its OpenAI-compatible operations outside the Format, such as
// embeddings.
func DecodeOpenAIError(body []byte) (message, typ, code string) {
	var wire openAIErrorBody
	if json.Unmarshal(body, &wire) != nil {
		return "", "", ""
	}
	switch wire.Error.Kind() {
	case jsontext.KindString:
		_ = json.Unmarshal(wire.Error, &message)
		return message, "", ""
	case jsontext.KindBeginObject:
		var e openAIErrorObject
		_ = json.Unmarshal(wire.Error, &e)
		switch e.Code.Kind() {
		case jsontext.KindString:
			_ = json.Unmarshal(e.Code, &code)
		case jsontext.KindNumber:
			code = string(e.Code)
		}
		return e.Message, e.Type, code
	}
	return "", "", ""
}

type openAIErrorBody struct {
	Error jsontext.Value `json:"error"`
}

type openAIErrorObject struct {
	Message string         `json:"message"`
	Type    string         `json:"type"`
	Code    jsontext.Value `json:"code"`
}
