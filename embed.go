package ai

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/internal/native"
)

const embeddingsRoute = "embeddings"

// InputType is what an embedding request's inputs are, for a retrieval model that embeds a search
// query and a stored document differently: e5 and Nomic mark each by a text prefix, and some
// hosted APIs by a parameter. The Client's EmbeddingProfile gives each type's convention, so a
// caller names the type and stays independent of the model. It is set in
// EmbeddingRequest.InputType.
type InputType int

// The input types of an embedding request.
const (
	// InputNone declares no type: the inputs are sent as given. It is the zero value.
	InputNone InputType = iota
	// InputQuery marks the inputs as search queries, matched against stored documents.
	InputQuery
	// InputDocument marks the inputs as documents, stored to be found by a query.
	InputDocument
)

var inputTypeNames = [...]string{InputNone: "none", InputQuery: "query", InputDocument: "document"}

// String returns the type's name, "none", "query", or "document", or, for an unknown type, its
// number in the form InputType(7), as fmt prints a value it has no name for.
func (t InputType) String() string {
	if t < 0 || int(t) >= len(inputTypeNames) {
		return fmt.Sprintf("InputType(%d)", int(t))
	}
	return inputTypeNames[t]
}

// MarshalText encodes the type by name, "none", "query", or "document", so a type in a JSON or
// other text configuration, such as an EmbeddingProfile's keys, reads as its name rather than its
// number. An unknown type is an error, as Embed refuses it.
func (t InputType) MarshalText() ([]byte, error) {
	if t < 0 || int(t) >= len(inputTypeNames) {
		return nil, fmt.Errorf("ai: unknown input type %d", int(t))
	}
	return []byte(inputTypeNames[t]), nil
}

// UnmarshalText decodes a type by name, accepting only "none", "query", and "document" and
// refusing any other name, the empty one and a number included, with an error that names it and
// the types accepted. It is strict so that a mistyped type in a configuration fails to decode
// instead of silently sending the inputs bare.
func (t *InputType) UnmarshalText(text []byte) error {
	i := slices.Index(inputTypeNames[:], string(text))
	if i < 0 {
		return fmt.Errorf("ai: unknown input type %q, want one of %s", text, strings.Join(inputTypeNames[:], ", "))
	}
	*t = InputType(i)
	return nil
}

// EmbeddingProfile is how the embedding model behind a Client's endpoint marks each input type:
// the convention Embed applies to a request of that type. Its zero value declares none, and every
// input is sent as given. go-ai ships no presets; a profile is the caller's, kept with the
// endpoint's configuration. It is set in Options.Embedding.
//
// A type the profile declares nothing for is a hint the model has no form for, as OpenAI's
// embeddings API has no input type, so Embed drops it and sends the inputs as given.
//
// EmbeddingProfile decodes from JSON under its snake_case names alone, matched case-sensitively
// whatever the decoder's options, so a member spelled as the Go field is unknown, an error under
// strict decoding. multilingual-e5's profile is:
//
//	{"input_types":{"document":{"prefix":"passage: "},"query":{"prefix":"query: "}}}
type EmbeddingProfile struct {
	// InputTypes maps each declared input type, by name in JSON, to its convention. New refuses
	// a convention for InputNone or for an unknown type.
	InputTypes map[InputType]InputConvention `json:"input_types,omitzero,case:strict"`
}

// InputConvention is how a model marks inputs of one type: a text prefix, a native member, or
// both.
type InputConvention struct {
	// Prefix is prepended to every input, as e5 reads "query: " before a query and "passage: "
	// before a document. Empty prepends nothing.
	Prefix string `json:"prefix,omitzero,case:strict"`
	// Native is native tier, as EmbeddingRequest.Native is: a JSON object whose members are
	// merged into the top level of the wire body, for a model whose API takes the type as a
	// parameter, such as {"input_type":"search_query"}. New refuses one that isn't a JSON object
	// with unique member names, or whose member clashes with one the typed fields set (model,
	// input, encoding_format); Embed refuses a request whose own Native has a member of the same
	// name.
	Native jsontext.Value `json:"native,omitzero,case:strict"`
}

