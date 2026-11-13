package ai

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-ai/format"
)

// modelsRoute is the OpenAI-compatible model list endpoint, relative to the base URL.
const modelsRoute = "models"

// Model is one entry of the endpoint's model list.
type Model struct {
	// ID is the model's id, the one a request's Model names.
	ID string
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
	for i, m := range wire.Data {
		models[i] = Model{ID: m.ID}
	}
	return models, nil
}

// modelsReply is the OpenAI-compatible model list reply body. Each entry's other members, such
// as owned_by, and llama.cpp's router's status, are the endpoint's, which the client doesn't
// keep.
type modelsReply struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}
