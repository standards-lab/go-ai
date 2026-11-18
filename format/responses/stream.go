package responses

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/protocol"
)

// NewStreamDecoder returns a decoder for one streamed reply: a stream of the spec's semantic
// events, each naming its type in its data, which it turns into the protocol's streaming events
// under the spec's names. An endpoint that follows the spec sends every event the protocol has,
// and the decoder passes each through as it came, numbered afresh; it never repeats an event
// the server sent. One that departs from it, as llama.cpp's router does, is normalized to the
// spec's stream:
//
//   - Numbering. The decoder numbers every event it makes, from 0, in order, whatever
//     sequence_number the server gave, or none.
//   - Placing. An item is placed at the output_index of its output_item.added, or, when that
//     event gives none, at the next in arrival order: the number of items added before it. An
//     event about an item that gives an output_index names the item there, and its item_id, when
//     it gives one, must be that item's; one that gives none names the item by its item_id. An
//     event about a content part that gives no content_index names the item's last part still
//     open, or, for a delta or a text's done event when no part is open, the next part.
//   - Aliases. response.reasoning_text.delta and response.reasoning_text.done, OpenAI's names,
//     which the router sends, are read as the spec's response.reasoning.delta and
//     response.reasoning.done.
//   - Filling. What the stream leaves out, the decoder makes: a reasoning item's
//     content_part.added, ahead of the first reasoning delta of a part not added; at a part's
//     content_part.done, the text's done event, output_text.done, refusal.done, or
//     reasoning.done, when the server sent none; and at an item's output_item.done, the done
//     events of each part still open, its text's and its own, and a function call's
//     function_call_arguments.done when the server sent none. Each is made only where the server
//     sent nothing in its place.
//
// Items and parts may be open at once, as the router opens every item ahead of closing any: each
// closes at its own done event, in whatever order those arrive. A part is added only once the
// one ahead of it in its item is done, as the spec has it. An item's output_item.done carries
// the whole item, which DecodeResponse makes of the same item, a reasoning item's
// encrypted_content its envelope; an item the server gives no status there, as the router gives
// a reasoning item none, is completed. The item must be the one the stream made: of the type,
// id, role, call_id, and name it was added with, its content the parts streamed and its
// arguments those streamed.
//
// An item of a type DecodeResponse skips, and a message's part of a type it skips, are skipped
// here too: their added and done events, and every event about them, make nothing, and the
// items and parts after them take the output and content indexes they would have were those not
// there, so the events agree with the Response DecodeResponse makes. An item to skip added at an
// output index below an item already added is an error, as the indexes already given can't be
// taken back.
//
// response.created opens the stream, its response in_progress, of no output, and of the id and
// model the server's response.created gives. The terminal event, response.completed,
// response.incomplete, or response.failed, closes it with its response, which DecodeResponse
// decodes, its id, model, and usage among it; its status must be the event's.
// response.completed and response.incomplete follow only once every item added is done, with no
// output index skipped, and their response's output must be those items, by id; an item of
// theirs the server gives no status, as the router gives a reasoning item none there too, has
// the status its output_item.done gave it, so the response agrees with the events.
//
// response.in_progress and response.queued, the reasoning summary's events
// (response.reasoning_summary_part.added and .done, response.reasoning_summary_text.delta and
// .done), response.output_text.annotation.added, and an event of a type the decoder doesn't know
// make nothing: the done item carries the summary, and the format drops annotations, as
// DecodeResponse does. The comments an endpoint sends to keep the connection alive never reach
// the decoder.
//
// An error frame, recognized from its data alone, is the endpoint's report that the response
// failed, and closes the stream as the spec has it: the error event, then the terminal
// response.failed, its response failed, its error the frame's code and message, of the id and model
// response.created gave, holding the output made before the frame. It takes either shape: the
// spec's error event, {"type":"error","error":{"type","code","message","param"}}, or a body of no
// type whose error member is an object, as llama.cpp's {"error":{"code","message","type"}} is. The
// items open at the frame get no done event: they are in the output in progress, in output order,
// with the content or arguments they had, as the Accumulator holds an item still open. A frame as
// the first event follows response.created, which every stream opens with. The spec has the server
// send response.failed after its error event; the stream has ended by then, and the client reads no
// further.
//
// An event that isn't JSON, a known event before response.created, a second response.created,
// an item added at an output index or under an id already added, or with content or arguments,
// or to skip below an item already added, an event about an item that wasn't added or is
// done, an item_id that isn't the id of the item at the event's output_index, a part added out of
// its order or of a type its item doesn't take, a delta or done event of a part that wasn't added,
// is done, or is of another type, a function call's arguments event of another item or after
// their done event, a done event whose text, arguments, or item isn't what the stream made, a
// terminal event whose response isn't the stream's, and an event after the terminal event are
// errors, after which the stream is broken. A stream that ends before its terminal event is
// io.ErrUnexpectedEOF.
func (*Format) NewStreamDecoder() format.StreamDecoder {
	return &streamDecoder{}
}

