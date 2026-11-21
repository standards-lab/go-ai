package messages

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// NewStreamDecoder returns a decoder for one streamed Messages reply: a stream of events each
// naming its type in its data, which it turns into the protocol's streaming events in the spec's
// order.
//
// message_start opens the stream with response.created, its response in_progress and of the
// reply's id and model, and gives the reply's usage so far, its output counts left to
// message_delta. Each content block then becomes what DecodeResponse makes of
// the same block, in the same output order:
//
//	thinking           a reasoning item: output_item.added, in progress and with no content,
//	                   and its one reasoning_text part, content_part.added, at the block's
//	                   start; a reasoning.delta for each thinking_delta; and at the block's stop
//	                   reasoning.done and content_part.done. A signature_delta makes no event:
//	                   the signature is kept for the item's output_item.done, whose
//	                   encrypted_content is the envelope DecodeResponse gives the same block,
//	                   {"thinking","signature"}, which EncodeRequest reads back
//	redacted_thinking  a reasoning item of no content: output_item.added at the block's start,
//	                   and an output_item.done whose envelope holds the block's {"data"}
//	text               an output_text part of the assistant message open, or of a new one,
//	                   added at the block's start: content_part.added, an output_text.delta for
//	                   each text_delta, and at the block's stop output_text.done and
//	                   content_part.done. Consecutive text blocks are parts of one message, as
//	                   DecodeResponse joins them, and the message ends at the next block of
//	                   another type, or at message_delta
//	tool_use           a function_call item: output_item.added, in progress, with the block's
//	                   id as its call_id, its name, and no arguments, at the block's start; a
//	                   function_call_arguments.delta for each input_json_delta; and at the
//	                   block's stop function_call_arguments.done, its arguments the deltas
//	                   joined
//
// A delta that is empty makes no event. A tool_use block of no input_json_delta stops with one
// argument delta of the input its start gave, or {} when it gave none, so the call's arguments are
// the empty object DecodeResponse gives a call of no input, never empty.
//
// Items and parts may be open at once: llama.cpp's router stops every block only at the end of the
// reply, after the last block began, where Anthropic stops each before the next begins. Each part
// closes at its own block's stop, in whatever order the stops arrive. An item's output_item.done,
// carrying the whole item, follows once its blocks have stopped, a message's once it has ended too,
// and once its status is known: an item that a later item follows is completed, and the last item
// waits for message_stop, by which the stop_reason that gives the response's status as
// DecodeResponse maps it is final, so only the last item can be incomplete, as the spec has it. A
// second part of a message beginning while its first is open, which neither endpoint sends, is an
// error, as the spec adds a part only once the one ahead of it is done.
//
// A stream may send several message_delta events after its last block. The last stop_reason one
// gives that isn't null is the reply's, and each count a delta's usage gives is cumulative and
// replaces the one before, a count it omits keeping its value. The usage counts as DecodeResponse
// counts it: input tokens are input_tokens with cache_read_input_tokens and
// cache_creation_input_tokens added, cached tokens cache_read_input_tokens, output tokens
// output_tokens, reasoning tokens output_tokens_details.thinking_tokens, and the total the input
// and output tokens' sum. message_stop closes the items still to close and then the stream, with
// the terminal event: response.completed or response.incomplete, as the stop_reason gives, its
// response of the reply's id and model, holding the output and the usage. The stream ends there;
// Messages sends no [DONE]. A stop_reason of pause_turn, which has no place in a stateless
// response, or one DecodeResponse doesn't map, is an error at the message_delta that gives it, as
// it is to DecodeResponse.
//
// ping events make nothing, as the comments the router sends to keep the connection alive never
// reach the decoder, and an event of a type the decoder doesn't know, or a delta of a type it
// doesn't know, is ignored, as Anthropic's versioning policy allows it to add them. A content
// block of a type DecodeResponse doesn't know is an error, as it is to DecodeResponse.
//
// An error frame, recognized from its data alone, is the endpoint's report that the response
// failed, and closes the stream as the spec has it: the error event, then the terminal
// response.failed, its response failed, its error the frame's code and message, holding the
// output made before the frame and the usage given so far. It takes either shape: Anthropic's,
// {"type":"error","error":{"type","message"}}, its payload the error's type and message; or a
// body with a string message whose type isn't a Messages event's, as llama.cpp's
// {"code","message","type":"server_error"} is, its payload the body's type, code, and message.
// The items open at the frame get no done event: they are in the output in progress, in output
// order, with the content or arguments they had, as the Accumulator holds an item still open. A
// frame as the first event follows response.created, which every stream opens with, and an event
// after the frame is an error.
//
// An event that isn't JSON, any event but ping before message_start, a second message_start, a
// block whose index began before, a delta or a stop of a block that didn't begin or has stopped,
// a delta of a known type for a block of another type, message_delta while a block is open, a
// block after message_delta, message_stop before a message_delta has given a stop_reason, and an
// event after message_stop are errors, after which the stream is broken. A stream that ends before
// message_stop is io.ErrUnexpectedEOF. Each item's id, its events' item_id, is the one
// DecodeResponse gives it: itemID derives it from the reply's id and the item's output index.
func (*Format) NewStreamDecoder() format.StreamDecoder {
	return &streamDecoder{}
}

