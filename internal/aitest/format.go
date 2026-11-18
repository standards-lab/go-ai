package aitest

import (
	"errors"
	"testing"

	"github.com/standards-lab/go-ai/format"
)

// AssertUnsupported fails t unless err is message, whole, and wraps a *format.UnsupportedError
// of want's Format, Feature, Value, and Declared. want's Message isn't compared: message is the
// account as the caller sees it, with every layer's wrapping.
func AssertUnsupported(t testing.TB, err error, want format.UnsupportedError, message string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %q", message)
	}
	if err.Error() != message {
		t.Errorf("error = %q, want %q", err, message)
	}
	got, ok := errors.AsType[*format.UnsupportedError](err)
	if !ok {
		t.Fatalf("error %q wraps no *format.UnsupportedError", err)
	}
	if got.Format != want.Format || got.Feature != want.Feature || got.Value != want.Value || got.Declared != want.Declared {
		t.Errorf("UnsupportedError = {Format:%q Feature:%q Value:%q Declared:%t}, want {Format:%q Feature:%q Value:%q Declared:%t}",
			got.Format, got.Feature, got.Value, got.Declared, want.Format, want.Feature, want.Value, want.Declared)
	}
}
