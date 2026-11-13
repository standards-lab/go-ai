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

// Embeddings answers POST /v1/embeddings with each vector, as base64 of
// little-endian float32s at its index, and the usage; any other request
// is 404.
func TestEmbeddings_Reply(t *testing.T) {
	e := apptest.Serve(t, apptest.Embeddings(3, 4, []float32{1, -2}, []float32{}))

	post, err := http.Post(e.URL+"/embeddings", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = post.Body.Close() }()
	body, err := io.ReadAll(post.Body)
	if err != nil {
		t.Fatal(err)
	}
	other, err := http.Get(e.URL + "/embeddings")
	if err != nil {
		t.Fatal(err)
	}
	_ = other.Body.Close()

	// 1 is 0x3f800000 and -2 is 0xc0000000, each little-endian.
	want := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":"AACAPwAAAMA="},` +
		`{"object":"embedding","index":1,"embedding":""}],"usage":{"prompt_tokens":3,"total_tokens":4}}`
	if string(body) != want {
		t.Errorf("reply = %s, want %s", body, want)
	}
	if other.StatusCode != http.StatusNotFound {
		t.Errorf("GET /v1/embeddings status = %d, want %d", other.StatusCode, http.StatusNotFound)
	}
}

// post POSTs an empty JSON object to url and returns the reply's status,
// Content-Type, and body.
func post(t *testing.T, url string) (status int, contentType, body string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(b)
}

// Reply answers POST /v1/<route> with its body as JSON; any other request
// is 404.
func TestReply_AnswersItsRoute(t *testing.T) {
	e := apptest.Serve(t, apptest.Reply("chat/completions", `{"id":"x"}`))

	status, contentType, body := post(t, e.URL+"/chat/completions")
	other, _, _ := post(t, e.URL+"/responses")
	got, err := http.Get(e.URL + "/chat/completions")
	if err != nil {
		t.Fatal(err)
	}
	_ = got.Body.Close()

	if status != http.StatusOK || contentType != "application/json" || body != `{"id":"x"}` {
		t.Errorf("reply = %d %s %s, want 200 application/json {\"id\":\"x\"}", status, contentType, body)
	}
	if other != http.StatusNotFound || got.StatusCode != http.StatusNotFound {
		t.Errorf("POST /v1/responses = %d and GET /v1/chat/completions = %d, want 404 each", other, got.StatusCode)
	}
}

// Stream answers POST /v1/<route> with each frame as one event's data, in
// order; any other request is 404.
func TestStream_WritesEachFrame(t *testing.T) {
	e := apptest.Serve(t, apptest.Stream("messages", `{"a":1}`, `[DONE]`))

	status, contentType, body := post(t, e.URL+"/messages")
	other, _, _ := post(t, e.URL+"/chat/completions")

	if want := "data: {\"a\":1}\n\ndata: [DONE]\n\n"; status != http.StatusOK || contentType != "text/event-stream" || body != want {
		t.Errorf("reply = %d %s %q, want 200 text/event-stream %q", status, contentType, body, want)
	}
	if other != http.StatusNotFound {
		t.Errorf("POST /v1/chat/completions = %d, want 404", other)
	}
}