// streamDecoder is a streamed reply's decoder, as NewStreamDecoder describes.
type streamDecoder struct {
	// seq is the next event's sequence number.
	seq int
	// created is set once response.created is made, and ended once the terminal event is made
	// or an error has broken the stream.
	created, ended bool
	// id and model are the reply's id and model, as response.created gave them, which an error
	// frame's response.failed bears.
	id, model string
	// items are the output items added, in the server's output order, done and open, those of a
	// type the format skips among them; an index not yet added is skipped.
	items []*streamItem
	// events are the events the current call has made.
	events []protocol.Event
}

// streamItem is an output item a stream is making, or one of a type it skips.
type streamItem struct {
	// output is the item's place in the server's output, as added; index is its place in the
	// output the events give, which counts no skipped item ahead of it; and id is its id.
	output, index int
	id            string
	// skipped is set for an item of a type the format skips, which holds its output index
	// and id alone, and of which no event makes anything.
	skipped bool
	// added is the item as its output_item.added made it.
	added protocol.Item
	// parts are a message's or a reasoning item's content parts, in order.
	parts []*streamPart
	// args is a function call's arguments so far, and argsDone is set at their done event.
	args     strings.Builder
	argsDone bool
	// done is set once output_item.done is made, carrying item.
	done bool
	item protocol.Item
}

// streamPart is a content part a stream is making: its type, output_text, refusal, or
// reasoning_text, and its text so far; or a message's part of a type the format skips.
type streamPart struct {
	kind string
	// index is the part's place in the content the events give, which counts no skipped part
	// ahead of it, and skipped is set for a part of a type the format skips, which makes no
	// event.
	index   int
	skipped bool
	text    strings.Builder
	// textDone is set at the part's text's done event, and done at the part's.
	textDone, done bool
}

// streamHead is what the decoder reads of every event first: its type, and the error member an
// error frame carries.
type streamHead struct {
	Type  string         `json:"type"`
	Error jsontext.Value `json:"error"`
}

// streamEvent is the part of a known event the decoder reads. OutputIndex and ContentIndex are
// nil when the event gives none, as the router's give none.
type streamEvent struct {
	OutputIndex  *int           `json:"output_index"`
	ContentIndex *int           `json:"content_index"`
	ItemID       string         `json:"item_id"`
	Item         *outputItem    `json:"item"`
	Part         *part          `json:"part"`
	Delta        string         `json:"delta"`
	Text         string         `json:"text"`
	Refusal      string         `json:"refusal"`
	Arguments    string         `json:"arguments"`
	Response     jsontext.Value `json:"response"`
}

// responseName is the part of response.created's response the decoder reads: the reply's id and
// model.
type responseName struct {
	ID    string `json:"id"`
	Model string `json:"model"`
}

// streamError is the part of an error frame's error the decoder reads beside what
// format.DecodeOpenAIError gives: the request parameter it concerns, a string or null.
type streamError struct {
	Error struct {
		Param string `json:"param"`
	} `json:"error"`
}

// aliases maps an event type an endpoint sends under another name to the spec's.
var aliases = map[string]string{
	"response.reasoning_text.delta": "response.reasoning.delta",
	"response.reasoning_text.done":  "response.reasoning.done",
}

// eventTypes are the event types the decoder reads, under the spec's names. Every other type
// makes nothing.
var eventTypes = map[string]bool{
	"response.created":                       true,
	"response.completed":                     true,
	"response.incomplete":                    true,
	"response.failed":                        true,
	"response.output_item.added":             true,
	"response.output_item.done":              true,
	"response.content_part.added":            true,
	"response.content_part.done":             true,
	"response.output_text.delta":             true,
	"response.output_text.done":              true,
	"response.refusal.delta":                 true,
	"response.refusal.done":                  true,
	"response.reasoning.delta":               true,
	"response.reasoning.done":                true,
	"response.function_call_arguments.delta": true,
	"response.function_call_arguments.done":  true,
}

