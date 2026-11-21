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
		words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) })
		for _, want := range []string{"circle", "square", "triangle"} {
			if !slices.Contains(words, want) && !slices.Contains(words, want+"s") {
				t.Errorf("the description %q doesn't name a %s", text, want)
			}
		}
	})
}

func TestAudioInResponsesRefused(t *testing.T) {
	c, model := endpoint(t, audio, openResponses, ai.Options{})
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
