//go:build integration

package integration_test

import (
	"encoding/json/v2"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// TestStructuredOutput asks for a country's capital under a strict JSON schema in each format,
// and wants a completed answer whose output_text decodes into a Go value of the schema's shape,
// no member unknown, as a caller decodes it, holding Paris and France.
func TestStructuredOutput(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		resp, err := c.Generate(requestContext(t), &protocol.Request{
			Model: model,
			Input: []protocol.Item{userText("What is the capital of France?")},
			Text: protocol.TextParam{Format: protocol.JSONSchemaFormat{
				Name:        "capital",
				Description: "A country and its capital city.",
				Schema: []byte(`{
					"type": "object",
					"properties": {
						"capital": {"type": "string"},
						"country": {"type": "string"}
					},
					"required": ["capital", "country"],
					"additionalProperties": false
				}`),
				Strict: true,
			}},
			MaxOutputTokens: agentBound,
		})
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		text := completedText(t, resp)
		var got struct {
			Capital string `json:"capital"`
			Country string `json:"country"`
		}
		if err := json.Unmarshal([]byte(text), &got, json.RejectUnknownMembers(true)); err != nil {
			t.Fatalf("output_text %q doesn't decode as the schema's object: %v", text, err)
		}
		if got.Capital != "Paris" || got.Country != "France" {
			t.Errorf("decoded %+v, want capital Paris, country France", got)
		}
	})
}
