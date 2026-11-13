package app_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/internal/app"
	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// completionsRoute is the route a chat request goes to under the default
// format, completions.
const completionsRoute = "chat/completions"

// chatEndpoint serves an endpoint that lists ids as its models and answers
// POST /v1/<route> with chat.
func chatEndpoint(t *testing.T, ids []string, route string, chat http.Handler) *apptest.Endpoint {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /v1/models", apptest.Models(ids...))
	mux.Handle("POST /v1/"+route, chat)
	return apptest.Serve(t, mux)
}

// chatBodies returns the body of each chat request to route e received,
// decoded as a JSON object, and fails t on any request but those and GET
// /v1/models.
func chatBodies(t *testing.T, e *apptest.Endpoint, route string) []map[string]any {
	t.Helper()
	var bodies []map[string]any
	for _, r := range e.Requests() {
		switch {
		case r.Method == http.MethodGet && r.Path == "/v1/models":
		case r.Method == http.MethodPost && r.Path == "/v1/"+route:
			var body map[string]any
			if err := json.Unmarshal(r.Body, &body); err != nil {
				t.Fatalf("chat request body %s: %v", r.Body, err)
			}
			bodies = append(bodies, body)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.Path)
		}
	}
	return bodies
}

// oneBody returns the one chat request to route e received, failing t
// unless there is exactly one.
func oneBody(t *testing.T, e *apptest.Endpoint, route string) map[string]any {
	t.Helper()
	bodies := chatBodies(t, e, route)
	if len(bodies) != 1 {
		t.Fatalf("sent %d chat requests, want 1", len(bodies))
	}
	return bodies[0]
}

// userContent returns the content of the user message in a completions
// request body: a string for a prompt alone, an array of parts with images.
func userContent(t *testing.T, body map[string]any) any {
	t.Helper()
	messages, _ := body["messages"].([]any)
	for _, m := range messages {
		if m, ok := m.(map[string]any); ok && m["role"] == "user" {
			return m["content"]
		}
	}
	t.Fatalf("request body %v has no user message", body)
	return nil
}

// usageJSON is a completions usage member: 78 prompt tokens, 25 completion
// tokens, reasoning of them, and 103 total, so a test tells each apart.
func usageJSON(reasoning int) string {
	return fmt.Sprintf(`{"prompt_tokens":78,"completion_tokens":25,"total_tokens":103,"completion_tokens_details":{"reasoning_tokens":%d}}`, reasoning)
}

// completionsReply is an unstreamed completions reply of content, with
// reasoning when it isn't empty, ending with finish, its usage usageJSON's
// with 12 reasoning tokens.
func completionsReply(content, reasoning, finish string) string {
	message := map[string]string{"role": "assistant", "content": content}
	if reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	m, err := json.Marshal(message)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":%s,"finish_reason":%q}],"usage":%s}`, m, finish, usageJSON(12))
}

// completionsChunks are the frames of a streamed completions reply: a
// reasoning delta when reasoning isn't empty, each of deltas as a content
// delta, in order, the finish chunk, the usage chunk, usageJSON's with 12
// reasoning tokens, and [DONE].
func completionsChunks(reasoning string, finish string, deltas ...string) []string {
	chunk := func(delta map[string]string, finish string) string {
		d, err := json.Marshal(delta)
		if err != nil {
			panic(err)
		}
		f := "null"
		if finish != "" {
			f = fmt.Sprintf("%q", finish)
		}
		return fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":%s,"finish_reason":%s}]}`, d, f)
	}
	frames := []string{chunk(map[string]string{"role": "assistant"}, "")}
	if reasoning != "" {
		frames = append(frames, chunk(map[string]string{"reasoning_content": reasoning}, ""))
	}
	for _, d := range deltas {
		frames = append(frames, chunk(map[string]string{"content": d}, ""))
	}
	return append(frames,
		chunk(map[string]string{}, finish),
		fmt.Sprintf(`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[],"usage":%s}`, usageJSON(12)),
		"[DONE]")
}

// completedStream is a streamed completions endpoint that lists ids and
// answers "Paris, Lyon" in three deltas, completed.
func completedStream(t *testing.T, ids ...string) *apptest.Endpoint {
	t.Helper()
	return chatEndpoint(t, ids, completionsRoute, apptest.Stream(completionsRoute, completionsChunks("", "stop", "Paris", ",", " Lyon")...))
}

