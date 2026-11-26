package ai_test

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
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
	"github.com/standards-lab/go-ai/protocol"
)

//go:embed format/completions/testdata/vllm/input_audio.json
var audioReply string

func tone(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("format/completions/testdata/vllm/tone.wav")
	if err != nil {
		t.Fatalf("read clip: %v", err)
	}
	return b
}

type opens struct {
	opened atomic.Int32
	closed atomic.Int32
}

type counted struct {
	io.Reader
	o *opens
}

func (c counted) Close() error {
	c.o.closed.Add(1)
	return nil
}

func reopenCounting(data []byte, o *opens) func(context.Context) (io.ReadCloser, error) {
	return func(context.Context) (io.ReadCloser, error) {
		o.opened.Add(1)
		return counted{Reader: bytes.NewReader(data), o: o}, nil
	}
}

func reopen(data []byte) func(context.Context) (io.ReadCloser, error) {
	return reopenCounting(data, &opens{})
}

func chatClient(t *testing.T, base string, opts ai.Options) *ai.Client {
	t.Helper()
	opts.TranscriptionMode = ai.TranscribeChat
	return newClient(t, base, opts)
}

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

const (
	cardFormatting = "\n\nFollow these specific instructions for formatting the answer:\n" +
		"* Only output the transcription, with no newlines.\n" +
		"* When transcribing numbers, write the digits, i.e. write 1.7 and not one point seven, and write 3 instead of three."
	noLanguageInstruction = "Transcribe the following speech segment in its original language into text." + cardFormatting
	englishInstruction    = "Transcribe the following speech segment in English into English text." + cardFormatting
)

func TestTranscriptionInstruction(t *testing.T) {
	cases := map[string]string{
		"":        noLanguageInstruction,
		"English": englishInstruction,
		"fr":      "Transcribe the following speech segment in fr into fr text." + cardFormatting,
	}
	for language, want := range cases {
		if got := ai.TranscriptionInstruction(language); got != want {
			t.Errorf("TranscriptionInstruction(%q) = %q, want %q", language, got, want)
		}
	}
}

func TestNewRefusesTranscriptionMode(t *testing.T) {
	c, err := ai.New("http://h/v1", http.DefaultClient, completions.New(completions.Options{}), ai.Options{TranscriptionMode: 2})
	if err == nil || !strings.Contains(err.Error(), "unknown transcription mode 2") || !strings.HasPrefix(err.Error(), "ai: ") {
		t.Errorf("New = %v, %v; want an error naming the unknown transcription mode", c, err)
	}
}

func TestClientTranscriptionMode(t *testing.T) {
	for _, want := range []ai.TranscriptionMode{ai.TranscribeRoute, ai.TranscribeChat} {
		opts := ai.Options{}
		if want != ai.TranscribeRoute {
			opts.TranscriptionMode = want
		}
		c, err := ai.New("http://h/v1", http.DefaultClient, completions.New(completions.Options{}), opts)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if got := c.TranscriptionMode(); got != want {
			t.Errorf("TranscriptionMode() = %v, want %v", got, want)
		}
	}
}

func TestTranscriptionModeText(t *testing.T) {
	for mode, name := range map[ai.TranscriptionMode]string{ai.TranscribeRoute: "route", ai.TranscribeChat: "chat"} {
		if got := mode.String(); got != name {
			t.Errorf("TranscriptionMode(%d).String() = %q, want %q", int(mode), got, name)
		}
		text, err := mode.MarshalText()
		if err != nil || string(text) != name {
			t.Errorf("TranscriptionMode(%d).MarshalText() = %q, %v; want %q", int(mode), text, err, name)
		}
		got := ai.TranscriptionMode(-1)
		if err := got.UnmarshalText([]byte(name)); err != nil || got != mode {
			t.Errorf("UnmarshalText(%q) = %d, %v; want %d", name, int(got), err, int(mode))
		}
	}
}

func TestTranscriptionModeTextRefusesUnknown(t *testing.T) {
	for _, mode := range []ai.TranscriptionMode{2, 7, -1} {
		want := fmt.Sprintf("TranscriptionMode(%d)", int(mode))
		if got := mode.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
		if text, err := mode.MarshalText(); err == nil || !strings.HasPrefix(err.Error(), "ai: ") {
			t.Errorf("TranscriptionMode(%d).MarshalText() = %q, %v; want an error", int(mode), text, err)
		}
	}
	for _, name := range []string{"", "Route", "CHAT", "0", "routes"} {
		mode := ai.TranscribeChat
		err := mode.UnmarshalText([]byte(name))
		want := fmt.Sprintf("ai: unknown transcription mode %q, want one of route, chat", name)
		if err == nil || err.Error() != want {
			t.Errorf("UnmarshalText(%q) = %v, want %q", name, err, want)
		}
		if mode != ai.TranscribeChat {
			t.Errorf("UnmarshalText(%q) changed the mode to %v", name, mode)
		}
	}
}

