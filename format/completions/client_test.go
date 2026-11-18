package completions_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"net/http"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// The tests here drive each recorded gap through ai.Client.Generate, over a loopback server that
// answers with the gap's recorded reply: under the zero profile the host's refusal reaches the
// caller, and under the profile that declares the gap the format refuses or narrows the request
// before the token source is called or anything is sent.

// tokenCounter is a token source that counts its calls.
type tokenCounter struct{ calls int }

func (c *tokenCounter) token(context.Context) (string, error) {
	c.calls++
	return "secret", nil
}

// newProfileClient returns a Client of the server at base speaking the completions format under
// opts, with the token source tokens.
func newProfileClient(t *testing.T, base string, opts completions.Options, tokens *tokenCounter) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, completions.New(opts), ai.Options{Token: tokens.token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// recordedMessage returns the error message of the recorded reply, read from its body apart from
// the client's own decoding.
func recordedMessage(t *testing.T, reply []byte) string {
	t.Helper()
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(reply, &body); err != nil || body.Error.Message == "" {
		t.Fatalf("recorded reply %s has no error message: %v", reply, err)
	}
	return body.Error.Message
}

// gap is a recorded gap: its reply, a request of the shape it was recorded for, the profile that
// declares it, and the refusal that profile gives the request, nil for a gap the format narrows.
type gap struct {
	name    string
	reply   string
	req     *protocol.Request
	opts    completions.Options
	refusal *format.UnsupportedError
}

// recordedGaps are the gaps the completions testdata records.
func recordedGaps() []gap {
	effort := func(e protocol.ReasoningEffort) gap {
		return gap{
			name:  "vLLM 0.34.0 effort " + string(e),
			reply: "vllm-0.34.0/effort_" + string(e) + ".json",
			req: &protocol.Request{Model: "agent",
				Input:     []protocol.Item{text(protocol.RoleUser, "Name three French cities, comma-separated, nothing else.")},
				Reasoning: protocol.ReasoningParam{Effort: e}},
			opts:    completions.Options{RefusedEfforts: []protocol.ReasoningEffort{e}},
			refusal: &format.UnsupportedError{Format: "completions", Feature: "reasoning effort", Value: string(e), Declared: true},
		}
	}
	tools := func(tc protocol.ToolChoice) *protocol.Request {
		r := withTools(tc)
		r.Model = "agent"
		r.Input = []protocol.Item{text(protocol.RoleUser, "What is the weather in Paris right now?")}
		r.Reasoning.Effort = protocol.ReasoningEffortLow
		return r
	}
	return []gap{
		effort(protocol.ReasoningEffortNone),
		effort(protocol.ReasoningEffortMinimal),
		effort(protocol.ReasoningEffortXHigh),
		effort(protocol.ReasoningEffortMax),
		{
			name:    "Foundry gpt-oss-120b tool_choice required",
			reply:   "foundry/oss_tool_choice_required.json",
			req:     tools(protocol.ToolChoiceRequired),
			opts:    completions.Options{NoToolChoiceRequired: true},
			refusal: &format.UnsupportedError{Format: "completions", Feature: "tool choice", Value: "required", Declared: true},
		},
		{
			name:  "vLLM 0.34.0 allowed_tools",
			reply: "vllm-0.34.0/allowed_tools.json",
			req: tools(protocol.AllowedToolChoice{
				Tools: []protocol.FunctionToolChoice{{Name: "get_time"}},
				Mode:  protocol.ToolChoiceRequired,
			}),
			opts: completions.Options{NoAllowedTools: true},
		},
	}
}

// TestGenerateRecordedGapUnderZeroProfile wants each recorded gap's 400, under the zero profile,
// to reach the caller as an *ai.APIError of the host's status and message, after the token was
// fetched and the request sent once.
func TestGenerateRecordedGapUnderZeroProfile(t *testing.T) {
	for _, g := range recordedGaps() {
		t.Run(g.name, func(t *testing.T) {
			reply := aitest.ReadTestdata(t, g.reply)
			srv := aitest.NewServer(t, http.StatusBadRequest, string(reply))
			tokens := &tokenCounter{}
			_, err := newProfileClient(t, srv.URL+"/v1", completions.Options{}, tokens).Generate(t.Context(), g.req)
			apiErr, ok := errors.AsType[*ai.APIError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want an *ai.APIError", err)
			}
			if apiErr.Status != http.StatusBadRequest {
				t.Errorf("APIError.Status = %d, want %d", apiErr.Status, http.StatusBadRequest)
			}
			if want := recordedMessage(t, reply); apiErr.Message != want {
				t.Errorf("APIError.Message = %q, want %q", apiErr.Message, want)
			}
			if tokens.calls != 1 {
				t.Errorf("token source called %d times, want 1", tokens.calls)
			}
			srv.Only(t)
		})
	}
}

// TestGenerateRecordedGapDeclared wants each recorded gap the format refuses, under the profile
// that declares it, refused as declared before the token source is called or anything is sent.
// The gap the format narrows instead, allowed_tools, is TestGenerateAllowedToolsNarrowed's.
func TestGenerateRecordedGapDeclared(t *testing.T) {
	for _, g := range recordedGaps() {
		if g.refusal == nil {
			continue
		}
		t.Run(g.name, func(t *testing.T) {
			srv := aitest.NewServer(t, http.StatusBadRequest, string(aitest.ReadTestdata(t, g.reply)))
			tokens := &tokenCounter{}
			_, err := newProfileClient(t, srv.URL+"/v1", g.opts, tokens).Generate(t.Context(), g.req)
			got, ok := errors.AsType[*format.UnsupportedError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want a *format.UnsupportedError", err)
			}
			if got.Format != g.refusal.Format || got.Feature != g.refusal.Feature || got.Value != g.refusal.Value || !got.Declared {
				t.Errorf("UnsupportedError = %+v, want %+v", *got, *g.refusal)
			}
			if tokens.calls != 0 {
				t.Errorf("token source called %d times for a refused request, want 0", tokens.calls)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

// TestGenerateAllowedToolsNarrowed wants the recorded allowed_tools request, under
// NoAllowedTools, sent narrowed: its allowed tool alone and its mode as a plain tool_choice, the
// form vLLM 0.34.0 takes, here answered with a recorded tool call.
func TestGenerateAllowedToolsNarrowed(t *testing.T) {
	var g gap
	for _, r := range recordedGaps() {
		if r.refusal == nil {
			g = r
		}
	}
	srv := aitest.NewServer(t, http.StatusOK, string(aitest.ReadTestdata(t, "vllm/tool_call.json")))
	tokens := &tokenCounter{}
	resp, err := newProfileClient(t, srv.URL+"/v1", g.opts, tokens).Generate(t.Context(), g.req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Status != protocol.StatusCompleted {
		t.Errorf("Status = %q, want the recorded call completed", resp.Status)
	}
	aitest.AssertJSON(t, toolMembers(t, srv.Only(t).Body), `{"tools":[`+getTimeWire+`],"tool_choice":"required"}`)
}
