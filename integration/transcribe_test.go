//go:build integration

package integration_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/standards-lab/go-ai"
)

func phrase(model string) *ai.TranscriptionRequest {
	return &ai.TranscriptionRequest{
		Model: model,
		Open: func(context.Context) (io.ReadCloser, error) {
			return os.Open(filepath.Join("testdata", "phrase.wav"))
		},
		FileName: "phrase.wav",
	}
}

func assertTranscription(t *testing.T, got *ai.Transcription) {
	t.Helper()
	assertAccessCode(t, got.Text)
	if u := got.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens < u.InputTokens+u.OutputTokens {
		t.Errorf("Usage = %+v, want input and output tokens counted, and total at least both", u)
	}
}

func TestTranscribeChat(t *testing.T) {
	c, model := endpoint(t, audio, completionsFormat, ai.Options{TranscriptionMode: ai.TranscribeChat})
	got, err := c.Transcribe(requestContext(t), phrase(model))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	assertTranscription(t, got)
}

func TestTranscribeRoute(t *testing.T) {
	c, model := endpoint(t, transcription, completionsFormat, ai.Options{})
	got, err := c.Transcribe(requestContext(t), phrase(model))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	assertTranscription(t, got)
}
