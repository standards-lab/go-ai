package ai

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/standards-lab/go-ai/format"
)

const tokenizeRoute = "tokenize"

// TokenizeRequest asks the engine to count a text's tokens in a model's own tokenizer, as a
// caller measuring chunks for an embedding model needs. Tokenizing sits outside the protocol and
// the Format, as embedding does: it goes to vLLM's tokenize route whatever format the Client
// speaks, and an endpoint without the route answers with an *APIError.
type TokenizeRequest struct {
	// Model is the model whose tokenizer counts, as the endpoint names it. It is required.
	Model string
	// Text is the text to count, sent as one prompt. An empty string is sent as it is.
	Text string
	// InputType is what the text will be embedded as. The prefix the Client's EmbeddingProfile
	// declares for it is prepended before counting, so the count is what Embed of the same text
	// and type takes; the convention's native members aren't sent. InputNone, the zero value, and
	// a type the profile declares nothing for count the text as given.
	InputType InputType
}

// Tokens is the engine's count of a text's tokens, special tokens included.
type Tokens struct {
	// Count is the text's tokens, the prompt tokens an embedding of the same text reports.
	Count int
	// MaxModelLen is the most tokens the model takes, as the reply's max_model_len gives it;
	// zero when the reply has none.
	MaxModelLen int
	// IDs is the token ids, in order, as the reply's tokens gives them; nil when it has none.
	IDs []int
}

// Tokenize counts req's text in its model's tokenizer through the endpoint's tokenize route,
// vLLM's POST /tokenize, which vLLM serves at the server's root, not below /v1: a Client whose
// base URL is the server's root reaches it, and one whose base URL is a gateway's API root that
// forwards it reaches it below that root. It sends the model, the text as prompt with the
// prefix the Client's EmbeddingProfile declares for the request's InputType, and
// add_special_tokens true, so the count is what an embedding of the same text takes. It sends
// nothing else, a convention's native members included. It refuses a nil request, one with no
// model, and an unknown input type, before the token source is called or anything is sent. A
// reply without a count, or whose count disagrees with its token ids when it has them, is an
// error. A non-2xx reply is an *APIError with the OpenAI-style error body's details; a transport
// failure or a cancelled ctx is returned wrapped. It sends through the Client's query, token
// source, and fixed headers, and none of the format's. With an Options.Retry policy, a reply or
// failure the policy retries is sent again, and the last attempt's error is returned as it would
// be without one.
func (c *Client) Tokenize(ctx context.Context, req *TokenizeRequest) (*Tokens, error) {
	if req == nil {
		return nil, errors.New("ai: tokenize: no request")
	}
	body, err := encodeTokenizeRequest(req, c.embedding)
	if err != nil {
		return nil, fmt.Errorf("ai: tokenize: %w", err)
	}
	reply, err := c.fetch(ctx, http.MethodPost, tokenizeRoute, fixedBody(nil, body), "application/json",
		format.DecodeOpenAIError)
	if err != nil {
		return nil, fmt.Errorf("ai: tokenize: %w", err)
	}
	tokens, err := decodeTokens(reply)
	if err != nil {
		return nil, fmt.Errorf("ai: tokenize: %w", err)
	}
	return tokens, nil
}

func encodeTokenizeRequest(req *TokenizeRequest, profile EmbeddingProfile) ([]byte, error) {
	switch {
	case req.Model == "":
		return nil, errors.New("request has no model")
	case !req.InputType.known():
		return nil, fmt.Errorf("request has unknown input type %d", int(req.InputType))
	}
	prompt := req.Text
	if conv, ok := profile.convention(req.InputType); ok {
		prompt = conv.Prefix + prompt
	}
	return json.Marshal(tokenizeRequest{Model: req.Model, Prompt: prompt, AddSpecialTokens: true})
}

func decodeTokens(body []byte) (*Tokens, error) {
	var wire tokenizeReply
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	switch {
	case wire.Count == nil:
		return nil, errors.New("response has no count")
	case *wire.Count < 0:
		return nil, fmt.Errorf("response counts %d tokens", *wire.Count)
	case wire.Tokens != nil && len(wire.Tokens) != *wire.Count:
		return nil, fmt.Errorf("response counts %d tokens but lists %d", *wire.Count, len(wire.Tokens))
	}
	return &Tokens{Count: *wire.Count, MaxModelLen: wire.MaxModelLen, IDs: wire.Tokens}, nil
}

type tokenizeRequest struct {
	Model            string `json:"model"`
	Prompt           string `json:"prompt"`
	AddSpecialTokens bool   `json:"add_special_tokens"`
}

type tokenizeReply struct {
	Count       *int  `json:"count"`
	MaxModelLen int   `json:"max_model_len"`
	Tokens      []int `json:"tokens"`
}