// Decode decodes one event of the stream, as NewStreamDecoder describes, from its data alone:
// the format names its events, where it does, in the data. With an error, the events it returns
// are those it made before it.
func (d *streamDecoder) Decode(e format.WireEvent) ([]protocol.Event, error) {
	d.events = nil
	if err := d.decode(e.Data); err != nil {
		d.ended = true
		return d.events, fmt.Errorf("responses: stream: %w", err)
	}
	return d.events, nil
}

// decode decodes data, an event's, making events in d.events.
func (d *streamDecoder) decode(data []byte) error {
	if d.ended {
		return errors.New("event after the end of the stream")
	}
	var head streamHead
	if err := json.Unmarshal(data, &head); err != nil {
		// The JSON error is formatted, not wrapped: an event cut short wraps
		// io.ErrUnexpectedEOF, which in a stream means the stream ended before its terminal
		// event, and this event arrived whole.
		return fmt.Errorf("decode event: %v", err)
	}
	if head.Type == "error" || (head.Type == "" && head.Error.Kind() == jsontext.KindBeginObject) {
		d.fail(data)
		return nil
	}
	typ := head.Type
	if name, ok := aliases[typ]; ok {
		typ = name
	}
	if !eventTypes[typ] {
		return nil
	}
	var e streamEvent
	if err := json.Unmarshal(data, &e); err != nil {
		return fmt.Errorf("decode %s: %v", typ, err)
	}
	if typ == "response.created" {
		if d.created {
			return errors.New("a second response.created")
		}
		var name responseName
		if len(e.Response) > 0 {
			if err := json.Unmarshal(e.Response, &name); err != nil {
				return fmt.Errorf("decode %s: %v", typ, err)
			}
		}
		d.id, d.model = name.ID, name.Model
		d.create()
		return nil
	}
	if !d.created {
		return fmt.Errorf("%s before response.created", typ)
	}
	switch typ {
	case "response.output_item.added":
		return d.addItem(e)
	case "response.output_item.done":
		return d.doneItem(e)
	case "response.content_part.added":
		return d.addPartEvent(e)
	case "response.content_part.done":
		return d.donePartEvent(e)
	case "response.output_text.delta":
		return d.delta(e, "output_text", e.Delta)
	case "response.refusal.delta":
		return d.delta(e, "refusal", e.Delta)
	case "response.reasoning.delta":
		return d.delta(e, "reasoning_text", e.Delta)
	case "response.output_text.done":
		return d.doneTextEvent(e, "output_text", e.Text)
	case "response.refusal.done":
		return d.doneTextEvent(e, "refusal", e.Refusal)
	case "response.reasoning.done":
		return d.doneTextEvent(e, "reasoning_text", e.Text)
	case "response.function_call_arguments.delta":
		it, err := d.functionCall(e)
		if err != nil || it.skipped {
			return err
		}
		it.args.WriteString(e.Delta)
		d.emit(protocol.FunctionCallArgumentsDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Delta: e.Delta})
		return nil
	case "response.function_call_arguments.done":
		it, err := d.functionCall(e)
		if err != nil || it.skipped {
			return err
		}
		return d.doneArgs(it, e.Arguments)
	default: // response.completed, response.incomplete, response.failed
		return d.end(typ, e.Response)
	}
}

// create makes response.created, once.
func (d *streamDecoder) create() {
	if d.created {
		return
	}
	d.created = true
	d.emit(protocol.ResponseCreated{SequenceNumber: d.next(), Response: &protocol.Response{ID: d.id, Model: d.model, Status: protocol.StatusInProgress}})
}

