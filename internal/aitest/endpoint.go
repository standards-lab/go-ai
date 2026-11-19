package aitest

import (
	"testing"

	"github.com/standards-lab/go-ai/aitest"
)

// Only returns the one request e received, failing t unless it received exactly one.
func Only(t testing.TB, e *aitest.Endpoint) aitest.Request {
	t.Helper()
	reqs := e.Requests()
	if len(reqs) != 1 {
		t.Fatalf("endpoint received %d requests, want 1", len(reqs))
	}
	return reqs[0]
}
