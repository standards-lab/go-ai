package responses

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// DecodeResponse decodes a response resource into a response, its output items in the server's
// order:
//
//	message        a message of its role: an output_text part for each output_text, and a
//	               refusal part for each refusal, their annotations and logprobs dropped
//	reasoning      a reasoning item: its content's reasoning_text parts, its summary's
//	               summary_text parts, and its encrypted_content the format's envelope,
//	               holding the server's encrypted_content and the reasoning_text parts'
//	               texts, which EncodeRequest reads back
//	function_call  a function_call item: its call_id, name, and arguments
//
// An output item of any other type, such as a hosted tool's call the spec defines or an
// extension's item, is one the protocol has no form for, and is skipped, the items after it
// decoded as if it weren't there; so is a message's content part of a type that isn't an output
// part, one of output_text, refusal, and reasoning_text, the types the format decodes. A part of
// one of those types its item doesn't take, such as a message's reasoning_text, is malformed, and
// an error naming it, as is a reasoning item's part of any type but reasoning_text in its content
// and summary_text in its summary. Every item's id and status are the server's, as it gave them:
// the format derives nothing.
//
// The response's status is the server's too, and must be completed, incomplete, or failed: one
// in progress or queued is a background response, which a stateless request never asks for, and
// is an error, as is any other. incomplete_details' reason, such as max_output_tokens or
// content_filter, and a failed response's error, its code and message, are taken as given. The
// response's id and model are its ID and Model. The usage is the server's input_tokens,
// output_tokens, total_tokens, input_tokens_details.cached_tokens, and
// output_tokens_details.reasoning_tokens.
func (*Format) DecodeResponse(body []byte) (*protocol.Response, error) {
	resp, err := decodeResponse(body)
	if err != nil {
		return nil, fmt.Errorf("responses: %w", err)
	}
	return resp, nil
}

func decodeResponse(body []byte) (*protocol.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	resp := &protocol.Response{ID: wire.ID, Model: wire.Model, Usage: protocol.Usage{
		InputTokens:  wire.Usage.InputTokens,
		OutputTokens: wire.Usage.OutputTokens,
		TotalTokens:  wire.Usage.TotalTokens,
		InputTokensDetails: protocol.InputTokensDetails{
			CachedTokens: wire.Usage.InputTokensDetails.CachedTokens,
		},
		OutputTokensDetails: protocol.OutputTokensDetails{
			ReasoningTokens: wire.Usage.OutputTokensDetails.ReasoningTokens,
		},
	}}
	switch s := protocol.Status(wire.Status); s {
	case protocol.StatusCompleted, protocol.StatusIncomplete, protocol.StatusFailed:
		resp.Status = s
	default:
		return nil, fmt.Errorf("response has status %q, not completed, incomplete, or failed", wire.Status)
	}
	if d := wire.IncompleteDetails; d != nil {
		resp.IncompleteDetails = &protocol.IncompleteDetails{Reason: protocol.IncompleteReason(d.Reason)}
	}
	if e := wire.Error; e != nil {
		resp.Error = &protocol.ResponseError{Code: e.Code, Message: e.Message}
	}
	for i, w := range wire.Output {
		it, ok, err := decodeItem(w)
		if err != nil {
			return nil, fmt.Errorf("output item %d: %w", i, err)
		}
		if ok {
			resp.Output = append(resp.Output, it)
		}
	}
	return resp, nil
}

func decodeItem(w outputItem) (protocol.Item, bool, error) {
	status := protocol.Status(w.Status)
	switch w.Type {
	case "message":
		m := protocol.Message{ID: w.ID, Status: status, Role: protocol.Role(w.Role)}
		for i, p := range w.Content {
			switch p.Type {
			case "output_text":
				m.Content = append(m.Content, protocol.OutputText{Text: p.Text})
			case "refusal":
				m.Content = append(m.Content, protocol.Refusal{Refusal: p.Refusal})
			default:
				if outputPart(p.Type) {
					return nil, false, fmt.Errorf("message content %d has type %q, which a message doesn't take", i, p.Type)
				}
			}
		}
		return m, true, nil
	case "reasoning":
		r := protocol.Reasoning{ID: w.ID, Status: status}
		texts := make([]string, 0, len(w.Content))
		for i, p := range w.Content {
			if p.Type != "reasoning_text" {
				return nil, false, fmt.Errorf("reasoning content %d has unknown type %q", i, p.Type)
			}
			r.Content = append(r.Content, protocol.ReasoningText{Text: p.Text})
			texts = append(texts, p.Text)
		}
		for i, p := range w.Summary {
			if p.Type != "summary_text" {
				return nil, false, fmt.Errorf("reasoning summary %d has unknown type %q", i, p.Type)
			}
			r.Summary = append(r.Summary, protocol.SummaryText{Text: p.Text})
		}
		sealed, err := sealEnvelope(envelope{EncryptedContent: &w.EncryptedContent, Content: &texts})
		if err != nil {
			return nil, false, err
		}
		r.EncryptedContent = sealed
		return r, true, nil
	case "function_call":
		return protocol.FunctionCall{
			ID:        w.ID,
			Status:    status,
			CallID:    w.CallID,
			Name:      w.Name,
			Arguments: w.Arguments,
		}, true, nil
	}
	return nil, false, nil
}

func knownItem(typ string) bool {
	return typ == "message" || typ == "reasoning" || typ == "function_call"
}

func outputPart(typ string) bool {
	return typ == "output_text" || typ == "refusal" || typ == "reasoning_text"
}

func sealEnvelope(e envelope) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return envelopePrefix + base64.StdEncoding.EncodeToString(b), nil
}

// DecodeError decodes the error body of a non-2xx reply. The spec defines none, so the format
// reads the OpenAI-style one its endpoints answer with, llama.cpp's router among them,
// {"error": {"message", "type", "code"}}, through format.DecodeOpenAIError, which gives a
// numeric code, as the router's is, as its decimal text.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	return format.DecodeOpenAIError(body)
}