// addItem reads output_item.added: the item at its output_index, or the next in arrival order,
// in progress, with no content or arguments; a reasoning item's encrypted_content and summary are
// dropped. An item of a type DecodeResponse skips makes no event, and the items after it in the
// output are placed as if it weren't there, so the events' output indexes leave no gap; once an
// item after it is added, its place can no longer be taken out, and it is an error.
func (d *streamDecoder) addItem(e streamEvent) error {
	if e.Item == nil {
		return errors.New("output_item.added has no item")
	}
	w := *e.Item
	output := len(d.items)
	if e.OutputIndex != nil {
		output = *e.OutputIndex
	}
	at, found := d.find(output)
	switch {
	case output < 0:
		return fmt.Errorf("output %d added, want an output index of 0 or more", output)
	case found:
		return fmt.Errorf("output %d was already added", output)
	case w.ID != "" && d.byID(w.ID) != nil:
		return fmt.Errorf("item %q was already added", w.ID)
	}
	if !knownItem(w.Type) {
		for _, it := range d.items[at:] {
			if !it.skipped {
				return fmt.Errorf("output %d of type %q, which the format skips, is added after output %d", output, w.Type, it.output)
			}
		}
		d.items = slices.Insert(d.items, at, &streamItem{output: output, id: w.ID, skipped: true})
		return nil
	}
	index := output
	for _, it := range d.items[:at] {
		if it.skipped {
			index--
		}
	}
	var added protocol.Item
	switch w.Type {
	case "message":
		if len(w.Content) > 0 {
			return fmt.Errorf("message %q is added with content", w.ID)
		}
		added = protocol.Message{ID: w.ID, Status: protocol.StatusInProgress, Role: protocol.Role(w.Role)}
	case "reasoning":
		// Its encrypted_content may be given, and incomplete, while it is in progress, the
		// spec says, and so may its summary, an in-progress item echoed with as much detail as
		// is available; the added event holds neither, and the item's output_item.done gives
		// the whole of each, its encrypted_content going into the envelope.
		if len(w.Content) > 0 {
			return fmt.Errorf("reasoning item %q is added with content", w.ID)
		}
		added = protocol.Reasoning{ID: w.ID, Status: protocol.StatusInProgress}
	default: // function_call
		if w.Arguments != "" {
			return fmt.Errorf("function call %q is added with arguments", w.ID)
		}
		added = protocol.FunctionCall{ID: w.ID, Status: protocol.StatusInProgress, CallID: w.CallID, Name: w.Name}
	}
	d.items = slices.Insert(d.items, at, &streamItem{output: output, index: index, id: w.ID, added: added})
	d.emit(protocol.OutputItemAdded{SequenceNumber: d.next(), OutputIndex: index, Item: added})
	return nil
}

// find returns the place in d.items of the item at output, the server's output index, and
// whether it was added; when it wasn't, the place is where it goes.
func (d *streamDecoder) find(output int) (int, bool) {
	return slices.BinarySearchFunc(d.items, output, func(it *streamItem, output int) int {
		return it.output - output
	})
}

// byID returns the item added under id, or nil.
func (d *streamDecoder) byID(id string) *streamItem {
	for _, it := range d.items {
		if it.id == id {
			return it
		}
	}
	return nil
}

// item returns the item an event names, added and not done, or skipped: the item at output when
// it is given, whose id must be id when that is given too, or else the item of id. The caller
// makes nothing of an event about a skipped item.
func (d *streamDecoder) item(output *int, id string) (*streamItem, error) {
	var it *streamItem
	switch {
	case output != nil:
		i, found := d.find(*output)
		if !found {
			return nil, fmt.Errorf("output %d wasn't added", *output)
		}
		it = d.items[i]
		if id != "" && it.id != id {
			return nil, fmt.Errorf("output %d is item %q, not %q", *output, it.id, id)
		}
	case id != "":
		if it = d.byID(id); it == nil {
			return nil, fmt.Errorf("item %q wasn't added", id)
		}
	default:
		return nil, errors.New("the event names no item, by output_index or item_id")
	}
	if it.done {
		return nil, fmt.Errorf("output %d is done", it.index)
	}
	return it, nil
}

// contentItem returns the item e names, which must be a message or a reasoning item, an item of
// content parts, or skipped.
func (d *streamDecoder) contentItem(e streamEvent) (*streamItem, error) {
	it, err := d.item(e.OutputIndex, e.ItemID)
	if err != nil {
		return nil, err
	}
	if isFunctionCall(it.added) {
		return nil, fmt.Errorf("output %d is a function call, which has no content parts", it.index)
	}
	return it, nil
}

// functionCall returns the function call e names, its arguments still open, or the skipped item
// it names.
func (d *streamDecoder) functionCall(e streamEvent) (*streamItem, error) {
	it, err := d.item(e.OutputIndex, e.ItemID)
	if err != nil {
		return nil, err
	}
	switch {
	case it.skipped:
		return it, nil
	case !isFunctionCall(it.added):
		return nil, fmt.Errorf("output %d is %T, not a function call", it.index, it.added)
	case it.argsDone:
		return nil, fmt.Errorf("output %d's arguments are done", it.index)
	}
	return it, nil
}

// isFunctionCall reports whether it is a function call.
func isFunctionCall(it protocol.Item) bool {
	_, ok := it.(protocol.FunctionCall)
	return ok
}

// addPartEvent reads content_part.added: the item's next part, at its content_index, or next
// when the event gives none.
func (d *streamDecoder) addPartEvent(e streamEvent) error {
	if e.Part == nil {
		return errors.New("content_part.added has no part")
	}
	it, err := d.contentItem(e)
	if err != nil || it.skipped {
		return err
	}
	index := len(it.parts)
	if e.ContentIndex != nil {
		index = *e.ContentIndex
	}
	return d.addPart(it, index, e.Part.Type, partText(*e.Part))
}