func TestTranscriptionModeJSON(t *testing.T) {
	type config struct {
		Mode ai.TranscriptionMode `json:"mode"`
	}
	for mode, want := range map[ai.TranscriptionMode]string{
		ai.TranscribeRoute: `{"mode":"route"}`,
		ai.TranscribeChat:  `{"mode":"chat"}`,
	} {
		data, err := json.Marshal(config{Mode: mode})
		if err != nil || string(data) != want {
			t.Fatalf("Marshal = %s, %v; want %s", data, err, want)
		}
		var got config
		if err := json.Unmarshal(data, &got); err != nil || got.Mode != mode {
			t.Errorf("Unmarshal(%s) = %v, %v; want %v", data, got.Mode, err, mode)
		}
	}
	var got config
	if err := json.Unmarshal([]byte(`{"mode":"speech"}`), &got); err == nil {
		t.Errorf("Unmarshal of an unknown mode = %v, want an error", got.Mode)
	}
}

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
		{"default instruction", "tone.wav", "", "", "wav", noLanguageInstruction},
		{"default instruction naming the language", "tone.wav", "English", "", "wav", englishInstruction},
		{"prompt in place of the instruction", "tone.wav", "English", "Write down what is said.", "wav",
			"Write down what is said."},
		{"mp3 extension in upper case", "dir/TONE.MP3", "", "", "mp3", noLanguageInstruction},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := serveChat(t, http.StatusOK, audioReply)
			c := chatClient(t, srv.URL(), ai.Options{
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
			got := aitest.Only(t, srv)
			if got.Method != http.MethodPost || got.Path != "/v1/chat/completions" || got.Query.Encode() != "autoload=false" {
				t.Errorf("request = %s %s?%s, want POST /v1/chat/completions?autoload=false", got.Method, got.Path, got.Query.Encode())
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

func TestTranscribeChatMaxOutputTokens(t *testing.T) {
	for _, bound := range []int{0, 1, 256} {
		t.Run(fmt.Sprint(bound), func(t *testing.T) {
			srv := serveChat(t, http.StatusOK, audioReply)
			c := chatClient(t, srv.URL(), ai.Options{})
			_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
				Model: "m", Open: reopen(tone(t)), FileName: "tone.wav", MaxOutputTokens: bound,
			})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			var body struct {
				MaxCompletionTokens *int `json:"max_completion_tokens"`
				MaxTokens           *int `json:"max_tokens"`
			}
			got := aitest.Only(t, srv)
			if err := json.Unmarshal(got.Body, &body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			if body.MaxTokens != nil {
				t.Errorf("body = %s, want no max_tokens", got.Body)
			}
			switch {
			case bound == 0 && body.MaxCompletionTokens != nil:
				t.Errorf("body = %s, want no max_completion_tokens", got.Body)
			case bound != 0 && (body.MaxCompletionTokens == nil || *body.MaxCompletionTokens != bound):
				t.Errorf("body = %s, want max_completion_tokens %d", got.Body, bound)
			}
		})
	}
}

func TestTranscribeChatNative(t *testing.T) {
	cases := []struct {
		name   string
		native jsontext.Value
		sent   bool
	}{
		{"none", nil, false},
		{"temperature", jsontext.Value(`{"temperature":0}`), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := serveChat(t, http.StatusOK, audioReply)
			c := chatClient(t, srv.URL(), ai.Options{})
			_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
				Model: "m", Open: reopen(tone(t)), FileName: "tone.wav", ChatNative: tc.native,
			})
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			got := aitest.Only(t, srv)
			var body map[string]jsontext.Value
			if err := json.Unmarshal(got.Body, &body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			temperature, ok := body["temperature"]
			switch {
			case !tc.sent && ok:
				t.Errorf("body = %s, want no temperature", got.Body)
			case tc.sent && (!ok || string(temperature) != "0"):
				t.Errorf("body = %s, want temperature 0", got.Body)
			}
		})
	}
}

