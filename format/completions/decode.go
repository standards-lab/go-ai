package completions

import (
	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// DecodeResponse decodes a Chat Completions response body. Each output item gets the id itemID
// derives from the reply's id and the item's place in the output, as Chat Completions supplies no
// item ids. The first choice's message's reasoning, when it has some, becomes a reasoning item
// ahead of the others, read as reasoningText reads it, from reasoning_content or vLLM's reasoning:
// its content one reasoning_text part of the text, and its encrypted_content the format's envelope
// holding it, which EncodeRequest reads back. The message then becomes an assistant message item,
// its content an output_text part and its refusal a refusal part; a message with neither gives no
// message item. Each of its tool_calls then becomes a function_call item, in order, its call_id the
// call's id; a call of a type other than function is an error. The first choice's finish_reason
// gives the response's status:
//
//	stop, tool_calls  completed
//	length            incomplete, reason max_output_tokens
//	content_filter    incomplete, reason content_filter
//
// The last output item takes the response's status, and the items before it are completed, as the
// spec has only the last item incomplete; a reasoning item takes its status like any other, so a
// reply of reasoning alone stopped at length is an incomplete reasoning item. Any other
// finish_reason, or a body with no choices, is an error. The reply's id and model give the
// response's ID and Model. The reply's usage gives the response's: prompt_tokens,
// completion_tokens, total_tokens, the cached input tokens from
// prompt_tokens_details.cached_tokens, and the reasoning tokens, from
// completion_tokens_details.reasoning_tokens or, when that is absent, the usage's top-level
// reasoning_tokens.
func (*Format) DecodeResponse(body []byte) (*protocol.Response, error) {
	var wire response
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("completions: decode response: %w", err)
	}
	if len(wire.Choices) == 0 {
		return nil, errors.New("completions: response has no choices")
	}
	choice := wire.Choices[0]
	resp := &protocol.Response{ID: wire.ID, Model: wire.Model, Usage: decodeUsage(wire.Usage)}
	nextItemID := func() string { return itemID(wire.ID, len(resp.Output)) }
	var ok bool
	if resp.Status, resp.IncompleteDetails, ok = decodeFinish(choice.FinishReason); !ok {
		return nil, fmt.Errorf("completions: response has unknown finish_reason %q", choice.FinishReason)
	}
	if r := reasoningText(choice.Message.ReasoningContent, choice.Message.Reasoning); r != "" {
		sealed, err := sealEnvelope(envelope{ReasoningContent: r})
		if err != nil {
			return nil, fmt.Errorf("completions: reasoning: %w", err)
		}
		resp.Output = append(resp.Output, protocol.Reasoning{
			ID:               nextItemID(),
			Status:           protocol.StatusCompleted,
			Content:          []protocol.ReasoningText{{Text: r}},
			EncryptedContent: sealed,
		})
	}
	var content []protocol.ContentPart
	if choice.Message.Content != "" {
		content = append(content, protocol.OutputText{Text: choice.Message.Content})
	}
	if choice.Message.Refusal != "" {
		content = append(content, protocol.Refusal{Refusal: choice.Message.Refusal})
	}
	if len(content) > 0 {
		resp.Output = append(resp.Output, protocol.Message{
			ID:      nextItemID(),
			Status:  protocol.StatusCompleted,
			Role:    protocol.RoleAssistant,
			Content: content,
		})
	}
	for i, c := range choice.Message.ToolCalls {
		if c.Type != "function" {
			return nil, fmt.Errorf("completions: response tool call %d has unknown type %q", i, c.Type)
		}
		resp.Output = append(resp.Output, protocol.FunctionCall{
			ID:        nextItemID(),
			Status:    protocol.StatusCompleted,
			CallID:    c.ID,
			Name:      c.Function.Name,
			Arguments: c.Function.Arguments,
		})
	}
	if last := len(resp.Output) - 1; last >= 0 {
		resp.Output[last] = withStatus(resp.Output[last], resp.Status)
	}
	return resp, nil
}

