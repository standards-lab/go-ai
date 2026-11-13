//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// reasoningRequest is the weather question under the required choice, with a reasoning effort,
// to the agent's model.
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

// leadingReasoning returns resp's first output item, failing t unless it is reasoning with text
// and the encrypted content a format sends back.
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

// TestReasoningRoundTrip asks with a reasoning effort for a call in each format, wants reasoning
// ahead of it, sends the response's output back as it came, reasoning included, with the call's
// output, and wants a completed answer that uses it. That the reasoning reached the wire, as the
// assistant message's reasoning, its thinking block, or the reasoning item's content, is the unit
// tier's to prove; the exported API doesn't show the body.
func TestReasoningRoundTrip(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		generate := generator(t, c)
		req := reasoningRequest(model)
		resp := generate(req)
		leadingReasoning(t, resp)
		answerWithWeather(t, req, resp, weatherCall(t, resp), generate)
	})
}

// reasoningText returns r's reasoning text, its parts joined.
func reasoningText(r protocol.Reasoning) string {
	var b strings.Builder
	for _, p := range r.Content {
		b.WriteString(p.Text)
	}
	return b.String()
}