func TestTranscribeChatReply(t *testing.T) {
	srv := serveChat(t, http.StatusOK, audioReply)
	c := chatClient(t, srv.URL(), ai.Options{})
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

func TestTranscribeChatIncomplete(t *testing.T) {
	cases := []struct {
		finish string
		status protocol.Status
		reason protocol.IncompleteReason
		want   string
	}{
		{"stop", protocol.StatusCompleted, "", ""},
		{"length", protocol.StatusIncomplete, protocol.ReasonMaxOutputTokens,
			"ai: transcribe: transcript incomplete (status incomplete, reason max_output_tokens): the reply " +
				"stopped at its output token bound before the transcript ended; send a shorter clip, or raise " +
				"the request's MaxOutputTokens or, when it sets none, the engine's default bound"},
		{"content_filter", protocol.StatusIncomplete, protocol.ReasonContentFilter,
			"ai: transcribe: transcript incomplete (status incomplete, reason content_filter): the endpoint's " +
				"content filter stopped the reply before the transcript ended; the clip can't be transcribed " +
				"whole through this endpoint's filter"},
	}
	for _, tc := range cases {
		t.Run(tc.finish, func(t *testing.T) {
			reply := strings.Replace(audioReply, `"finish_reason":"stop"`, `"finish_reason":"`+tc.finish+`"`, 1)
			srv := serveChat(t, http.StatusOK, reply)
			c := chatClient(t, srv.URL(), ai.Options{Retry: ai.RetryPolicy{MaxAttempts: 3}})
			got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{Model: "m", Open: reopen(tone(t)), FileName: "tone.wav"})
			if n := len(srv.Requests()); n != 1 {
				t.Errorf("server received %d requests, want 1", n)
			}
			if tc.status == protocol.StatusCompleted {
				if err != nil || got.Text != "It sounds like a gentle, rhythmic tapping." {
					t.Errorf("Transcribe = %+v, %v; want the reply's text", got, err)
				}
				return
			}
			if got != nil {
				t.Errorf("Transcribe = %+v, want no Transcription", got)
			}
			e, ok := errors.AsType[*ai.IncompleteTranscriptError](err)
			if !ok {
				t.Fatalf("Transcribe error = %v, want an *IncompleteTranscriptError", err)
			}
			if e.Status != tc.status || e.Reason != tc.reason {
				t.Errorf("IncompleteTranscriptError = %+v, want status %s, reason %s", *e, tc.status, tc.reason)
			}
			if e.Text != "It sounds like a gentle, rhythmic tapping." {
				t.Errorf("IncompleteTranscriptError.Text = %q, want the reply's content", e.Text)
			}
			if want := (ai.TranscriptionUsage{InputTokens: 44, OutputTokens: 10, TotalTokens: 54}); e.Usage != want {
				t.Errorf("IncompleteTranscriptError.Usage = %+v, want the reply's usage %+v", e.Usage, want)
			}
			if err.Error() != tc.want {
				t.Errorf("Transcribe error = %q, want %q", err, tc.want)
			}
		})
	}
}

func TestTranscribeChatIncompleteNoContent(t *testing.T) {
	reply := strings.Replace(audioReply, `"finish_reason":"stop"`, `"finish_reason":"length"`, 1)
	reply = strings.Replace(reply, `"content":"It sounds like a gentle, rhythmic tapping."`, `"content":null`, 1)
	srv := serveChat(t, http.StatusOK, reply)
	c := chatClient(t, srv.URL(), ai.Options{})
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{Model: "m", Open: reopen(tone(t)), FileName: "tone.wav"})
	if got != nil {
		t.Errorf("Transcribe = %+v, want no Transcription", got)
	}
	e, ok := errors.AsType[*ai.IncompleteTranscriptError](err)
	if !ok {
		t.Fatalf("Transcribe error = %v, want an *IncompleteTranscriptError", err)
	}
	want := ai.IncompleteTranscriptError{
		Status: protocol.StatusIncomplete,
		Reason: protocol.ReasonMaxOutputTokens,
		Usage:  ai.TranscriptionUsage{InputTokens: 44, OutputTokens: 10, TotalTokens: 54},
	}
	if *e != want {
		t.Errorf("IncompleteTranscriptError = %+v, want %+v", *e, want)
	}
}

