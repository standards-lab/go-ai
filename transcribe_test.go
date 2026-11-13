package ai_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/internal/aitest"
)

// transcribeReply is llama.cpp's transcription reply, as gb10-2 sends it.
const transcribeReply = `{"type":"transcript.text.done","text":"The access code is 7429.",` +
	`"usage":{"type":"tokens","input_tokens":174,"output_tokens":11,"total_tokens":185,` +
	`"input_tokens_details":{"cached_tokens":169}}}`

// waitLimit bounds a wait on something that should happen at once, so a hang fails the test
// rather than stalling it. Nothing sleeps for it.
const waitLimit = 10 * time.Second

// open returns an Open that gives r, once, as the clip, for a request of one attempt.
func open(r io.Reader) func(context.Context) (io.ReadCloser, error) {
	return func(context.Context) (io.ReadCloser, error) { return io.NopCloser(r), nil }
}

// clip returns n bytes of a clip, varied so a reordered or dropped chunk shows.
func clip(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 7)
	}
	return b
}

// formPart is one part of a multipart form as the server read it.
type formPart struct {
	fileName    string
	contentType string
	body        []byte
}

// readForm parses a recorded request's multipart form into its parts by form name, failing t
// unless the request is multipart/form-data with a boundary, each name appearing once.
func readForm(t *testing.T, got aitest.Request) map[string]formPart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(got.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("Content-Type = %q (%v), want multipart/form-data with a boundary", got.Header.Get("Content-Type"), err)
	}
	parts := make(map[string]formPart)
	r := multipart.NewReader(bytes.NewReader(got.Body), params["boundary"])
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatalf("read form: %v", err)
		}
		body, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("read part %q: %v", p.FormName(), err)
		}
		if _, dup := parts[p.FormName()]; dup {
			t.Fatalf("form has part %q twice", p.FormName())
		}
		parts[p.FormName()] = formPart{fileName: p.FileName(), contentType: p.Header.Get("Content-Type"), body: body}
	}
}

func TestTranscribeWireRequest(t *testing.T) {
	audio := clip(64 << 10)
	cases := []struct {
		name   string
		req    *ai.TranscriptionRequest
		fields map[string]string
	}{
		{
			"required fields",
			&ai.TranscriptionRequest{Model: "m", Open: open(bytes.NewReader(audio)), FileName: "clip.wav"},
			map[string]string{"model": "m", "response_format": "json"},
		},
		{
			"language and prompt",
			&ai.TranscriptionRequest{Model: "m", Open: open(bytes.NewReader(audio)), FileName: "clip.wav",
				Language: "en", Prompt: "Transcribe the code as digits."},
			map[string]string{"model": "m", "response_format": "json", "language": "en",
				"prompt": "Transcribe the code as digits."},
		},
		{
			// language and prompt are Native's to send when the typed fields leave them unset.
			"native fields",
			&ai.TranscriptionRequest{Model: "m", Open: open(bytes.NewReader(audio)), FileName: "clip.wav",
				Native: url.Values{"temperature": {"0"}, "language": {"en"}, "prompt": {"Digits."}}},
			map[string]string{"model": "m", "response_format": "json", "temperature": "0", "language": "en",
				"prompt": "Digits."},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, transcribeReply)
			c := newClient(t, srv.URL+"/v1/", ai.Options{
				Query:  url.Values{"autoload": {"false"}},
				Token:  func(context.Context) (string, error) { return "secret", nil },
				Header: http.Header{"X-Consumer-Key": {"k"}},
			})
			if _, err := c.Transcribe(t.Context(), tc.req); err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			got := srv.Only(t)
			if got.Method != http.MethodPost || got.Path != "/v1/audio/transcriptions" || got.RawQuery != "autoload=false" {
				t.Errorf("request = %s %s?%s, want POST /v1/audio/transcriptions?autoload=false",
					got.Method, got.Path, got.RawQuery)
			}
			for k, want := range map[string]string{"Accept": "application/json", "Authorization": "Bearer secret",
				"X-Consumer-Key": "k"} {
				if v := got.Header.Get(k); v != want {
					t.Errorf("%s = %q, want %q", k, v, want)
				}
			}
			parts := readForm(t, got)
			file, ok := parts["file"]
			if !ok {
				t.Fatalf("form has no file part; parts %v", slices.Sorted(maps.Keys(parts)))
			}
			if file.fileName != "clip.wav" || file.contentType != "application/octet-stream" {
				t.Errorf("file part = name %q, type %q; want clip.wav, application/octet-stream",
					file.fileName, file.contentType)
			}
			if !bytes.Equal(file.body, audio) {
				t.Errorf("file part has %d bytes, not the clip's %d", len(file.body), len(audio))
			}
			delete(parts, "file")
			fields := make(map[string]string)
			for name, p := range parts {
				if p.fileName != "" {
					t.Errorf("field %q is a file part, %q", name, p.fileName)
				}
				fields[name] = string(p.body)
			}
			if !maps.Equal(fields, tc.fields) {
				t.Errorf("fields = %v, want %v", fields, tc.fields)
			}
		})
	}
}

