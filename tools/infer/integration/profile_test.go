//go:build integration

package integration_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestDeclaredSkip(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusTeapot)
	}))
	defer srv.Close()
	const loopback role = "AI_LOOPBACK_BASE_URL"
	t.Setenv(string(loopback), srv.URL+"/v1")
	t.Setenv("AI_LOOPBACK_MODEL", "loopback")
	t.Setenv("AI_LOOPBACK_PROFILE", `{"responses":{"refused_efforts":["none"]}}`)

	var scenario *testing.T
	t.Run("chat", func(t *testing.T) {
		scenario = t
		res := runOn(t, loopback, []string{"INFER_FORMAT=responses"}, "chat", "--reasoning", "none", "--max-tokens", answerBound, "hello")
		t.Fatalf("runOn returned %+v, want the scenario skipped", res)
	})

	if !scenario.Skipped() {
		t.Error("the scenario wasn't skipped")
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("the endpoint got %d requests, want none", n)
	}
}
