package app_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/aitest"
)

const transcriptionsRoute = "audio/transcriptions"

var clipBytes = []byte("RIFF\x00\x01fake wav clip\xff")

func writeClip(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, clipBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func routeReply(text string) aitest.TranscriptionReply {
	return aitest.TranscriptionReply{Text: text, InputTokens: 11, OutputTokens: 4, TotalTokens: 15}
}

func transcribeEndpoint(t *testing.T, ids []string, chat aitest.Reply) *aitest.Endpoint {
	t.Helper()
	return aitest.Serve(t, aitest.Routes{
		Models:          listing(ids...),
		Transcriptions:  always[aitest.TranscriptionRequest](routeReply("hello world")),
		ChatCompletions: always[aitest.ChatRequest](chat),
	})
}

func completedChat() aitest.Reply {
	return chatReply("", "stop", "the clip's words")
}

type upload struct {
	fields   map[string]string
	fileName string
	file     []byte
}

func uploads(t *testing.T, e *aitest.Endpoint) []upload {
	t.Helper()
	var forms []upload
	for _, r := range e.Requests() {
		switch {
		case r.Method == http.MethodGet && r.Path == "/v1/models":
		case r.Method == http.MethodPost && r.Path == "/v1/"+completionsRoute:
		case r.Method == http.MethodPost && r.Path == "/v1/"+transcriptionsRoute:
			forms = append(forms, parseUpload(t, r))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.Path)
		}
	}
	return forms
}

func parseUpload(t *testing.T, r aitest.Request) upload {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		t.Fatalf("Content-Type = %q, want multipart/form-data", r.Header.Get("Content-Type"))
	}
	u := upload{fields: map[string]string{}}
	mr := multipart.NewReader(bytes.NewReader(r.Body), params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return u
		}
		if err != nil {
			t.Fatalf("multipart body: %v", err)
		}
		b, err := io.ReadAll(p)
		if err != nil {
			t.Fatalf("multipart part %s: %v", p.FormName(), err)
		}
		if p.FormName() == "file" {
			_, disposition, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
			if err != nil {
				t.Fatalf("file part's Content-Disposition: %v", err)
			}
			u.fileName, u.file = disposition["filename"], b
			continue
		}
		u.fields[p.FormName()] = string(b)
	}
}

func whisper(e *aitest.Endpoint) map[string]string {
	return map[string]string{"INFER_BASE_URL": e.URL(), "INFER_MODEL": "whisper"}
}

func wantRouteUpload(t *testing.T, e *aitest.Endpoint, model string) {
	t.Helper()
	forms := uploads(t, e)
	if len(forms) != 1 {
		t.Fatalf("sent %d transcription route requests, want 1", len(forms))
	}
	u := forms[0]
	if u.fields["model"] != model || u.fileName != "clip.wav" || !bytes.Equal(u.file, clipBytes) {
		t.Errorf("form = fields %v, file %q of %q; want model %s and the clip as clip.wav", u.fields, u.fileName, u.file, model)
	}
	for _, r := range e.Requests() {
		if r.Path == "/v1/"+completionsRoute {
			t.Errorf("request %s %s, want no chat request", r.Method, r.Path)
		}
	}
}

func TestTranscribe_Route(t *testing.T) {
	e := transcribeEndpoint(t, nil, completedChat())

	r := runApp(t, whisper(e), "transcribe", writeClip(t, "clip.wav"))

	wantCode(t, r, process.ExitOK)
	wantRouteUpload(t, e, "whisper")
	if want := "hello world\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if want := "usage: 11 input tokens, 4 output tokens, 15 total tokens\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
}

func TestTranscribe_Chat(t *testing.T) {
	e := transcribeEndpoint(t, nil, completedChat())

	r := runApp(t, whisper(e), "transcribe", "--mode", "chat", writeClip(t, "clip.wav"))

	wantCode(t, r, process.ExitOK)
	if want := "the clip's words\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if want := "usage: 78 input tokens, 25 output tokens, 103 total tokens\n"; r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if forms := uploads(t, e); len(forms) != 0 {
		t.Errorf("sent %d transcription route requests, want none", len(forms))
	}
	body := oneBody(t, e, completionsRoute)
	if body["model"] != "whisper" {
		t.Errorf("model = %v, want whisper", body["model"])
	}
	parts, _ := userContent(t, body).([]any)
	if len(parts) != 2 {
		t.Fatalf("user content = %v, want the clip and the instruction", userContent(t, body))
	}
	audio, _ := parts[0].(map[string]any)
	input, _ := audio["input_audio"].(map[string]any)
	if audio["type"] != "input_audio" || input["data"] != base64.StdEncoding.EncodeToString(clipBytes) || input["format"] != "wav" {
		t.Errorf("first part = %v, want the clip as input_audio, base64, format wav", audio)
	}
	text, _ := parts[1].(map[string]any)
	if text["type"] != "text" || text["text"] != ai.TranscriptionInstruction("") {
		t.Errorf("second part = %v, want go-ai's transcription instruction", text)
	}
}

