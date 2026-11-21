package messages

import (
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"strconv"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// DecodeResponse decodes a Messages response body into output items in the order of its content
// blocks:
//
//	thinking           a reasoning item: its content one reasoning_text part of the thinking
//	                   text, and its encrypted_content the format's envelope holding the text
//	                   and the signature, which EncodeRequest reads back
//	redacted_thinking  a reasoning item of no content, its envelope holding the block's data
//	text               an output_text part of an assistant message: each run of consecutive
//	                   text blocks becomes one message, a part per block, in order
//	tool_use           a function_call item: its call_id the block's id, its name the block's,
//	                   and its arguments the block's input as JSON text
//
// A block of any other type is an error. Each output item gets the id itemID derives from the
// reply's id and the item's place in the output, as Messages supplies no item ids. The reply's
// stop_reason gives the response's status:
//
//	end_turn, tool_use, stop_sequence  completed
//	max_tokens                         incomplete, reason max_output_tokens
//	model_context_window_exceeded      incomplete, reason go-ai:context_window
//	refusal                            incomplete, reason content_filter
//
// The last output item takes the response's status, and the items before it are completed, as
// the spec has only the last item incomplete. pause_turn, which asks the caller to send the turn
// back for the endpoint to continue, has no place in a stateless response, and is an error, as is
// any other stop_reason. model_context_window_exceeded is the input and output filling the
// model's context window, not the request's max_tokens, so it takes protocol.ReasonContextWindow.
//
// The reply's id and model are the response's ID and Model. The reply's usage gives the
// response's: InputTokens is input_tokens with the prompt cache's cache_read_input_tokens and
// cache_creation_input_tokens added, as Messages counts those apart from input_tokens and the
// spec's input_tokens counts every input token; InputTokensDetails.CachedTokens is
// cache_read_input_tokens, the input served from the cache; OutputTokens is output_tokens;
// OutputTokensDetails.ReasoningTokens is output_tokens_details.thinking_tokens; and TotalTokens
// is the input and output tokens' sum, which Messages doesn't report.
func (*Format) DecodeResponse(body []byte) (*protocol.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("messages: decode response: %w", err)
	}
	resp := &protocol.Response{ID: wire.ID, Model: wire.Model, Usage: decodeUsage(wire.Usage)}
	var ok bool
	if resp.Status, resp.IncompleteDetails, ok = decodeStop(wire.StopReason); !ok {
		return nil, fmt.Errorf("messages: response has unknown stop_reason %q", wire.StopReason)
	}
	nextItemID := func() string { return itemID(wire.ID, len(resp.Output)) }
	var runText []protocol.ContentPart
	closeRun := func() {
		if runText == nil {
			return
		}
		resp.Output = append(resp.Output, protocol.Message{
			ID:      nextItemID(),
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: runText,
		})
		runText = nil
	}
	for i, b := range wire.Content {
		if b.Type == "text" {
			runText = append(runText, protocol.OutputText{Text: b.Text})
			continue
		}
		closeRun()
		switch b.Type {
		case "thinking", "redacted_thinking":
			r, err := decodeReasoning(b)
			if err != nil {
				return nil, fmt.Errorf("messages: response content block %d: %w", i, err)
			}
			r.ID = nextItemID()
			resp.Output = append(resp.Output, r)
		case "tool_use":
			resp.Output = append(resp.Output, protocol.FunctionCall{
				ID:        nextItemID(),
				Status:    protocol.StatusCompleted,
				CallID:    b.ID,
				Name:      b.Name,
				Arguments: string(b.Input),
			})
		default:
			return nil, fmt.Errorf("messages: response content block %d has unknown type %q", i, b.Type)
		}
	}
	closeRun()
	if last := len(resp.Output) - 1; last >= 0 {
		resp.Output[last] = withStatus(resp.Output[last], resp.Status)
	}
	return resp, nil
}

func decodeReasoning(b block) (protocol.Reasoning, error) {
	r := protocol.Reasoning{Status: protocol.StatusCompleted}
	var e envelope
	if b.Type == "thinking" {
		r.Content = []protocol.ReasoningText{{Text: b.Thinking}}
		e = envelope{Thinking: &b.Thinking, Signature: &b.Signature}
	} else {
		e = envelope{Data: &b.Data}
	}
	sealed, err := sealEnvelope(e)
	if err != nil {
		return protocol.Reasoning{}, err
	}
	r.EncryptedContent = sealed
	return r, nil
}

func sealEnvelope(e envelope) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return envelopePrefix + base64.StdEncoding.EncodeToString(b), nil
}

func withStatus(it protocol.Item, status protocol.Status) protocol.Item {
	switch it := it.(type) {
	case protocol.Reasoning:
		it.Status = status
		return it
	case protocol.Message:
		it.Status = status
		return it
	case protocol.FunctionCall:
		it.Status = status
		return it
	}
	return it
}

// See docs/design.md, "Implementation notes".
func itemID(reply string, output int) string {
	if reply == "" {
		return ""
	}
	return reply + "/" + strconv.Itoa(output)
}

func decodeUsage(u usage) protocol.Usage {
	input := count(u.InputTokens) + count(u.CacheReadInputTokens) + count(u.CacheCreationInputTokens)
	output := count(u.OutputTokens)
	return protocol.Usage{
		InputTokens:         input,
		OutputTokens:        output,
		TotalTokens:         input + output,
		InputTokensDetails:  protocol.InputTokensDetails{CachedTokens: count(u.CacheReadInputTokens)},
		OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: count(u.OutputTokensDetails.ThinkingTokens)},
	}
}

func count(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

func decodeStop(reason string) (protocol.Status, *protocol.IncompleteDetails, bool) {
	switch reason {
	case "end_turn", "tool_use", "stop_sequence":
		return protocol.StatusCompleted, nil, true
	case "max_tokens":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}, true
	case "model_context_window_exceeded":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonContextWindow}, true
	case "refusal":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}, true
	}
	return "", nil, false
}

// DecodeError decodes an error body of either shape a Messages endpoint answers with:
// Anthropic's, {"type": "error", "error": {"type", "message"}}, and llama.cpp's OpenAI-style one,
// {"error": {"code", "message", "type"}}. Both keep the message and the type in an error object,
// so format.DecodeOpenAIError, which describes the shapes it reads, decodes either; Anthropic's
// gives no code, and its top-level type, always "error", isn't the error's.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	return format.DecodeOpenAIError(body)
}