func reasoningText(reasoningContent, reasoning string) string {
	if reasoningContent != "" {
		return reasoningContent
	}
	return reasoning
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

// See docs/design.md, "Implementation notes".
func decodeUsage(u usage) protocol.Usage {
	reasoning := u.CompletionTokensDetails.ReasoningTokens
	if reasoning == nil {
		reasoning = u.ReasoningTokens
	}
	var reasoningTokens int
	if reasoning != nil {
		reasoningTokens = *reasoning
	}
	return protocol.Usage{
		InputTokens:  u.PromptTokens,
		OutputTokens: u.CompletionTokens,
		TotalTokens:  u.TotalTokens,
		InputTokensDetails: protocol.InputTokensDetails{
			CachedTokens: u.PromptTokensDetails.CachedTokens,
		},
		OutputTokensDetails: protocol.OutputTokensDetails{
			ReasoningTokens: reasoningTokens,
		},
	}
}

func decodeFinish(reason string) (protocol.Status, *protocol.IncompleteDetails, bool) {
	switch reason {
	case "stop", "tool_calls":
		return protocol.StatusCompleted, nil, true
	case "length":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonMaxOutputTokens}, true
	case "content_filter":
		return protocol.StatusIncomplete, &protocol.IncompleteDetails{Reason: protocol.ReasonContentFilter}, true
	}
	return "", nil, false
}

func sealEnvelope(e envelope) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return envelopePrefix + base64.StdEncoding.EncodeToString(b), nil
}

// DecodeError decodes an OpenAI-style error body with format.DecodeOpenAIError, which
// describes the shapes it reads.
func (*Format) DecodeError(body []byte) (message, typ, code string) {
	return format.DecodeOpenAIError(body)
}

