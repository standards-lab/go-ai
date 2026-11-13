package ai_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/internal/aitest"
)

// audioReply is vLLM 0.33.0's Chat Completions reply to Gemma 4 E4B given tone.wav as an
// input_audio part, as format/completions recorded it.
//
//go:embed format/completions/testdata/vllm/input_audio.json
var audioReply string

// tone reads the clip audioReply answers, one second of a 440 Hz sine as WAV, failing t when it
// can't.
func tone(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("format/completions/testdata/vllm/tone.wav")
	if err != nil {
		t.Fatalf("read clip: %v", err)
	}
	return b
}

// opens counts the calls of an Open and the closes of what it returned.
type opens struct {
	opened atomic.Int32
	closed atomic.Int32
}

// counted is a ReadCloser whose Close the opens count.
type counted struct {
	io.Reader
	o *opens
}

func (c counted) Close() error {
	c.o.closed.Add(1)
	return nil
}

// reopenCounting returns an Open that gives a new reader of data on every call, each call and
// each close counted in o.
func reopenCounting(data []byte, o *opens) func(context.Context) (io.ReadCloser, error) {
	return func(context.Context) (io.ReadCloser, error) {
		o.opened.Add(1)
		return counted{Reader: bytes.NewReader(data), o: o}, nil
	}
}

// reopen returns an Open that gives a new reader of data on every call.
func reopen(data []byte) func(context.Context) (io.ReadCloser, error) {
	return reopenCounting(data, &opens{})
}

// chatClient returns a Client of base speaking Chat Completions that transcribes in chat mode,
// adjusted by opts.
func chatClient(t *testing.T, base string, opts ai.Options) *ai.Client {
	t.Helper()
	opts.TranscriptionMode = ai.TranscribeChat
	return newClient(t, base, opts)
}

// chatBody is the part of a Chat Completions request body a chat-mode Transcribe sets.
type chatBody struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content []struct {
			Type       string `json:"type"`
			Text       string `json:"text"`
			InputAudio struct {
				Data   string `json:"data"`
				Format string `json:"format"`
			} `json:"input_audio"`
		} `json:"content"`
	} `json:"messages"`
}

func TestTranscriptionInstruction(t *testing.T) {
	cases := map[string]string{
		"":        "Transcribe the following speech segment into text. Only output the transcription, with no newlines.",
		"English": "Transcribe the following speech segment in English into English text. Only output the transcription, with no newlines.",
		"fr":      "Transcribe the following speech segment in fr into fr text. Only output the transcription, with no newlines.",
	}
	for language, want := range cases {
		if got := ai.TranscriptionInstruction(language); got != want {
			t.Errorf("TranscriptionInstruction(%q) = %q, want %q", language, got, want)
		}
	}
}

func TestNewRefusesTranscriptionMode(t *testing.T) {
	c, err := ai.New("http://h/v1", http.DefaultClient, completions.New(), ai.Options{TranscriptionMode: 2})
	if err == nil || !strings.Contains(err.Error(), "unknown transcription mode 2") || !strings.HasPrefix(err.Error(), "ai: ") {
		t.Errorf("New = %v, %v; want an error naming the unknown transcription mode", c, err)
	}
}