func (p EmbeddingProfile) check() error {
	for _, t := range slices.Sorted(maps.Keys(p.InputTypes)) {
		conv := p.InputTypes[t]
		switch {
		case t == InputNone:
			return errors.New("ai: Options.Embedding declares a convention for input type none, which sends the inputs as given")
		case t < 0 || int(t) >= len(inputTypeNames):
			return fmt.Errorf("ai: Options.Embedding declares a convention for unknown input type %d", int(t))
		case !native.Valid(conv.Native):
			return fmt.Errorf("ai: Options.Embedding's %s convention: native field isn't a JSON object with unique member names", t)
		}
		if _, err := native.Marshal(embeddingRequest{Native: conv.Native}); err != nil {
			return fmt.Errorf("ai: Options.Embedding's %s convention: %w", t, err)
		}
	}
	return nil
}

func (p EmbeddingProfile) clone() EmbeddingProfile {
	if p.InputTypes == nil {
		return EmbeddingProfile{}
	}
	out := make(map[InputType]InputConvention, len(p.InputTypes))
	for t, conv := range p.InputTypes {
		conv.Native = slices.Clone(conv.Native)
		out[t] = conv
	}
	return EmbeddingProfile{InputTypes: out}
}

// EmbeddingRequest asks an embedding model for one vector per input. Embeddings sit outside the
// protocol and the Format: they go to the OpenAI-compatible embeddings endpoint whatever format
// the Client speaks, and an endpoint without the route answers with an *APIError.
type EmbeddingRequest struct {
	// Model is the embedding model's id. It is required.
	Model string
	// Input is the texts to embed, at least one, sent as one request: how large a batch is, and
	// how many run at once, are the caller's. An empty string is sent as it is: llama.cpp embeds
	// it, and an endpoint that refuses it answers with an *APIError.
	Input []string
	// InputType is what the inputs are, a query or a document, sent in the convention the
	// Client's EmbeddingProfile declares for it. InputNone, the zero value, sends the inputs as
	// given, and so does a type the profile declares nothing for: the hint is dropped.
	InputType InputType
	// Native is the native tier: a JSON object whose members are merged into the top level of
	// the wire body as they are, for a feature of the endpoint the typed fields don't cover,
	// such as dimensions. Its members mean what the endpoint makes them mean. A member that
	// clashes with one the typed fields set (model, input, encoding_format) is refused.
	Native jsontext.Value
}

// Embeddings is an embedding model's reply: one vector per input, in input order.
type Embeddings struct {
	// Vectors holds the embedding of each input, Vectors[i] that of Input[i].
	Vectors [][]float32
	// Usage counts the tokens the request took.
	Usage EmbeddingUsage
}

// EmbeddingUsage counts the tokens an embedding request took, in the endpoint's names.
// PromptTokens is what protocol.Usage calls InputTokens; an embedding generates no output, so
// there is no output count, and TotalTokens is PromptTokens on endpoints that count nothing else.
type EmbeddingUsage struct {
	// PromptTokens is the tokens of every input together.
	PromptTokens int
	// TotalTokens is the tokens the request took in all.
	TotalTokens int
}