// NewStreamDecoder returns a decoder for one streamed Chat Completions reply: a stream of
// chat.completion.chunk events, which it turns into the protocol's streaming events in the spec's
// order. Chat Completions names no event types: every event's data is a chunk, or the [DONE] that
// ends the stream. Of each chunk it reads the id, the first choice, index 0, and the usage.
//
// The stream opens with response.created, its response in_progress and bearing the stream's id
// and model, made at the first event that claims the reply or ends the stream: any but a leading
// chunk of no id and no choices, which makes no event, as below. The choice's
// reasoning deltas, each read as reasoningText reads it, from reasoning_content or vLLM's
// reasoning, make the reasoning item, first in the output, as DecodeResponse orders
// it: output_item.added, in progress and with no content, when the first of them arrives, then its
// one reasoning_text part, with content_part.added, a reasoning.delta for each, and at its close
// reasoning.done and content_part.done, and output_item.done carrying the whole item. The item
// closes when the message's content, or the first tool call, begins, or at the finish_reason, and
// its output_item.done carries the encrypted_content envelope DecodeResponse gives the same
// reasoning, which EncodeRequest reads back, so the folded Response can be sent back as it came.
//
// The choice's content and refusal deltas then make the assistant message: output_item.added, in
// progress, when the first of them arrives; then for each run of content, or of refusal, a
// part, output_text or refusal, with content_part.added, its deltas, its done event, and
// content_part.done; and output_item.done carrying the whole message. A delta that is empty
// makes no event.
//
// The choice's tool_calls deltas then make a function_call item for each call, found by its
// index. The fragments are assembled by index in whatever order they arrive, as Chat Completions
// has it: fragments of several calls may interleave, and one chunk may carry fragments of several
// calls, in any order. A call's first fragment carries its id, the item's call_id, its type,
// function, and its name; the first call's closes the item open before it, the reasoning or the
// message, completed, and each call's adds the call: output_item.added, in progress, with its
// call_id and name and no arguments. A call's output index is its tool-call index counted from
// the first after the reasoning and the message, so the output has DecodeResponse's order, which
// follows the tool_calls array, whatever order the calls begin in; the calls are added in the
// order they begin, which the spec allows, as it routes events by output_index and item_id and
// orders them by sequence_number. Each fragment's argument text, the first's included, is a
// function_call_arguments.delta of its call. The calls stay open until the finish_reason, where
// each closes, in index order, with function_call_arguments.done, its arguments the deltas
// joined, and output_item.done carrying the whole call. A first fragment of another type or with
// no name is an error, as is a later fragment naming another id or function than its call's, a
// negative index, message text after a call has begun, and reasoning after the message or a call
// has begun: the output keeps DecodeResponse's order, reasoning, then the message, then the
// calls. So is a finish_reason when the calls' indexes skip one, as the output would have a gap.
//
// An item closed because a later one began is completed. The items open when the finish_reason
// arrives close then: the last in output order takes the status it gives, as DecodeResponse maps
// it, and those before it are completed, so only the last item can be incomplete, a reasoning
// item as any other. It translates the reasoning, the message, and its calls alone: a chunk's
// other members are ignored.
//
// The stream ends at data [DONE], or at the end of the stream once the finish_reason has arrived,
// where it closes with the terminal event: response.completed or response.incomplete, as the
// finish_reason gives, its response bearing the stream's id and model and holding the output and
// the usage of the chunk that carried it. A stream that ends with no finish_reason is
// io.ErrUnexpectedEOF; so is [DONE] without one.
//
// An error frame, an event whose data is an OpenAI-style error body, {"error": {...}}, as llama.cpp
// sends when generation fails mid-stream, is the endpoint's report that the response failed, and
// closes the stream as the spec has it: the error event, its payload the frame's type, code,
// message, and param, then the terminal response.failed, its response failed and bearing the
// stream's id and model, its error the frame's code and message, holding the output made before the
// frame and the usage given so far. The items open at the frame get no done event: they are in the
// output in progress, in output order, with the content or arguments they had, as the Accumulator
// holds an item still open, since the spec keeps incomplete for an item of an incomplete response
// and has no failed item status. A frame as the first event follows response.created, which every
// stream opens with, and an event after the frame is an error, as one after [DONE] is.
//
// A chunk that isn't JSON, an unknown finish_reason, a delta after the finish_reason, or an event
// after [DONE] is an error. The stream's id is the first id a chunk carries, which every chunk
// repeats; a chunk with another id is an error. The stream's model is the model of the chunk
// that set its id, which every chunk repeats too, and isn't checked. A chunk with no id and no
// choices, which some endpoints send ahead of the reply with only the prompt's content filter
// verdict, claims nothing about the reply: it neither sets the stream's id or model nor is
// checked against them, and its usage, if it carries any, is read. Leading the stream, it makes
// no event, not even response.created, so that response.created bears the id and model of the
// chunk after it. A chunk with no id but with choices is held to the stream's id as any
// other. Each item's id, its events' item_id, is the one DecodeResponse gives it: itemID derives
// it from the stream's id and the item's output index, as Chat Completions supplies no item ids.
func (*Format) NewStreamDecoder() format.StreamDecoder {
	return &streamDecoder{}
}

type streamDecoder struct {
	seq            int
	id, model      string
	chunked        bool
	received       bool
	created, ended bool
	finished       bool
	status         protocol.Status
	details        *protocol.IncompleteDetails
	usage          protocol.Usage
	output         []protocol.Item
	reasoning      *streamReasoning
	message        *streamMessage
	calls          map[int]*streamCall
	called         bool
	callBase       int
	events         []protocol.Event
}

type streamMessage struct {
	id      string
	index   int
	content []protocol.ContentPart
	part    partKind
	text    strings.Builder
}

type streamReasoning struct {
	id    string
	index int
	text  strings.Builder
}

type streamCall struct {
	id           string
	index        int
	callID, name string
	args         strings.Builder
}

type partKind int

const (
	partNone partKind = iota
	partText
	partRefusal
)

func (d *streamDecoder) Decode(e format.WireEvent) ([]protocol.Event, error) {
	d.events = nil
	if err := d.decode(e.Data); err != nil {
		d.ended = true
		return d.events, fmt.Errorf("completions: stream: %w", err)
	}
	return d.events, nil
}