// agent is the environment of a run against e with the model agent set.
func agent(e *apptest.Endpoint) map[string]string {
	return map[string]string{"INFER_BASE_URL": e.URL, "INFER_MODEL": "agent"}
}

// The prompt is the positional arguments joined by spaces, stdin then
// unread; with none it is stdin, less one final line ending.
func TestChat_Prompt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"arguments", []string{"chat", "name", "three  cities", "in France"}, "ignored\n", "name three  cities in France"},
		{"one argument", []string{"chat", "hello"}, "", "hello"},
		{"stdin", []string{"chat"}, "first line\nsecond line\n", "first line\nsecond line"},
		{"stdin with CRLF", []string{"chat"}, "first line\r\n", "first line"},
		{"stdin without a final newline", []string{"chat"}, "first line\n\n", "first line\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completedStream(t)

			r := newHarnessIn(t, agent(e), tc.stdin).run(tc.args...)

			wantCode(t, r, process.ExitOK)
			if got := userContent(t, oneBody(t, e, completionsRoute)); got != tc.want {
				t.Errorf("prompt = %q, want %q", got, tc.want)
			}
		})
	}
}

// No prompt at all, neither an argument nor stdin, is a usage error, exit
// 2, before any request, the model listing included.
func TestChat_NoPrompt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
	}{
		{"empty stdin", []string{"chat"}, ""},
		{"a newline on stdin", []string{"chat"}, "\n"},
		{"an empty argument", []string{"chat", ""}, "ignored"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completedStream(t, "agent")

			r := newHarnessIn(t, map[string]string{"INFER_BASE_URL": e.URL}, tc.stdin).run(tc.args...)

			wantCode(t, r, process.ExitUsage)
			if want := "infer chat: no prompt"; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			wantNoRequest(t, e)
		})
	}
}

// Each typed flag sets its member of the completions body, and --native's
// members are merged into its top level.
func TestChat_FlagsReachTheBody(t *testing.T) {
	e := completedStream(t)

	r := runApp(t, agent(e), "chat", "--instructions", "Answer briefly.", "--reasoning", "high",
		"--max-tokens", "64", "--native", `{"temperature":0.25,"top_k":5}`, "hello")

	wantCode(t, r, process.ExitOK)
	body := oneBody(t, e, completionsRoute)
	for member, want := range map[string]any{
		"model":                 "agent",
		"reasoning_effort":      "high",
		"max_completion_tokens": 64.0,
		"temperature":           0.25,
		"top_k":                 5.0,
	} {
		if body[member] != want {
			t.Errorf("body[%q] = %v, want %v", member, body[member], want)
		}
	}
	messages, _ := body["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v, want the instructions and the prompt", messages)
	}
	if first, _ := messages[0].(map[string]any); first["content"] != "Answer briefly." || first["role"] == "user" {
		t.Errorf("first message = %v, want the instructions", first)
	}
}

// With no typed flag given, the body carries none of their members.
func TestChat_UnsetFlagsSendNothing(t *testing.T) {
	e := completedStream(t)

	r := runApp(t, agent(e), "chat", "hello")

	wantCode(t, r, process.ExitOK)
	body := oneBody(t, e, completionsRoute)
	for _, member := range []string{"reasoning_effort", "max_completion_tokens", "temperature"} {
		if v, ok := body[member]; ok {
			t.Errorf("body[%q] = %v, want it absent", member, v)
		}
	}
	if messages, _ := body["messages"].([]any); len(messages) != 1 {
		t.Errorf("messages = %v, want the prompt alone", messages)
	}
}

// pngHeader is the start of every PNG file, which content sniffing reads as
// image/png.
var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")

