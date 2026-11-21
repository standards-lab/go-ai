package ai

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-ai/format"
)

const modelsRoute = "models"

// Model is one entry of the endpoint's model list.
type Model struct {
	// ID is the model's id, the one a request's Model names.
	ID string
	// Native is the native tier: the list's entry for the model, the whole JSON object as the
	// endpoint sent it, id and all, for the members the typed fields don't cover, such as
	// vLLM's root and max_model_len, or llama.cpp's router's status. Its members mean what the
	// endpoint makes them mean. It is the Model's own copy, not a view of the reply.
	Native jsontext.Value
}

// Models GETs the endpoint's OpenAI-compatible models route, whatever format the Client speaks,
// and returns its list, in the reply's order. An endpoint without the route answers with an
// *APIError, with the OpenAI-style error body's details; a transport failure or a cancelled ctx
// is returned wrapped. It sends through the Client's query, token source, and fixed headers, and
// none of the format's. With an Options.Retry policy, a reply or failure the policy retries is
// sent again, which runs nothing twice: a GET is safe to repeat.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	reply, err := c.fetch(ctx, http.MethodGet, modelsRoute, nil, "", format.DecodeOpenAIError)
	if err != nil {
		return nil, fmt.Errorf("ai: models: %w", err)
	}
	var wire modelsReply
	if err := json.Unmarshal(reply, &wire); err != nil {
		return nil, fmt.Errorf("ai: models: decode response: %w", err)
	}
	models := make([]Model, len(wire.Data))
	for i, entry := range wire.Data {
		var m struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(entry, &m); err != nil {
			return nil, fmt.Errorf("ai: models: decode response: %w", err)
		}
		models[i] = Model{ID: m.ID, Native: entry}
	}
	return models, nil
}

type modelsReply struct {
	Data []jsontext.Value `json:"data"`
}
