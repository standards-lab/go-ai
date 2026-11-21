package app_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/tools/infer/internal/app"
	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

const completionsRoute = "chat/completions"

func chatEndpoint(t *testing.T, ids []string, route string, reply aitest.Reply) *aitest.Endpoint {
	t.Helper()
	routes := aitest.Routes{Models: listing(ids...)}
	if route == completionsRoute {
		routes.ChatCompletions = always[aitest.ChatRequest](reply)
	} else {
		routes.Raw = map[string]func(context.Context, aitest.RawRequest) aitest.Reply{route: always[aitest.RawRequest](reply)}
	}
	return aitest.Serve(t, routes)
}

func chatBodies(t *testing.T, e *aitest.Endpoint, route string) []map[string]any {
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

func oneBody(t *testing.T, e *aitest.Endpoint, route string) map[string]any {
	t.Helper()
	bodies := chatBodies(t, e, route)
	if len(bodies) != 1 {
		t.Fatalf("sent %d chat requests, want 1", len(bodies))
	}
	return bodies[0]
}

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

func chatReply(reasoning, finish string, text ...string) aitest.ChatReply {
	return aitest.ChatReply{
		Chunks:           text,
		Reasoning:        reasoning,
		Finish:           finish,
		PromptTokens:     78,
		CompletionTokens: 25,
		TotalTokens:      103,
		ReasoningTokens:  12,
	}
}

func frames(data ...string) []aitest.Frame {
	fs := make([]aitest.Frame, len(data))
	for i, d := range data {
		fs[i] = aitest.Frame{Data: d}
	}
	return fs
}

func completedStream(t *testing.T, ids ...string) *aitest.Endpoint {
	t.Helper()
	return chatEndpoint(t, ids, completionsRoute, chatReply("", "stop", "Paris", ",", " Lyon"))
}

func agent(e *aitest.Endpoint) map[string]string {
	return map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "agent"}
}

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
		{"stdin ending in a blank line", []string{"chat"}, "first line\n\n", "first line\n"},
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

			r := newHarnessIn(t, map[string]string{"INFER_BASE_URL": e.URL()}, tc.stdin).run(tc.args...)

			wantCode(t, r, process.ExitUsage)
			if want := "infer chat: no prompt"; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			wantNoRequest(t, e)
		})
	}
}

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

var pngHeader = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")

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

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "chat", "--image", tc.image, "hello")

			wantCode(t, r, tc.code)
			if !strings.HasPrefix(r.stderr, tc.want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, tc.want)
			}
			wantNoRequest(t, e)
		})
	}
}

