package aitest_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/protocol"
)

// chatClient returns a Client of e speaking Chat Completions, adjusted by opts. Its zero
// RetryPolicy, unless opts sets one, retries nothing, so an error reply is the call's at once.
func chatClient(t *testing.T, e *aitest.Endpoint, opts ai.Options) *ai.Client {
	t.Helper()
	c, err := ai.New(e.URL(), http.DefaultClient, completions.New(completions.Options{}), opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// stream ranges c's stream of req, folding its events, and returns the folded Response and the
// error that ended the stream, if any.
func stream(t *testing.T, c *ai.Client, req *protocol.Request) (*protocol.Response, error) {
	t.Helper()
	var acc protocol.Accumulator
	for ev, err := range c.GenerateStream(t.Context(), req) {
		if err != nil {
			return acc.Response(), err
		}
		if err := acc.Add(ev); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	return acc.Response(), nil
}

// TestChatRequestDecoded wants the route function to get the request go-ai's Chat Completions
// client sends decoded: its model, stream flag, max tokens, reasoning effort, and schema name, and
// each message's role, text, images from data URLs and remote URLs, and audio.
func TestChatRequestDecoded(t *testing.T) {
	png, clip := []byte("\x89PNG fake"), []byte("RIFF fake wav")
	req := &protocol.Request{
		Model:        "m",
		Instructions: "Be brief.",
		Input: []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
			protocol.InputText{Text: "What is "},
			protocol.InputImage{ImageURL: protocol.DataURL("image/png", png)},
			protocol.InputImage{ImageURL: "https://example.com/cat.jpg"},
			protocol.InputAudio{Data: clip, Format: protocol.AudioFormatWAV},
			protocol.InputText{Text: "this?"},
		}}},
		Reasoning:       protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow},
		Text:            protocol.TextParam{Format: protocol.JSONSchemaFormat{Name: "answer", Schema: []byte(`{"type":"object"}`)}},
		MaxOutputTokens: 64,
	}
	var got []aitest.ChatRequest
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(_ context.Context, r aitest.ChatRequest) aitest.Reply {
		got = append(got, r)
		return aitest.ChatReply{Text: "{}"}
	}})
	c := chatClient(t, e, ai.Options{})
	if _, err := c.Generate(t.Context(), req); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := stream(t, c, req); err != nil {
		t.Fatalf("GenerateStream: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("route got %d requests, want 2", len(got))
	}
	wantMessages := []aitest.ChatMessage{
		{Role: "system", Text: "Be brief."},
		{
			Role: "user",
			Text: "What is this?",
			Images: []aitest.ChatImage{
				{MediaType: "image/png", Data: png},
				{URL: "https://example.com/cat.jpg"},
			},
			Audio: []aitest.ChatAudio{{Format: "wav", Data: clip}},
		},
	}
	for i, r := range got {
		if r.Arrival != i+1 || !bytes.Equal(r.Body, e.Requests()[i].Body) {
			t.Errorf("request %d: arrival %d with %d body bytes, want arrival %d with the body as it arrived",
				i, r.Arrival, len(r.Body), i+1)
		}
		if r.Model != "m" || r.Stream != (i == 1) || r.MaxTokens != 64 || r.ReasoningEffort != "low" || r.SchemaName != "answer" {
			t.Errorf("request %d = model %q, stream %t, max tokens %d, effort %q, schema %q; "+
				"want m, %t, 64, low, answer", i, r.Model, r.Stream, r.MaxTokens, r.ReasoningEffort, r.SchemaName, i == 1)
		}
		if !reflect.DeepEqual(r.Messages, wantMessages) {
			t.Errorf("request %d messages = %+v\nwant %+v", i, r.Messages, wantMessages)
		}
	}
}

// TestChatRequestMaxTokens wants max_tokens read when a request has no max_completion_tokens,
// and max_completion_tokens preferred when it has both.
func TestChatRequestMaxTokens(t *testing.T) {
	var got int
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(_ context.Context, r aitest.ChatRequest) aitest.Reply {
		got = r.MaxTokens
		return aitest.ChatReply{Text: "ok"}
	}})
	for body, want := range map[string]int{
		`{"model":"m","messages":[],"max_tokens":9}`:                               9,
		`{"model":"m","messages":[],"max_tokens":9,"max_completion_tokens":12}`:    12,
		`{"model":"m","messages":[{"role":"user","content":"hi"}]}`:                0,
		`{"model":"m","messages":[{"role":"user","content":null}],"max_tokens":3}`: 3,
	} {
		got = -1
		if status, reply := send(t, e, http.MethodPost, "/chat/completions", body); status != http.StatusOK {
			t.Errorf("%s: status %d %s, want 200", body, status, reply)
		}
		if got != want {
			t.Errorf("%s: MaxTokens %d, want %d", body, got, want)
		}
	}
}