// Each --image is a part after the prompt, in the order given: a file read
// and inlined as a data URL, its type from its extension or else sniffed
// from its content, and an http(s) URL as given.
func TestChat_Images(t *testing.T) {
	dir := t.TempDir()
	jpeg := filepath.Join(dir, "photo.jpg")
	sniffed := filepath.Join(dir, "scan")
	for path, data := range map[string][]byte{jpeg: []byte("not really a jpeg"), sniffed: pngHeader} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	e := completedStream(t)

	r := runApp(t, agent(e), "chat", "--image", jpeg, "--image", "https://example.test/cat.png",
		"describe these", "--image", sniffed, "--image", "HTTP://example.test/dog")

	wantCode(t, r, process.ExitOK)
	parts, _ := userContent(t, oneBody(t, e, completionsRoute)).([]any)
	var got []string
	for _, p := range parts {
		p, _ := p.(map[string]any)
		switch p["type"] {
		case "text":
			got = append(got, "text "+fmt.Sprint(p["text"]))
		case "image_url":
			img, _ := p["image_url"].(map[string]any)
			got = append(got, "image "+fmt.Sprint(img["url"]))
		default:
			got = append(got, fmt.Sprint(p))
		}
	}
	want := []string{
		"text describe these",
		"image data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("not really a jpeg")),
		"image https://example.test/cat.png",
		"image data:image/png;base64," + base64.StdEncoding.EncodeToString(pngHeader),
		"image HTTP://example.test/dog",
	}
	if !slices.Equal(got, want) {
		t.Errorf("user content = %q, want %q", got, want)
	}
}

// A file that isn't an image, by its extension or its content, is a usage
// error, and a file that can't be read an error, exit 1; neither sends.
func TestChat_ImageRefusals(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(notes, pngHeader, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		image string
		code  int
		want  string
	}{
		{"not an image", notes, process.ExitUsage, "infer chat: --image " + notes + ": not an image"},
		{"missing", filepath.Join(dir, "missing.png"), process.ExitFailure, "infer chat: --image: open "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completedStream(t, "agent")

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "chat", "--image", tc.image, "hello")

			wantCode(t, r, tc.code)
			if !strings.HasPrefix(r.stderr, tc.want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, tc.want)
			}
			wantNoRequest(t, e)
		})
	}
}

// A request go-ai refuses is a usage error, exit 2, carrying go-ai's
// reason, before any request, the model listing included: by its Validate,
// or by the format's encoding.
func TestChat_RefusedRequests(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"max tokens 1", []string{"--max-tokens", "1"}, "max output tokens 1 is below the minimum, 16"},
		{"max tokens 15", []string{"--max-tokens", "15"}, "max output tokens 15 is below the minimum, 16"},
		{"negative max tokens", []string{"--max-tokens", "-1"}, "max output tokens -1 is negative"},
		{"unknown effort", []string{"--reasoning", "extreme"}, `unknown reasoning effort "extreme"`},
		{"malformed native", []string{"--native", `{"temperature":`}, "native field isn't a JSON object"},
		{"array native", []string{"--native", `[1,2]`}, "native field isn't a JSON object"},
		{"string native", []string{"--native", `"x"`}, "native field isn't a JSON object"},
		{"repeated native member", []string{"--native", `{"top_k":1,"top_k":2}`}, "native field isn't a JSON object"},
		{"native clashing with a typed member", []string{"--native", `{"stream":false}`}, "native member stream clashes"},
		{"native clashing unstreamed", []string{"--stream=false", "--max-tokens", "20", "--native", `{"max_completion_tokens":5}`}, "native member max_completion_tokens clashes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completedStream(t, "agent")

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, append(append([]string{"chat"}, tc.args...), "hello")...)

			wantCode(t, r, process.ExitUsage)
			if first, _, _ := strings.Cut(r.stderr, "\n"); !strings.HasPrefix(first, "infer chat: ") || !strings.Contains(first, tc.want) || strings.Count(r.stderr, "infer chat: ") != 1 {
				t.Errorf("stderr = %q, want one infer chat error carrying %q", r.stderr, tc.want)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			wantNoRequest(t, e)
		})
	}
}

// Each effort go-ai defines is accepted, and 16, go-ai's minimum, is.
func TestChat_AcceptedValues(t *testing.T) {
	for _, effort := range []string{"none", "low", "medium", "high", "xhigh"} {
		t.Run(effort, func(t *testing.T) {
			e := completedStream(t)

			r := runApp(t, agent(e), "chat", "--reasoning", effort, "--max-tokens", "16", "hello")

			wantCode(t, r, process.ExitOK)
			body := oneBody(t, e, completionsRoute)
			if body["reasoning_effort"] != effort || body["max_completion_tokens"] != 16.0 {
				t.Errorf("body = %v, want reasoning_effort %s and max_completion_tokens 16", body, effort)
			}
		})
	}
}