// TestTranscribeChatRequest wants one Chat Completions request to the format's route: the
// model, and one user message of the clip as an input_audio part, in the format the file name's
// extension gives, then the instruction, Prompt or else TranscriptionInstruction's of Language.
func TestTranscribeChatRequest(t *testing.T) {
	clip := tone(t)
	cases := []struct {
		name        string
		fileName    string
		language    string
		prompt      string
		format      string
		instruction string
	}{
		{"default instruction", "tone.wav", "", "", "wav",
			"Transcribe the following speech segment into text. Only output the transcription, with no newlines."},
		{"default instruction naming the language", "tone.wav", "English", "", "wav",
			"Transcribe the following speech segment in English into English text. Only output the transcription, with no newlines."},
		{"prompt in place of the instruction", "tone.wav", "English", "Write down what is said.", "wav",
			"Write down what is said."},
		{"mp3 extension in upper case", "dir/TONE.MP3", "", "", "mp3",
			"Transcribe the following speech segment into text. Only output the transcription, with no newlines."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, audioReply)
			c := chatClient(t, srv.URL+"/v1", ai.Options{
				Query: url.Values{"autoload": {"false"}},
				Token: func(context.Context) (string, error) { return "secret", nil },
			})
			_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
				Model: "google/gemma-4-E4B-it", Open: reopen(clip), FileName: tc.fileName,
				Language: tc.language, Prompt: tc.prompt,
			})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			got := srv.Only(t)
			if got.Method != http.MethodPost || got.Path != "/v1/chat/completions" || got.RawQuery != "autoload=false" {
				t.Errorf("request = %s %s?%s, want POST /v1/chat/completions?autoload=false", got.Method, got.Path, got.RawQuery)
			}
			for k, want := range map[string]string{"Content-Type": "application/json", "Authorization": "Bearer secret"} {
				if v := got.Header.Get(k); v != want {
					t.Errorf("%s = %q, want %q", k, v, want)
				}
			}
			var body chatBody
			if err := json.Unmarshal(got.Body, &body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if body.Model != "google/gemma-4-E4B-it" || len(body.Messages) != 1 || body.Messages[0].Role != "user" ||
				len(body.Messages[0].Content) != 2 {
				t.Fatalf("body = %s, want the model and one user message of two parts", got.Body)
			}
			audio, text := body.Messages[0].Content[0], body.Messages[0].Content[1]
			if audio.Type != "input_audio" || audio.InputAudio.Format != tc.format ||
				audio.InputAudio.Data != base64.StdEncoding.EncodeToString(clip) {
				t.Errorf("part 0 = %s %s with %d characters of data, want input_audio %s of the clip",
					audio.Type, audio.InputAudio.Format, len(audio.InputAudio.Data), tc.format)
			}
			if text.Type != "text" || text.Text != tc.instruction {
				t.Errorf("part 1 = %s %q, want text %q", text.Type, text.Text, tc.instruction)
			}
		})
	}
}

// TestTranscribeChatReply wants the reply's output text and usage, from vLLM's recorded reply.
func TestTranscribeChatReply(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, audioReply)
	c := chatClient(t, srv.URL, ai.Options{})
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{Model: "m", Open: reopen(tone(t)), FileName: "tone.wav"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := ai.Transcription{
		Text:  "It sounds like a gentle, rhythmic tapping.",
		Usage: ai.TranscriptionUsage{InputTokens: 44, OutputTokens: 10, TotalTokens: 54},
	}
	if *got != want {
		t.Errorf("Transcribe = %+v, want %+v", *got, want)
	}
}