func TestChat_RefusedRequests(t *testing.T) {
	const (
		nativeObject = "protocol: request's native field isn't a JSON object with unique member names"
		noMaxTokens  = "messages: request has no max output tokens, which Messages requires as max_tokens"
	)
	for _, tc := range []struct {
		name   string
		format string
		args   []string
		want   string
	}{
		{"responses max tokens 1", "responses", []string{"--max-tokens", "1"}, "--max-tokens: responses: request's max output tokens 1 is below the minimum, 16"},
		{"responses max tokens 15", "responses", []string{"--max-tokens", "15"}, "--max-tokens: responses: request's max output tokens 15 is below the minimum, 16"},
		{"negative max tokens", "", []string{"--max-tokens", "-1"}, "--max-tokens: protocol: request's max output tokens -1 is negative"},
		{"unknown effort", "", []string{"--reasoning", "extreme"}, `--reasoning: protocol: request has unknown reasoning effort "extreme"`},
		{"malformed native", "", []string{"--native", `{"temperature":`}, "--native: " + nativeObject},
		{"array native", "", []string{"--native", `[1,2]`}, "--native: " + nativeObject},
		{"string native", "", []string{"--native", `"x"`}, "--native: " + nativeObject},
		{"repeated native member", "", []string{"--native", `{"top_k":1,"top_k":2}`}, "--native: " + nativeObject},
		{"native clashing with a typed member", "", []string{"--native", `{"stream":false}`}, "--native: completions: native member stream clashes with a member the request sets"},
		{"native clashing unstreamed", "", []string{"--stream=false", "--max-tokens", "20", "--native", `{"max_completion_tokens":5}`}, "--native: completions: native member max_completion_tokens clashes with a member the request sets"},
		{"messages without max tokens", "messages", nil, "--format messages needs --max-tokens: " + noMaxTokens},
		{"two flags refused", "responses", []string{"--max-tokens", "5", "--native", `[1]`}, "--native: " + nativeObject},
		{"native clashing with --reasoning", "", []string{"--reasoning", "low", "--native", `{"reasoning_effort":"low"}`}, "--native: completions: native member reasoning_effort clashes with a member the request sets"},
		{"responses max tokens 5 with an effort", "responses", []string{"--reasoning", "low", "--max-tokens", "5"}, "--max-tokens: responses: request's max output tokens 5 is below the minimum, 16"},
		{"spec effort through messages", "messages", []string{"--max-tokens", "64", "--reasoning", "minimal"}, "--reasoning: messages: reasoning effort minimal has no Messages form"},
		{"native clash through messages without max tokens", "messages", []string{"--native", `{"stream":false}`}, "--format messages needs --max-tokens: " + noMaxTokens},
		{"array native through messages without max tokens", "messages", []string{"--native", `[1]`}, "--native: " + nativeObject},
		{"effort and native refused", "", []string{"--reasoning", "extreme", "--native", `[1]`}, `--reasoning: protocol: request has unknown reasoning effort "extreme"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completedStream(t, "agent")
			env := map[string]string{"INFER_BASE_URL": e.URL()}
			if tc.format != "" {
				env["INFER_FORMAT"] = tc.format
			}

			r := runApp(t, env, append(append([]string{"chat"}, tc.args...), "hello")...)

			wantCode(t, r, process.ExitUsage)
			if first, _, _ := strings.Cut(r.stderr, "\n"); first != "infer chat: "+tc.want || strings.Count(r.stderr, "infer chat: ") != 1 {
				t.Errorf("stderr = %q, want one error, %q", r.stderr, "infer chat: "+tc.want)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			wantNoRequest(t, e)
		})
	}
}

const refusesNone = `{"responses":{"refused_efforts":["none"]}}`

const declaredSuffix = "; declared by --profile (INFER_PROFILE)"

func TestChat_DeclaredRefusals(t *testing.T) {
	const declares = "is refused by the deployment, as its profile declares"
	for _, tc := range []struct {
		name    string
		format  string
		profile string
		args    []string
		want    string
	}{
		{"responses, from the flag", "responses", "", []string{"--profile", refusesNone, "--reasoning", "none"}, "--reasoning: responses: reasoning effort none " + declares},
		{"responses, from the variable", "responses", refusesNone, []string{"--reasoning", "none"}, "--reasoning: responses: reasoning effort none " + declares},
		{"completions", "", `{"completions":{"refused_efforts":["low"]}}`, []string{"--reasoning", "low"}, "--reasoning: completions: reasoning effort low " + declares},
		{"messages", "messages", `{"messages":{"refused_efforts":["high"]}}`, []string{"--reasoning", "high", "--max-tokens", "64"}, "--reasoning: messages: reasoning effort high " + declares},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completedStream(t, "agent")
			env := map[string]string{"INFER_BASE_URL": e.URL()}
			if tc.format != "" {
				env["INFER_FORMAT"] = tc.format
			}
			if tc.profile != "" {
				env["INFER_PROFILE"] = tc.profile
			}

			r := runApp(t, env, append(append([]string{"chat"}, tc.args...), "hello")...)

			wantCode(t, r, process.ExitUsage)
			want := "infer chat: " + tc.want + declaredSuffix
			if first, _, _ := strings.Cut(r.stderr, "\n"); first != want || strings.Count(r.stderr, "infer chat: ") != 1 {
				t.Errorf("stderr = %q, want one error, %q", r.stderr, want)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			wantNoRequest(t, e)
		})
	}
}

func TestChat_AcceptedValues(t *testing.T) {
	for _, effort := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
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

func TestChat_SmallMaxTokens(t *testing.T) {
	messagesStream := aitest.RawReply{Frames: frames(
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"stop_reason":null,"usage":{"input_tokens":3,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":1}}`,
		`{"type":"message_stop"}`)}
	for _, tc := range []struct {
		format string
		route  string
		member string
		reply  aitest.Reply
	}{
		{"completions", completionsRoute, "max_completion_tokens", chatReply("Hi", "stop", "Hi")},
		{"messages", "messages", "max_tokens", messagesStream},
	} {
		for _, maxTokens := range []int{1, 15} {
			t.Run(fmt.Sprintf("%s %d", tc.format, maxTokens), func(t *testing.T) {
				e := chatEndpoint(t, nil, tc.route, tc.reply)
				env := agent(e)
				env["INFER_FORMAT"] = tc.format

				r := runApp(t, env, "chat", "--max-tokens", strconv.Itoa(maxTokens), "hello")

				wantCode(t, r, process.ExitOK)
				if body := oneBody(t, e, tc.route); body[tc.member] != float64(maxTokens) {
					t.Errorf("body = %v, want %s %d", body, tc.member, maxTokens)
				}
			})
		}
	}
}