// partText returns the text of p, an output_text's, reasoning_text's, or refusal's.
func partText(p part) string {
	if p.Type == "refusal" {
		return p.Refusal
	}
	return p.Text
}

// addPart adds a part of kind and text to it at index, the server's content index, which must be
// its next, once the part ahead of it is done, and of a type its item takes: output_text or
// refusal of a message, and reasoning_text of a reasoning item. A message's part of a type that
// isn't an output part is skipped, as DecodeResponse skips it: it makes no event, and the parts
// after it are placed as if it weren't there.
func (d *streamDecoder) addPart(it *streamItem, index int, kind, text string) error {
	n := len(it.parts)
	switch {
	case index != n:
		return fmt.Errorf("output %d content %d added, want content %d next", it.index, index, n)
	case n > 0 && !it.parts[n-1].done:
		return fmt.Errorf("output %d content %d added while content %d is open", it.index, index, n-1)
	}
	_, reasoning := it.added.(protocol.Reasoning)
	if !reasoning && !outputPart(kind) {
		it.parts = append(it.parts, &streamPart{kind: kind, skipped: true})
		return nil
	}
	if (reasoning && kind != "reasoning_text") || (!reasoning && kind != "output_text" && kind != "refusal") {
		return fmt.Errorf("output %d content %d has type %q, which a %T doesn't take", it.index, index, kind, it.added)
	}
	p := &streamPart{kind: kind, index: len(it.content())}
	p.text.WriteString(text)
	it.parts = append(it.parts, p)
	d.emit(protocol.ContentPartAdded{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Part: contentPart(kind, text)})
	return nil
}

// content returns the parts of it the events give: those not skipped, in order.
func (it *streamItem) content() []*streamPart {
	var parts []*streamPart
	for _, p := range it.parts {
		if !p.skipped {
			parts = append(parts, p)
		}
	}
	return parts
}

// contentPart returns the protocol's part of kind holding text.
func contentPart(kind, text string) protocol.ContentPart {
	switch kind {
	case "refusal":
		return protocol.Refusal{Refusal: text}
	case "reasoning_text":
		return protocol.ReasoningText{Text: text}
	}
	return protocol.OutputText{Text: text}
}

// part returns the part of kind a delta or a text's done event of e names, its text still open:
// the part at e's content_index, or, when e gives none, the item's last part still open, or else
// its next. A reasoning item's next part, not added, is added here, filling the
// content_part.added the router doesn't send. The part is nil, and the event makes nothing, when
// the item or the part is skipped.
func (d *streamDecoder) part(e streamEvent, kind string) (*streamItem, *streamPart, error) {
	it, err := d.contentItem(e)
	if err != nil || it.skipped {
		return nil, nil, err
	}
	n := len(it.parts)
	index := n
	switch {
	case e.ContentIndex != nil:
		index = *e.ContentIndex
	case n > 0 && !it.parts[n-1].done:
		index = n - 1
	}
	if _, reasoning := it.added.(protocol.Reasoning); reasoning && index == n {
		if err := d.addPart(it, index, kind, ""); err != nil {
			return nil, nil, err
		}
	}
	if index < 0 || index >= len(it.parts) {
		return nil, nil, fmt.Errorf("output %d content %d wasn't added", it.index, index)
	}
	p := it.parts[index]
	switch {
	case p.done:
		return nil, nil, fmt.Errorf("output %d content %d is done", it.index, index)
	case p.skipped:
		return nil, nil, nil
	case p.kind != kind:
		return nil, nil, fmt.Errorf("output %d content %d is %s, not %s", it.index, index, p.kind, kind)
	case p.textDone:
		return nil, nil, fmt.Errorf("output %d content %d's text is done", it.index, index)
	}
	return it, p, nil
}

// delta reads an output_text.delta, a refusal.delta, or a reasoning.delta, of a part of kind.
func (d *streamDecoder) delta(e streamEvent, kind, delta string) error {
	it, p, err := d.part(e, kind)
	if err != nil || p == nil {
		return err
	}
	p.text.WriteString(delta)
	switch kind {
	case "output_text":
		d.emit(protocol.OutputTextDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Delta: delta})
	case "refusal":
		d.emit(protocol.RefusalDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Delta: delta})
	default:
		d.emit(protocol.ReasoningDelta{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Delta: delta})
	}
	return nil
}