type streamDecoder struct {
	seq                     int
	created, started, ended bool
	id, model               string
	usage                   usage
	stopped                 bool
	stopReason              string
	final                   bool
	status                  protocol.Status
	details                 *protocol.IncompleteDetails
	blocks                  map[int]*streamBlock
	items                   []*streamItem
	events                  []protocol.Event
}

type streamItem struct {
	id     string
	index  int
	blocks []*streamBlock
	ended  bool
	done   bool
	item   protocol.Item
}

type streamBlock struct {
	typ             string
	item            *streamItem
	part            int
	stopped         bool
	text            strings.Builder
	deltas          int
	signature, data string
	callID, name    string
	input           jsontext.Value
}

var eventTypes = map[string]bool{
	"message_start":       true,
	"message_delta":       true,
	"message_stop":        true,
	"content_block_start": true,
	"content_block_delta": true,
	"content_block_stop":  true,
	"ping":                true,
	"error":               true,
}

func (d *streamDecoder) Decode(e format.WireEvent) ([]protocol.Event, error) {
	d.events = nil
	if err := d.decode(e.Data); err != nil {
		d.ended = true
		return d.events, fmt.Errorf("messages: stream: %w", err)
	}
	return d.events, nil
}

// See docs/design.md, "Implementation notes".
func (d *streamDecoder) decode(data []byte) error {
	if d.ended {
		return errors.New("event after the end of the stream")
	}
	var e streamEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return fmt.Errorf("decode event: %v", err)
	}
	if e.Type == "error" || (!eventTypes[e.Type] && e.Message.Kind() == jsontext.KindString) {
		d.fail(e.Type == "error", data)
		return nil
	}
	if e.Type == "ping" || !eventTypes[e.Type] {
		return nil
	}
	if e.Type == "message_start" {
		return d.start(e.Message)
	}
	if !d.started {
		return fmt.Errorf("%s before message_start", e.Type)
	}
	switch e.Type {
	case "content_block_start":
		return d.startBlock(e.Index, e.ContentBlock)
	case "content_block_delta":
		return d.delta(e.Index, e.Delta)
	case "content_block_stop":
		return d.stopBlock(e.Index)
	case "message_delta":
		return d.messageDelta(e.Delta.StopReason, e.Usage)
	default:
		return d.end()
	}
}

