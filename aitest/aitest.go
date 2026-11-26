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

const (
	chatPath           = "/v1/chat/completions"
	embeddingsPath     = "/v1/embeddings"
	transcriptionsPath = "/v1/audio/transcriptions"
	modelsPath         = "/v1/models"
	tokenizePath       = "/tokenize"
	apiTokenizePath    = "/v1/tokenize"
)

const maxFormMemory = 32 << 20

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
	// Tokenize answers POST /tokenize, vLLM's tokenize route at the server's root, and POST
	// /v1/tokenize, the route a gateway forwards below its API root. Requests to the two paths
	// are numbered apart.
	Tokenize func(ctx context.Context, req TokenizeRequest) Reply
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
	// Input is the texts to embed, in order, as they arrived, any prefix the client added
	// included. A single string input is one text.
	Input []string
	// Native holds the body's other members, those beside model, input, and encoding_format, as
	// one JSON object in the order they arrived, such as {"dimensions":256}; empty when it has
	// none.
	Native jsontext.Value
}

// TokenizeRequest is a request to the tokenize route, decoded.
type TokenizeRequest struct {
	// Arrival is the request's place among the path's requests, from 1.
	Arrival int
	// Model is the request's model.
	Model string
	// Prompt is the text to tokenize, as it arrived, any prefix the client added included.
	Prompt string
	// AddSpecialTokens is the request's add_special_tokens, false when it has none.
	AddSpecialTokens bool
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
	t       testing.TB
	url     string
	routes  Routes
	testCtx context.Context

	mu       sync.Mutex
	reqs     []Request
	arrivals map[string]int
}

// Serve starts an [Endpoint] answering with routes. It is closed when t's test ends: every
// request's context is cancelled, then the server waits for its handlers and closes.
func Serve(t testing.TB, routes Routes) *Endpoint {
	t.Helper()
	for key := range routes.Raw {
		if p := "/v1/" + key; p == chatPath || p == embeddingsPath || p == transcriptionsPath || p == modelsPath || p == apiTokenizePath {
			t.Fatalf("aitest: Raw route %q is a typed route; give its function in Routes", key)
		}
	}
	e := &Endpoint{t: t, routes: routes, arrivals: map[string]int{}}
	var cancel context.CancelFunc
	e.testCtx, cancel = context.WithCancel(context.Background())
	hs := httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(func() {
		cancel()
		hs.Close()
	})
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
	defer context.AfterFunc(e.testCtx, cancel)()

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
	case (req.Path == tokenizePath || req.Path == apiTokenizePath) && req.Method == http.MethodPost && rt.Tokenize != nil:
		tr, err := decodeTokenizeRequest(req.Body)
		if err != nil {
			return nil, asked{}, true, err
		}
		tr.Arrival = e.arrive(req.Path)
		return rt.Tokenize(ctx, tr), asked{}, true, nil
	}
	key, ok := strings.CutPrefix(req.Path, "/v1/")
	if f := rt.Raw[key]; ok && f != nil {
		return f(ctx, RawRequest{Arrival: e.arrive(req.Path), Request: req}), asked{}, true, nil
	}
	return nil, asked{}, false, nil
}

func (e *Endpoint) arrive(path string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.arrivals[path]++
	return e.arrivals[path]
}

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
	other, err := otherMembers(body, "model", "input", "encoding_format")
	if err != nil {
		return EmbeddingRequest{}, fmt.Errorf("decode embeddings request: %w", err)
	}
	req.Native = other
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

func decodeTokenizeRequest(body []byte) (TokenizeRequest, error) {
	var wire struct {
		Model            string `json:"model"`
		Prompt           string `json:"prompt"`
		AddSpecialTokens bool   `json:"add_special_tokens"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return TokenizeRequest{}, fmt.Errorf("decode tokenize request: %w", err)
	}
	return TokenizeRequest{Model: wire.Model, Prompt: wire.Prompt, AddSpecialTokens: wire.AddSpecialTokens}, nil
}

func otherMembers(body []byte, typed ...string) (jsontext.Value, error) {
	d := jsontext.NewDecoder(bytes.NewReader(body))
	if _, err := d.ReadToken(); err != nil {
		return nil, err
	}
	var out jsontext.Value
	for d.PeekKind() != '}' {
		tok, err := d.ReadToken()
		if err != nil {
			return nil, err
		}
		name := tok.String()
		v, err := d.ReadValue()
		if err != nil {
			return nil, err
		}
		if slices.Contains(typed, name) {
			continue
		}
		if len(out) == 0 {
			out = jsontext.Value{'{'}
		} else {
			out = append(out, ',')
		}
		out, err = jsontext.AppendQuote(out, name)
		if err != nil {
			return nil, err
		}
		out = append(append(out, ':'), v...)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return append(out, '}'), nil
}

func decodeTranscriptionRequest(req Request) (TranscriptionRequest, error) {
	r, err := http.NewRequest(req.Method, "http://aitest"+req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return TranscriptionRequest{}, err
	}
	r.Header = req.Header
	if err := r.ParseMultipartForm(maxFormMemory); err != nil {
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

func readPart(f multipart.File) ([]byte, error) {
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}