func TestTranscribe_ReopensEachAttempt(t *testing.T) {
	e := aitest.Serve(t, aitest.Routes{Transcriptions: func(_ context.Context, req aitest.TranscriptionRequest) aitest.Reply {
		if req.Arrival == 1 {
			return aitest.ErrorReply{Status: http.StatusServiceUnavailable, Message: "overloaded", RetryAfter: "0"}
		}
		return routeReply("hello world")
	}})

	r := runApp(t, whisper(e), "transcribe", "--retries", "2", writeClip(t, "clip.wav"))

	wantCode(t, r, process.ExitOK)
	forms := uploads(t, e)
	if len(forms) != 2 {
		t.Fatalf("sent %d transcription route requests, want 2", len(forms))
	}
	for i, u := range forms {
		if !bytes.Equal(u.file, clipBytes) {
			t.Errorf("attempt %d sent file %q, want the whole clip", i+1, u.file)
		}
	}
	if want := "hello world\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
}

func TestTranscribe_ChatThroughMessages(t *testing.T) {
	e := transcribeEndpoint(t, nil, completedChat())
	env := whisper(e)
	env["INFER_FORMAT"] = "messages"
	missing := filepath.Join(t.TempDir(), "missing.wav")

	r := runApp(t, env, "transcribe", "--mode", "chat", missing)

	wantCode(t, r, process.ExitFailure)
	want := "infer transcribe: --mode chat needs a format that carries audio; --format messages doesn't: " +
		"ai: transcribe: messages: input 0: content 0: part protocol.InputAudio has no Messages form\n"
	if r.stderr != want {
		t.Errorf("stderr = %q, want %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	wantNoRequest(t, e)
}

func TestTranscribe_ChatThroughMessagesNoExtension(t *testing.T) {
	e := transcribeEndpoint(t, nil, completedChat())
	env := whisper(e)
	env["INFER_FORMAT"] = "messages"
	missing := filepath.Join(t.TempDir(), "missing.txt")

	r := runApp(t, env, "transcribe", "--mode", "chat", missing)

	wantCode(t, r, process.ExitFailure)
	if !strings.HasPrefix(r.stderr, "infer transcribe: ai: transcribe: ") || !strings.Contains(r.stderr, "no audio extension") {
		t.Errorf("stderr = %q, want go-ai's refusal of the extension alone", r.stderr)
	}
	wantNoRequest(t, e)
}

func notFound() aitest.Reply {
	return aitest.RawReply{Status: http.StatusNotFound, Body: []byte(`{"detail":"Not Found"}`)}
}

func TestTranscribe_NotFound(t *testing.T) {
	const notFoundError = `infer transcribe: ai: transcribe: 404 Not Found: {"detail":"Not Found"}`
	for _, tc := range []struct {
		name    string
		mode    string
		route   aitest.Reply
		chat    aitest.Reply
		want    string
		uploads int
	}{
		{"route 404", "route", notFound(), completedChat(),
			notFoundError + "\n(the endpoint serves no transcription route; try --mode chat)\n", 1},
		{"chat 404", "chat", routeReply("hello world"), notFound(),
			notFoundError + "\n", 0},
		{"route 500", "route", aitest.RawReply{
			Status: http.StatusInternalServerError,
			Header: http.Header{"Content-Type": {"text/plain; charset=utf-8"}},
			Body:   []byte("boom\n"),
		}, completedChat(), "infer transcribe: ai: transcribe: 500 Internal Server Error: boom\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := aitest.Serve(t, aitest.Routes{
				Transcriptions:  always[aitest.TranscriptionRequest](tc.route),
				ChatCompletions: always[aitest.ChatRequest](tc.chat),
			})

			r := runApp(t, whisper(e), "transcribe", "--mode", tc.mode, writeClip(t, "clip.wav"))

			wantCode(t, r, process.ExitFailure)
			if r.stderr != tc.want {
				t.Errorf("stderr = %q, want %q", r.stderr, tc.want)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			if n := len(uploads(t, e)); n != tc.uploads {
				t.Errorf("sent %d transcription route requests, want %d", n, tc.uploads)
			}
			if tc.mode == "route" {
				for _, req := range e.Requests() {
					if req.Path == "/v1/"+completionsRoute {
						t.Errorf("request %s %s, want no chat request", req.Method, req.Path)
					}
				}
			}
		})
	}
}

