package aitest

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// The typed routes' paths, below the server's root.
const (
	chatPath           = "/v1/chat/completions"
	embeddingsPath     = "/v1/embeddings"
	transcriptionsPath = "/v1/audio/transcriptions"
	modelsPath         = "/v1/models"
)

// maxForm bounds the part of a transcription upload held in memory; the rest goes to temporary
// files, which the request's end removes.
const maxForm = 32 << 20

// Routes gives the function that answers each route of an [Endpoint]. A route whose function is
// nil answers 404 with an OpenAI-style error body, as an endpoint without the route does.
//
// A route function runs on the server's goroutine, so it reports a failure with t.Errorf, never
// t.Fatal. Its context is the request's own, which also ends when the test ends. Requests to one
// route are numbered in the order they arrive, from 1, apart from every other route's.
type Routes struct {
	// ChatCompletions answers POST /v1/chat/completions.
	ChatCompletions func(ctx context.Context, req ChatRequest) Reply
	// Embeddings answers POST /v1/embeddings.
	Embeddings func(ctx context.Context, req EmbeddingRequest) Reply
	// Transcriptions answers POST /v1/audio/transcriptions.
	Transcriptions func(ctx context.Context, req TranscriptionRequest) Reply
	// Models answers GET /v1/models.
	Models func(ctx context.Context, req ModelsRequest) Reply
	// Raw answers any other path below /v1, keyed by the path after /v1/, such as "messages" or
	// "responses", whatever the method. A key that names a typed route above fails Serve.
	Raw map[string]func(ctx context.Context, req RawRequest) Reply
}

// Request is one request an [Endpoint] received, as it arrived on the wire.
type Request struct {
	// Method is the request's method.
	Method string
	// Path is the URL's path, such as /v1/models.
	Path string
	// Query is the URL's query.
	Query url.Values
	// Header is the request's header.
	Header http.Header
	// Body is the request's body.
	Body []byte
}

// EmbeddingRequest is a request to the embeddings route, decoded.
type EmbeddingRequest struct {
	// Arrival is the request's place among the route's requests, from 1.
	Arrival int
	// Model is the request's model.
	Model string
	// Input is the texts to embed, in order. A single string input is one text.
	Input []string
}

// TranscriptionRequest is a request to the transcription route, its multipart form decoded.
type TranscriptionRequest struct {
	// Arrival is the request's place among the route's requests, from 1.
	Arrival int
	// Model is the form's model field.
	Model string
	// Prompt is the form's prompt field, empty when it has none.
	Prompt string
	// Language is the form's language field, empty when it has none.
	Language string
	// FileName is the file part's file name as the client sent it, a path's directories
	// included.
	FileName string
	// File is the file part's bytes.
	File []byte
	// Fields holds the form's other fields, such as response_format.
	Fields url.Values
}

// ModelsRequest is a request to the models route.
type ModelsRequest struct {
	// Arrival is the request's place among the route's requests, from 1.
	Arrival int
}

// RawRequest is a request to a Raw route.
type RawRequest struct {
	// Arrival is the request's place among the route's requests, from 1.
	Arrival int
	// Request is the request as it arrived.
	Request
}

// Endpoint is a loopback OpenAI-compatible model endpoint: an httptest server that records each
// request, then answers it with the function its Routes give.
type Endpoint struct {
	t      testing.TB
	url    string
	routes Routes
	// ctx ends when the test ends, and with it every request's context.
	ctx context.Context

	mu       sync.Mutex
	reqs     []Request
	arrivals map[string]int
}

// Serve starts an [Endpoint] answering with routes. It is closed when t's test ends: every
// request's context is cancelled, then the server waits for its handlers and closes.
func Serve(t testing.TB, routes Routes) *Endpoint {
	t.Helper()
	for key := range routes.Raw {
		if p := "/v1/" + key; p == chatPath || p == embeddingsPath || p == transcriptionsPath || p == modelsPath {
			t.Fatalf("aitest: Raw route %q is a typed route; give its function in Routes", key)
		}
	}
	e := &Endpoint{t: t, routes: routes, arrivals: map[string]int{}}
	hs := httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(hs.Close)
	// Cleanups run last first, so the contexts end before Close waits on the handlers using them.
	var cancel context.CancelFunc
	e.ctx, cancel = context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e.url = hs.URL + "/v1"
	return e
}

// URL returns the endpoint's API root, the server's URL ending in /v1, as a client's base URL
// names it.
func (e *Endpoint) URL() string { return e.url }

// Requests returns the requests the endpoint has received so far, in the order they arrived,
// those no route answered included.
func (e *Endpoint) Requests() []Request {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.reqs)
}

// serve records r, then dispatches it to its route's function and writes the reply.
func (e *Endpoint) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		e.t.Errorf("aitest: read %s %s: %v", r.Method, r.URL.Path, err)
	}
	req := Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Header: r.Header.Clone(), Body: body}
	e.mu.Lock()
	e.reqs = append(e.reqs, req)
	e.mu.Unlock()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	defer context.AfterFunc(e.ctx, cancel)()

	reply, a, ok, err := e.dispatch(ctx, req)
	switch {
	case !ok:
		writeError(w, http.StatusNotFound, "invalid_request_error",
			fmt.Sprintf("aitest: no route function for %s %s", req.Method, req.Path))
	case err != nil:
		writeError(w, http.StatusBadRequest, "invalid_request_error", "aitest: "+err.Error())
	case reply == nil:
		e.t.Errorf("aitest: the route function for %s %s returned no reply", req.Method, req.Path)
		writeError(w, http.StatusInternalServerError, "server_error", "aitest: the route function returned no reply")
	default:
		reply.write(ctx, w, a)
	}
}

