package aitest

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"math"
	"net/http"
	"strings"
)

// Reply is what a route function answers with. Only the package's own reply types are Replies:
// [ChatReply], [EmbeddingsReply], [TokenizeReply], [TranscriptionReply], [ModelsReply],
// [ErrorReply], and [RawReply]. A typed reply is the wire shape of its route, and is written as
// that shape whichever route returns it.
type Reply interface {
	write(ctx context.Context, w http.ResponseWriter, a asked)
}

type asked struct {
	arrival             int
	model               string
	stream, streamUsage bool
}

// EmbeddingsReply is an embeddings route's reply: one vector per input, as encoding_format
// base64 shapes it, with the request's usage.
type EmbeddingsReply struct {
	// Vectors holds each embedding, Vectors[i] at index i, each sent as standard base64 of
	// little-endian float32s.
	Vectors [][]float32
	// PromptTokens is the reply's usage.prompt_tokens.
	PromptTokens int
	// TotalTokens is the reply's usage.total_tokens.
	TotalTokens int
}

func (r EmbeddingsReply) write(_ context.Context, w http.ResponseWriter, _ asked) {
	type item struct {
		Object    string `json:"object"`
		Index     int    `json:"index"`
		Embedding string `json:"embedding"`
	}
	type usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
	body := struct {
		Object string `json:"object"`
		Data   []item `json:"data"`
		Usage  usage  `json:"usage"`
	}{Object: "list", Data: make([]item, len(r.Vectors)), Usage: usage{r.PromptTokens, r.TotalTokens}}
	for i, v := range r.Vectors {
		b := make([]byte, 4*len(v))
		for j, f := range v {
			binary.LittleEndian.PutUint32(b[4*j:], math.Float32bits(f))
		}
		body.Data[i] = item{Object: "embedding", Index: i, Embedding: base64.StdEncoding.EncodeToString(b)}
	}
	writeJSON(w, http.StatusOK, body)
}

// TokenizeReply is a tokenize route's reply, as vLLM shapes it: the count, the model's most
// tokens, and the token ids, with token_strs null.
type TokenizeReply struct {
	// Count is the reply's count, written as given, so a test can make it disagree with Tokens.
	Count int
	// MaxModelLen is the reply's max_model_len; zero writes none.
	MaxModelLen int
	// Tokens is the reply's tokens, the token ids; nil writes none.
	Tokens []int
}

func (r TokenizeReply) write(_ context.Context, w http.ResponseWriter, _ asked) {
	writeJSON(w, http.StatusOK, struct {
		Count       int       `json:"count"`
		MaxModelLen int       `json:"max_model_len,omitzero"`
		Tokens      []int     `json:"tokens,omitzero"`
		TokenStrs   *[]string `json:"token_strs"`
	}{r.Count, r.MaxModelLen, r.Tokens, nil})
}

// TranscriptionReply is a transcription route's reply, as response_format json shapes it,
// whatever format the request asked for.
type TranscriptionReply struct {
	// Text is the transcript.
	Text string
	// InputTokens is the reply's usage.input_tokens.
	InputTokens int
	// OutputTokens is the reply's usage.output_tokens.
	OutputTokens int
	// TotalTokens is the reply's usage.total_tokens.
	TotalTokens int
}

func (r TranscriptionReply) write(_ context.Context, w http.ResponseWriter, _ asked) {
	type usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	}
	writeJSON(w, http.StatusOK, struct {
		Text  string `json:"text"`
		Usage usage  `json:"usage"`
	}{r.Text, usage{r.InputTokens, r.OutputTokens, r.TotalTokens}})
}

// ModelsReply is a models route's reply: the model list of Entries, in order.
type ModelsReply struct {
	// Entries are the list's entries, each a whole JSON object, such as
	// {"id":"m","object":"model"}, written as given.
	Entries []jsontext.Value
}