// TestChatRequestUndecodable wants a chat request body that doesn't decode answered 400 with an
// OpenAI-style error, the route function never called.
func TestChatRequestUndecodable(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
		t.Error("route function called for an undecodable request")
		return aitest.ChatReply{}
	}})
	for name, body := range map[string]string{
		"not JSON":           `{"model":`,
		"content not parts":  `{"model":"m","messages":[{"role":"user","content":7}]}`,
		"unknown part":       `{"model":"m","messages":[{"role":"user","content":[{"type":"file"}]}]}`,
		"bad audio base64":   `{"model":"m","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"%%","format":"wav"}}]}]}`,
		"bad image base64":   `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,%%"}}]}]}`,
		"data URL, no comma": `{"model":"m","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png"}}]}]}`,
	} {
		status, reply := send(t, e, http.MethodPost, "/chat/completions", body)
		if status != http.StatusBadRequest || !strings.Contains(reply, `"error":{"message":"aitest: decode chat request`) {
			t.Errorf("%s: %d %s, want 400 with an OpenAI-style error decoding the chat request", name, status, reply)
		}
	}
}

// TestChatReply wants a typed chat reply to reach Generate as its Response: the text or the
// refusal as the message's part, the finish reason as its status and incomplete reason, and the
// usage.
func TestChatReply(t *testing.T) {
	usage := protocol.Usage{InputTokens: 5, OutputTokens: 3, TotalTokens: 8}
	cases := []struct {
		name    string
		reply   aitest.ChatReply
		part    protocol.ContentPart
		status  protocol.Status
		details *protocol.IncompleteDetails
	}{
		{"text, completed", aitest.ChatReply{Text: "Paris"},
			protocol.OutputText{Text: "Paris"}, protocol.StatusCompleted, nil},
		{"text in chunks, stop given", aitest.ChatReply{Chunks: []string{"Pa", "ris"}, Finish: "stop"},
			protocol.OutputText{Text: "Paris"}, protocol.StatusCompleted, nil},
		{"refusal", aitest.ChatReply{Refusal: "I can't help with that."},
			protocol.Refusal{Refusal: "I can't help with that."}, protocol.StatusCompleted, nil},
		{"incomplete at length", aitest.ChatReply{Text: "Par", Finish: "length"},
			protocol.OutputText{Text: "Par"}, protocol.StatusIncomplete,
			&protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}},
		{"incomplete at content_filter", aitest.ChatReply{Text: "Pa", Finish: "content_filter"},
			protocol.OutputText{Text: "Pa"}, protocol.StatusIncomplete,
			&protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.reply
			r.PromptTokens, r.CompletionTokens, r.TotalTokens = 5, 3, 8
			e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply { return r }})
			c := chatClient(t, e, ai.Options{})
			for _, streamed := range []bool{false, true} {
				var resp *protocol.Response
				var err error
				if streamed {
					resp, err = stream(t, c, userText("m", "Capital of France?"))
				} else {
					resp, err = c.Generate(t.Context(), userText("m", "Capital of France?"))
				}
				if err != nil {
					t.Fatalf("streamed %t: %v", streamed, err)
				}
				checkChatResponse(t, resp, streamed, tc.part, tc.status, tc.details, usage)
			}
		})
	}
}

// checkChatResponse fails t unless resp is the model m's, of one assistant message of part, with
// status, details, and usage.
func checkChatResponse(t *testing.T, resp *protocol.Response, streamed bool, part protocol.ContentPart,
	status protocol.Status, details *protocol.IncompleteDetails, usage protocol.Usage) {
	t.Helper()
	if resp.Model != "m" || resp.Status != status || !reflect.DeepEqual(resp.IncompleteDetails, details) || resp.Usage != usage {
		t.Errorf("streamed %t: response model %q, %s %+v, usage %+v; want m, %s %+v, %+v",
			streamed, resp.Model, resp.Status, resp.IncompleteDetails, resp.Usage, status, details, usage)
	}
	if len(resp.Output) != 1 {
		t.Fatalf("streamed %t: output %+v, want one message", streamed, resp.Output)
	}
	msg, ok := resp.Output[0].(protocol.Message)
	if !ok || msg.Role != protocol.RoleAssistant || !reflect.DeepEqual(msg.Content, []protocol.ContentPart{part}) {
		t.Errorf("streamed %t: output %+v, want an assistant message of %+v", streamed, resp.Output[0], part)
	}
}