// See docs/design.md, "Implementation notes".
func (d *streamDecoder) decode(data []byte) error {
	if d.ended {
		return errors.New("event after the end of the stream")
	}
	d.received = true
	if string(data) == "[DONE]" {
		d.open()
		return d.end()
	}
	var c chunk
	if err := json.Unmarshal(data, &c); err != nil {
		d.open()
		return fmt.Errorf("decode chunk: %v", err)
	}
	if len(c.Error) > 0 && c.Error.Kind() != jsontext.KindNull {
		d.open()
		d.fail(data, c.Error)
		return nil
	}
	switch {
	case c.claimsNothing():
	case !d.chunked:
		d.id, d.model, d.chunked = c.ID, c.Model, true
		d.open()
	case c.ID != d.id:
		return fmt.Errorf("chunk of reply %q in the stream of reply %q", c.ID, d.id)
	}
	if c.Usage != nil {
		d.usage = decodeUsage(*c.Usage)
	}
	for _, choice := range c.Choices {
		if choice.Index != 0 {
			continue
		}
		delta := choice.Delta
		reasoning := reasoningText(delta.ReasoningContent, delta.Reasoning)
		if d.finished && (reasoning+delta.Content+delta.Refusal != "" || len(delta.ToolCalls) > 0) {
			return errors.New("delta after the finish_reason")
		}
		if err := d.reason(reasoning); err != nil {
			return err
		}
		if err := d.delta(partText, delta.Content); err != nil {
			return err
		}
		if err := d.delta(partRefusal, delta.Refusal); err != nil {
			return err
		}
		for _, f := range delta.ToolCalls {
			if err := d.toolCall(f); err != nil {
				return err
			}
		}
		if choice.FinishReason != "" {
			if d.finished {
				return errors.New("a second finish_reason")
			}
			var ok bool
			if d.status, d.details, ok = decodeFinish(choice.FinishReason); !ok {
				return fmt.Errorf("chunk has unknown finish_reason %q", choice.FinishReason)
			}
			d.finished = true
			if err := d.closeReasoning(d.status); err != nil {
				return err
			}
			d.closeMessage(d.status)
			if err := d.closeCalls(d.status); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *streamDecoder) End() ([]protocol.Event, error) {
	if d.ended {
		return nil, nil
	}
	d.events = nil
	if d.received {
		d.open()
	}
	if err := d.end(); err != nil {
		return d.events, fmt.Errorf("completions: stream: %w", err)
	}
	return d.events, nil
}

func (d *streamDecoder) open() {
	if d.created {
		return
	}
	d.created = true
	d.emit(protocol.ResponseCreated{SequenceNumber: d.next(), Response: &protocol.Response{ID: d.id, Model: d.model, Status: protocol.StatusInProgress}})
}

func (d *streamDecoder) end() error {
	d.ended = true
	if !d.finished {
		return fmt.Errorf("stream ended with no finish_reason: %w", io.ErrUnexpectedEOF)
	}
	resp := &protocol.Response{
		ID:                d.id,
		Model:             d.model,
		Output:            slices.Clone(d.output),
		Status:            d.status,
		IncompleteDetails: d.details,
		Usage:             d.usage,
	}
	if d.status == protocol.StatusCompleted {
		d.emit(protocol.ResponseCompleted{SequenceNumber: d.next(), Response: resp})
	} else {
		d.emit(protocol.ResponseIncomplete{SequenceNumber: d.next(), Response: resp})
	}
	return nil
}

// See docs/design.md, "Implementation notes".
func (d *streamDecoder) fail(data []byte, frame jsontext.Value) {
	d.ended = true
	var payload protocol.ErrorPayload
	payload.Message, payload.Type, payload.Code = format.DecodeOpenAIError(data)
	var param struct {
		Param string `json:"param"`
	}
	if frame.Kind() == jsontext.KindBeginObject {
		_ = json.Unmarshal(frame, &param)
	}
	payload.Param = param.Param
	d.emit(protocol.ErrorEvent{SequenceNumber: d.next(), Error: payload})
	output := append(slices.Clone(d.output), d.openItems()...)
	d.emit(protocol.ResponseFailed{SequenceNumber: d.next(), Response: &protocol.Response{
		ID:     d.id,
		Model:  d.model,
		Output: output,
		Status: protocol.StatusFailed,
		Error:  &protocol.ResponseError{Code: payload.Code, Message: payload.Message},
		Usage:  d.usage,
	}})
}

func (d *streamDecoder) openItems() []protocol.Item {
	var items []protocol.Item
	switch {
	case d.reasoning != nil:
		r := d.reasoning
		items = append(items, protocol.Reasoning{ID: r.id, Status: protocol.StatusInProgress, Content: []protocol.ReasoningText{{Text: r.text.String()}}})
	case d.message != nil:
		m := d.message
		content := slices.Clone(m.content)
		switch m.part {
		case partText:
			content = append(content, protocol.OutputText{Text: m.text.String()})
		case partRefusal:
			content = append(content, protocol.Refusal{Refusal: m.text.String()})
		}
		items = append(items, protocol.Message{ID: m.id, Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: content})
	}
	for _, i := range slices.Sorted(maps.Keys(d.calls)) {
		c := d.calls[i]
		items = append(items, protocol.FunctionCall{ID: c.id, Status: protocol.StatusInProgress, CallID: c.callID, Name: c.name, Arguments: c.args.String()})
	}
	return items
}

func (d *streamDecoder) reason(text string) error {
	if text == "" {
		return nil
	}
	r := d.reasoning
	if r == nil {
		if d.message != nil || d.called {
			return errors.New("reasoning after the message or a tool call")
		}
		r = &streamReasoning{id: itemID(d.id, len(d.output)), index: len(d.output)}
		d.reasoning = r
		d.emit(protocol.OutputItemAdded{SequenceNumber: d.next(), OutputIndex: r.index, Item: protocol.Reasoning{ID: r.id, Status: protocol.StatusInProgress}})
		d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), ItemID: r.id, OutputIndex: r.index, Part: protocol.ReasoningText{}})
	}
	r.text.WriteString(text)
	d.emit(protocol.ReasoningDelta{SequenceNumber: d.next(), ItemID: r.id, OutputIndex: r.index, Delta: text})
	return nil
}