func TestTranscribe_ChatIncomplete(t *testing.T) {
	e := transcribeEndpoint(t, nil, chatReply("", "length", "the clip's"))

	r := runApp(t, whisper(e), "transcribe", "--mode", "chat", writeClip(t, "clip.wav"))

	wantCode(t, r, process.ExitFailure)
	if want := "status incomplete, reason max_output_tokens"; !strings.HasPrefix(r.stderr, "infer transcribe: ") || !strings.Contains(r.stderr, want) {
		t.Errorf("stderr = %q, want one infer transcribe error naming %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want no transcript", r.stdout)
	}
}

func TestTranscribe_MissingFile(t *testing.T) {
	e := transcribeEndpoint(t, nil, completedChat())
	missing := filepath.Join(t.TempDir(), "missing.wav")

	r := runApp(t, whisper(e), "transcribe", missing)

	wantCode(t, r, process.ExitFailure)
	if !strings.HasPrefix(r.stderr, "infer transcribe: ") || !strings.Contains(r.stderr, "no such file") {
		t.Errorf("stderr = %q, want one infer transcribe error naming the missing file", r.stderr)
	}
	wantNoRequest(t, e)
}

func TestTranscribe_ModeSetting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		env      string
		args     []string
		wantChat bool
	}{
		{"unset", "", nil, false},
		{"variable chat", "chat", nil, true},
		{"variable route", "route", nil, false},
		{"flag chat over variable route", "route", []string{"--mode", "chat"}, true},
		{"flag route over variable chat", "chat", []string{"--mode", "route"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := transcribeEndpoint(t, nil, completedChat())
			env := whisper(e)
			env["INFER_TRANSCRIPTION_MODE"] = tc.env

			r := runApp(t, env, append(append([]string{"transcribe"}, tc.args...), writeClip(t, "clip.wav"))...)

			wantCode(t, r, process.ExitOK)
			if tc.wantChat {
				oneBody(t, e, completionsRoute)
				if forms := uploads(t, e); len(forms) != 0 {
					t.Errorf("sent %d transcription route requests, want none in chat mode", len(forms))
				}
				return
			}
			wantRouteUpload(t, e, "whisper")
		})
	}
}

func TestTranscribe_UnknownMode(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		args []string
	}{
		{"flag", "", []string{"--mode", "stream"}},
		{"variable", "stream", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := transcribeEndpoint(t, []string{"whisper"}, completedChat())

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL(), "INFER_TRANSCRIPTION_MODE": tc.env},
				append(append([]string{"transcribe"}, tc.args...), writeClip(t, "clip.wav"))...)

			wantCode(t, r, process.ExitUsage)
			if want := `infer transcribe: transcription mode: unknown transcription mode "stream": --mode (INFER_TRANSCRIPTION_MODE) is route or chat`; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			wantNoRequest(t, e)
		})
	}
}

func TestTranscribe_FallsBackToTheOneModel(t *testing.T) {
	e := transcribeEndpoint(t, []string{"whisper-large"}, completedChat())

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL()}, "transcribe", writeClip(t, "clip.wav"))

	wantCode(t, r, process.ExitOK)
	wantRouteUpload(t, e, "whisper-large")
	if reqs := e.Requests(); reqs[0].Path != "/v1/models" {
		t.Errorf("first request = %s %s, want the model list first", reqs[0].Method, reqs[0].Path)
	}
}

func TestTranscribe_OneFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"none", nil},
		{"two", []string{"a.wav", "b.wav"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := transcribeEndpoint(t, []string{"whisper"}, completedChat())

			r := runApp(t, unreadable(e.URL()), append([]string{"transcribe"}, tc.args...)...)

			wantCode(t, r, process.ExitUsage)
			if !strings.HasPrefix(r.stderr, "infer transcribe: ") {
				t.Errorf("stderr = %q, want an infer transcribe usage error", r.stderr)
			}
			wantNothingBuilt(t, r)
			wantNoRequest(t, e)
		})
	}
}

func TestTranscribe_Help(t *testing.T) {
	e := transcribeEndpoint(t, []string{"whisper"}, completedChat())

	r := runApp(t, unreadable(e.URL()), "transcribe", "--help")

	wantCode(t, r, process.ExitUsage)
	for _, want := range []string{"<file>", "--mode", "route", "chat", "INFER_TRANSCRIPTION_MODE"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout = %q, want transcribe's help naming %s", r.stdout, want)
		}
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}
