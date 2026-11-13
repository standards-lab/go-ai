//go:build integration

package integration_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// TestChat asks the agent a one-word question in each format and wants a completed answer that
// names it, with input and output tokens counted.
func TestChat(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model:           model,
			Instructions:    "Answer in one word.",
			Input:           []protocol.Item{userText("What is the capital of France?")},
			MaxOutputTokens: agentBound,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		text := completedText(t, resp)
		if !strings.Contains(strings.ToLower(text), "paris") {
			t.Errorf("output_text %q doesn't name Paris", text)
		}
		if u := resp.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens != u.InputTokens+u.OutputTokens {
			t.Errorf("Usage = %+v, want input and output counted, and total their sum", u)
		}
	})
}

// TestChatIncompleteAtMaxOutputTokens asks for a long answer under a small bound, and wants the
// response incomplete for max_output_tokens, with no more output tokens than the bound.
func TestChatIncompleteAtMaxOutputTokens(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		const bound = 16
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model:           model,
			Input:           []protocol.Item{userText("Write a long essay on the history of Paris.")},
			MaxOutputTokens: bound,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		want := protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}
		if resp.Status != protocol.StatusIncomplete || resp.IncompleteDetails == nil || *resp.IncompleteDetails != want {
			t.Errorf("Status = %q, IncompleteDetails = %+v; want incomplete, %+v", resp.Status, resp.IncompleteDetails, want)
		}
		if resp.Usage.OutputTokens > bound {
			t.Errorf("Usage.OutputTokens = %d, want at most %d", resp.Usage.OutputTokens, bound)
		}
	})
}

// TestChatUnknownModelIsAPIError asks the agent's endpoint for a model it doesn't serve, and wants
// an *APIError of 404 carrying the endpoint's message and type.
func TestChatUnknownModelIsAPIError(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, _ string) {
		_, err := c.Generate(requestContext(t), &protocol.Request{
			Model:           "go-ai/no-such-model",
			Input:           []protocol.Item{userText("Hello.")},
			MaxOutputTokens: 16,
		})
		var apiErr *ai.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("Generate error = %v, want an *APIError", err)
		}
		if apiErr.Status != http.StatusNotFound || apiErr.Message == "" || apiErr.Type == "" {
			t.Errorf("APIError = %d %q (%q), want 404 with the endpoint's message and type", apiErr.Status, apiErr.Message, apiErr.Type)
		}
	})
}