func (d *streamDecoder) closeReasoning(status protocol.Status) error {
	r := d.reasoning
	if r == nil {
		return nil
	}
	text := r.text.String()
	sealed, err := sealEnvelope(envelope{ReasoningContent: text})
	if err != nil {
		return fmt.Errorf("reasoning: %w", err)
	}
	part := protocol.ReasoningText{Text: text}
	d.emit(protocol.ReasoningDone{SequenceNumber: d.next(), ItemID: r.id, OutputIndex: r.index, Text: text})
	d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), ItemID: r.id, OutputIndex: r.index, Part: part})
	item := protocol.Reasoning{ID: r.id, Status: status, Content: []protocol.ReasoningText{part}, EncryptedContent: sealed}
	d.emit(protocol.OutputItemDone{SequenceNumber: d.next(), OutputIndex: r.index, Item: item})
	d.output = append(d.output, item)
	d.reasoning = nil
	return nil
}

func (d *streamDecoder) delta(kind partKind, text string) error {
	if text == "" {
		return nil
	}
	if d.called {
		return errors.New("message text after a tool call")
	}
	if err := d.closeReasoning(protocol.StatusCompleted); err != nil {
		return err
	}
	m := d.message
	if m == nil {
		m = &streamMessage{id: itemID(d.id, len(d.output)), index: len(d.output)}
		d.message = m
		d.emit(protocol.OutputItemAdded{
			SequenceNumber: d.next(),
			OutputIndex:    m.index,
			Item:           protocol.Message{ID: m.id, Status: protocol.StatusInProgress, Role: protocol.RoleAssistant},
		})
	}
	if m.part != kind {
		d.closePart()
		m.part = kind
		var part protocol.ContentPart = protocol.OutputText{}
		if kind == partRefusal {
			part = protocol.Refusal{}
		}
		d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), ItemID: m.id, OutputIndex: m.index, ContentIndex: len(m.content), Part: part})
	}
	m.text.WriteString(text)
	if kind == partRefusal {
		d.emit(protocol.RefusalDelta{SequenceNumber: d.next(), ItemID: m.id, OutputIndex: m.index, ContentIndex: len(m.content), Delta: text})
	} else {
		d.emit(protocol.OutputTextDelta{SequenceNumber: d.next(), ItemID: m.id, OutputIndex: m.index, ContentIndex: len(m.content), Delta: text})
	}
	return nil
}

