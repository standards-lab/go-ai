package aitest_test

import (
	"bufio"
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
)

func newClient(t *testing.T, e *aitest.Endpoint, wf format.Format) *ai.Client {
	t.Helper()
	c, err := ai.New(e.URL(), http.DefaultClient, wf, ai.Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func send(t *testing.T, e *aitest.Endpoint, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, e.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	return resp.StatusCode, string(b)
}

func rawOK(context.Context, aitest.RawRequest) aitest.Reply {
	return aitest.RawReply{Body: []byte(`{}`)}
}

func userText(model, text string) *protocol.Request {
	return &protocol.Request{
		Model: model,
		Input: []protocol.Item{protocol.Message{
			Role:    protocol.RoleUser,
			Content: []protocol.ContentPart{protocol.InputText{Text: text}},
		}},
		MaxOutputTokens: 64,
	}
}

func TestServeRecordsRequests(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"messages": rawOK,
	}})
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, e.URL()+"/messages?beta=true", strings.NewReader(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Test", "one")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	send(t, e, http.MethodGet, "/unknown", "")

	got := e.Requests()
	if len(got) != 2 {
		t.Fatalf("Requests = %d requests, want 2", len(got))
	}
	first := got[0]
	if first.Method != http.MethodPost || first.Path != "/v1/messages" ||
		!reflect.DeepEqual(first.Query, url.Values{"beta": {"true"}}) || string(first.Body) != `{"a":1}` {
		t.Errorf("Requests[0] = %s %s?%v %q, want POST /v1/messages?beta=true {\"a\":1}", first.Method, first.Path, first.Query, first.Body)
	}
	if h := first.Header.Get("X-Test"); h != "one" {
		t.Errorf("Requests[0] header X-Test = %q, want one", h)
	}
	if got[1].Method != http.MethodGet || got[1].Path != "/v1/unknown" || len(got[1].Body) != 0 {
		t.Errorf("Requests[1] = %s %s %q, want GET /v1/unknown with no body", got[1].Method, got[1].Path, got[1].Body)
	}
}

func TestServeRecordsConcurrentRequests(t *testing.T) {
	const n = 50
	var mu sync.Mutex
	var arrivals []int
	e := aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"responses": func(_ context.Context, req aitest.RawRequest) aitest.Reply {
			mu.Lock()
			arrivals = append(arrivals, req.Arrival)
			mu.Unlock()
			return aitest.RawReply{Body: []byte(`{}`)}
		},
	}})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, e.URL()+"/responses", strings.NewReader(strconv.Itoa(i)))
			if err != nil {
				t.Error(err)
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			_ = resp.Body.Close()
			_ = e.Requests()
		})
	}
	wg.Wait()

	var bodies []int
	for _, r := range e.Requests() {
		b, err := strconv.Atoi(string(r.Body))
		if err != nil {
			t.Fatalf("recorded body %q: %v", r.Body, err)
		}
		bodies = append(bodies, b)
	}
	slices.Sort(bodies)
	slices.Sort(arrivals)
	for i := range n {
		if len(bodies) != n || bodies[i] != i {
			t.Fatalf("recorded bodies = %v, want 0 to %d each once", bodies, n-1)
		}
		if len(arrivals) != n || arrivals[i] != i+1 {
			t.Fatalf("arrival numbers = %v, want 1 to %d each once", arrivals, n)
		}
	}
}

