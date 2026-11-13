package apptest

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/lifecycle"

	"github.com/standards-lab/go-ai/tools/infer/internal/app"
)

// Recorder records events of type T in order. It is safe for concurrent
// use, as an endpoint's handler records from its own goroutine. The zero
// value is ready to use.
type Recorder[T any] struct {
	mu     sync.Mutex
	events []T
}

// Record appends event.
func (r *Recorder[T]) Record(event T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

// Log returns a copy of the events recorded so far.
func (r *Recorder[T]) Log() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.events)
}

// Builds returns a Recorder of the handle of each node a's runs build, in
// the order each Build begins them: depth-first from the roots, each node
// once per Build, a node whose constructor fails included. A run that
// builds nothing records nothing. Call it before a runs: the graph calls
// only the observers it has when a Build begins.
func Builds(a *app.App) *Recorder[graph.Ref] {
	r := &Recorder[graph.Ref]{}
	a.Graph().Observe(r.Record)
	return r
}

// Names returns the name of each node in refs, in order, as each handle's
// Name reports it, so a test compares what a run built with the names the
// dispatcher labels its errors with.
func Names(refs []graph.Ref) []string {
	names := make([]string, len(refs))
	for i, ref := range refs {
		names[i] = ref.Name()
	}
	return names
}

// ErrHalted is what a run of an App given to [Halt] fails with, labelled
// "lifecycle config".
var ErrHalted = errors.New("halted")

// Halt makes a's lifecycle configuration node fail with ErrHalted. The
// dispatcher adds that node to every Build of a command that declares
// nodes, so such a run fails its Build before anything starts, and a run
// whose constructors do no I/O does none at all. A run that builds nothing,
// such as help or version, is unaffected. Halt is a Replace, so it panics
// once a has run a Build.
func Halt(a *app.App) {
	a.Graph().Replace(a.Nodes().LifecycleConfig, func(*graph.Scope) (lifecycle.Config, error) {
		return lifecycle.Config{}, ErrHalted
	})
}

// envPrefix opens the name of every variable infer reads.
const envPrefix = "INFER_"

// Env gives the test the environment a run reads: every INFER_ variable the
// test inherited is set empty, which infer reads as unset, and then each of
// vars, a name and its value, is set. The variables are restored when the
// test ends. Like testing.T.Setenv, it can't be used in a parallel test.
func Env(t testing.TB, vars map[string]string) {
	t.Helper()
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, envPrefix) {
			t.Setenv(name, "")
		}
	}
	for name, value := range vars {
		t.Setenv(name, value)
	}
}

// Request is what an [Endpoint] received in one request.
type Request struct {
	Method string
	Path   string // the URL path, such as /v1/models
	Header http.Header
	Body   []byte
}

// Endpoint is a loopback model endpoint for a run to send to: an
// httptest.Server that records each request, then answers it with the
// handler the test gave.
type Endpoint struct {
	// URL is the endpoint's API root, the server's URL with /v1, as a run's
	// base URL names it.
	URL string

	requests Recorder[Request]
}

// Serve starts an [Endpoint] that answers with h, and closes it when the
// test ends. A request whose body can't be read fails the test.
func Serve(t testing.TB, h http.Handler) *Endpoint {
	t.Helper()
	e := &Endpoint{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("endpoint: read %s %s: %v", r.Method, r.URL.Path, err)
		}
		e.requests.Record(Request{Method: r.Method, Path: r.URL.Path, Header: r.Header.Clone(), Body: body})
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	e.URL = srv.URL + "/v1"
	return e
}

// Requests returns a copy of the requests e received so far, in order.
func (e *Endpoint) Requests() []Request { return e.requests.Log() }

// Models returns a handler that answers GET /v1/models with the
// OpenAI-compatible model list of ids, in order, and any other request
// with 404.
func Models(ids ...string) http.Handler {
	type model struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	list := struct {
		Object string  `json:"object"`
		Data   []model `json:"data"`
	}{Object: "list", Data: []model{}}
	for _, id := range ids {
		list.Data = append(list.Data, model{ID: id, Object: "model"})
	}
	body, err := json.Marshal(list)
	if err != nil {
		panic(err) // a list of strings always marshals
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

// Embeddings returns a handler that answers POST /v1/embeddings with the
// OpenAI-compatible embeddings reply of vectors, vectors[i] at index i, as
// encoding_format base64 shapes it, and usage of promptTokens and
// totalTokens; it answers any other request with 404. It replies with
// vectors whatever the request's input, so a test gives one vector per
// input it sends.
func Embeddings(promptTokens, totalTokens int, vectors ...[]float32) http.Handler {
	type item struct {
		Object    string `json:"object"`
		Index     int    `json:"index"`
		Embedding string `json:"embedding"`
	}
	type usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
	reply := struct {
		Object string `json:"object"`
		Data   []item `json:"data"`
		Usage  usage  `json:"usage"`
	}{Object: "list", Data: []item{}, Usage: usage{PromptTokens: promptTokens, TotalTokens: totalTokens}}
	for i, v := range vectors {
		b := make([]byte, 4*len(v))
		for j, f := range v {
			binary.LittleEndian.PutUint32(b[4*j:], math.Float32bits(f))
		}
		reply.Data = append(reply.Data, item{Object: "embedding", Index: i, Embedding: base64.StdEncoding.EncodeToString(b)})
	}
	body, err := json.Marshal(reply)
	if err != nil {
		panic(err) // strings and integers always marshal
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

// Reply returns a handler that answers POST /v1/<route>, such as
// /v1/chat/completions, with body as application/json, whatever the
// request, and any other request with 404. A test writes body in the wire
// shape of the format the run speaks.
func Reply(route, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/"+route {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})
}

// Stream returns a handler that answers POST /v1/<route> as
// text/event-stream, whatever the request, writing each of frames, in
// order, as one server-sent event's data and flushing it before the next,
// and any other request with 404. A frame is the event's data alone, which
// is all a go-ai format decodes, such as a Chat Completions chunk or its
// closing [DONE]; a frame holds no newline.
func Stream(route string, frames ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/"+route {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		rc := http.NewResponseController(w)
		for _, f := range frames {
			if _, err := io.WriteString(w, "data: "+f+"\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	})
}