// See docs/design.md, "Implementation notes".
func (d *streamDecoder) start(message jsontext.Value) error {
	if d.started {
		return errors.New("a second message_start")
	}
	var m streamMessage
	if len(message) > 0 {
		if err := json.Unmarshal(message, &m); err != nil {
			return fmt.Errorf("decode message_start's message: %v", err)
		}
	}
	d.started, d.id, d.model, d.usage = true, m.ID, m.Model, m.Usage
	d.usage.OutputTokens, d.usage.OutputTokensDetails = nil, outputTokensDetails{}
	d.blocks = map[int]*streamBlock{}
	d.create()
	return nil
}

func (d *streamDecoder) create() {
	if d.created {
		return
	}
	d.created = true
	d.emit(protocol.ResponseCreated{SequenceNumber: d.next(), Response: &protocol.Response{ID: d.id, Model: d.model, Status: protocol.StatusInProgress}})
}

func (d *streamDecoder) startBlock(index int, b block) error {
	switch {
	case d.stopped:
		return fmt.Errorf("content block %d began after message_delta", index)
	case d.blocks[index] != nil:
		return fmt.Errorf("content block %d began twice", index)
	}
	sb := &streamBlock{typ: b.Type}
	if b.Type == "text" {
		if m := d.openMessage(); m != nil {
			if last := m.blocks[len(m.blocks)-1]; !last.stopped {
				return fmt.Errorf("text block %d began while the message's text block before it is open", index)
			}
			sb.item, sb.part = m, len(m.blocks)
			m.blocks = append(m.blocks, sb)
		} else if err := d.addItem(sb); err != nil {
			return err
		}
		d.blocks[index] = sb
		d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), ItemID: sb.item.id, OutputIndex: sb.item.index, ContentIndex: sb.part, Part: protocol.OutputText{}})
		d.writeText(sb, b.Text)
		return nil
	}
	switch b.Type {
	case "thinking":
		if err := d.addItem(sb); err != nil {
			return err
		}
		d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), ItemID: sb.item.id, OutputIndex: sb.item.index, Part: protocol.ReasoningText{}})
		d.writeText(sb, b.Thinking)
		sb.signature = b.Signature
	case "redacted_thinking":
		sb.data = b.Data
		if err := d.addItem(sb); err != nil {
			return err
		}
	case "tool_use":
		sb.callID, sb.name, sb.input = b.ID, b.Name, b.Input
		if err := d.addItem(sb); err != nil {
			return err
		}
	default:
		return fmt.Errorf("content block %d has unknown type %q", index, b.Type)
	}
	d.blocks[index] = sb
	return nil
}

func (d *streamDecoder) openMessage() *streamItem {
	if n := len(d.items); n > 0 {
		if last := d.items[n-1]; !last.ended {
			return last
		}
	}
	return nil
}

func (d *streamDecoder) addItem(b *streamBlock) error {
	if m := d.openMessage(); m != nil {
		m.ended = true
	}
	it := &streamItem{id: itemID(d.id, len(d.items)), index: len(d.items), blocks: []*streamBlock{b}, ended: b.typ != "text"}
	b.item = it
	d.items = append(d.items, it)
	if err := d.settle(it.index); err != nil {
		return err
	}
	var added protocol.Item
	switch b.typ {
	case "text":
		added = protocol.Message{ID: it.id, Status: protocol.StatusInProgress, Role: protocol.RoleAssistant}
	case "tool_use":
		added = protocol.FunctionCall{ID: it.id, Status: protocol.StatusInProgress, CallID: b.callID, Name: b.name}
	default:
		added = protocol.Reasoning{ID: it.id, Status: protocol.StatusInProgress}
	}
	d.emit(protocol.OutputItemAdded{SequenceNumber: d.next(), OutputIndex: it.index, Item: added})
	return nil
}

func (d *streamDecoder) writeText(b *streamBlock, text string) {
	if text == "" {
		return
	}
	b.text.WriteString(text)
	it := b.item
	if b.typ == "thinking" {
		d.emit(protocol.ReasoningDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Delta: text})
	} else {
		d.emit(protocol.OutputTextDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: b.part, Delta: text})
	}
}