// TestTranscribeNativeFieldOrder covers the order the form's fields go in: the typed fields,
// then Native's keys sorted and each key's values in order, a key of several values sent once
// per value, then the file.
func TestTranscribeNativeFieldOrder(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, transcribeReply)
	c := newClient(t, srv.URL, ai.Options{})
	_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model: "m", Open: open(strings.NewReader("RIFF")), FileName: "clip.wav", Language: "en",
		Native: url.Values{"temperature": {"0"}, "extra": {"b", "a"}, "max_tokens": {"64"}},
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	got := srv.Only(t)
	_, params, err := mime.ParseMediaType(got.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("Content-Type: %v", err)
	}
	var order []string
	r := multipart.NewReader(bytes.NewReader(got.Body), params["boundary"])
	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read form: %v", err)
		}
		body, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("read part %q: %v", p.FormName(), err)
		}
		order = append(order, p.FormName()+"="+string(body))
	}
	want := []string{"model=m", "response_format=json", "language=en", "extra=b", "extra=a", "max_tokens=64",
		"temperature=0", "file=RIFF"}
	if !slices.Equal(order, want) {
		t.Errorf("form = %q\nwant   %q", order, want)
	}
}

func TestTranscribeDecodesReply(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, transcribeReply)
	c := newClient(t, srv.URL, ai.Options{})
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model: "m", Open: open(strings.NewReader("RIFF")), FileName: "clip.wav",
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := ai.Transcription{
		Text:  "The access code is 7429.",
		Usage: ai.TranscriptionUsage{InputTokens: 174, OutputTokens: 11, TotalTokens: 185},
	}
	if *got != want {
		t.Errorf("Transcribe = %+v, want %+v", *got, want)
	}
}

// TestTranscribeAnyFormat wants a route-mode Transcribe to upload the clip as a multipart form to
// the OpenAI-compatible transcription route on a Client of a format whose own route is
// elsewhere, with none of the format's headers, and decode the route's reply.
func TestTranscribeAnyFormat(t *testing.T) {
	formats := map[string]format.Format{"messages": messages.New(), "responses": responses.New(responses.Options{})}
	for name, wireFormat := range formats {
		t.Run(name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, transcribeReply)
			c, err := ai.New(srv.URL+"/v1", http.DefaultClient, wireFormat, ai.Options{})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
				Model: "m", Open: open(strings.NewReader("RIFF")), FileName: "clip.wav",
			})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			if got.Text != "The access code is 7429." {
				t.Errorf("Text = %q, want the route's text", got.Text)
			}
			req := srv.Only(t)
			if req.Method != http.MethodPost || req.Path != "/v1/audio/transcriptions" {
				t.Errorf("request = %s %s, want POST /v1/audio/transcriptions", req.Method, req.Path)
			}
			if v := req.Header.Get("Anthropic-Version"); v != "" {
				t.Errorf("anthropic-version = %q, want none: the format's headers are its route's", v)
			}
			parts := readForm(t, req)
			if file := parts["file"]; string(file.body) != "RIFF" || file.fileName != "clip.wav" {
				t.Errorf("file part = %q named %q, want the clip named clip.wav", file.body, file.fileName)
			}
			if string(parts["model"].body) != "m" {
				t.Errorf("model field = %q, want m", parts["model"].body)
			}
		})
	}
}