// doneTextEvent reads an output_text.done, a refusal.done, or a reasoning.done, of a part of
// kind, its text text.
func (d *streamDecoder) doneTextEvent(e streamEvent, kind, text string) error {
	it, p, err := d.part(e, kind)
	if err != nil || p == nil {
		return err
	}
	return d.doneText(it, p, text)
}

// doneText makes the done event of the text of p, a part of it, which must be text, the text its
// deltas made.
func (d *streamDecoder) doneText(it *streamItem, p *streamPart, text string) error {
	if text != p.text.String() {
		return fmt.Errorf("output %d content %d's text %q isn't the text its deltas made, %q", it.index, p.index, text, p.text.String())
	}
	p.textDone = true
	switch p.kind {
	case "output_text":
		d.emit(protocol.OutputTextDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Text: text})
	case "refusal":
		d.emit(protocol.RefusalDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Refusal: text})
	default:
		d.emit(protocol.ReasoningDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Text: text})
	}
	return nil
}

// donePartEvent reads content_part.done: the part at its content_index, or the item's last part
// when it gives none, which must be the part streamed. The text's done event, when the server
// sent none, is made ahead of it. A skipped part is done, and makes nothing.
func (d *streamDecoder) donePartEvent(e streamEvent) error {
	if e.Part == nil {
		return errors.New("content_part.done has no part")
	}
	it, err := d.contentItem(e)
	if err != nil || it.skipped {
		return err
	}
	index := len(it.parts) - 1
	if e.ContentIndex != nil {
		index = *e.ContentIndex
	}
	if index < 0 || index >= len(it.parts) {
		return fmt.Errorf("output %d content %d wasn't added", it.index, index)
	}
	p := it.parts[index]
	text := partText(*e.Part)
	switch {
	case p.done:
		return fmt.Errorf("output %d content %d is done", it.index, index)
	case e.Part.Type != p.kind:
		return fmt.Errorf("output %d content %d is done as %s, added as %s", it.index, index, e.Part.Type, p.kind)
	case p.skipped:
		p.done = true
		return nil
	case text != p.text.String():
		return fmt.Errorf("output %d content %d is done with text %q, not the text streamed, %q", it.index, index, text, p.text.String())
	}
	return d.closePart(it, p)
}

// closePart makes the done events of p, a part of it, that the server didn't send: its text's,
// and its own. A skipped part is done, and makes nothing.
func (d *streamDecoder) closePart(it *streamItem, p *streamPart) error {
	p.done = true
	if p.skipped {
		return nil
	}
	if !p.textDone {
		if err := d.doneText(it, p, p.text.String()); err != nil {
			return err
		}
	}
	d.emit(protocol.ContentPartDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, ContentIndex: p.index, Part: contentPart(p.kind, p.text.String())})
	return nil
}

// doneArgs makes function_call_arguments.done of it, its arguments args, which must be those its
// deltas made.
func (d *streamDecoder) doneArgs(it *streamItem, args string) error {
	if args != it.args.String() {
		return fmt.Errorf("output %d's arguments %q aren't the arguments their deltas made, %q", it.index, args, it.args.String())
	}
	it.argsDone = true
	d.emit(protocol.FunctionCallArgumentsDone{SequenceNumber: d.next(), ItemID: it.id, OutputIndex: it.index, Arguments: args})
	return nil
}

// doneItem reads output_item.done: the whole item, which DecodeResponse makes of it, completed
// when the server gives it no status, and which must be the item streamed. The done events of
// its parts, and of a function call's arguments, that the server didn't send are made ahead of
// it. A skipped item's makes nothing.
func (d *streamDecoder) doneItem(e streamEvent) error {
	if e.Item == nil {
		return errors.New("output_item.done has no item")
	}
	it, err := d.item(e.OutputIndex, e.Item.ID)
	if err != nil || it.skipped {
		return err
	}
	done, ok, err := decodeItem(*e.Item)
	switch {
	case err != nil:
		return fmt.Errorf("output %d: %w", it.index, err)
	case !ok:
		return fmt.Errorf("output %d: item of type %q is done where a %T was added", it.index, e.Item.Type, it.added)
	}
	if err := it.check(done); err != nil {
		return fmt.Errorf("output %d: %w", it.index, err)
	}
	done = withStatus(done, protocol.StatusCompleted)
	for _, p := range it.parts {
		if !p.done {
			if err := d.closePart(it, p); err != nil {
				return err
			}
		}
	}
	if isFunctionCall(it.added) && !it.argsDone {
		if err := d.doneArgs(it, it.args.String()); err != nil {
			return err
		}
	}
	it.done, it.item = true, done
	d.emit(protocol.OutputItemDone{SequenceNumber: d.next(), OutputIndex: it.index, Item: done})
	return nil
}