// Streamed, the default, the request asks for a stream and each text
// delta is written in order, the text ended with a newline; the summary
// goes to stderr.
func TestChat_Streamed(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute,
		apptest.Stream(completionsRoute, completionsChunks("Three cities.", "stop", "Paris", ",", " Lyon", ", Marseille")...))

	r := runApp(t, agent(e), "chat", "name three")

	wantCode(t, r, process.ExitOK)
	if want := "Paris, Lyon, Marseille\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if body := oneBody(t, e, completionsRoute); body["stream"] != true {
		t.Errorf("body stream = %v, want true", body["stream"])
	}
	want := "status: completed\nusage: 78 input tokens, 25 output tokens, 12 reasoning tokens, 103 total tokens\nreasoning items: 1\n"
	if r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

// liveOut is a stdout that reports each write as it happens and counts its
// flushes, for a test that watches the text arrive while the stream is
// open. It is safe for concurrent use.
type liveOut struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	flushes int
	wrote   chan string // each write's text, as it is written
}

func (o *liveOut) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf.Write(p)
	o.wrote <- string(p)
	return len(p), nil
}

func (o *liveOut) Flush() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.flushes++
	return nil
}

// Each delta reaches stdout, flushed, while the stream is still open: the
// endpoint sends the next delta only once stdout has the one before it.
func TestChat_StreamWritesAsItArrives(t *testing.T) {
	deltas := []string{"one", " two", " three"}
	frames := completionsChunks("", "stop", deltas...)
	out := &liveOut{wrote: make(chan string, 16)}
	e := chatEndpoint(t, nil, completionsRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		next := 0
		for _, f := range frames {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", f)
			_ = rc.Flush()
			if next < len(deltas) && strings.Contains(f, `"content":`) {
				select {
				case got := <-out.wrote:
					if got != deltas[next] {
						t.Errorf("stdout write %d = %q, want %q", next, got, deltas[next])
					}
				case <-time.After(5 * time.Second):
					t.Errorf("delta %q not written to stdout while the stream is open", deltas[next])
					return
				}
				next++
			}
		}
	}))
	apptest.Env(t, agent(e))
	a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: out, Stderr: new(bytes.Buffer)})

	code := a.Run(context.Background(), []string{"chat", "count"})

	if code != process.ExitOK {
		t.Errorf("code = %d, want %d", code, process.ExitOK)
	}
	if got, want := out.buf.String(), "one two three\n"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if out.flushes != len(deltas)+1 {
		t.Errorf("stdout flushed %d times, want once per delta and once for the newline, %d", out.flushes, len(deltas)+1)
	}
}

// --stream=false sends a request that doesn't ask for a stream and writes
// the reply's text, ended with a newline, and the same summary.
func TestChat_Unstreamed(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute,
		apptest.Reply(completionsRoute, completionsReply("Paris, Lyon, Marseille", "Three cities.", "stop")))

	r := runApp(t, agent(e), "chat", "--stream=false", "name three")

	wantCode(t, r, process.ExitOK)
	if want := "Paris, Lyon, Marseille\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	body := oneBody(t, e, completionsRoute)
	if body["stream"] != false {
		t.Errorf("body stream = %v, want false", body["stream"])
	}
	if _, ok := body["stream_options"]; ok {
		t.Errorf("body stream_options = %v, want it absent", body["stream_options"])
	}
	want := "status: completed\nusage: 78 input tokens, 25 output tokens, 12 reasoning tokens, 103 total tokens\nreasoning items: 1\n"
	if r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

// A reply with no reasoning counts no reasoning items, whatever reasoning
// tokens the endpoint reports, so the count alone proves no reasoning.
func TestChat_NoReasoningItems(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		chat http.Handler
	}{
		{"streamed", nil, apptest.Stream(completionsRoute, completionsChunks("", "stop", "Paris")...)},
		{"unstreamed", []string{"--stream=false"}, apptest.Reply(completionsRoute, completionsReply("Paris", "", "stop"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := chatEndpoint(t, nil, completionsRoute, tc.chat)

			r := runApp(t, agent(e), append(append([]string{"chat", "--reasoning", "none"}, tc.args...), "capital?")...)

			wantCode(t, r, process.ExitOK)
			if !strings.HasSuffix(r.stderr, "\nreasoning items: 0\n") {
				t.Errorf("stderr = %q, want it to end with reasoning items: 0", r.stderr)
			}
		})
	}
}