// TestTranscribeChatRefusesRequest covers what chat mode refuses, and when: before the clip is
// opened, the token source is called, or anything is sent.
func TestTranscribeChatRefusesRequest(t *testing.T) {
	ok := func() *ai.TranscriptionRequest {
		return &ai.TranscriptionRequest{Model: "m", FileName: "clip.wav"}
	}
	cases := []struct {
		name     string
		messages bool
		req      func(*ai.TranscriptionRequest) *ai.TranscriptionRequest
		want     string
	}{
		{"nil", false, func(*ai.TranscriptionRequest) *ai.TranscriptionRequest { return nil }, "no request"},
		{"no model", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest { r.Model = ""; return r }, "no model"},
		{"no Open", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest { r.Open = nil; return r }, "no Open"},
		{"no file name", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest { r.FileName = ""; return r },
			"no file name"},
		{"unknown extension", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest { r.FileName = "clip.ogg"; return r },
			"no audio extension"},
		{"no extension", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest { r.FileName = "clip"; return r },
			"no audio extension"},
		{"native fields", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest {
			r.Native = url.Values{"temperature": {"0"}}
			return r
		}, "takes no native fields"},
		// Messages refuses the audio, ahead of the max_tokens the request has none of, which
		// the caller can't set.
		{"a format that refuses audio", true, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest { return r },
			"messages: input 0: content 0: part protocol.InputAudio has no Messages form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusOK, audioReply)
			called := false
			opts := ai.Options{
				TranscriptionMode: ai.TranscribeChat,
				Token:             func(context.Context) (string, error) { called = true; return "", nil },
			}
			var wireFormat format.Format = completions.New()
			if tc.messages {
				wireFormat = messages.New()
			}
			c, err := ai.New(srv.URL, http.DefaultClient, wireFormat, opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			var o opens
			r := ok()
			r.Open = reopenCounting(tone(t), &o)
			got, err := c.Transcribe(t.Context(), tc.req(r))
			if err == nil {
				t.Fatalf("Transcribe = %+v, nil; want an error containing %q", got, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "ai: transcribe: ") {
				t.Errorf("Transcribe error = %q, want it to start ai: transcribe: and contain %q", err, tc.want)
			}
			if called {
				t.Error("the token source was called")
			}
			if n := o.opened.Load(); n != 0 {
				t.Errorf("the clip was opened %d times, want 0", n)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

// TestTranscribeChatEmptyClip wants an empty clip, which protocol.Request.Validate refuses,
// refused once it is read, the clip closed, and nothing sent.
func TestTranscribeChatEmptyClip(t *testing.T) {
	srv := aitest.NewServer(t, http.StatusOK, audioReply)
	c := chatClient(t, srv.URL, ai.Options{})
	var o opens
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{Model: "m", Open: reopenCounting(nil, &o), FileName: "clip.wav"})
	if err == nil || !strings.Contains(err.Error(), "no data") {
		t.Errorf("Transcribe = %+v, %v; want an error that the clip has no data", got, err)
	}
	if o.opened.Load() != 1 || o.closed.Load() != 1 {
		t.Errorf("clip opened %d times and closed %d, want once each", o.opened.Load(), o.closed.Load())
	}
	if n := len(srv.Requests()); n != 0 {
		t.Errorf("server received %d requests, want 0", n)
	}
}

// TestTranscribeChatAPIError wants a non-2xx reply decoded by the Client's format.
func TestTranscribeChatAPIError(t *testing.T) {
	reply := `{"error":{"message":"The model does not exist.","type":"NotFoundError","param":"model","code":404}}`
	srv := aitest.NewServer(t, http.StatusNotFound, reply)
	c := chatClient(t, srv.URL, ai.Options{})
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{Model: "m", Open: reopen(tone(t)), FileName: "tone.wav"})
	apiErr, ok := errors.AsType[*ai.APIError](err)
	if !ok {
		t.Fatalf("Transcribe = %+v, %v; want an *APIError", got, err)
	}
	if apiErr.Status != http.StatusNotFound || apiErr.Message != "The model does not exist." || apiErr.Code != "404" {
		t.Errorf("APIError = %+v, want 404 with the body's message and code", apiErr)
	}
}

// TestTranscribeOpensPerAttempt has each mode answered 503, then 2xx, under a policy, and wants
// the clip opened once per attempt, each closed by the time Transcribe returns, and the same
// body sent each time.
func TestTranscribeOpensPerAttempt(t *testing.T) {
	modes := []struct {
		name string
		mode ai.TranscriptionMode
		ok   string
	}{
		{"route", ai.TranscribeRoute, transcribeReply},
		{"chat", ai.TranscribeChat, audioReply},
	}
	for _, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c, s := retryingIn(t, m.mode, policy, nil, step{status: http.StatusServiceUnavailable},
					step{status: http.StatusBadGateway}, step{status: http.StatusOK, body: m.ok})
				var o opens
				_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
					Model: "m", Open: reopenCounting(clip(64<<10), &o), FileName: "clip.wav",
				})
				if err != nil {
					t.Fatalf("Transcribe: %v", err)
				}
				if len(s.sent) != 3 {
					t.Fatalf("%d requests sent, want 3", len(s.sent))
				}
				if o.opened.Load() != 3 || o.closed.Load() != 3 {
					t.Errorf("clip opened %d times and closed %d, want 3 each", o.opened.Load(), o.closed.Load())
				}
				if s.sent[0].body != s.sent[1].body || s.sent[1].body != s.sent[2].body {
					t.Error("the attempts sent different bodies, want the same body each time")
				}
			})
		})
	}
}

// TestTranscribeOpenError wants a clip that won't open returned at once under a policy, wrapped,
// after the token source and before anything is sent.
func TestTranscribeOpenError(t *testing.T) {
	for _, mode := range []ai.TranscriptionMode{ai.TranscribeRoute, ai.TranscribeChat} {
		synctest.Test(t, func(t *testing.T) {
			c, s := retryingIn(t, mode, policy, nil, step{status: http.StatusOK, body: okReply})
			errGone := errors.New("object not found")
			var opened int
			_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
				Model: "m", FileName: "clip.wav",
				Open: func(context.Context) (io.ReadCloser, error) { opened++; return nil, errGone },
			})
			if !errors.Is(err, errGone) || !strings.Contains(err.Error(), "open clip: ") {
				t.Errorf("mode %d: Transcribe error = %v, want an open clip error wrapping %v", mode, err, errGone)
			}
			if opened != 1 || len(s.sent) != 0 {
				t.Errorf("mode %d: clip opened %d times and %d requests sent, want 1 and 0", mode, opened, len(s.sent))
			}
		})
	}
}