func TestChat_Streamed(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute,
		chatReply("Three cities.", "stop", "Paris", ",", " Lyon", ", Marseille"))

	r := runApp(t, agent(e), "chat", "name three")

	wantCode(t, r, process.ExitOK)
	if want := "Paris, Lyon, Marseille\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if body := oneBody(t, e, completionsRoute); body["stream"] != true {
		t.Errorf("body stream = %v, want true", body["stream"])
	}
	want := "status: completed\nusage: 78 input tokens, 25 output tokens (12 reasoning), 103 total tokens\nreasoning items: 1\n"
	if r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

type liveOut struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	wrote chan string
}

func (o *liveOut) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf.Write(p)
	o.wrote <- string(p)
	return len(p), nil
}

func TestChat_StreamWritesAsItArrives(t *testing.T) {
	deltas := []string{"one", " two", " three"}
	for held := 1; held <= len(deltas); held++ {
		t.Run(fmt.Sprintf("held after %d", held), func(t *testing.T) {
			hold := &aitest.Gate{}
			reply := chatReply("", "stop", deltas...)
			reply.Hold, reply.HoldAfter = hold, held
			e := chatEndpoint(t, nil, completionsRoute, reply)
			out := &liveOut{wrote: make(chan string, 16)}
			apptest.Env(t, agent(e))
			a := app.New(cli.Streams{Stdin: strings.NewReader(""), Stdout: bufio.NewWriter(out), Stderr: new(bytes.Buffer)})

			done := make(chan int, 1)
			go func() { done <- a.Run(context.Background(), []string{"chat", "count"}) }()
			hold.Await(t, 1)
			for i, want := range deltas[:held] {
				select {
				case got := <-out.wrote:
					if got != want {
						t.Errorf("stdout write %d = %q, want %q", i, got, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatalf("delta %q not written to stdout while the stream is open", want)
				}
			}
			hold.Open()
			code := finish(t, done)

			if code != process.ExitOK {
				t.Errorf("code = %d, want %d", code, process.ExitOK)
			}
			out.mu.Lock()
			defer out.mu.Unlock()
			if got, want := out.buf.String(), "one two three\n"; got != want {
				t.Errorf("stdout = %q, want %q, the closing newline flushed too", got, want)
			}
		})
	}
}

func TestChat_Unstreamed(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute,
		chatReply("Three cities.", "stop", "Paris, Lyon, Marseille"))

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
	want := "status: completed\nusage: 78 input tokens, 25 output tokens (12 reasoning), 103 total tokens\nreasoning items: 1\n"
	if r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

func TestChat_NoReasoningItems(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		chat aitest.Reply
	}{
		{"streamed", nil, chatReply("", "stop", "Paris")},
		{"unstreamed", []string{"--stream=false"}, chatReply("", "stop", "Paris")},
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

func TestChat_EndsTheTextWithOneNewline(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
	}{
		{"ending in a newline", "done\n", "done\n"},
		{"empty", "", "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := chatEndpoint(t, nil, completionsRoute, chatReply("", "stop", tc.text))

			r := runApp(t, agent(e), "chat", "--stream=false", "hello")

			wantCode(t, r, process.ExitOK)
			if r.stdout != tc.want {
				t.Errorf("stdout = %q, want %q", r.stdout, tc.want)
			}
		})
	}
}

func TestChat_Incomplete(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		chat aitest.Reply
	}{
		{"streamed", nil, chatReply("", "length", "1,", " 2,")},
		{"unstreamed", []string{"--stream=false"}, chatReply("", "length", "1, 2,")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := chatEndpoint(t, nil, completionsRoute, tc.chat)

			r := runApp(t, agent(e), append(append([]string{"chat", "--max-tokens", "16"}, tc.args...), "count")...)

			wantCode(t, r, process.ExitFailure)
			if want := "1, 2,\n"; r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
			want := "status: incomplete\nusage: 78 input tokens, 25 output tokens (12 reasoning), 103 total tokens\nreasoning items: 0\n" +
				"infer chat: incomplete: max_output_tokens\n"
			if r.stderr != want {
				t.Errorf("stderr = %q, want %q", r.stderr, want)
			}
		})
	}
}