func TestTranscribeChatIncompleteNoUsage(t *testing.T) {
	reply := strings.Replace(audioReply, `"finish_reason":"stop"`, `"finish_reason":"length"`, 1)
	reply = strings.Replace(reply,
		`"usage":{"prompt_tokens":44,"total_tokens":54,"completion_tokens":10,"prompt_tokens_details":null,"completion_tokens_details":{"reasoning_tokens":0}},`, ``, 1)
	srv := serveChat(t, http.StatusOK, reply)
	c := chatClient(t, srv.URL(), ai.Options{})
	_, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{Model: "m", Open: reopen(tone(t)), FileName: "tone.wav"})
	e, ok := errors.AsType[*ai.IncompleteTranscriptError](err)
	if !ok {
		t.Fatalf("Transcribe error = %v, want an *IncompleteTranscriptError", err)
	}
	if e.Usage != (ai.TranscriptionUsage{}) {
		t.Errorf("IncompleteTranscriptError.Usage = %+v, want zero for a reply without usage", e.Usage)
	}
}

func TestIncompleteTranscriptErrorText(t *testing.T) {
	cases := []struct {
		err  ai.IncompleteTranscriptError
		want string
	}{
		{ai.IncompleteTranscriptError{Status: protocol.StatusIncomplete},
			"transcript incomplete (status incomplete): the model stopped before the transcript ended"},
		{ai.IncompleteTranscriptError{Status: protocol.StatusIncomplete, Text: "one two two two"},
			"transcript incomplete (status incomplete): the model stopped before the transcript ended"},
		{ai.IncompleteTranscriptError{Status: protocol.StatusIncomplete, Reason: "tool_budget"},
			"transcript incomplete (status incomplete, reason tool_budget): the model stopped before the transcript ended"},
		{ai.IncompleteTranscriptError{Status: protocol.StatusFailed},
			"transcript incomplete (status failed): the endpoint reported that the reply failed; send the request again " +
				"or check the endpoint"},
		{ai.IncompleteTranscriptError{Status: protocol.StatusInProgress},
			"transcript incomplete (status in_progress): the reply isn't completed, so its text can't be taken as the " +
				"whole transcript"},
		{ai.IncompleteTranscriptError{},
			"transcript incomplete (status none): the reply isn't completed, so its text can't be taken as the whole " +
				"transcript"},
	}
	for _, tc := range cases {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("%+v.Error() = %q, want %q", tc.err, got, tc.want)
		}
	}
}

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
		{"ChatNative that isn't an object", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest {
			r.ChatNative = jsontext.Value(`[0]`)
			return r
		}, "native field isn't a JSON object"},
		{"ChatNative clashing with a typed field", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest {
			r.ChatNative = jsontext.Value(`{"temperature":0,"model":"other"}`)
			return r
		}, "native member model clashes"},
		{"ChatNative stream", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest {
			r.ChatNative = jsontext.Value(`{"stream":true}`)
			return r
		}, "native member stream clashes"},
		{"negative MaxOutputTokens", false, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest {
			r.MaxOutputTokens = -1
			return r
		}, "MaxOutputTokens -1 is negative"},
		{"a format that refuses audio", true, func(r *ai.TranscriptionRequest) *ai.TranscriptionRequest { return r },
			"messages: input 0: content 0: part protocol.InputAudio has no Messages form"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := serveChat(t, http.StatusOK, audioReply)
			called := false
			opts := ai.Options{
				TranscriptionMode: ai.TranscribeChat,
				Token:             func(context.Context) (string, error) { called = true; return "", nil },
			}
			var wireFormat format.Format = completions.New(completions.Options{})
			if tc.messages {
				wireFormat = messages.New(messages.Options{})
			}
			c, err := ai.New(srv.URL(), http.DefaultClient, wireFormat, opts)
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
			if tc.messages {
				aitest.AssertUnsupported(t, err, format.UnsupportedError{
					Format: "messages", Feature: "content part", Value: "protocol.InputAudio",
				}, "ai: transcribe: "+tc.want)
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

func TestTranscribeChatEmptyClip(t *testing.T) {
	srv := serveChat(t, http.StatusOK, audioReply)
	c := chatClient(t, srv.URL(), ai.Options{})
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

func TestTranscribeChatAPIError(t *testing.T) {
	reply := `{"error":{"message":"The model does not exist.","type":"NotFoundError","param":"model","code":404}}`
	srv := serveChat(t, http.StatusNotFound, reply)
	c := chatClient(t, srv.URL(), ai.Options{})
	got, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{Model: "m", Open: reopen(tone(t)), FileName: "tone.wav"})
	apiErr, ok := errors.AsType[*ai.APIError](err)
	if !ok {
		t.Fatalf("Transcribe = %+v, %v; want an *APIError", got, err)
	}
	if apiErr.Status != http.StatusNotFound || apiErr.Message != "The model does not exist." || apiErr.Code != "404" {
		t.Errorf("APIError = %+v, want 404 with the body's message and code", apiErr)
	}
}

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
