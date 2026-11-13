package apptest_test

import (
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/graph"

	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

func TestRecorder_LogsInOrder(t *testing.T) {
	var r apptest.Recorder[string]
	if log := r.Log(); len(log) != 0 {
		t.Errorf("zero Recorder Log() = %q, want empty", log)
	}

	r.Record("a")
	r.Record("b")
	r.Record("a")

	if want := []string{"a", "b", "a"}; !slices.Equal(r.Log(), want) {
		t.Errorf("Log() = %q, want %q", r.Log(), want)
	}
}

func TestRecorder_LogIsACopy(t *testing.T) {
	var r apptest.Recorder[string]
	r.Record("a")

	log := r.Log()
	log[0] = "changed"
	r.Record("b")

	if want := []string{"a", "b"}; !slices.Equal(r.Log(), want) {
		t.Errorf("Log() = %q, want %q", r.Log(), want)
	}
	if log[0] != "changed" || len(log) != 1 {
		t.Errorf("earlier Log() = %q, want it unchanged by later Records", log)
	}
}

// Run under -race, concurrent Records prove the Recorder's lock.
func TestRecorder_ConcurrentRecords(t *testing.T) {
	var r apptest.Recorder[string]
	var wg sync.WaitGroup
	const n = 50
	for i := range n {
		wg.Go(func() { r.Record(strconv.Itoa(i)) })
	}
	wg.Wait()

	if got := len(r.Log()); got != n {
		t.Errorf("len(Log()) = %d, want %d", got, n)
	}
}

// Names reads each handle's name, in order.
func TestNames_ReadsEachHandle(t *testing.T) {
	g := graph.New()
	refs := []graph.Ref{g.Value("no color", false), g.Value("lifecycle config", 0)}

	if got, want := apptest.Names(refs), []string{"no color", "lifecycle config"}; !slices.Equal(got, want) {
		t.Errorf("Names() = %q, want %q", got, want)
	}
	if got := apptest.Names(nil); len(got) != 0 {
		t.Errorf("Names(nil) = %q, want empty", got)
	}
}

// Env empties an inherited INFER_ variable, sets the test's own, and
// leaves any other variable alone.
func TestEnv_ClearsInheritedAndSetsGiven(t *testing.T) {
	t.Setenv("INFER_INHERITED", "inherited")
	t.Setenv("APPTEST_OTHER", "other")

	apptest.Env(t, map[string]string{"INFER_BASE_URL": "http://example.test/v1"})

	if got := os.Getenv("INFER_INHERITED"); got != "" {
		t.Errorf("INFER_INHERITED = %q, want empty", got)
	}
	if got := os.Getenv("INFER_BASE_URL"); got != "http://example.test/v1" {
		t.Errorf("INFER_BASE_URL = %q, want the given value", got)
	}
	if got := os.Getenv("APPTEST_OTHER"); got != "other" {
		t.Errorf("APPTEST_OTHER = %q, want it untouched", got)
	}
}

// An Endpoint records each request, its body included, and answers with the
// handler; its URL is the server's API root.
func TestServe_RecordsEachRequest(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent", "vision"))
	if !strings.HasSuffix(e.URL, "/v1") {
		t.Fatalf("URL = %q, want it to end in /v1", e.URL)
	}

	reply := get(t, e.URL+"/models", "Bearer t")
	post, err := http.Post(e.URL+"/other", "text/plain", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	_ = post.Body.Close()

	if want := `{"object":"list","data":[{"id":"agent","object":"model"},{"id":"vision","object":"model"}]}`; reply != want {
		t.Errorf("models reply = %s, want %s", reply, want)
	}
	if post.StatusCode != http.StatusNotFound {
		t.Errorf("POST /v1/other status = %d, want %d", post.StatusCode, http.StatusNotFound)
	}
	reqs := e.Requests()
	if len(reqs) != 2 {
		t.Fatalf("Requests() = %d requests, want 2", len(reqs))
	}
	if r := reqs[0]; r.Method != http.MethodGet || r.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("first request = %s %s, Authorization %q; want GET /v1/models, Bearer t", r.Method, r.Path, r.Header.Get("Authorization"))
	}
	if r := reqs[1]; r.Method != http.MethodPost || r.Path != "/v1/other" || string(r.Body) != "body" {
		t.Errorf("second request = %s %s %q; want POST /v1/other \"body\"", r.Method, r.Path, r.Body)
	}
}

// Models with no ids lists none, as an empty array.
func TestModels_Empty(t *testing.T) {
	e := apptest.Serve(t, apptest.Models())

	if got, want := get(t, e.URL+"/models", ""), `{"object":"list","data":[]}`; got != want {
		t.Errorf("reply = %s, want %s", got, want)
	}
}

// get GETs url with the Authorization header auth, when it isn't empty, and
// returns the reply's body.
func get(t *testing.T, url, auth string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
