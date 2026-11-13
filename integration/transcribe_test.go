//go:build integration

package integration_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format/completions"
)

// phrase is the TranscriptionRequest of phrase.wav for model, opening the file on each attempt.
func phrase(model string) *ai.TranscriptionRequest {
	return &ai.TranscriptionRequest{
		Model: model,
		Open: func(context.Context) (io.ReadCloser, error) {
			return os.Open(filepath.Join("testdata", "phrase.wav"))
		},
		FileName: "phrase.wav",
	}
}

// assertTranscription fails t unless got carries phrase.wav's words and counts input and output
// tokens, and a total of at least both.
func assertTranscription(t *testing.T, got *ai.Transcription) {
	t.Helper()
	assertAccessCode(t, got.Text)
	if u := got.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens < u.InputTokens+u.OutputTokens {
		t.Errorf("Usage = %+v, want input and output tokens counted, and total at least both", u)
	}
}

// TestTranscribeChat transcribes phrase.wav on the audio endpoint in chat mode, through Chat
// Completions, as for an endpoint with no transcription route, and wants its words.
func TestTranscribeChat(t *testing.T) {
	c, model := endpoint(t, audio, completions.New(), ai.Options{TranscriptionMode: ai.TranscribeChat})
	got, err := c.Transcribe(requestContext(t), phrase(model))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	assertTranscription(t, got)
}

// TestTranscribeRoute streams phrase.wav from its file to the transcription route of the
// endpoint AI_TRANSCRIPTION_BASE_URL names, in route mode, the default, and wants its words. It
// skips unless that variable is set, as the audio endpoint may serve no route; the endpoint, as
// every role's, lists exactly one model, which is sent to.
func TestTranscribeRoute(t *testing.T) {
	c, model := endpoint(t, transcription, completions.New(), ai.Options{})
	got, err := c.Transcribe(requestContext(t), phrase(model))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	assertTranscription(t, got)
}
