package integration_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-ai/tools/infer/integration"
)

// This file is untagged, so the unit tier runs its tests: Endpoint's
// mapping of a role's variables, which every scenario runs through, is
// checked on every pull request.

// logRecorder is a testing.TB that keeps the text of each Log, Logf,
// Fatalf, and Skipf call before passing it on, so a test reads what the
// harness, or a run through it, printed.
type logRecorder struct {
	testing.TB
	mu   sync.Mutex
	text strings.Builder
}

func (r *logRecorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.text.WriteString(s + "\n")
}

func (r *logRecorder) Log(args ...any) {
	r.Helper()
	r.record(fmt.Sprint(args...))
	r.TB.Log(args...)
}

func (r *logRecorder) Logf(format string, args ...any) {
	r.Helper()
	r.record(fmt.Sprintf(format, args...))
	r.TB.Logf(format, args...)
}

func (r *logRecorder) Fatalf(format string, args ...any) {
	r.Helper()
	r.record(fmt.Sprintf(format, args...))
	r.TB.Fatalf(format, args...)
}

func (r *logRecorder) Skipf(format string, args ...any) {
	r.Helper()
	r.record(fmt.Sprintf(format, args...))
	r.TB.Skipf(format, args...)
}

// String returns everything recorded so far.
func (r *logRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.text.String()
}

// The token and header values a test maps, which nothing may print.
const (
	tokenSecret  = "t0ken-secret"
	headerSecret = "head3r-secret"
)

// unsetInfer unsets INFER_TOKEN and INFER_HEADER for the test, as Main
// does for the suite, restoring them when it ends.
func unsetInfer(t *testing.T) {
	t.Helper()
	for _, name := range []string{"INFER_TOKEN", "INFER_HEADER"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

// Endpoint maps a role's base URL onto the INFER_BASE_URL override, and
// its token and header onto INFER_TOKEN and INFER_HEADER in the test's
// environment, never in the overrides a run logs; it logs the mapping by
// name, never a value.
func TestEndpoint_MapsCredentials(t *testing.T) {
	unsetInfer(t)
	header := "api-key: " + headerSecret + "\nx-tenant: docs"
	t.Setenv("AI_TEST_BASE_URL", "http://localhost:8000/v1")
	t.Setenv("AI_TEST_TOKEN", tokenSecret)
	t.Setenv("AI_TEST_HEADER", header)
	rec := &logRecorder{TB: t}

	overrides := integration.Endpoint(rec, "AI_TEST_BASE_URL")

	if want := []string{"INFER_BASE_URL=http://localhost:8000/v1"}; !slices.Equal(overrides, want) {
		t.Errorf("overrides = %q, want %q", overrides, want)
	}
	if got := os.Getenv("INFER_TOKEN"); got != tokenSecret {
		t.Errorf("INFER_TOKEN = %q, want AI_TEST_TOKEN's", got)
	}
	if got := os.Getenv("INFER_HEADER"); got != header {
		t.Errorf("INFER_HEADER = %q, want AI_TEST_HEADER's", got)
	}
	logged := rec.String()
	for _, want := range []string{"INFER_TOKEN set from AI_TEST_TOKEN", "INFER_HEADER set from AI_TEST_HEADER"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log = %q, want it to say %q", logged, want)
		}
	}
	for _, secret := range []string{tokenSecret, headerSecret} {
		if strings.Contains(logged, secret) || strings.Contains(strings.Join(overrides, " "), secret) {
			t.Errorf("log = %q, overrides = %q, want neither to show %q", logged, overrides, secret)
		}
	}
}

// With neither credential variable set, Endpoint sets neither of infer's,
// and logs nothing of them, so the program runs as it did before they
// existed.
func TestEndpoint_NoCredentials(t *testing.T) {
	unsetInfer(t)
	t.Setenv("AI_TEST_BASE_URL", "http://localhost:8000/v1")
	t.Setenv("AI_TEST_TOKEN", "")
	t.Setenv("AI_TEST_HEADER", "")
	rec := &logRecorder{TB: t}

	integration.Endpoint(rec, "AI_TEST_BASE_URL")

	for _, name := range []string{"INFER_TOKEN", "INFER_HEADER"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s = %q, want it unset", name, v)
		}
	}
	if logged := rec.String(); logged != "" {
		t.Errorf("log = %q, want nothing", logged)
	}
}