func TestServeClosesWhenTestEnds(t *testing.T) {
	var base string
	ended := make(chan error, 1)
	t.Run("serve", func(t *testing.T) {
		arrived := make(chan struct{})
		e := aitest.Serve(t, aitest.Routes{Models: func(ctx context.Context, _ aitest.ModelsRequest) aitest.Reply {
			close(arrived)
			<-ctx.Done()
			ended <- ctx.Err()
			return aitest.ModelsReply{}
		}})
		base = e.URL()
		go func() {
			resp, err := http.Get(base + "/models")
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		<-arrived
	})
	select {
	case err := <-ended:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("route context's error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("route context didn't end when the test ended")
	}
	if resp, err := http.Get(base + "/models"); err == nil {
		_ = resp.Body.Close()
		t.Errorf("GET after the test ended = %s, want a connection error", resp.Status)
	}
}

func TestUnscriptedRouteAnswers404(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{})
	c := newClient(t, e, messages.New(messages.Options{}))
	cases := []struct {
		name string
		call func() error
	}{
		{"embeddings", func() error {
			_, err := c.Embed(t.Context(), &ai.EmbeddingRequest{Model: "m", Input: []string{"x"}})
			return err
		}},
		{"transcriptions", func() error {
			_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
				Model:    "m",
				FileName: "clip.wav",
				Open: func(context.Context) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("RIFF")), nil
				},
			})
			return err
		}},
		{"models", func() error {
			_, err := c.Models(t.Context())
			return err
		}},
		{"unknown path", func() error {
			_, err := c.Generate(t.Context(), userText("m", "hi"))
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr, ok := errors.AsType[*ai.APIError](tc.call())
			if !ok {
				t.Fatal("error wraps no *ai.APIError")
			}
			if apiErr.Status != http.StatusNotFound || apiErr.Message == "" || apiErr.Type != "invalid_request_error" {
				t.Errorf("APIError = {Status:%d Message:%q Type:%q}, want 404 with a message and type invalid_request_error",
					apiErr.Status, apiErr.Message, apiErr.Type)
			}
		})
	}
	status, body := send(t, e, http.MethodPost, "/no/such/route", "")
	if status != http.StatusNotFound || !strings.HasPrefix(body, `{"error":{"message":"`) || !strings.Contains(body, `"type":"invalid_request_error"`) {
		t.Errorf("POST /v1/no/such/route = %d %s, want 404 with an OpenAI-style error body", status, body)
	}
}

func TestEmbeddings(t *testing.T) {
	var got aitest.EmbeddingRequest
	e := aitest.Serve(t, aitest.Routes{Embeddings: func(_ context.Context, req aitest.EmbeddingRequest) aitest.Reply {
		got = req
		return aitest.EmbeddingsReply{
			Vectors:      [][]float32{{0.5, -1.25}, {3, 0}, {}},
			PromptTokens: 7,
			TotalTokens:  9,
		}
	}})
	c := newClient(t, e, completions.New(completions.Options{}))
	emb, err := c.Embed(t.Context(), &ai.EmbeddingRequest{Model: "embedder", Input: []string{"a", "b", ""}})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if got.Arrival != 1 || got.Model != "embedder" || !slices.Equal(got.Input, []string{"a", "b", ""}) {
		t.Errorf("route got %+v, want arrival 1, model embedder, inputs [a b \"\"]", got)
	}
	want := &ai.Embeddings{
		Vectors: [][]float32{{0.5, -1.25}, {3, 0}, {}},
		Usage:   ai.EmbeddingUsage{PromptTokens: 7, TotalTokens: 9},
	}
	if !reflect.DeepEqual(emb, want) {
		t.Errorf("Embed = %+v, want %+v", emb, want)
	}
}

func TestTranscription(t *testing.T) {
	var got aitest.TranscriptionRequest
	e := aitest.Serve(t, aitest.Routes{Transcriptions: func(_ context.Context, req aitest.TranscriptionRequest) aitest.Reply {
		got = req
		return aitest.TranscriptionReply{Text: "hello there", InputTokens: 11, OutputTokens: 3, TotalTokens: 14}
	}})
	c := newClient(t, e, completions.New(completions.Options{}))
	if c.TranscriptionMode() != ai.TranscribeRoute {
		t.Fatalf("TranscriptionMode = %v, want route", c.TranscriptionMode())
	}
	clip := "RIFF\x00\x01clip"
	tr, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model:    "whisper",
		FileName: "clip.wav",
		Language: "en",
		Prompt:   "names: Ada",
		Native:   url.Values{"temperature": {"0"}},
		Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(clip)), nil
		},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := aitest.TranscriptionRequest{
		Arrival:  1,
		Model:    "whisper",
		Prompt:   "names: Ada",
		Language: "en",
		FileName: "clip.wav",
		File:     []byte(clip),
		Fields:   url.Values{"response_format": {"json"}, "temperature": {"0"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("route got %+v\nwant %+v", got, want)
	}
	wantTr := &ai.Transcription{Text: "hello there", Usage: ai.TranscriptionUsage{InputTokens: 11, OutputTokens: 3, TotalTokens: 14}}
	if !reflect.DeepEqual(tr, wantTr) {
		t.Errorf("Transcribe = %+v, want %+v", tr, wantTr)
	}
}

func TestTranscriptionFileNameAsSent(t *testing.T) {
	var got string
	e := aitest.Serve(t, aitest.Routes{Transcriptions: func(_ context.Context, req aitest.TranscriptionRequest) aitest.Reply {
		got = req.FileName
		return aitest.TranscriptionReply{Text: "hi"}
	}})
	c := newClient(t, e, completions.New(completions.Options{}))
	_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model:    "whisper",
		FileName: "recordings/2026/clip.wav",
		Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("RIFF")), nil
		},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if want := "recordings/2026/clip.wav"; got != want {
		t.Errorf("FileName = %q, want %q, as sent", got, want)
	}
}