func TestTranscribeRefusesReply(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, `{"text":7429}`)
	c := newClient(t, srv.URL, ai.Options{})
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model: "m", Open: open(strings.NewReader("RIFF")), FileName: "clip.wav",
	})
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Errorf("Transcribe = %+v, %v; want a decode response error", got, err)
	}
}

// TestTranscribeRefusesRequest covers what Transcribe refuses, and when: before the token source
// is called or anything is sent.
func TestTranscribeRefusesRequest(t *testing.T) {
	file := strings.NewReader("RIFF")
	native := func(key string) *ai.TranscriptionRequest {
		return &ai.TranscriptionRequest{Model: "m", Open: open(file), FileName: "clip.wav", Language: "en",
			Prompt: "Digits.", Native: url.Values{"temperature": {"0"}, key: {"x"}}}
	}
	cases := []struct {
		name string
		req  *ai.TranscriptionRequest
		want string
	}{
		{"nil", nil, "no request"},
		{"no model", &ai.TranscriptionRequest{Open: open(file), FileName: "clip.wav"}, "no model"},
		{"no Open", &ai.TranscriptionRequest{Model: "m", FileName: "clip.wav"}, "no Open"},
		{"no file name", &ai.TranscriptionRequest{Model: "m", Open: open(file)}, "no file name"},
		{"native field of no name", native(""), "native field has an empty name"},
		{"native model", native("model"), "native field model clashes"},
		{"native response_format", native("response_format"), "native field response_format clashes"},
		{"native file", native("file"), "native field file clashes"},
		{"native language when set", native("language"), "native field language clashes"},
		{"native prompt when set", native("prompt"), "native field prompt clashes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, transcribeReply)
			called := false
			c := newClient(t, srv.URL, ai.Options{
				Token: func(context.Context) (string, error) { called = true; return "", nil },
			})
			got, err := c.Transcribe(t.Context(), tc.req)
			if err == nil {
				t.Fatalf("Transcribe = %+v, nil; want an error containing %q", got, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Transcribe error = %q, want it to contain %q", err, tc.want)
			}
			if called {
				t.Error("the token source was called")
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

// TestTranscribeAPIError covers a non-2xx reply: an *APIError with the OpenAI-style error body's
// details, whatever format the Client speaks.
func TestTranscribeAPIError(t *testing.T) {
	reply := `{"error":{"code":400,"message":"Only 'json' response_format is supported for transcription",` +
		`"type":"invalid_request_error"}}`
	srv := aitest.NewServer(t, http.StatusBadRequest, reply)
	c := newClient(t, srv.URL, ai.Options{})
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model: "m", Open: open(strings.NewReader("RIFF")), FileName: "clip.wav",
	})
	apiErr, ok := errors.AsType[*ai.APIError](err)
	if !ok {
		t.Fatalf("Transcribe = %+v, %v; want an *APIError", got, err)
	}
	if apiErr.Status != http.StatusBadRequest ||
		apiErr.Message != "Only 'json' response_format is supported for transcription" ||
		apiErr.Type != "invalid_request_error" || apiErr.Code != "400" || string(apiErr.Body) != reply {
		t.Errorf("APIError = %+v, want 400 with the body's message, type, and code", apiErr)
	}
}

func TestTranscribeTokenErrorSendsNothing(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, transcribeReply)
	errExpired := errors.New("token expired")
	c := newClient(t, srv.URL, ai.Options{
		Token: func(context.Context) (string, error) { return "", errExpired },
	})
	_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model: "m", Open: open(bytes.NewReader(clip(64 << 10))), FileName: "clip.wav",
	})
	if !errors.Is(err, errExpired) {
		t.Errorf("Transcribe error = %v, want it to wrap %v", err, errExpired)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// drainServer starts a server that reads each request's body to its end or its failure, which
// it ignores, as the upload's failure is the test's subject, and closes arrived on the first
// request's arrival, once its header is read and before its body is.
func drainServer(t *testing.T) (url string, arrived <-chan struct{}) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(ch) })
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, transcribeReply)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, ch
}

