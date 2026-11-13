//go:build integration

package integration_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// TestImageDescription sends shapes.png to the vision model in each format and wants a completed
// description that names each of its shapes.
func TestImageDescription(t *testing.T) {
	eachFormat(t, vision, func(t *testing.T, c *ai.Client, model string) {
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model: model,
			Input: []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
				protocol.InputText{Text: "Describe this image in one sentence."},
				protocol.InputImage{ImageURL: protocol.DataURL("image/png", aitest.ReadTestdata(t, "shapes.png"))},
			}}},
			MaxOutputTokens: 256,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		text := completedText(t, resp)
		// shapes.png is a red circle, a blue square, and a green triangle. Each shape must be
		// named as a word of its own, singular or plural, in any phrasing.
		words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
		for _, want := range []string{"circle", "square", "triangle"} {
			if !slices.Contains(words, want) && !slices.Contains(words, want+"s") {
				t.Errorf("the description %q doesn't name a %s", text, want)
			}
		}
	})
}

// TestAudioInResponsesRefused sends phrase.wav to the audio model as Open Responses' audio
// extension, go-ai:input_audio, which the spec has no part for, and wants the endpoint to refuse
// it with an *APIError of 400 carrying its message.
func TestAudioInResponsesRefused(t *testing.T) {
	c, model := endpoint(t, audio, openResponses.new(), ai.Options{})
	_, err := c.Generate(requestContext(t), &protocol.Request{
		Model: model,
		Input: []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
			protocol.InputText{Text: "Transcribe this audio clip."},
			protocol.InputAudio{Data: aitest.ReadTestdata(t, "phrase.wav"), Format: protocol.AudioFormatWAV},
		}}},
		MaxOutputTokens: 64,
	})
	var apiErr *ai.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Generate error = %v, want an *APIError", err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Message == "" {
		t.Errorf("APIError = %v, want 400 with the endpoint's message", apiErr)
	}
}

// assertAccessCode fails t unless text, a transcript of phrase.wav, carries its words.
// phrase.wav says "The access code is seven four two nine." The code may come back as digits or
// words, spaced or not: compared with everything but letters and digits stripped, either form
// passes, after "accesscode".
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