func TestModels(t *testing.T) {
	entries := []string{
		`{"id":"gemma","object":"model","owned_by":"llamacpp","status":{"value":"loaded"}}`,
		`{"id":"qwen","object":"model","max_model_len":8192}`,
	}
	e := aitest.Serve(t, aitest.Routes{Models: func(context.Context, aitest.ModelsRequest) aitest.Reply {
		return aitest.ModelsReply{Entries: []jsontext.Value{jsontext.Value(entries[0]), jsontext.Value(entries[1])}}
	}})
	models, err := newClient(t, e, completions.New(completions.Options{})).Models(t.Context())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("Models = %d models, want 2", len(models))
	}
	for i, id := range []string{"gemma", "qwen"} {
		if models[i].ID != id || string(models[i].Native) != entries[i] {
			t.Errorf("Models[%d] = %q %s, want %q %s", i, models[i].ID, models[i].Native, id, entries[i])
		}
	}
}

func TestArrivalNumbers(t *testing.T) {
	var models, raw []int
	e := aitest.Serve(t, aitest.Routes{
		Models: func(_ context.Context, req aitest.ModelsRequest) aitest.Reply {
			models = append(models, req.Arrival)
			return aitest.ModelsReply{}
		},
		Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
			"responses": func(_ context.Context, req aitest.RawRequest) aitest.Reply {
				raw = append(raw, req.Arrival)
				return aitest.RawReply{Body: []byte(`{}`)}
			},
		},
	})
	c := newClient(t, e, completions.New(completions.Options{}))
	for range 2 {
		if _, err := c.Models(t.Context()); err != nil {
			t.Fatalf("Models: %v", err)
		}
	}
	send(t, e, http.MethodPost, "/responses", "{}")
	if !slices.Equal(models, []int{1, 2}) || !slices.Equal(raw, []int{1}) {
		t.Errorf("arrivals = models %v, responses %v; want [1 2] and [1]", models, raw)
	}
}

func TestRawReplyStream(t *testing.T) {
	frames := []aitest.Frame{
		{Event: "message_start", Data: `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0}}}`},
		{Event: "content_block_start", Data: `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`},
		{Event: "content_block_delta", Data: `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":", world"}}`},
		{Event: "content_block_stop", Data: `{"type":"content_block_stop","index":0}`},
		{Event: "message_delta", Data: `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":2}}`},
		{Event: "message_stop", Data: `{"type":"message_stop"}`},
	}
	var got aitest.RawRequest
	e := aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"messages": func(_ context.Context, req aitest.RawRequest) aitest.Reply {
			got = req
			return aitest.RawReply{Frames: frames}
		},
	}})
	c := newClient(t, e, messages.New(messages.Options{}))
	var acc protocol.Accumulator
	for ev, err := range c.GenerateStream(t.Context(), userText("m", "hi")) {
		if err != nil {
			t.Fatalf("GenerateStream: %v", err)
		}
		if err := acc.Add(ev); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	resp := acc.Response()
	if text := resp.OutputText(); text != "Hello, world" || resp.Status != protocol.StatusCompleted {
		t.Errorf("stream = %q %s, want \"Hello, world\" completed", text, resp.Status)
	}
	if got.Arrival != 1 || got.Method != http.MethodPost || got.Path != "/v1/messages" || len(got.Body) == 0 {
		t.Errorf("route got arrival %d, %s %s with %d body bytes; want 1, POST /v1/messages with a body",
			got.Arrival, got.Method, got.Path, len(got.Body))
	}
}