// TestChatReplyStreamChunks wants a streamed chat reply's text in the chunks given, each a
// delta, then its finish, usage, and [DONE], and the usage left out when the request doesn't ask
// for it.
func TestChatReplyStreamChunks(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
		return aitest.ChatReply{Chunks: []string{"Hel", "lo"}, PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4}
	}})
	var deltas []string
	for ev, err := range chatClient(t, e, ai.Options{}).GenerateStream(t.Context(), userText("m", "hi")) {
		if err != nil {
			t.Fatalf("GenerateStream: %v", err)
		}
		if d, ok := ev.(protocol.OutputTextDelta); ok {
			deltas = append(deltas, d.Delta)
		}
	}
	if !reflect.DeepEqual(deltas, []string{"Hel", "lo"}) {
		t.Errorf("text deltas = %q, want [Hel lo]", deltas)
	}
	for body, usage := range map[string]bool{
		`{"model":"m","messages":[],"stream":true,"stream_options":{"include_usage":true}}`: true,
		`{"model":"m","messages":[],"stream":true}`:                                         false,
	} {
		status, reply := send(t, e, http.MethodPost, "/chat/completions", body)
		if status != http.StatusOK || !strings.Contains(reply, `"finish_reason":"stop"`) ||
			!strings.HasSuffix(reply, "data: [DONE]\n\n") {
			t.Errorf("%s: %d %s, want 200, a finish reason of stop, and data: [DONE] last", body, status, reply)
		}
		if got := strings.Contains(reply, `"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}`); got != usage {
			t.Errorf("%s: stream carries usage %t, want %t: %s", body, got, usage, reply)
		}
	}
}

// TestChatReplyStreamBreak wants a stream that breaks after a chunk to end with no finish reason
// or [DONE], which GenerateStream reports as io.ErrUnexpectedEOF after the text so far.
func TestChatReplyStreamBreak(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
		return aitest.ChatReply{Chunks: []string{"one ", "two ", "three"}, BreakAfter: 2}
	}})
	resp, err := stream(t, chatClient(t, e, ai.Options{}), userText("m", "count"))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("GenerateStream error = %v, want io.ErrUnexpectedEOF", err)
	}
	if resp.Status != protocol.StatusInProgress || resp.OutputText() != "one two " {
		t.Errorf("partial response = %s %q, want in_progress \"one two \"", resp.Status, resp.OutputText())
	}
	_, reply := send(t, e, http.MethodPost, "/chat/completions", `{"model":"m","messages":[],"stream":true}`)
	if strings.Contains(reply, "finish_reason\":\"") || strings.Contains(reply, "[DONE]") || strings.Contains(reply, "three") {
		t.Errorf("broken stream = %s, want no finish reason, [DONE], or text after the break", reply)
	}
}

// TestErrorReply wants an error reply, on the chat route and any other, to reach the client as an
// *ai.APIError of its status and message, its Retry-After written as given and read as the
// client reads it: seconds, an HTTP date, or nothing for a value it can't read.
func TestErrorReply(t *testing.T) {
	date := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	cases := []struct {
		name       string
		reply      aitest.ErrorReply
		status     int
		min, max   time.Duration
		retryAfter string
	}{
		{"no Retry-After", aitest.ErrorReply{Status: http.StatusBadRequest, Message: "bad model"},
			http.StatusBadRequest, 0, 0, ""},
		{"zero status", aitest.ErrorReply{Message: "boom"}, http.StatusInternalServerError, 0, 0, ""},
		{"Retry-After in seconds", aitest.ErrorReply{Status: http.StatusTooManyRequests, Message: "slow down", RetryAfter: "7"},
			http.StatusTooManyRequests, 7 * time.Second, 7 * time.Second, "7"},
		{"Retry-After an HTTP date", aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "loading", RetryAfter: date},
			http.StatusServiceUnavailable, 58 * time.Minute, time.Hour, date},
		{"Retry-After unreadable", aitest.ErrorReply{Status: http.StatusTooManyRequests, Message: "later", RetryAfter: "soon"},
			http.StatusTooManyRequests, 0, 0, "soon"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{
				ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply { return tc.reply },
				Embeddings:      func(context.Context, aitest.EmbeddingRequest) aitest.Reply { return tc.reply },
			})
			c := chatClient(t, e, ai.Options{Retry: ai.RetryPolicy{}})
			_, genErr := c.Generate(t.Context(), userText("m", "hi"))
			_, embErr := c.Embed(t.Context(), &ai.EmbeddingRequest{Model: "m", Input: []string{"x"}})
			for route, err := range map[string]error{"chat": genErr, "embeddings": embErr} {
				apiErr, ok := errors.AsType[*ai.APIError](err)
				if !ok {
					t.Fatalf("%s: error %v wraps no *ai.APIError", route, err)
				}
				if apiErr.Status != tc.status || apiErr.Message != tc.reply.Message ||
					apiErr.RetryAfter < tc.min || apiErr.RetryAfter > tc.max {
					t.Errorf("%s: APIError = {Status:%d Message:%q RetryAfter:%v}, want %d %q in [%v, %v]",
						route, apiErr.Status, apiErr.Message, apiErr.RetryAfter, tc.status, tc.reply.Message, tc.min, tc.max)
				}
			}
			if len(e.Requests()) != 2 {
				t.Errorf("endpoint got %d requests, want 2, none retried", len(e.Requests()))
			}
			resp, err := http.Post(e.URL()+"/embeddings", "application/json", strings.NewReader(`{"model":"m","input":"x"}`))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if got := resp.Header.Values("Retry-After"); tc.retryAfter == "" && len(got) != 0 ||
				tc.retryAfter != "" && (len(got) != 1 || got[0] != tc.retryAfter) {
				t.Errorf("Retry-After = %q, want %q exactly", got, tc.retryAfter)
			}
		})
	}
}

