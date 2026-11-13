//go:build integration

package integration_test

import (
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/integration"
)

// TestChatImage sends shapes.png to the vision model with --image, in each
// format, and wants a completed description naming each of its shapes, so
// the file reached the model inline.
func TestChatImage(t *testing.T) {
	eachFormat(t, func(t *testing.T, formatEnv string) {
		res := runOn(t, vision, []string{formatEnv}, "chat", "--max-tokens", "256",
			"--image", integration.Media(t, "shapes.png"), "Describe this image in one sentence.")

		wantCode(t, res, process.ExitOK)
		if s := chatSummary(t, res.Stderr); s.status != "completed" {
			t.Errorf("status = %q, want completed", s.status)
		}
		// shapes.png is a red circle, a blue square, and a green triangle.
		// Each shape must be named as a word of its own, singular or
		// plural, in any phrasing.
		words := strings.FieldsFunc(strings.ToLower(res.Stdout), func(r rune) bool { return !unicode.IsLetter(r) })
		for _, want := range []string{"circle", "square", "triangle"} {
			if !slices.Contains(words, want) && !slices.Contains(words, want+"s") {
				t.Errorf("the description %q doesn't name a %s", res.Stdout, want)
			}
		}
	})
}

// TestTranscribeChat transcribes phrase.wav on the audio endpoint with
// --mode chat, through Chat Completions, as for an endpoint with no
// transcription route, and wants its words.
func TestTranscribeChat(t *testing.T) {
	res := runOn(t, audio, []string{"INFER_FORMAT=completions"},
		"transcribe", "--mode", "chat", integration.Media(t, "phrase.wav"))

	wantCode(t, res, process.ExitOK)
	assertAccessCode(t, res.Stdout)
}

// TestTranscribeRoute transcribes phrase.wav on the endpoint
// AI_TRANSCRIPTION_BASE_URL names in route mode, the default, and wants its
// words. It skips unless that variable is set, as no workstation endpoint
// serves the transcription route.
func TestTranscribeRoute(t *testing.T) {
	res := runOn(t, transcription, nil, "transcribe", integration.Media(t, "phrase.wav"))

	wantCode(t, res, process.ExitOK)
	assertAccessCode(t, res.Stdout)
}

// assertAccessCode fails t unless text, a transcript of phrase.wav, carries
// its words. phrase.wav says "The access code is seven four two nine." The
// code may come back as digits or words, spaced or not: compared with
// everything but letters and digits stripped, either form passes, after
// "accesscode".
func assertAccessCode(t *testing.T, text string) {
	t.Helper()
	got := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
	if !strings.Contains(got, "accesscode") ||
		(!strings.Contains(got, "7429") && !strings.Contains(got, "sevenfourtwonine")) {
		t.Errorf("the transcript %q doesn't carry the access code 7429", text)
	}
}