func TestRawReplyBody(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"responses": func(context.Context, aitest.RawRequest) aitest.Reply {
			return aitest.RawReply{Body: []byte(`{"id":"resp_1","object":"response","model":"m","status":"completed",` +
				`"output":[{"id":"msg_1","type":"message","role":"assistant","status":"completed",` +
				`"content":[{"type":"output_text","text":"Paris","annotations":[]}]}],` +
				`"usage":{"input_tokens":4,"output_tokens":1,"total_tokens":5}}`)}
		},
	}})
	resp, err := newClient(t, e, responses.New(responses.Options{})).Generate(t.Context(), userText("m", "Capital of France?"))
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.OutputText() != "Paris" || resp.Status != protocol.StatusCompleted {
		t.Errorf("Generate = %q %s, want \"Paris\" completed", resp.OutputText(), resp.Status)
	}
}

func TestRawReplyOnTypedRoute(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Embeddings: func(context.Context, aitest.EmbeddingRequest) aitest.Reply {
		return aitest.RawReply{
			Status: http.StatusServiceUnavailable,
			Header: http.Header{"Retry-After": {"7"}},
			Body:   []byte(`{"error":{"message":"loading model","type":"unavailable_error"}}`),
		}
	}})
	_, err := newClient(t, e, completions.New(completions.Options{})).Embed(t.Context(), &ai.EmbeddingRequest{Model: "m", Input: []string{"x"}})
	apiErr, ok := errors.AsType[*ai.APIError](err)
	if !ok {
		t.Fatalf("Embed error %v wraps no *ai.APIError", err)
	}
	if apiErr.Status != http.StatusServiceUnavailable || apiErr.Message != "loading model" ||
		apiErr.Type != "unavailable_error" || apiErr.RetryAfter != 7*time.Second {
		t.Errorf("APIError = {Status:%d Message:%q Type:%q RetryAfter:%v}, want 503 \"loading model\" unavailable_error 7s",
			apiErr.Status, apiErr.Message, apiErr.Type, apiErr.RetryAfter)
	}
}

func TestRawReplyFlushesEachFrame(t *testing.T) {
	frames := []aitest.Frame{{Event: "one", Data: `{"n":1}`}, {Data: "two\nlines"}, {Data: "[DONE]"}}
	e := aitest.Serve(t, aitest.Routes{Raw: map[string]func(context.Context, aitest.RawRequest) aitest.Reply{
		"stream": func(context.Context, aitest.RawRequest) aitest.Reply {
			return aitest.RawReply{Header: http.Header{"X-Stream": {"yes"}}, Frames: frames}
		},
	}})
	u, err := url.Parse(e.URL())
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := fmt.Fprintf(conn, "POST /v1/stream HTTP/1.1\r\nHost: %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", u.Host); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Stream") != "yes" {
		t.Errorf("header = %v, want Content-Type text/event-stream and X-Stream yes", resp.Header)
	}
	if !slices.Equal(resp.TransferEncoding, []string{"chunked"}) {
		t.Fatalf("TransferEncoding = %v, want chunked", resp.TransferEncoding)
	}
	var chunks []string
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read chunk size: %v", err)
		}
		size, err := strconv.ParseInt(strings.TrimSpace(line), 16, 64)
		if err != nil {
			t.Fatalf("chunk size %q: %v", line, err)
		}
		chunk := make([]byte, size+int64(len("\r\n")))
		if _, err := io.ReadFull(r, chunk); err != nil {
			t.Fatalf("read chunk: %v", err)
		}
		if size == 0 {
			break
		}
		chunks = append(chunks, string(chunk[:size]))
	}
	want := []string{"event: one\ndata: {\"n\":1}\n\n", "data: two\ndata: lines\n\n", "data: [DONE]\n\n"}
	if !slices.Equal(chunks, want) {
		t.Errorf("chunks = %q\nwant     %q", chunks, want)
	}
}
