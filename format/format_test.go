package format_test

import (
	"testing"

	"github.com/standards-lab/go-ai/format"
)

func TestDecodeOpenAIError(t *testing.T) {
	cases := []struct {
		name               string
		body               string
		message, typ, code string
	}{
		{"numeric code", `{"error":{"code":400,"message":"model 'x' not found","type":"invalid_request_error"}}`,
			"model 'x' not found", "invalid_request_error", "400"},
		{"string code", `{"error":{"message":"slow down","type":"rate_limit","code":"rate_limit_exceeded","param":null}}`,
			"slow down", "rate_limit", "rate_limit_exceeded"},
		{"message only", `{"error":{"message":"boom"}}`, "boom", "", ""},
		{"string error", `{"error":"loading model"}`, "loading model", "", ""},
		{"members before a mistyped one", `{"error":{"message":"boom","type":7,"code":"c"}}`, "boom", "", ""},
		{"error of another shape", `{"error":42}`, "", "", ""},
		{"no error member", `{"detail":"boom"}`, "", "", ""},
		{"not JSON", "upstream down\n", "", "", ""},
		{"empty", "", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			message, typ, code := format.DecodeOpenAIError([]byte(tc.body))
			if message != tc.message || typ != tc.typ || code != tc.code {
				t.Errorf("DecodeOpenAIError = {%q %q %q}, want {%q %q %q}",
					message, typ, code, tc.message, tc.typ, tc.code)
			}
		})
	}
}

func TestUnsupportedErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		err  format.UnsupportedError
		want string
	}{
		{"its message", format.UnsupportedError{Format: "f", Feature: "tool choice", Value: "x", Message: "no tools here"},
			"no tools here"},
		{"feature and value", format.UnsupportedError{Format: "f", Feature: "tool choice", Value: "x"},
			"tool choice x has no f form"},
		{"feature alone", format.UnsupportedError{Format: "f", Feature: "max output tokens"},
			"max output tokens has no f form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error = %q, want %q", got, tc.want)
			}
		})
	}
}