func (d *streamDecoder) toolCall(f toolCallDelta) error {
	if f.Index < 0 {
		return fmt.Errorf("tool call of negative index %d", f.Index)
	}
	c := d.calls[f.Index]
	if c != nil {
		if (f.ID != "" && f.ID != c.callID) || (f.Function.Name != "" && f.Function.Name != c.name) {
			return fmt.Errorf("a fragment of tool call %d names call %q of %q, not call %q of %q", f.Index, f.ID, f.Function.Name, c.callID, c.name)
		}
	} else {
		if f.Type != "function" {
			return fmt.Errorf("tool call %d has unknown type %q", f.Index, f.Type)
		}
		if f.Function.Name == "" {
			return fmt.Errorf("tool call %d's first fragment has no function name", f.Index)
		}
		if !d.called {
			if err := d.closeReasoning(protocol.StatusCompleted); err != nil {
				return err
			}
			d.closeMessage(protocol.StatusCompleted)
			d.called, d.callBase, d.calls = true, len(d.output), map[int]*streamCall{}
		}
		index := d.callBase + f.Index
		c = &streamCall{id: itemID(d.id, index), index: index, callID: f.ID, name: f.Function.Name}
		d.calls[f.Index] = c
		d.emit(protocol.OutputItemAdded{
			SequenceNumber: d.next(),
			OutputIndex:    c.index,
			Item:           protocol.FunctionCall{ID: c.id, Status: protocol.StatusInProgress, CallID: c.callID, Name: c.name},
		})
	}
	if args := f.Function.Arguments; args != "" {
		c.args.WriteString(args)
		d.emit(protocol.FunctionCallArgumentsDelta{SequenceNumber: d.next(), ItemID: c.id, OutputIndex: c.index, Delta: args})
	}
	return nil
}

func (d *streamDecoder) closeCalls(status protocol.Status) error {
	indexes := slices.Sorted(maps.Keys(d.calls))
	for i, index := range indexes {
		if index != i {
			return fmt.Errorf("tool call %d began, but tool call %d didn't", index, i)
		}
	}
	for i, index := range indexes {
		c := d.calls[index]
		s := protocol.StatusCompleted
		if i == len(indexes)-1 {
			s = status
		}
		args := c.args.String()
		d.emit(protocol.FunctionCallArgumentsDone{SequenceNumber: d.next(), ItemID: c.id, OutputIndex: c.index, Arguments: args})
		call := protocol.FunctionCall{ID: c.id, Status: s, CallID: c.callID, Name: c.name, Arguments: args}
		d.emit(protocol.OutputItemDone{SequenceNumber: d.next(), OutputIndex: c.index, Item: call})
		d.output = append(d.output, call)
	}
	d.calls = nil
	return nil
}

func (d *streamDecoder) closePart() {
	m := d.message
	if m.part == partNone {
		return
	}
	text, index := m.text.String(), len(m.content)
	var part protocol.ContentPart
	if m.part == partRefusal {
		part = protocol.Refusal{Refusal: text}
		d.emit(protocol.RefusalDone{SequenceNumber: d.next(), ItemID: m.id, OutputIndex: m.index, ContentIndex: index, Refusal: text})
	} else {
		part = protocol.OutputText{Text: text}
		d.emit(protocol.OutputTextDone{SequenceNumber: d.next(), ItemID: m.id, OutputIndex: m.index, ContentIndex: index, Text: text})
	}
	d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), ItemID: m.id, OutputIndex: m.index, ContentIndex: index, Part: part})
	m.content = append(m.content, part)
	m.part = partNone
	m.text.Reset()
}

func (d *streamDecoder) closeMessage(status protocol.Status) {
	if d.message == nil {
		return
	}
	d.closePart()
	m := protocol.Message{ID: d.message.id, Status: status, Role: protocol.RoleAssistant, Content: d.message.content}
	d.emit(protocol.OutputItemDone{SequenceNumber: d.next(), OutputIndex: d.message.index, Item: m})
	d.output = append(d.output, m)
	d.message = nil
}

func (d *streamDecoder) emit(e protocol.Event) {
	d.events = append(d.events, e)
}

func (d *streamDecoder) next() int {
	n := d.seq
	d.seq++
	return n
}
