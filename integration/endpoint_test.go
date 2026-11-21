//go:build integration

package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
)

type role string

const (
	agent         role = "AI_AGENT_BASE_URL"
	vision        role = "AI_VISION_BASE_URL"
	audio         role = "AI_AUDIO_BASE_URL"
	embedding     role = "AI_EMBEDDING_BASE_URL"
	transcription role = "AI_TRANSCRIPTION_BASE_URL"
)

var roles = []role{agent, vision, audio, embedding, transcription}

const agentBound = 1024

const requestTimeout = 3 * time.Minute

func endpoint(t *testing.T, r role, f testFormat, opts ai.Options) (*ai.Client, string) {
	t.Helper()
	c := roleClient(t, r, f, opts)
	if model := namedModel(string(r), os.Getenv); model != "" {
		return c, model
	}
	return c, oneModel(t, r, listModels(t, c, r))
}

func roleClient(t *testing.T, r role, f testFormat, opts ai.Options) *ai.Client {
	t.Helper()
	base := os.Getenv(string(r))
	if base == "" {
		t.Skipf("%s is unset: set it to the endpoint's API root, such as http://localhost:8000/v1", r)
	}
	p, err := readProfile(string(r), os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	wire := skipDeclared{Format: f.new(p), t: t, variable: profileVariable(string(r))}
	c, err := newClient(string(r), base, wire, opts, os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func listModels(t *testing.T, c *ai.Client, r role) []ai.Model {
	t.Helper()
	models, err := c.Models(requestContext(t))
	if err != nil {
		t.Fatalf("Models of %s: %v", r, err)
	}
	return models
}

func oneModel(t *testing.T, r role, models []ai.Model) string {
	t.Helper()
	if len(models) != 1 || models[0].ID == "" {
		t.Fatalf("%s lists %+v, want exactly one model", r, models)
	}
	return models[0].ID
}

func requestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	t.Cleanup(cancel)
	return ctx
}

type testFormat struct {
	name string
	new  func(p profile) format.Format
}

var formats = []testFormat{
	completionsFormat,
	messagesFormat,
	openResponses,
}

var completionsFormat = testFormat{name: "completions", new: func(p profile) format.Format { return completions.New(p.Completions) }}

var messagesFormat = testFormat{name: "messages", new: func(p profile) format.Format { return messages.New(p.Messages) }}

var openResponses = testFormat{name: "responses", new: func(p profile) format.Format { return responses.New(p.Responses) }}

func eachFormat(t *testing.T, r role, scenario func(t *testing.T, c *ai.Client, model string)) {
	t.Helper()
	eachFormatOf(t, r, formats, scenario)
}

func eachFormatOf(t *testing.T, r role, fs []testFormat, scenario func(t *testing.T, c *ai.Client, model string)) {
	t.Helper()
	for _, f := range fs {
		t.Run(f.name, func(t *testing.T) {
			c, model := endpoint(t, r, f, ai.Options{})
			scenario(t, c, model)
		})
	}
}

func userText(s string) protocol.Message {
	return protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: s}}}
}

func completedText(t *testing.T, resp *protocol.Response) string {
	t.Helper()
	if resp.Status != protocol.StatusCompleted {
		t.Fatalf("Status = %q (%+v), want completed; output %#v", resp.Status, resp.IncompleteDetails, resp.Output)
	}
	output := resp.Output
	for len(output) > 0 {
		if _, ok := output[0].(protocol.Reasoning); !ok {
			break
		}
		output = output[1:]
	}
	if len(output) != 1 {
		t.Fatalf("Output has %d items after its reasoning, want 1 message: %#v", len(output), resp.Output)
	}
	m, ok := output[0].(protocol.Message)
	if !ok || m.Role != protocol.RoleAssistant || m.Status != protocol.StatusCompleted {
		t.Fatalf("Output's message = %#v, want a completed assistant message", output[0])
	}
	var text strings.Builder
	for _, p := range m.Content {
		if o, ok := p.(protocol.OutputText); ok {
			text.WriteString(o.Text)
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		t.Fatalf("the message has no output_text: %#v", m.Content)
	}
	return text.String()
}

func TestModels(t *testing.T) {
	for _, r := range roles {
		t.Run(string(r), func(t *testing.T) {
			c := roleClient(t, r, completionsFormat, ai.Options{})
			models := listModels(t, c, r)
			if named := namedModel(string(r), os.Getenv); named != "" {
				if len(models) == 0 {
					t.Fatalf("%s lists no model", r)
				}
				t.Logf("%s lists %d models; %s names %s", r, len(models), modelVariable(string(r)), named)
				return
			}
			t.Logf("%s serves %s", r, oneModel(t, r, models))
		})
	}
}
