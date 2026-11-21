package completions_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

func newProfileClient(t *testing.T, base string, opts completions.Options, tokens *aitest.TokenCounter) *ai.Client {
	t.Helper()
	c, err := ai.New(base, http.DefaultClient, completions.New(opts), ai.Options{Token: tokens.Token})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

type gap struct {
	name    string
	reply   string
	req     *protocol.Request
	opts    completions.Options
	refusal *format.UnsupportedError
}

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

func TestGenerateRecordedGapUnderZeroProfile(t *testing.T) {
	for _, g := range recordedGaps() {
		t.Run(g.name, func(t *testing.T) {
			reply := aitest.ReadTestdata(t, g.reply)
			srv := serve(t, http.StatusBadRequest, string(reply))
			tokens := &aitest.TokenCounter{}
			_, err := newProfileClient(t, srv.URL(), completions.Options{}, tokens).Generate(t.Context(), g.req)
			apiErr, ok := errors.AsType[*ai.APIError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want an *ai.APIError", err)
			}
			if apiErr.Status != http.StatusBadRequest {
				t.Errorf("APIError.Status = %d, want %d", apiErr.Status, http.StatusBadRequest)
			}
			if want := aitest.ReplyMessage(t, reply); apiErr.Message != want {
				t.Errorf("APIError.Message = %q, want %q", apiErr.Message, want)
			}
			if tokens.Calls != 1 {
				t.Errorf("token source called %d times, want 1", tokens.Calls)
			}
			aitest.Only(t, srv)
		})
	}
}

func TestGenerateRecordedGapDeclared(t *testing.T) {
	for _, g := range recordedGaps() {
		if g.refusal == nil {
			continue
		}
		t.Run(g.name, func(t *testing.T) {
			srv := serve(t, http.StatusBadRequest, string(aitest.ReadTestdata(t, g.reply)))
			tokens := &aitest.TokenCounter{}
			_, err := newProfileClient(t, srv.URL(), g.opts, tokens).Generate(t.Context(), g.req)
			got, ok := errors.AsType[*format.UnsupportedError](err)
			if !ok {
				t.Fatalf("Generate error = %v, want a *format.UnsupportedError", err)
			}
			if got.Format != g.refusal.Format || got.Feature != g.refusal.Feature || got.Value != g.refusal.Value || !got.Declared {
				t.Errorf("UnsupportedError = %+v, want %+v", *got, *g.refusal)
			}
			if tokens.Calls != 0 {
				t.Errorf("token source called %d times for a refused request, want 0", tokens.Calls)
			}
			if n := len(srv.Requests()); n != 0 {
				t.Errorf("server received %d requests, want 0", n)
			}
		})
	}
}

func TestGenerateAllowedToolsNarrowed(t *testing.T) {
	var g gap
	for _, r := range recordedGaps() {
		if r.refusal == nil {
			g = r
		}
	}
	srv := serve(t, http.StatusOK, string(aitest.ReadTestdata(t, "vllm/tool_call.json")))
	tokens := &aitest.TokenCounter{}
	resp, err := newProfileClient(t, srv.URL(), g.opts, tokens).Generate(t.Context(), g.req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if resp.Status != protocol.StatusCompleted {
		t.Errorf("Status = %q, want the recorded call completed", resp.Status)
	}
	aitest.AssertJSON(t, toolMembers(t, aitest.Only(t, srv).Body), `{"tools":[`+getTimeWire+`],"tool_choice":"required"}`)
}