// withStatus returns it, a message, reasoning item, or function call, in status when it has no
// status of its own.
func withStatus(it protocol.Item, status protocol.Status) protocol.Item {
	switch item := it.(type) {
	case protocol.Message:
		if item.Status == "" {
			item.Status = status
		}
		return item
	case protocol.Reasoning:
		if item.Status == "" {
			item.Status = status
		}
		return item
	case protocol.FunctionCall:
		if item.Status == "" {
			item.Status = status
		}
		return item
	}
	return it
}

// statusOf returns the status of it, a message, reasoning item, or function call.
func statusOf(it protocol.Item) protocol.Status {
	switch it := it.(type) {
	case protocol.Message:
		return it.Status
	case protocol.Reasoning:
		return it.Status
	case protocol.FunctionCall:
		return it.Status
	}
	return ""
}

// check reports whether done, the item an output_item.done gives, is the item it streamed: of
// its type and id, a message of its role and of the parts streamed, a reasoning item of the
// parts streamed, and a function call of its call_id, name, and the arguments streamed.
func (it *streamItem) check(done protocol.Item) error {
	switch added := it.added.(type) {
	case protocol.Message:
		m, ok := done.(protocol.Message)
		switch {
		case !ok:
			return fmt.Errorf("item %T is done where a message was added", done)
		case m.ID != added.ID || m.Role != added.Role:
			return fmt.Errorf("message %q of role %s is done where message %q of role %s was added", m.ID, m.Role, added.ID, added.Role)
		}
		return it.checkParts(m.Content)
	case protocol.Reasoning:
		r, ok := done.(protocol.Reasoning)
		switch {
		case !ok:
			return fmt.Errorf("item %T is done where a reasoning item was added", done)
		case r.ID != added.ID:
			return fmt.Errorf("reasoning item %q is done where reasoning item %q was added", r.ID, added.ID)
		}
		content := make([]protocol.ContentPart, len(r.Content))
		for i, p := range r.Content {
			content[i] = p
		}
		return it.checkParts(content)
	case protocol.FunctionCall:
		c, ok := done.(protocol.FunctionCall)
		switch {
		case !ok:
			return fmt.Errorf("item %T is done where a function call was added", done)
		case c.ID != added.ID || c.CallID != added.CallID || c.Name != added.Name:
			return fmt.Errorf("function call %q, call %q of %s, is done where function call %q, call %q of %s, was added", c.ID, c.CallID, c.Name, added.ID, added.CallID, added.Name)
		case c.Arguments != it.args.String():
			return fmt.Errorf("arguments %q aren't the arguments streamed, %q", c.Arguments, it.args.String())
		}
	}
	return nil
}

// checkParts reports whether content, a done item's, is the parts it streamed, in order, the
// skipped parts aside, as DecodeResponse skips them.
func (it *streamItem) checkParts(content []protocol.ContentPart) error {
	parts := it.content()
	if len(content) != len(parts) {
		return fmt.Errorf("the item has %d content parts, want the %d streamed", len(content), len(parts))
	}
	for i, p := range parts {
		if want := contentPart(p.kind, p.text.String()); content[i] != want {
			return fmt.Errorf("content %d %#v isn't the part streamed, %#v", i, content[i], want)
		}
	}
	return nil
}

// end reads a terminal event of typ, its response raw, closing the stream with it, as
// NewStreamDecoder describes.
func (d *streamDecoder) end(typ string, raw jsontext.Value) error {
	if len(raw) == 0 {
		return fmt.Errorf("%s has no response", typ)
	}
	resp, err := decodeResponse(raw)
	if err != nil {
		return fmt.Errorf("%s: %w", typ, err)
	}
	want := map[string]protocol.Status{
		"response.completed":  protocol.StatusCompleted,
		"response.incomplete": protocol.StatusIncomplete,
		"response.failed":     protocol.StatusFailed,
	}[typ]
	if resp.Status != want {
		return fmt.Errorf("%s's response has status %s", typ, resp.Status)
	}
	if want != protocol.StatusFailed {
		if err := d.checkOutput(resp.Output); err != nil {
			return fmt.Errorf("%s: %w", typ, err)
		}
		// An item the server gives no status here, as the router gives a reasoning item
		// none, has the status its output_item.done gave it.
		items := d.known()
		for i, it := range resp.Output {
			resp.Output[i] = withStatus(it, statusOf(items[i].item))
		}
	}
	d.ended = true
	switch want {
	case protocol.StatusCompleted:
		d.emit(protocol.ResponseCompleted{SequenceNumber: d.next(), Response: resp})
	case protocol.StatusIncomplete:
		d.emit(protocol.ResponseIncomplete{SequenceNumber: d.next(), Response: resp})
	default:
		d.emit(protocol.ResponseFailed{SequenceNumber: d.next(), Response: resp})
	}
	return nil
}

