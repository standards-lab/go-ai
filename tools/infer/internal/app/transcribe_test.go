package app_test

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// transcriptionsRoute is the route a route-mode transcription goes to.
const transcriptionsRoute = "audio/transcriptions"

// clipBytes is the content of the clip a test transcribes: not real audio,
// as neither infer nor the endpoint reads it, but bytes a test can find on
// the wire.
var clipBytes = []byte("RIFF\x00\x01fake wav clip\xff")

// writeClip writes clipBytes to a file named name in a fresh directory and
// returns its path.
func writeClip(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, clipBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// routeReply is a transcription route reply of text, with usage of 11 input
// tokens, 4 output, and 15 total.
func routeReply(text string) string {
	return `{"text":"` + text + `","usage":{"type":"tokens","input_tokens":11,"output_tokens":4,"total_tokens":15}}`
}

// transcribeEndpoint serves an endpoint that lists ids as its models,
// answers the transcription route with routeReply's "hello world", and
// answers a completions chat request with chat.
func transcribeEndpoint(t *testing.T, ids []string, chat http.Handler) *apptest.Endpoint {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("GET /v1/models", apptest.Models(ids...))
	mux.Handle("POST /v1/"+transcriptionsRoute, apptest.Reply(transcriptionsRoute, routeReply("hello world")))
	mux.Handle("POST /v1/"+completionsRoute, chat)
	return apptest.Serve(t, mux)
}

// completedChat is a completions handler that answers "the clip's words",
// completed.
func completedChat() http.Handler {
	return apptest.Reply(completionsRoute, completionsReply("the clip's words", "", "stop"))
}

// upload is the multipart form of a route-mode transcription request.
type upload struct {
	fields   map[string]string
	fileName string
	file     []byte
}

// uploads returns the form of each transcription route request e received,
// in order, and fails t on any request but those, chat requests, and GET
// /v1/models.
func uploads(t *testing.T, e *apptest.Endpoint) []upload {
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

// parseUpload decodes r's multipart form, failing t when it isn't one.
func parseUpload(t *testing.T, r apptest.Request) upload {
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
			// The header's own filename, as sent: Part.FileName would
			// reduce a path to its base name.
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

// whisper is the environment of a run against e with the model whisper set.
func whisper(e *apptest.Endpoint) map[string]string {
	return map[string]string{"INFER_BASE_URL": e.URL, "INFER_MODEL": "whisper"}
}

// wantRouteUpload fails t unless e received exactly one transcription route
// request, its form naming model and carrying the clip as clip.wav, and no
// chat request.
func wantRouteUpload(t *testing.T, e *apptest.Endpoint, model string) {
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

// Route mode, the default, posts the clip's bytes and the model as a
// multipart form to the transcription route, writes the transcript to
// stdout, ending it with a newline, and the usage to stderr alone.
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

// Chat mode sends one chat request in the client's format, completions by
// default: one user message of the clip as an input_audio part, in the
// encoding its extension gives, and go-ai's transcription instruction; the
// reply's text is the transcript.
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

// Each attempt opens the file again, so an attempt a retry makes sends the
// whole clip, not what a first attempt left of a reader: --retries reaches
// the transcriber. The endpoint answers the first upload 503 with
// Retry-After 0, then the route's reply.
func TestTranscribe_ReopensEachAttempt(t *testing.T) {
	var attempts atomic.Int32
	mux := http.NewServeMux()
	route := apptest.Reply(transcriptionsRoute, routeReply("hello world"))
	mux.Handle("POST /v1/"+transcriptionsRoute, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		route.ServeHTTP(w, r)
	}))
	e := apptest.Serve(t, mux)

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

// Chat mode through messages, a format that can't carry audio, is go-ai's
// refusal, exit 1, after the mode and the format it names, before the file
// is opened or anything is sent: the file doesn't exist, so a run that
// opened it would fail with a file error instead.
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

// notFound answers every request 404, as an endpoint without the route
// does, with vLLM's body.
func notFound() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"detail":"Not Found"}`)
	})
}

// A route-mode 404 is the endpoint's error, exit 1, reported once, and a
// second line hinting at chat mode; infer sends the upload once and tries
// no chat request. A 404 in chat mode, or another status in route mode,
// is the error alone: the hint is for a missing transcription route.
func TestTranscribe_NotFound(t *testing.T) {
	const notFoundError = `infer transcribe: ai: transcribe: 404 Not Found: {"detail":"Not Found"}`
	for _, tc := range []struct {
		name    string
		mode    string
		route   http.Handler // the transcription route's handler
		chat    http.Handler // the chat route's handler
		want    string
		uploads int
	}{
		{"route 404", "route", notFound(), completedChat(),
			notFoundError + "\n(the endpoint serves no transcription route; try --mode chat)\n", 1},
		{"chat 404", "chat", apptest.Reply(transcriptionsRoute, routeReply("hello world")), notFound(),
			notFoundError + "\n", 0},
		{"route 500", "route", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}), completedChat(), "infer transcribe: ai: transcribe: 500 Internal Server Error: boom\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.Handle("POST /v1/"+transcriptionsRoute, tc.route)
			mux.Handle("POST /v1/"+completionsRoute, tc.chat)
			e := apptest.Serve(t, mux)

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

// A chat-mode reply the model didn't complete is exit 1, naming its status
// and reason, with no transcript written.
func TestTranscribe_ChatIncomplete(t *testing.T) {
	e := transcribeEndpoint(t, nil, apptest.Reply(completionsRoute, completionsReply("the clip's", "", "length")))

	r := runApp(t, whisper(e), "transcribe", "--mode", "chat", writeClip(t, "clip.wav"))

	wantCode(t, r, process.ExitFailure)
	if want := "status incomplete, reason max_output_tokens"; !strings.HasPrefix(r.stderr, "infer transcribe: ") || !strings.Contains(r.stderr, want) {
		t.Errorf("stderr = %q, want one infer transcribe error naming %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want no transcript", r.stdout)
	}
}

// A file that doesn't exist is exit 1, the open's error named, and sends
// no upload.
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

// The mode is INFER_TRANSCRIPTION_MODE's when set, and --mode's when given,
// whichever the variable says; neither is route.
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

// An unknown mode, from the flag or the variable, is a usage error, exit
// 2, naming the flag and its variable, before any request.
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

			r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_TRANSCRIPTION_MODE": tc.env},
				append(append([]string{"transcribe"}, tc.args...), writeClip(t, "clip.wav"))...)

			wantCode(t, r, process.ExitUsage)
			if want := `infer transcribe: transcription mode: unknown transcription mode "stream": --mode (INFER_TRANSCRIPTION_MODE) is route or chat`; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			wantNoRequest(t, e)
		})
	}
}

// With no model set, transcribe lists the endpoint's models and sends to
// the one listed.
func TestTranscribe_FallsBackToTheOneModel(t *testing.T) {
	e := transcribeEndpoint(t, []string{"whisper-large"}, completedChat())

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "transcribe", writeClip(t, "clip.wav"))

	wantCode(t, r, process.ExitOK)
	wantRouteUpload(t, e, "whisper-large")
	if reqs := e.Requests(); reqs[0].Path != "/v1/models" {
		t.Errorf("first request = %s %s, want the model list first", reqs[0].Method, reqs[0].Path)
	}
}

// transcribe takes exactly one positional argument; none, or two, is a
// usage error, exit 2, that builds and sends nothing.
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

			r := runApp(t, unreadable(e.URL), append([]string{"transcribe"}, tc.args...)...)

			wantCode(t, r, process.ExitUsage)
			if !strings.HasPrefix(r.stderr, "infer transcribe: ") {
				t.Errorf("stderr = %q, want an infer transcribe usage error", r.stderr)
			}
			wantNothingBuilt(t, r)
			wantNoRequest(t, e)
		})
	}
}

// transcribe's help names --mode and its variable, and builds and sends
// nothing.
func TestTranscribe_Help(t *testing.T) {
	e := transcribeEndpoint(t, []string{"whisper"}, completedChat())

	r := runApp(t, unreadable(e.URL), "transcribe", "--help")

	wantCode(t, r, process.ExitUsage)
	for _, want := range []string{"<file>", "--mode", "route", "chat", "INFER_TRANSCRIPTION_MODE"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("stdout = %q, want transcribe's help naming %s", r.stdout, want)
		}
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}
