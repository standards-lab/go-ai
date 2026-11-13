//go:build integration

package integration_test

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/protocol"
)

// folded is a streamed reply folded: the Response its events make, and the text of its
// output_text, function-call arguments, and reasoning deltas, each joined, with the item ids the
// output_text and arguments deltas named.
type folded struct {
	resp      *protocol.Response
	text      strings.Builder
	arguments strings.Builder
	reasoning strings.Builder
	textIDs   map[string]bool
	argIDs    map[string]bool
}

// accumulate ranges a stream of req from c, folds its events, and returns them folded, failing t
// on a stream error or an event the Accumulator refuses.
func accumulate(t *testing.T, c *ai.Client, req *protocol.Request) *folded {
	t.Helper()
	var acc protocol.Accumulator
	f := &folded{textIDs: map[string]bool{}, argIDs: map[string]bool{}}
	for e, err := range c.GenerateStream(requestContext(t), req) {
		if err != nil {
			t.Fatalf("GenerateStream: %v", err)
		}
		switch d := e.(type) {
		case protocol.OutputTextDelta:
			f.text.WriteString(d.Delta)
			f.textIDs[d.ItemID] = true
		case protocol.FunctionCallArgumentsDelta:
			f.arguments.WriteString(d.Delta)
			f.argIDs[d.ItemID] = true
		case protocol.ReasoningDelta:
			f.reasoning.WriteString(d.Delta)
		}
		if err := acc.Add(e); err != nil {
			t.Fatalf("Accumulator.Add: %v", err)
		}
	}
	f.resp = acc.Response()
	return f
}

// streamer returns a generate for answerWithWeather that streams with c and folds the events.
func streamer(t *testing.T, c *ai.Client) func(*protocol.Request) *protocol.Response {
	return func(req *protocol.Request) *protocol.Response {
		t.Helper()
		return accumulate(t, c, req).resp
	}
}

// TestStreamText streams a chat reply through each format, folds its events, and wants the
// Response completed with its usage, its text the deltas joined, and its message an id, the
// item_id of every delta.
func TestStreamText(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		f := accumulate(t, c, &protocol.Request{
			Model:           model,
			Instructions:    "Answer in one sentence.",
			Input:           []protocol.Item{userText("What is the capital of France?")},
			MaxOutputTokens: agentBound,
		})
		text := completedText(t, f.resp)
		if text != f.text.String() {
			t.Errorf("output_text %q, want the deltas joined, %q", text, f.text.String())
		}
		for _, it := range f.resp.Output {
			if m, ok := it.(protocol.Message); ok && (m.ID == "" || len(f.textIDs) != 1 || !f.textIDs[m.ID]) {
				t.Errorf("message id %q, deltas' item ids %v; want an id, the item_id of every delta", m.ID, f.textIDs)
			}
		}
		if u := f.resp.Usage; u.InputTokens == 0 || u.OutputTokens == 0 || u.TotalTokens != u.InputTokens+u.OutputTokens {
			t.Errorf("Usage = %+v, want input and output counted, and total their sum", u)
		}
	})
}

// TestStreamFunctionCall streams a reply that must call get_weather through each format, folds
// its events, and wants the Response completed with one function call: a call id, the function's
// name, arguments that parse as a JSON object naming the city, the deltas joined, and an id, the
// item_id of every arguments delta.
func TestStreamFunctionCall(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		f := accumulate(t, c, &protocol.Request{
			Model:           model,
			Input:           []protocol.Item{weatherQuestion},
			Tools:           []protocol.FunctionTool{getWeather},
			ToolChoice:      protocol.ToolChoiceRequired,
			MaxOutputTokens: agentBound,
		})
		call := weatherCall(t, f.resp)
		if call.Arguments != f.arguments.String() {
			t.Errorf("arguments %q, want the deltas joined, %q", call.Arguments, f.arguments.String())
		}
		var args struct {
			City string `json:"city"`
		}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil || !strings.Contains(args.City, "Paris") {
			t.Errorf("arguments %q: %v; want a JSON object naming Paris", call.Arguments, err)
		}
		if call.ID == "" || len(f.argIDs) != 1 || !f.argIDs[call.ID] {
			t.Errorf("function call id %q, arguments deltas' item ids %v; want an id, the item_id of every delta", call.ID, f.argIDs)
		}
	})
}

// TestStreamReasoningRoundTrip streams a reply that must call get_weather through each format,
// with a reasoning effort, and wants its accumulated output to hold reasoning ahead of the call,
// completed, its text the reasoning deltas joined. It sends that output back as it came,
// reasoning included, with the call's output, streams again, and wants the accumulated Response
// completed with an answer that uses it.
func TestStreamReasoningRoundTrip(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		req := reasoningRequest(model)
		f := accumulate(t, c, req)
		r := leadingReasoning(t, f.resp)
		if got := reasoningText(r); got != f.reasoning.String() || r.Status != protocol.StatusCompleted {
			t.Errorf("reasoning %q, status %q; want it completed, the reasoning deltas joined, %q", got, r.Status, f.reasoning.String())
		}
		answerWithWeather(t, req, f.resp, weatherCall(t, f.resp), streamer(t, c))
	})
}

// TestStreamCancel streams a long answer through each format and cancels the context once a few
// output_text deltas have arrived. It wants no event after the cancel, the context's error as the
// sequence's one error, and the accumulated Response in progress, its message holding the text
// the deltas made so far.
func TestStreamCancel(t *testing.T) {
	eachFormat(t, agent, func(t *testing.T, c *ai.Client, model string) {
		const cancelAt = 5
		ctx, cancel := context.WithCancel(requestContext(t))
		defer cancel()
		var acc protocol.Accumulator
		var deltas strings.Builder
		var n int
		var errs []error
		for e, err := range c.GenerateStream(ctx, &protocol.Request{
			Model:           model,
			Input:           []protocol.Item{userText("Write a long essay, of at least two thousand words, on the history of Paris.")},
			Reasoning:       protocol.ReasoningParam{Effort: protocol.ReasoningEffortLow},
			MaxOutputTokens: agentBound,
		}) {
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if len(errs) > 0 || ctx.Err() != nil {
				t.Errorf("event %#v follows the cancel or the error", e)
			}
			if err := acc.Add(e); err != nil {
				t.Fatalf("Accumulator.Add: %v", err)
			}
			if d, ok := e.(protocol.OutputTextDelta); ok {
				deltas.WriteString(d.Delta)
				if n++; n == cancelAt {
					cancel()
				}
			}
		}
		if n < cancelAt {
			t.Fatalf("the stream ended after %d output_text deltas, before the cancel at %d; errors %v", n, cancelAt, errs)
		}
		if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
			t.Fatalf("GenerateStream errors = %v, want one context.Canceled", errs)
		}
		resp := acc.Response()
		if resp.Status != protocol.StatusInProgress || len(resp.Output) == 0 {
			t.Fatalf("Response = %#v, want it in progress, with output", resp)
		}
		m, ok := resp.Output[len(resp.Output)-1].(protocol.Message)
		switch {
		case !ok:
			t.Fatalf("last output = %#v, want the message", resp.Output[len(resp.Output)-1])
		case m.Status != protocol.StatusInProgress || len(m.Content) != 1 || m.Content[0] != (protocol.OutputText{Text: deltas.String()}):
			t.Errorf("message = %#v, want it in progress, holding the deltas joined, %q", m, deltas.String())
		}
	})
}