// A text that ends with a newline is ended by it; an empty one is an empty
// line.
func TestChat_EndsTheTextWithOneNewline(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"ending in a newline", "done\n", "done\n"},
		{"empty", "", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := chatEndpoint(t, nil, completionsRoute, apptest.Reply(completionsRoute, completionsReply(tc.text, "", "stop")))

			r := runApp(t, agent(e), "chat", "--stream=false", "hello")

			wantCode(t, r, process.ExitOK)
			if r.stdout != tc.want {
				t.Errorf("stdout = %q, want %q", r.stdout, tc.want)
			}
		})
	}
}

// A reply cut off at the token bound keeps the text written, ended with a
// newline, writes its summary, and exits 1 naming its status and reason.
func TestChat_Incomplete(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		chat http.Handler
	}{
		{"streamed", nil, apptest.Stream(completionsRoute, completionsChunks("", "length", "1,", " 2,")...)},
		{"unstreamed", []string{"--stream=false"}, apptest.Reply(completionsRoute, completionsReply("1, 2,", "", "length"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := chatEndpoint(t, nil, completionsRoute, tc.chat)

			r := runApp(t, agent(e), append(append([]string{"chat", "--max-tokens", "16"}, tc.args...), "count")...)

			wantCode(t, r, process.ExitFailure)
			if want := "1, 2,\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
			want := "status: incomplete\nusage: 78 input tokens, 25 output tokens, 12 reasoning tokens, 103 total tokens\nreasoning items: 0\n" +
				"infer chat: incomplete: max_output_tokens\n"
			if r.stderr != want {
				t.Errorf("stderr = %q, want %q", r.stderr, want)
			}
		})
	}
}

// A stream the endpoint fails part way keeps the text written and exits 1
// with the endpoint's error.
func TestChat_Failed(t *testing.T) {
	frames := completionsChunks("", "stop", "Par")[:2]
	frames = append(frames, `{"error":{"message":"engine crashed","type":"server_error","code":"500"}}`)
	e := chatEndpoint(t, nil, completionsRoute, apptest.Stream(completionsRoute, frames...))

	r := runApp(t, agent(e), "chat", "capital?")

	wantCode(t, r, process.ExitFailure)
	if want := "Par\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if !strings.HasPrefix(r.stderr, "status: failed\n") || !strings.HasSuffix(r.stderr, "\ninfer chat: failed: engine crashed (500)\n") {
		t.Errorf("stderr = %q, want the summary and then the failure", r.stderr)
	}
}

// An endpoint's refusal exits 1 with its error, and nothing on stdout.
func TestChat_EndpointRefuses(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad prompt","type":"invalid_request_error"}}`))
	}))

	r := runApp(t, agent(e), "chat", "hello")

	wantCode(t, r, process.ExitFailure)
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if !strings.HasPrefix(r.stderr, "infer chat: ") || !strings.Contains(r.stderr, "bad prompt") {
		t.Errorf("stderr = %q, want the endpoint's error", r.stderr)
	}
}

// With no model set, chat lists the endpoint's models first and sends to
// the one listed.
func TestChat_FallsBackToTheOneModel(t *testing.T) {
	e := completedStream(t, "agent")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "chat", "hello")

	wantCode(t, r, process.ExitOK)
	reqs := e.Requests()
	if len(reqs) != 2 || reqs[0].Path != "/v1/models" {
		t.Fatalf("requests = %d, want the model list and then the chat", len(reqs))
	}
	if body := oneBody(t, e, completionsRoute); body["model"] != "agent" {
		t.Errorf("body model = %v, want agent", body["model"])
	}
}

// Several models listed leave the model open: exit 1, no chat sent.
func TestChat_FallbackRefusal(t *testing.T) {
	e := completedStream(t, "agent", "vision")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "chat", "hello")

	wantCode(t, r, process.ExitFailure)
	if want := "infer chat: several models: agent, vision; pass --model\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if bodies := chatBodies(t, e, completionsRoute); len(bodies) != 0 {
		t.Errorf("sent %d chat requests, want none", len(bodies))
	}
}