// TestChatTranscription wants a chat-mode Transcribe to reach the chat route with its clip
// decoded as audio, and the typed reply's text to come back as the transcript.
func TestChatTranscription(t *testing.T) {
	clip := []byte("RIFF fake wav")
	var got aitest.ChatRequest
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(_ context.Context, r aitest.ChatRequest) aitest.Reply {
		got = r
		return aitest.ChatReply{Text: "hello there", PromptTokens: 40, CompletionTokens: 2, TotalTokens: 42}
	}})
	c := chatClient(t, e, ai.Options{TranscriptionMode: ai.TranscribeChat})
	tr, err := c.Transcribe(t.Context(), &ai.TranscriptionRequest{
		Model:    "audio-model",
		FileName: "clip.wav",
		Open:     func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(clip)), nil },
	})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	want := &ai.Transcription{Text: "hello there", Usage: ai.TranscriptionUsage{InputTokens: 40, OutputTokens: 2, TotalTokens: 42}}
	if !reflect.DeepEqual(tr, want) {
		t.Errorf("Transcribe = %+v, want %+v", tr, want)
	}
	if got.Model != "audio-model" || len(got.Messages) != 1 || got.Messages[0].Role != "user" ||
		!reflect.DeepEqual(got.Messages[0].Audio, []aitest.ChatAudio{{Format: "wav", Data: clip}}) ||
		got.Messages[0].Text != ai.TranscriptionInstruction("") {
		t.Errorf("route got %+v, want audio-model, one user message of the wav clip and the instruction", got)
	}
}

// TestChatReplyReasoning wants a chat reply's reasoning and reasoning tokens to reach Generate
// and GenerateStream: the Response a reasoning item of the reasoning ahead of the message, and
// its usage the reasoning tokens.
func TestChatReplyReasoning(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{ChatCompletions: func(context.Context, aitest.ChatRequest) aitest.Reply {
		return aitest.ChatReply{Reasoning: "France's capital is Paris.", Chunks: []string{"Pa", "ris"},
			PromptTokens: 5, CompletionTokens: 9, TotalTokens: 14, ReasoningTokens: 6}
	}})
	c := chatClient(t, e, ai.Options{})
	for _, streamed := range []bool{false, true} {
		var resp *protocol.Response
		var err error
		if streamed {
			resp, err = stream(t, c, userText("m", "Capital of France?"))
		} else {
			resp, err = c.Generate(t.Context(), userText("m", "Capital of France?"))
		}
		if err != nil {
			t.Fatalf("streamed %t: %v", streamed, err)
		}
		if len(resp.Output) != 2 {
			t.Fatalf("streamed %t: output %+v, want a reasoning item and a message", streamed, resp.Output)
		}
		r, ok := resp.Output[0].(protocol.Reasoning)
		if !ok || !reflect.DeepEqual(r.Content, []protocol.ReasoningText{{Text: "France's capital is Paris."}}) {
			t.Errorf("streamed %t: output[0] = %+v, want a reasoning item of the reasoning", streamed, resp.Output[0])
		}
		if resp.OutputText() != "Paris" {
			t.Errorf("streamed %t: text = %q, want Paris", streamed, resp.OutputText())
		}
		if got := resp.Usage.OutputTokensDetails.ReasoningTokens; got != 6 || resp.Usage.OutputTokens != 9 {
			t.Errorf("streamed %t: usage %+v, want 9 output tokens, 6 of them reasoning", streamed, resp.Usage)
		}
	}
}
