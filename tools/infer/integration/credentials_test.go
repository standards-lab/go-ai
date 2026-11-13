//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/integration"
)

// TestCredentials runs the built program against a loopback endpoint, as
// a role whose AI_<ROLE>_TOKEN and AI_<ROLE>_HEADER are set: the token and
// header reach the endpoint through INFER_TOKEN and INFER_HEADER, and the
// run's log, the shell line that replays it included, shows neither value.
// It needs no live endpoint, so it never skips.
func TestCredentials(t *testing.T) {
	var mu sync.Mutex
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Clone()
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"loopback","object":"model"}]}`)
	}))
	defer srv.Close()
	t.Setenv("AI_LOOPBACK_BASE_URL", srv.URL+"/v1")
	t.Setenv("AI_LOOPBACK_TOKEN", tokenSecret)
	t.Setenv("AI_LOOPBACK_HEADER", "api-key: "+headerSecret)
	rec := &logRecorder{TB: t}

	res := integration.RunEnv(rec, integration.Endpoint(rec, "AI_LOOPBACK_BASE_URL"), "models")

	wantCode(t, res, process.ExitOK)
	mu.Lock()
	defer mu.Unlock()
	if a := got.Get("Authorization"); a != "Bearer "+tokenSecret {
		t.Errorf("Authorization = %q, want AI_LOOPBACK_TOKEN's as the bearer token", a)
	}
	if k := got.Get("Api-Key"); k != headerSecret {
		t.Errorf("api-key = %q, want AI_LOOPBACK_HEADER's", k)
	}
	logged := rec.String()
	if !strings.Contains(logged, "INFER_BASE_URL="+srv.URL+"/v1") {
		t.Errorf("log = %q, want the run's shell line with its base URL", logged)
	}
	for _, secret := range []string{tokenSecret, headerSecret} {
		if strings.Contains(logged, secret) {
			t.Errorf("log = %q, want it never to show %q", logged, secret)
		}
	}
}