// Embed sends req to the endpoint's embeddings route and returns one vector per input, in input
// order. A request of an input type the Client's EmbeddingProfile declares is sent in its
// convention: the prefix before every input, and the native members merged into the body beside
// the request's own Native. It refuses a nil request, one with no model or no input, an unknown
// input type, a Native that isn't a JSON object with unique member names or that clashes with
// the typed fields, and a Native with a member the convention's native also has, before the token
// source is called or anything is sent. The vectors travel as base64 of little-endian float32 (encoding_format "base64"), and
// each reply item's index places its vector; a reply whose items don't match the inputs one to
// one, or whose vector isn't whole float32s, is an error. A non-2xx reply is an *APIError with
// the OpenAI-style error body's details; a transport failure or a cancelled ctx is returned
// wrapped. It sends through the Client's query, token source, and fixed headers, and none of
// the format's. With an Options.Retry policy, a reply or failure the policy retries is sent again, and
// the last attempt's error is returned as it would be without one.
func (c *Client) Embed(ctx context.Context, req *EmbeddingRequest) (*Embeddings, error) {
	if req == nil {
		return nil, errors.New("ai: embed: no request")
	}
	body, err := encodeEmbeddingRequest(req, c.embedding)
	if err != nil {
		return nil, fmt.Errorf("ai: embed: %w", err)
	}
	reply, err := c.fetch(ctx, http.MethodPost, embeddingsRoute, fixedBody(nil, body), "application/json",
		format.DecodeOpenAIError)
	if err != nil {
		return nil, fmt.Errorf("ai: embed: %w", err)
	}
	e, err := decodeEmbeddings(reply, len(req.Input))
	if err != nil {
		return nil, fmt.Errorf("ai: embed: %w", err)
	}
	return e, nil
}

func encodeEmbeddingRequest(req *EmbeddingRequest, profile EmbeddingProfile) ([]byte, error) {
	switch {
	case req.Model == "":
		return nil, errors.New("request has no model")
	case len(req.Input) == 0:
		return nil, errors.New("request has no input")
	case req.InputType < 0 || int(req.InputType) >= len(inputTypeNames):
		return nil, fmt.Errorf("request has unknown input type %d", int(req.InputType))
	case !native.Valid(req.Native):
		return nil, errors.New("request's native field isn't a JSON object with unique member names")
	}
	input, nat := req.Input, req.Native
	if conv, ok := profile.InputTypes[req.InputType]; ok && req.InputType != InputNone {
		if conv.Prefix != "" {
			input = make([]string, len(req.Input))
			for i, s := range req.Input {
				input[i] = conv.Prefix + s
			}
		}
		merged, err := native.Merge(req.Native, conv.Native)
		if ce, ok := errors.AsType[*native.ClashError](err); ok {
			return nil, fmt.Errorf("native member %s clashes with the %s convention's native member", ce.Member, req.InputType)
		}
		if err != nil {
			return nil, err
		}
		nat = merged
	}
	return native.Marshal(embeddingRequest{
		Model:          req.Model,
		Input:          input,
		EncodingFormat: "base64",
		Native:         nat,
	})
}

func decodeEmbeddings(body []byte, n int) (*Embeddings, error) {
	var wire embeddingReply
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(wire.Data) != n {
		return nil, fmt.Errorf("response has %d embeddings for %d inputs", len(wire.Data), n)
	}
	vectors := make([][]float32, n)
	for i, d := range wire.Data {
		switch {
		case d.Index < 0 || d.Index >= n:
			return nil, fmt.Errorf("response embedding %d has index %d, outside the %d inputs", i, d.Index, n)
		case vectors[d.Index] != nil:
			return nil, fmt.Errorf("response embedding %d repeats index %d", i, d.Index)
		}
		v, err := decodeVector(d.Embedding)
		if err != nil {
			return nil, fmt.Errorf("response embedding %d: %w", i, err)
		}
		vectors[d.Index] = v
	}
	return &Embeddings{
		Vectors: vectors,
		Usage:   EmbeddingUsage{PromptTokens: wire.Usage.PromptTokens, TotalTokens: wire.Usage.TotalTokens},
	}, nil
}

func decodeVector(s string) ([]float32, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("embedding isn't base64: %w", err)
	}
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("embedding of %d bytes isn't whole float32s", len(b))
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v, nil
}

type embeddingRequest struct {
	Model          string         `json:"model"`
	Input          []string       `json:"input"`
	EncodingFormat string         `json:"encoding_format"`
	Native         jsontext.Value `json:",embed"`
}

type embeddingReply struct {
	Data []struct {
		Index     int    `json:"index"`
		Embedding string `json:"embedding"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}