func TestChat_Failed(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute, aitest.RawReply{Frames: frames(
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`{"id":"chatcmpl-1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Par"},"finish_reason":null}]}`,
		`{"error":{"message":"engine crashed","type":"server_error","code":"500"}}`)})

	r := runApp(t, agent(e), "chat", "capital?")

	wantCode(t, r, process.ExitFailure)
	if want := "Par\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if !strings.HasPrefix(r.stderr, "status: failed\n") || !strings.HasSuffix(r.stderr, "\ninfer chat: failed: engine crashed (500)\n") {
		t.Errorf("stderr = %q, want the summary and then the failure", r.stderr)
	}
}

func TestChat_EndpointRefuses(t *testing.T) {
	e := chatEndpoint(t, nil, completionsRoute, aitest.ErrorReply{Status: http.StatusBadRequest, Message: "bad prompt"})

	r := runApp(t, agent(e), "chat", "hello")

	wantCode(t, r, process.ExitFailure)
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if !strings.HasPrefix(r.stderr, "infer chat: ") || !strings.Contains(r.stderr, "bad prompt") {
		t.Errorf("stderr = %q, want the endpoint's error", r.stderr)
	}
}

func TestChat_FallsBackToTheOneModel(t *testing.T) {
	e := completedStream(t, "agent")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "chat", "hello")

	wantCode(t, r, process.ExitOK)
	reqs := e.Requests()
	if len(reqs) != 2 || reqs[0].Path != "/v1/models" {
		t.Fatalf("requests = %d, want the model list and then the chat", len(reqs))
	}
	if body := oneBody(t, e, completionsRoute); body["model"] != "agent" {
		t.Errorf("body model = %v, want agent", body["model"])
	}
}

func TestChat_FallbackRefusal(t *testing.T) {
	e := completedStream(t, "agent", "vision")

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "chat", "hello")

	wantCode(t, r, process.ExitFailure)
	if want := "infer chat: several models: agent, vision; pass --model\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if bodies := chatBodies(t, e, completionsRoute); len(bodies) != 0 {
		t.Errorf("sent %d chat requests, want none", len(bodies))
	}
}

func TestChat_Help(t *testing.T) {
	e := completedStream(t, "agent")

	r := runApp(t, unreadable(e.URL()), "chat", "--help")

	wantCode(t, r, process.ExitUsage)
	for _, want := range []string{"--instructions", "--image", "--reasoning", "--max-tokens", "--stream", "--idle-timeout duration", "INFER_IDLE_TIMEOUT",
		"JSON object merged into the request's top level, for parameters the typed flags don't cover"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout = %q, want chat's help naming %s", r.stdout, want)
		}
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}

