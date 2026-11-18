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

// role is an endpoint a scenario runs on, named by the environment variable that holds its API
// root.
type role string

// The roles, each its variable.
const (
	agent         role = "AI_AGENT_BASE_URL"
	vision        role = "AI_VISION_BASE_URL"
	audio         role = "AI_AUDIO_BASE_URL"
	embedding     role = "AI_EMBEDDING_BASE_URL"
	transcription role = "AI_TRANSCRIPTION_BASE_URL"
)

// roles are every role, for the scenarios that run on each.
var roles = []role{agent, vision, audio, embedding, transcription}

// agentBound is the MaxOutputTokens of a request to the agent: a reasoning model reasons ahead of
// its answer or its call, within the same bound.
const agentBound = 1024

// requestTimeout bounds one request. The endpoints have their models loaded, so this covers
// generation only, which on a reasoning model takes tens of seconds.
const requestTimeout = 3 * time.Minute

// endpoint returns a Client of r's endpoint, speaking f under r's profile, AI_<ROLE>_PROFILE, as
// readProfile reads it, adjusted by opts and by r's credentials, AI_<ROLE>_TOKEN and
// AI_<ROLE>_HEADER, as newClient reads them, and the model to send to: the one AI_<ROLE>_MODEL
// names, as namedModel reads it, when set, for an endpoint whose model list isn't one model, such
// as a cloud account's that lists its whole catalog; else the one model the endpoint lists. The
// format is wrapped in skipDeclared, so a request the profile declares a gap of skips t. It skips
// t when r's variable is unset, and fails t when the profile or a credential doesn't parse, New
// refuses the URL or the credentials, or, with no model named, the model list can't be had or
// names other than exactly one model; no failure prints a token or header value.
func endpoint(t *testing.T, r role, f testFormat, opts ai.Options) (*ai.Client, string) {
	t.Helper()
	c := roleClient(t, r, f, opts)
	if model := namedModel(string(r), os.Getenv); model != "" {
		return c, model
	}
	return c, oneModel(t, r, listModels(t, c, r))
}

// roleClient returns a Client of r's endpoint as endpoint does, without choosing a model.
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

// listModels returns the models c's endpoint, r's, lists, failing t when the list can't be had.
func listModels(t *testing.T, c *ai.Client, r role) []ai.Model {
	t.Helper()
	models, err := c.Models(requestContext(t))
	if err != nil {
		t.Fatalf("Models of %s: %v", r, err)
	}
	return models
}

// oneModel returns the ID of the one model in models, r's list, failing t when the list names
// other than exactly one.
func oneModel(t *testing.T, r role, models []ai.Model) string {
	t.Helper()
	if len(models) != 1 || models[0].ID == "" {
		t.Fatalf("%s lists %+v, want exactly one model", r, models)
	}
	return models[0].ID
}

// requestContext is t's context, bounded by requestTimeout.
func requestContext(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	t.Cleanup(cancel)
	return ctx
}

// testFormat is a format a scenario runs through: its name and its constructor, which builds it
// with its member of the role's profile.
type testFormat struct {
	name string
	new  func(p profile) format.Format
}

// formats are the formats a scenario runs through with eachFormat, by name.
var formats = []testFormat{
	completionsFormat,
	messagesFormat,
	openResponses,
}

// completionsFormat is the Chat Completions format; a scenario the endpoint honors in some
// formats only, such as the vision stream's, names it among them, and one of a route the format
// doesn't shape, such as the embed scenario's, speaks it as the client's format.
var completionsFormat = testFormat{name: "completions", new: func(p profile) format.Format { return completions.New(p.Completions) }}

// messagesFormat is the Messages format; a scenario of its own, such as the allowed_tools
// choice's, runs it alone.
var messagesFormat = testFormat{name: "messages", new: func(p profile) format.Format { return messages.New(p.Messages) }}

// openResponses is the Open Responses format; a scenario of its own, such as the audio refusal's,
// runs it alone. Unprofiled it is the spec's form, which sends a reasoning item back with no
// text, so a deployment that reads the reasoning from its text, as vLLM does, needs
// resend_reasoning_text in the role's profile for the round trips to pass.
var openResponses = testFormat{name: "responses", new: func(p profile) format.Format { return responses.New(p.Responses) }}

// eachFormat runs scenario as a subtest of t for each of formats, named for the format, with a
// Client of r's endpoint speaking it and the endpoint's model, so one piece of test code covers
// every format, the format the only difference. Messages requires max_tokens, so a scenario sets
// MaxOutputTokens on every request.
func eachFormat(t *testing.T, r role, scenario func(t *testing.T, c *ai.Client, model string)) {
	t.Helper()
	eachFormatOf(t, r, formats, scenario)
}

// eachFormatOf runs scenario as eachFormat does, through fs alone, for a scenario the endpoint
// honors in some formats only.
func eachFormatOf(t *testing.T, r role, fs []testFormat, scenario func(t *testing.T, c *ai.Client, model string)) {
	t.Helper()
	for _, f := range fs {
		t.Run(f.name, func(t *testing.T) {
			c, model := endpoint(t, r, f, ai.Options{})
			scenario(t, c, model)
		})
	}
}

// userText is a user message of one input_text part.
func userText(s string) protocol.Message {
	return protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: s}}}
}

// completedText returns the output_text of resp, failing t unless resp is completed and its
// output is one completed assistant message with non-empty output_text, after the reasoning a
// reasoning model puts ahead of it.
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

// TestModels lists each role's endpoint and wants exactly one model, as endpoint does, so a
// misnamed or crowded endpoint fails here by name before any scenario of it runs. A role whose
// AI_<ROLE>_MODEL names its model needs no such list, so it wants the list to name at least one.
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