func (r ModelsReply) write(_ context.Context, w http.ResponseWriter, _ asked) {
	var b strings.Builder
	b.WriteString(`{"object":"list","data":[`)
	for i, e := range r.Entries {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(e)
	}
	b.WriteString(`]}`)
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, b.String())
}

// RawReply is a reply written as given: its status, its headers, and either a JSON body or a
// stream of server-sent events.
type RawReply struct {
	// Status is the reply's status code; zero is 200.
	Status int
	// Header holds the reply's headers. Without a Content-Type, the reply's is application/json,
	// or text/event-stream when Frames is non-nil.
	Header http.Header
	// Body is the reply's body, written as given when Frames is nil.
	Body []byte
	// Frames, when non-nil, makes the reply a stream of server-sent events, each frame written
	// and flushed in turn, Body unused. An empty, non-nil Frames is a stream of no events.
	Frames []Frame
	// Hold, when not nil, holds the stream of Frames once its first HoldAfter frames are
	// written and flushed, so the client has read them, until the test lets it go with
	// [Gate.Release] or [Gate.Open]; the stream then resumes. If the request's context ends
	// while the stream is held, as it does when the client cancels or closes the connection and
	// when the test ends, the stream ends there. A reply with Frames nil ignores it.
	Hold *Gate
	// HoldAfter is how many frames a held stream writes before Hold holds it: zero holds it
	// before the first, once its headers are flushed, and a count past the last frame holds it
	// after the last, before the body ends.
	HoldAfter int
}

// Frame is one server-sent event of a [RawReply].
type Frame struct {
	// Event is the event's type, written as its event field; empty writes none.
	Event string
	// Data is the event's data, written as given, a data field for each of its lines.
	Data string
}

func (r RawReply) write(ctx context.Context, w http.ResponseWriter, _ asked) {
	for k, vs := range r.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		ct := "application/json"
		if r.Frames != nil {
			ct = "text/event-stream"
		}
		w.Header().Set("Content-Type", ct)
	}
	status := r.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if r.Frames == nil {
		_, _ = w.Write(r.Body)
		return
	}
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		return
	}
	holdAt := min(max(r.HoldAfter, 0), len(r.Frames))
	for i, f := range r.Frames {
		if i == holdAt && !hold(ctx, r.Hold) {
			return
		}
		if _, err := io.WriteString(w, f.encode()); err != nil {
			return
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
	if holdAt == len(r.Frames) {
		hold(ctx, r.Hold)
	}
}

func hold(ctx context.Context, g *Gate) bool {
	return g == nil || g.Wait(ctx) == nil
}

func (f Frame) encode() string {
	var b strings.Builder
	if f.Event != "" {
		b.WriteString("event: " + f.Event + "\n")
	}
	for line := range strings.SplitSeq(f.Data, "\n") {
		b.WriteString("data: " + line + "\n")
	}
	b.WriteString("\n")
	return b.String()
}

// ErrorReply is an error reply on any route: an OpenAI-style error body,
// {"error":{"message":...}}, of any status, with a Retry-After header when RetryAfter is set.
type ErrorReply struct {
	// Status is the reply's status code; zero is 500.
	Status int
	// Message is the error's message.
	Message string
	// RetryAfter, when not empty, is the reply's Retry-After header, written exactly as given:
	// seconds such as "7", an HTTP date, or a value no client can read.
	RetryAfter string
}

func (r ErrorReply) write(_ context.Context, w http.ResponseWriter, _ asked) {
	status := r.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}
	if r.RetryAfter != "" {
		w.Header().Set("Retry-After", r.RetryAfter)
	}
	writeError(w, status, "", r.Message)
}

func writeError(w http.ResponseWriter, status int, typ, message string) {
	type detail struct {
		Message string `json:"message"`
		Type    string `json:"type,omitzero"`
	}
	writeJSON(w, status, struct {
		Error detail `json:"error"`
	}{detail{message, typ}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	body := mustMarshal(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic("aitest: a reply shape, all strings, integers, and their structs, failed to encode: " + err.Error())
	}
	return b
}