func TestChat_Messages(t *testing.T) {
	stream := aitest.RawReply{Frames: frames(
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"stop_reason":null,"usage":{"input_tokens":31,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Paris"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", Lyon"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":8}}`,
		`{"type":"message_stop"}`)}
	e := chatEndpoint(t, nil, "messages", stream)
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
	if want := "status: completed\nusage: 31 input tokens, 8 output tokens, 39 total tokens\nreasoning items: 0\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

func TestChat_ReasoningSummary(t *testing.T) {
	completions := func(reasoning string, tokens int) aitest.Reply {
		reply := chatReply(reasoning, "stop", "Paris")
		reply.ReasoningTokens = tokens
		return reply
	}
	messages := func(thinking bool, tokens string) aitest.Reply {
		content := `{"type":"text","text":"Paris"}`
		if thinking {
			content = `{"type":"thinking","thinking":"Think.","signature":"sig"},` + content
		}
		return aitest.RawReply{Body: []byte(`{"id":"msg_1","type":"message","role":"assistant","model":"agent","content":[` + content + `],"stop_reason":"end_turn","stop_sequence":null,` +
			`"usage":{"input_tokens":78,"output_tokens":25` + tokens + `}}`)}
	}
	responses := func(reasoning bool, tokens int) aitest.Reply {
		output := `{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","annotations":[],"text":"Paris"}]}`
		if reasoning {
			output = `{"id":"rs_1","type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"Think."}],"encrypted_content":"","status":"completed"},` + output
		}
		return aitest.RawReply{Body: []byte(`{"id":"resp_1","object":"response","status":"completed","output":[` + output + `],` +
			fmt.Sprintf(`"usage":{"input_tokens":78,"output_tokens":25,"total_tokens":103,"output_tokens_details":{"reasoning_tokens":%d}}}`, tokens))}
	}
	const (
		reported    = "usage: 78 input tokens, 25 output tokens (12 reasoning), 103 total tokens\nreasoning items: 1\n"
		notReported = "usage: 78 input tokens, 25 output tokens (reasoning not reported), 103 total tokens\nreasoning items: 1\n"
		none        = "usage: 78 input tokens, 25 output tokens, 103 total tokens\nreasoning items: 0\n"
	)
	for _, tc := range []struct {
		name   string
		format string
		route  string
		reply  aitest.Reply
		want   string
	}{
		{"completions reported", "completions", completionsRoute, completions("Think.", 12), reported},
		{"completions zero with a reasoning item", "completions", completionsRoute, completions("Think.", 0), notReported},
		{"completions zero without one", "completions", completionsRoute, completions("", 0), none},
		{"messages reported", "messages", "messages", messages(true, `,"output_tokens_details":{"thinking_tokens":12}`), reported},
		{"messages unreported with a thinking block", "messages", "messages", messages(true, ""), notReported},
		{"messages zero without one", "messages", "messages", messages(false, `,"output_tokens_details":{"thinking_tokens":0}`), none},
		{"responses reported", "responses", "responses", responses(true, 12), reported},
		{"responses zero with a reasoning item", "responses", "responses", responses(true, 0), notReported},
		{"responses zero without one", "responses", "responses", responses(false, 0), none},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := chatEndpoint(t, nil, tc.route, tc.reply)
			env := agent(e)
			env["INFER_FORMAT"] = tc.format

			r := runApp(t, env, "chat", "--stream=false", "--max-tokens", "64", "capital?")

			wantCode(t, r, process.ExitOK)
			if want := "status: completed\n" + tc.want; r.stderr != want {
				t.Errorf("stderr = %q, want %q", r.stderr, want)
			}
		})
	}
}

func TestChat_Responses(t *testing.T) {
	reply := `{"id":"resp_1","object":"response","status":"completed","output":[` +
		`{"id":"rs_1","type":"reasoning","summary":[],"content":[{"type":"reasoning_text","text":"Think."}],"encrypted_content":"","status":"completed"},` +
		`{"id":"msg_1","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","annotations":[],"text":"Paris"}]}],` +
		`"usage":{"input_tokens":91,"output_tokens":80,"total_tokens":171,"output_tokens_details":{"reasoning_tokens":70}}}`
	e := chatEndpoint(t, nil, "responses", aitest.RawReply{Body: []byte(reply)})
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
	if want := "status: completed\nusage: 91 input tokens, 80 output tokens (70 reasoning), 171 total tokens\nreasoning items: 1\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

func TestChat_StreamBreaks(t *testing.T) {
	breaks := chatReply("", "stop", "Par")
	breaks.BreakAfter = 1
	e := chatEndpoint(t, nil, completionsRoute, breaks)

	r := runApp(t, agent(e), "chat", "capital?")

	wantCode(t, r, process.ExitFailure)
	if want := "Par\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if !strings.HasPrefix(r.stderr, "infer chat: ") || !strings.Contains(r.stderr, "unexpected EOF") || strings.Contains(r.stderr, "status:") {
		t.Errorf("stderr = %q, want the break alone", r.stderr)
	}
}
