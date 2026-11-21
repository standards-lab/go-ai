//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

func reasoningRequest(model string) *protocol.Request {
	return &protocol.Request{
		Model:           model,
		Input:           []protocol.Item{weatherQuestion},
		Tools:           []protocol.FunctionTool{getWeather},
		ToolChoice:      protocol.ToolChoiceRequired,
		Reasoning:       protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow},
		MaxOutputTokens: agentBound,
	}
}

func leadingReasoning(t *testing.T, resp *protocol.Response) protocol.Reasoning {
	t.Helper()
	if len(resp.Output) == 0 {
		t.Fatalf("Output is empty, want reasoning ahead of the call")
	}
	r, ok := resp.Output[0].(protocol.Reasoning)
	if !ok {
		t.Fatalf("Output[0] = %#v, want reasoning", resp.Output[0])
	}
	if strings.TrimSpace(reasoningText(r)) == "" || r.EncryptedContent == "" {
		t.Fatalf("reasoning = %#v, want text and encrypted content", r)
	}
	return r
}

func TestReasoningRoundTrip(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		generate := generator(t, c)
		req := reasoningRequest(model)
		resp := generate(req)
		leadingReasoning(t, resp)
		answerWithWeather(t, req, resp, weatherCall(t, resp), generate)
	})
}

func reasoningText(r protocol.Reasoning) string {
	var b strings.Builder
	for _, p := range r.Content {
		b.WriteString(p.Text)
	}
	return b.String()
}
