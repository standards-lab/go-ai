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

func TestChatImage(t *testing.T) {
	eachFormat(t, func(t *testing.T, formatEnv string) {
		res := runOn(t, vision, []string{formatEnv}, "chat", "--max-tokens", "256",
			"--image", integration.Media(t, "shapes.png"), "Describe this image in one sentence.")

		wantCode(t, res, process.ExitOK)
		if s := chatSummary(t, res.Stderr); s.status != "completed" {
			t.Errorf("status = %q, want completed", s.status)
		}
		words := strings.FieldsFunc(strings.ToLower(res.Stdout), func(r rune) bool { return !unicode.IsLetter(r) })
		for _, want := range []string{"circle", "square", "triangle"} {
			if !slices.Contains(words, want) && !slices.Contains(words, want+"s") {
				t.Errorf("the description %q doesn't name a %s", res.Stdout, want)
			}
		}
	})
}

func TestTranscribeChat(t *testing.T) {
	res := runOn(t, audio, []string{"INFER_FORMAT=completions"},
		"transcribe", "--mode", "chat", integration.Media(t, "phrase.wav"))

	wantCode(t, res, process.ExitOK)
	assertAccessCode(t, res.Stdout)
}

func TestTranscribeRoute(t *testing.T) {
	res := runOn(t, transcription, nil, "transcribe", integration.Media(t, "phrase.wav"))

	wantCode(t, res, process.ExitOK)
	assertAccessCode(t, res.Stdout)
}

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