// checkOutput reports whether output, a completed or incomplete response's, is the items
// streamed: every one done, no output index skipped, and each of output the one streamed in its
// place, by id. A skipped item holds its output index, needs no done event, and has no place in
// output, as DecodeResponse skips it there too.
func (d *streamDecoder) checkOutput(output []protocol.Item) error {
	for i, it := range d.items {
		switch {
		case it.output != i:
			return fmt.Errorf("output %d wasn't added", i)
		case !it.done && !it.skipped:
			return fmt.Errorf("output %d isn't done", it.index)
		}
	}
	items := d.known()
	if len(output) != len(items) {
		return fmt.Errorf("the response has %d output items, want the %d streamed", len(output), len(items))
	}
	for i, it := range output {
		if id := itemID(it); id != items[i].id {
			return fmt.Errorf("the response's output %d is item %q, not %q, the one streamed", i, id, items[i].id)
		}
	}
	return nil
}

// known returns the items added that aren't skipped, in output order: those the events give.
func (d *streamDecoder) known() []*streamItem {
	var items []*streamItem
	for _, it := range d.items {
		if !it.skipped {
			items = append(items, it)
		}
	}
	return items
}

// itemID returns the id of it, a message, reasoning item, or function call.
func itemID(it protocol.Item) string {
	switch it := it.(type) {
	case protocol.Message:
		return it.ID
	case protocol.Reasoning:
		return it.ID
	case protocol.FunctionCall:
		return it.ID
	}
	return ""
}

// fail closes the stream at an error frame, data, as NewStreamDecoder describes: the error
// event, then response.failed. Both shapes hold the error in an error member, the error object
// of an OpenAI-style error body.
func (d *streamDecoder) fail(data []byte) {
	d.ended = true
	d.create()
	var payload protocol.ErrorPayload
	payload.Message, payload.Type, payload.Code = format.DecodeOpenAIError(data)
	var param streamError
	// A param that isn't a string, such as null, leaves it empty; the rest is still the error.
	_ = json.Unmarshal(data, &param)
	payload.Param = param.Error.Param
	d.emit(protocol.ErrorEvent{SequenceNumber: d.next(), Error: payload})
	d.emit(protocol.ResponseFailed{SequenceNumber: d.next(), Response: &protocol.Response{
		ID:     d.id,
		Model:  d.model,
		Output: d.output(),
		Status: protocol.StatusFailed,
		Error:  &protocol.ResponseError{Code: payload.Code, Message: payload.Message},
	}})
}

// output returns the items added, in output order, the skipped aside: each done item as it was
// done, and each open one in progress with the content or the arguments made so far, as the
// Accumulator holds it.
func (d *streamDecoder) output() []protocol.Item {
	var out []protocol.Item
	for _, it := range d.known() {
		if it.done {
			out = append(out, it.item)
			continue
		}
		switch item := it.added.(type) {
		case protocol.Message:
			for _, p := range it.content() {
				item.Content = append(item.Content, contentPart(p.kind, p.text.String()))
			}
			out = append(out, item)
		case protocol.Reasoning:
			for _, p := range it.content() {
				item.Content = append(item.Content, protocol.ReasoningText{Text: p.text.String()})
			}
			out = append(out, item)
		case protocol.FunctionCall:
			item.Arguments = it.args.String()
			out = append(out, item)
		}
	}
	return out
}

// End ends the stream, as NewStreamDecoder describes: it makes nothing once the terminal event,
// an error frame, or an error has ended it, and is io.ErrUnexpectedEOF before.
func (d *streamDecoder) End() ([]protocol.Event, error) {
	if d.ended {
		return nil, nil
	}
	d.ended = true
	return nil, fmt.Errorf("responses: stream: stream ended before its terminal event: %w", io.ErrUnexpectedEOF)
}

// emit makes e, the stream's next event.
func (d *streamDecoder) emit(e protocol.Event) {
	d.events = append(d.events, e)
}

// next returns the next event's sequence number.
func (d *streamDecoder) next() int {
	n := d.seq
	d.seq++
	return n
}