func (d *streamDecoder) delta(index int, delta streamDelta) error {
	b, err := d.openBlock(index)
	if err != nil {
		return err
	}
	var want string
	switch delta.Type {
	case "text_delta":
		want = "text"
	case "thinking_delta", "signature_delta":
		want = "thinking"
	case "input_json_delta":
		want = "tool_use"
	default:
		return nil
	}
	if b.typ != want {
		return fmt.Errorf("%s of content block %d, a %s block", delta.Type, index, b.typ)
	}
	switch delta.Type {
	case "text_delta":
		d.writeText(b, delta.Text)
	case "thinking_delta":
		d.writeText(b, delta.Thinking)
	case "signature_delta":
		b.signature += delta.Signature
	case "input_json_delta":
		d.writeArgs(b, delta.PartialJSON)
	}
	return nil
}

func (d *streamDecoder) writeArgs(b *streamBlock, args string) {
	b.deltas++
	if args == "" {
		return
	}
	b.text.WriteString(args)
	d.emit(protocol.FunctionCallArgumentsDelta{SequenceNumber: d.next(), ItemID: b.item.id, OutputIndex: b.item.index, Delta: args})
}

func (d *streamDecoder) openBlock(index int) (*streamBlock, error) {
	b := d.blocks[index]
	switch {
	case b == nil:
		return nil, fmt.Errorf("content block %d didn't begin", index)
	case b.stopped:
		return nil, fmt.Errorf("content block %d has stopped", index)
	}
	return b, nil
}

func (d *streamDecoder) stopBlock(index int) error {
	b, err := d.openBlock(index)
	if err != nil {
		return err
	}
	b.stopped = true
	it := b.item
	text := b.text.String()
	switch b.typ {
	case "text":
		d.emit(protocol.OutputTextDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: b.part, Text: text})
		d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: b.part, Part: protocol.OutputText{Text: text}})
	case "thinking":
		d.emit(protocol.ReasoningDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Text: text})
		d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Part: protocol.ReasoningText{Text: text}})
	case "tool_use":
		if b.deltas == 0 {
			input := string(b.input)
			if input == "" {
				input = "{}"
			}
			d.writeArgs(b, input)
		}
		d.emit(protocol.FunctionCallArgumentsDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Arguments: b.text.String()})
	}
	return d.settle(len(d.items))
}

func (d *streamDecoder) messageDelta(stopReason string, u usage) error {
	for i, it := range d.items {
		if !it.stoppedAll() {
			return fmt.Errorf("message_delta while a content block of output %d is open", i)
		}
	}
	if stopReason != "" {
		if _, _, ok := decodeStop(stopReason); !ok {
			return fmt.Errorf("message_delta has unknown stop_reason %q", stopReason)
		}
		d.stopReason = stopReason
	}
	d.stopped = true
	d.usage.update(u)
	if m := d.openMessage(); m != nil {
		m.ended = true
	}
	return nil
}

// See docs/design.md, "Implementation notes".
func (d *streamDecoder) settle(n int) error {
	for _, it := range d.items[:n] {
		if it.done || !it.ended || !it.stoppedAll() {
			continue
		}
		status := protocol.StatusCompleted
		if it.index == len(d.items)-1 {
			if !d.final {
				continue
			}
			status = d.status
		}
		if err := d.closeItem(it, status); err != nil {
			return err
		}
	}
	return nil
}

func (it *streamItem) stoppedAll() bool {
	for _, b := range it.blocks {
		if !b.stopped {
			return false
		}
	}
	return true
}

func (it *streamItem) content() []protocol.ContentPart {
	content := make([]protocol.ContentPart, len(it.blocks))
	for i, b := range it.blocks {
		content[i] = protocol.OutputText{Text: b.text.String()}
	}
	return content
}