// dispatch decodes req for its route and calls the route's function, returning its reply and
// what req asked of the reply's shape. It reports false when no function answers req, and an
// error when req can't be decoded for its route.
func (e *Endpoint) dispatch(ctx context.Context, req Request) (Reply, asked, bool, error) {
	rt := e.routes
	switch {
	case req.Path == chatPath && req.Method == http.MethodPost && rt.ChatCompletions != nil:
		cr, a, err := decodeChatRequest(req.Body)
		if err != nil {
			return nil, asked{}, true, err
		}
		cr.Arrival = e.arrive(req.Path)
		a.arrival = cr.Arrival
		return rt.ChatCompletions(ctx, cr), a, true, nil
	case req.Path == embeddingsPath && req.Method == http.MethodPost && rt.Embeddings != nil:
		er, err := decodeEmbeddingRequest(req.Body)
		if err != nil {
			return nil, asked{}, true, err
		}
		er.Arrival = e.arrive(req.Path)
		return rt.Embeddings(ctx, er), asked{}, true, nil
	case req.Path == transcriptionsPath && req.Method == http.MethodPost && rt.Transcriptions != nil:
		tr, err := decodeTranscriptionRequest(req)
		if err != nil {
			return nil, asked{}, true, err
		}
		tr.Arrival = e.arrive(req.Path)
		return rt.Transcriptions(ctx, tr), asked{}, true, nil
	case req.Path == modelsPath && req.Method == http.MethodGet && rt.Models != nil:
		return rt.Models(ctx, ModelsRequest{Arrival: e.arrive(req.Path)}), asked{}, true, nil
	}
	key, ok := strings.CutPrefix(req.Path, "/v1/")
	if f := rt.Raw[key]; ok && f != nil {
		return f(ctx, RawRequest{Arrival: e.arrive(req.Path), Request: req}), asked{}, true, nil
	}
	return nil, asked{}, false, nil
}

// arrive counts a request to the route at path and returns its arrival number.
func (e *Endpoint) arrive(path string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.arrivals[path]++
	return e.arrivals[path]
}

// decodeEmbeddingRequest decodes an embeddings request body, whose input is a string or an
// array of strings.
func decodeEmbeddingRequest(body []byte) (EmbeddingRequest, error) {
	var wire struct {
		Model string         `json:"model"`
		Input jsontext.Value `json:"input"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return EmbeddingRequest{}, fmt.Errorf("decode embeddings request: %w", err)
	}
	var req EmbeddingRequest
	req.Model = wire.Model
	if wire.Input.Kind() == '"' {
		var s string
		if err := json.Unmarshal(wire.Input, &s); err != nil {
			return EmbeddingRequest{}, fmt.Errorf("decode embeddings input: %w", err)
		}
		req.Input = []string{s}
		return req, nil
	}
	if err := json.Unmarshal(wire.Input, &req.Input); err != nil {
		return EmbeddingRequest{}, fmt.Errorf("decode embeddings input: %w", err)
	}
	return req, nil
}

// decodeTranscriptionRequest decodes a transcription request's multipart form.
func decodeTranscriptionRequest(req Request) (TranscriptionRequest, error) {
	// The body was read whole to record it, so the form is parsed from that copy.
	r, err := http.NewRequest(req.Method, "http://aitest"+req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return TranscriptionRequest{}, err
	}
	r.Header = req.Header
	if err := r.ParseMultipartForm(maxForm); err != nil {
		return TranscriptionRequest{}, fmt.Errorf("decode transcription form: %w", err)
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("file")
	if err != nil {
		return TranscriptionRequest{}, fmt.Errorf("decode transcription form's file: %w", err)
	}
	data, err := readPart(file)
	if err != nil {
		return TranscriptionRequest{}, fmt.Errorf("read transcription form's file: %w", err)
	}
	// The file name as sent: the parsed form's Filename keeps a path's base name alone, which
	// would hide a client that sends the path it read the clip from.
	_, disposition, err := mime.ParseMediaType(header.Header.Get("Content-Disposition"))
	if err != nil {
		return TranscriptionRequest{}, fmt.Errorf("decode transcription form's file disposition: %w", err)
	}
	fields := url.Values(r.MultipartForm.Value)
	tr := TranscriptionRequest{
		Model:    fields.Get("model"),
		Prompt:   fields.Get("prompt"),
		Language: fields.Get("language"),
		FileName: disposition["filename"],
		File:     data,
		Fields:   url.Values{},
	}
	for k, v := range fields {
		if k != "model" && k != "prompt" && k != "language" {
			tr.Fields[k] = v
		}
	}
	return tr, nil
}

// readPart reads a form's file part whole and closes it.
func readPart(f multipart.File) ([]byte, error) {
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