// TestTranscribeReaderError has the clip's read fail part way under a policy, and wants its
// error returned, the clip opened once: the clip's own failure isn't the endpoint's, so it isn't
// retried, whatever the transport made of the upload it cut short.
func TestTranscribeReaderError(t *testing.T) {
	base, _ := drainServer(t)
	c := newClient(t, base, ai.Options{Retry: ai.RetryPolicy{MaxAttempts: 3}})
	errBroken := errors.New("disk read failed")
	var opened atomic.Int32
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model: "m",
		Open: func(context.Context) (io.ReadCloser, error) {
			opened.Add(1)
			return io.NopCloser(io.MultiReader(bytes.NewReader(clip(64<<10)), iotest.ErrReader(errBroken))), nil
		},
		FileName: "clip.wav",
	})
	if !errors.Is(err, errBroken) || !strings.Contains(err.Error(), "upload: ") {
		t.Errorf("Transcribe = %+v, %v; want an upload error wrapping %v", got, err, errBroken)
	}
	if n := opened.Load(); n != 1 {
		t.Errorf("clip opened %d times, want 1", n)
	}
}

// stallingReader is a clip, such as a slow file, that gives one chunk, then, once the request
// has reached the server, stalls until ctx ends, and gives another chunk: a clip that never
// fails, so the upload's failure is the request's alone.
type stallingReader struct {
	ctx     context.Context
	arrived <-chan struct{}
	// stalled is closed when the reader stalls, the clip partly sent.
	stalled chan struct{}
	// ended is set when the stalled Read returns.
	ended atomic.Bool
	reads int
}

func (r *stallingReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads == 2 {
		<-r.arrived
		close(r.stalled)
		<-r.ctx.Done()
		r.ended.Store(true)
	}
	return copy(p, clip(len(p))), nil
}

// TestTranscribeCancelMidUpload cancels the context while the clip is partly uploaded: the
// request has reached the server before the clip's second read, which waits for it, so the
// upload streams rather than buffering the clip, and
// Transcribe returns ctx's error promptly, only after the upload stopped reading the clip. The
// clip itself never fails, so the error is the cancellation's, not the upload's write into the
// pipe the cancelled request closed.
func TestTranscribeCancelMidUpload(t *testing.T) {
	base, arrived := drainServer(t)
	c := newClient(t, base, ai.Options{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	file := &stallingReader{ctx: ctx, arrived: arrived, stalled: make(chan struct{})}
	type result struct {
		t   *ai.Transcription
		err error
		// ended is whether the clip's stalled Read had returned when Transcribe did.
		ended bool
	}
	done := make(chan result, 1)
	go func() {
		got, err := c.Transcribe(ctx, &ai.TranscriptionRequest{Model: "m", Open: open(file), FileName: "clip.wav"})
		done <- result{got, err, file.ended.Load()}
	}()
	select {
	case <-file.stalled:
	case res := <-done:
		t.Fatalf("Transcribe = %+v, %v before the upload stalled", res.t, res.err)
	case <-time.After(waitLimit):
		t.Fatal("the upload never reached the server with the clip partly read")
	}
	cancel()
	select {
	case res := <-done:
		if !errors.Is(res.err, context.Canceled) {
			t.Errorf("Transcribe = %+v, %v; want an error wrapping context.Canceled", res.t, res.err)
		}
		if !res.ended {
			t.Error("Transcribe returned while the upload was still reading the clip")
		}
	case <-time.After(waitLimit):
		t.Fatalf("Transcribe didn't return within %v of the cancellation", waitLimit)
	}
}