func (d *streamDecoder) closeItem(it *streamItem, status protocol.Status) error {
	first := it.blocks[0]
	switch first.typ {
	case "text":
		it.item = protocol.Message{ID: it.id, Status: status, Role: protocol.RoleAssistant, Content: it.content()}
	case "thinking", "redacted_thinking":
		r, err := decodeReasoning(block{Type: first.typ, Thinking: first.text.String(), Signature: first.signature, Data: first.data})
		if err != nil {
			return fmt.Errorf("reasoning: %w", err)
		}
		r.ID, r.Status = it.id, status
		it.item = r
	case "tool_use":
		it.item = protocol.FunctionCall{ID: it.id, Status: status, CallID: first.callID, Name: first.name, Arguments: first.text.String()}
	}
	it.done = true
	d.emit(protocol.OutputItemDone{SequenceNumber: d.next(), OutputIndex: it.index, Item: it.item})
	return nil
}

func (d *streamDecoder) End() ([]protocol.Event, error) {
	if d.ended {
		return nil, nil
	}
	d.ended = true
	return nil, fmt.Errorf("messages: stream: stream ended before message_stop: %w", io.ErrUnexpectedEOF)
}

func (d *streamDecoder) end() error {
	switch {
	case !d.stopped:
		return errors.New("message_stop before message_delta")
	case d.stopReason == "":
		return errors.New("message_stop before a message_delta gave a stop_reason")
	}
	d.status, d.details, _ = decodeStop(d.stopReason)
	d.final = true
	if err := d.settle(len(d.items)); err != nil {
		return err
	}
	d.ended = true
	resp := &protocol.Response{
		ID:                d.id,
		Model:             d.model,
		Output:            d.output(),
		Status:            d.status,
		IncompleteDetails: d.details,
		Usage:             decodeUsage(d.usage),
	}
	if d.status == protocol.StatusCompleted {
		d.emit(protocol.ResponseCompleted{SequenceNumber: d.next(), Response: resp})
	} else {
		d.emit(protocol.ResponseIncomplete{SequenceNumber: d.next(), Response: resp})
	}
	return nil
}

func (d *streamDecoder) fail(anthropic bool, data []byte) {
	d.ended = true
	d.create()
	var payload protocol.ErrorPayload
	if anthropic {
		payload.Message, payload.Type, payload.Code = format.DecodeOpenAIError(data)
	} else {
		asOpenAIErrorBody := append(append([]byte(`{"error":`), data...), '}')
		payload.Message, payload.Type, payload.Code = format.DecodeOpenAIError(asOpenAIErrorBody)
	}
	d.emit(protocol.ErrorEvent{SequenceNumber: d.next(), Error: payload})
	d.emit(protocol.ResponseFailed{SequenceNumber: d.next(), Response: &protocol.Response{
		ID:     d.id,
		Model:  d.model,
		Output: d.output(),
		Status: protocol.StatusFailed,
		Error:  &protocol.ResponseError{Code: payload.Code, Message: payload.Message},
		Usage:  decodeUsage(d.usage),
	}})
}

func (d *streamDecoder) output() []protocol.Item {
	var out []protocol.Item
	for _, it := range d.items {
		if it.done {
			out = append(out, it.item)
			continue
		}
		first := it.blocks[0]
		switch first.typ {
		case "text":
			out = append(out, protocol.Message{ID: it.id, Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: it.content()})
		case "thinking":
			out = append(out, protocol.Reasoning{ID: it.id, Status: protocol.StatusInProgress, Content: []protocol.ReasoningText{{Text: first.text.String()}}})
		case "redacted_thinking":
			out = append(out, protocol.Reasoning{ID: it.id, Status: protocol.StatusInProgress})
		case "tool_use":
			out = append(out, protocol.FunctionCall{ID: it.id, Status: protocol.StatusInProgress, CallID: first.callID, Name: first.name, Arguments: first.text.String()})
		}
	}
	return out
}

func (d *streamDecoder) emit(e protocol.Event) {
	d.events = append(d.events, e)
}

func (d *streamDecoder) next() int {
	n := d.seq
	d.seq++
	return n
}