// chat's help names each of its flags, --native's in the brief's words,
// and builds and sends nothing.
func TestChat_Help(t *testing.T) {
	e := completedStream(t, "agent")

	r := runApp(t, unreadable(e.URL), "chat", "--help")

	wantCode(t, r, process.ExitUsage)
	for _, want := range []string{"--instructions", "--image", "--reasoning", "--max-tokens", "--stream", "--idle-timeout", "INFER_IDLE_TIMEOUT",
		"JSON object merged into the request's top level, for parameters the typed flags don't cover"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout = %q, want chat's help naming %s", r.stdout, want)
		}
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}

// Through messages, --max-tokens and --reasoning reach the wire as
// max_tokens and thinking with output_config.effort, and the stream's text
// deltas are written in order.
func TestChat_Messages(t *testing.T) {
	frames := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"stop_reason":null,"usage":{"input_tokens":31,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Paris"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", Lyon"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":8}}`,
		`{"type":"message_stop"}`,
	}
	e := chatEndpoint(t, nil, "messages", apptest.Stream("messages", frames...))
	env := agent(e)
	env["INFER_FORMAT"] = "messages"

	r := runApp(t, env, "chat", "--max-tokens", "32", "--reasoning", "low", "--instructions", "Be brief.", "name two")

	wantCode(t, r, process.ExitOK)
	if want := "Paris, Lyon\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	body := oneBody(t, e, "messages")
	thinking, _ := body["thinking"].(map[string]any)
	config, _ := body["output_config"].(map[string]any)
	if body["max_tokens"] != 32.0 || thinking["type"] != "adaptive" || config["effort"] != "low" || body["system"] != "Be brief." || body["stream"] != true {
		t.Errorf("body = %v, want max_tokens 32, adaptive thinking of effort low, the system prompt, and stream", body)
	}
	if want := "status: completed\nusage: 31 input tokens, 8 output tokens, 0 reasoning tokens, 39 total tokens\nreasoning items: 0\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

// Messages requires max_tokens, so a messages chat with no --max-tokens is
// the format's refusal, exit 2, before any request.
func TestChat_MessagesWithoutMaxTokens(t *testing.T) {
	e := chatEndpoint(t, []string{"agent"}, "messages", apptest.Stream("messages"))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_FORMAT": "messages"}, "chat", "hello")

	wantCode(t, r, process.ExitUsage)
	if want := "infer chat: messages: request has no max output tokens"; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	wantNoRequest(t, e)
}

// Through responses, --max-tokens and --reasoning reach the wire as
// max_output_tokens and reasoning.effort, and an unstreamed reply's
// reasoning item and reasoning tokens are counted in the summary.
func TestChat_Responses(t *testing.T) {
	reply := `{"id":"resp_1","object":"response","status":"completed","output":[` +
		`{"id":"rs_1","type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"Think."}],"encrypted_content":"","status":"completed"},` +
		`{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","annotations":[],"text":"Paris"}]}],` +
		`"usage":{"input_tokens":91,"output_tokens":80,"total_tokens":171,"output_tokens_details":{"reasoning_tokens":70}}}`
	e := chatEndpoint(t, nil, "responses", apptest.Reply("responses", reply))
	env := agent(e)
	env["INFER_FORMAT"] = "responses"

	r := runApp(t, env, "chat", "--stream=false", "--max-tokens", "100", "--reasoning", "medium", "capital?")

	wantCode(t, r, process.ExitOK)
	if want := "Paris\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	body := oneBody(t, e, "responses")
	reasoning, _ := body["reasoning"].(map[string]any)
	if body["max_output_tokens"] != 100.0 || reasoning["effort"] != "medium" || body["stream"] != false {
		t.Errorf("body = %v, want max_output_tokens 100, reasoning effort medium, and no stream", body)
	}
	if want := "status: completed\nusage: 91 input tokens, 80 output tokens, 70 reasoning tokens, 171 total tokens\nreasoning items: 1\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

// A stream that breaks before its terminal event keeps the text written,
// its line ended, and exits 1 with the break, writing no summary.
func TestChat_StreamBreaks(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute, apptest.Stream(completionsRoute, completionsChunks("", "stop", "Par")[:2]...))

	r := runApp(t, agent(e), "chat", "capital?")

	wantCode(t, r, process.ExitFailure)
	if want := "Par\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if !strings.HasPrefix(r.stderr, "infer chat: ") || !strings.Contains(r.stderr, "unexpected EOF") || strings.Contains(r.stderr, "status:") {
		t.Errorf("stderr = %q, want the break alone", r.stderr)
	}
}
