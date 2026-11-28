package responses_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

type streamResult struct {
	events []protocol.Event
	err    error
}

func decodeStream(t *testing.T, body string) streamResult {
	t.Helper()
	wf := responses.New(responses.Options{})
	decoder := wf.NewStreamDecoder()
	events := wf.EventStream().NewReader(strings.NewReader(body))
	var r streamResult
	for {
		e, err := events.Next()
		if errors.Is(err, io.EOF) {
			made, err := decoder.End()
			r.events = append(r.events, made...)
			r.err = err
			return r
		}
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		made, err := decoder.Decode(e)
		r.events = append(r.events, made...)
		if err != nil {
			r.err = err
			return r
		}
	}
}

func stream(events ...string) string {
	var b strings.Builder
	for i, data := range events {
		var e struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(data), &e)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", e.Type, data)
		if i == 0 {
			b.WriteString(":\n\n")
		}
	}
	return b.String()
}

func terminalResponse(t *testing.T, body string) *protocol.Response {
	t.Helper()
	var response jsontext.Value
	events := format.SSE.NewReader(strings.NewReader(body))
	for {
		e, err := events.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		var event struct {
			Type     string         `json:"type"`
			Response jsontext.Value `json:"response"`
		}
		if err := json.Unmarshal(e.Data, &event); err != nil {
			t.Fatal(err)
		}
		switch event.Type {
		case "response.completed", "response.incomplete", "response.failed":
			response = event.Response
		}
	}
	if response == nil {
		t.Fatal("the stream has no terminal event")
	}
	resp, err := decode(string(response))
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	for i, it := range resp.Output {
		if r, ok := it.(protocol.Reasoning); ok && r.Status == "" {
			r.Status = protocol.StatusCompleted
			resp.Output[i] = r
		}
	}
	return resp
}

func trace(events []protocol.Event) string {
	var b strings.Builder
	for i := 0; i < len(events); i++ {
		switch e := events[i].(type) {
		case protocol.ReasoningDelta:
			n := 1
			for i+n < len(events) {
				if _, ok := events[i+n].(protocol.ReasoningDelta); !ok {
					break
				}
				n++
			}
			if n == 1 {
				fmt.Fprintf(&b, "%d response.reasoning.delta %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Delta)
				continue
			}
			last := events[i+n-1].(protocol.ReasoningDelta)
			fmt.Fprintf(&b, "%d-%d response.reasoning.delta %d %d %s, %d deltas\n", e.SequenceNumber, last.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, n)
			i += n - 1
		case protocol.ReasoningDone:
			fmt.Fprintf(&b, "%d response.reasoning.done %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Text)
		case protocol.ResponseCreated:
			fmt.Fprintf(&b, "%d response.created %s\n", e.SequenceNumber, e.Response.Status)
		case protocol.ResponseCompleted:
			fmt.Fprintf(&b, "%d response.completed %d items\n", e.SequenceNumber, len(e.Response.Output))
		case protocol.ResponseIncomplete:
			fmt.Fprintf(&b, "%d response.incomplete %d items\n", e.SequenceNumber, len(e.Response.Output))
		case protocol.ResponseFailed:
			fmt.Fprintf(&b, "%d response.failed %d items\n", e.SequenceNumber, len(e.Response.Output))
		case protocol.ErrorEvent:
			fmt.Fprintf(&b, "%d error %#v\n", e.SequenceNumber, e.Error)
		case protocol.OutputItemAdded:
			fmt.Fprintf(&b, "%d response.output_item.added %d %#v\n", e.SequenceNumber, e.OutputIndex, e.Item)
		case protocol.OutputItemDone:
			fmt.Fprintf(&b, "%d response.output_item.done %d %#v\n", e.SequenceNumber, e.OutputIndex, e.Item)
		case protocol.ContentPartAdded:
			fmt.Fprintf(&b, "%d response.content_part.added %d %d %s %#v\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Part)
		case protocol.ContentPartDone:
			fmt.Fprintf(&b, "%d response.content_part.done %d %d %s %#v\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Part)
		case protocol.OutputTextDelta:
			fmt.Fprintf(&b, "%d response.output_text.delta %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Delta)
		case protocol.OutputTextDone:
			fmt.Fprintf(&b, "%d response.output_text.done %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Text)
		case protocol.RefusalDelta:
			fmt.Fprintf(&b, "%d response.refusal.delta %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Delta)
		case protocol.RefusalDone:
			fmt.Fprintf(&b, "%d response.refusal.done %d %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ContentIndex, e.ItemID, e.Refusal)
		case protocol.FunctionCallArgumentsDelta:
			fmt.Fprintf(&b, "%d response.function_call_arguments.delta %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ItemID, e.Delta)
		case protocol.FunctionCallArgumentsDone:
			fmt.Fprintf(&b, "%d response.function_call_arguments.done %d %s %q\n", e.SequenceNumber, e.OutputIndex, e.ItemID, e.Arguments)
		default:
			fmt.Fprintf(&b, "%T\n", e)
		}
	}
	return b.String()
}

func assertTrace(t *testing.T, events []protocol.Event, want string) {
	t.Helper()
	var lines []string
	for line := range strings.Lines(strings.TrimSpace(want)) {
		lines = append(lines, strings.TrimLeft(line, "\t"))
	}
	if got, want := trace(events), strings.Join(lines, "")+"\n"; got != want {
		t.Errorf("events:\n%s\nwant:\n%s", got, want)
	}
}

func event(typ, members string) string {
	if members == "" {
		return `{"type":"` + typ + `"}`
	}
	return `{"type":"` + typ + `",` + members + `}`
}

const (
	created    = `{"type":"response.created","response":{"id":"resp_1","object":"response","model":"m-1","status":"in_progress"}}`
	inProgress = `{"type":"response.in_progress","response":{"id":"resp_1","object":"response","status":"in_progress"}}`

	rsAdded  = `{"type":"reasoning","id":"rs_1","summary":[],"content":[],"encrypted_content":"","status":"in_progress"}`
	rsDone   = `{"type":"reasoning","id":"rs_1","summary":[],"content":[{"type":"reasoning_text","text":"Need the tool."}],"encrypted_content":""}`
	msgAdded = `{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}`
	msgDone  = `{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","annotations":[],"logprobs":[],"text":"Checking."}]}`
	fcAdded  = `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"","status":"in_progress"}`
	fcDone   = `{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}","status":"completed"}`

	usageJSON = `"usage":{"input_tokens":7,"output_tokens":5,"total_tokens":12,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":2}}`
)

func added(item string) string    { return event("response.output_item.added", `"item":`+item) }
func itemDone(item string) string { return event("response.output_item.done", `"item":`+item) }

func delta(typ, id, delta string) string {
	return event(typ, fmt.Sprintf(`"item_id":%q,"delta":%s`, id, quote(delta)))
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func terminal(typ, status, extra string, output ...string) string {
	return event(typ, fmt.Sprintf(`"response":{"id":"resp_1","object":"response","model":"m-1","status":%q,"output":[%s],%s}`, status, strings.Join(output, ","), extra))
}

var (
	routerOpen = []string{
		created,
		inProgress,
		added(rsAdded),
		delta("response.reasoning_text.delta", "rs_1", "Need"),
		delta("response.reasoning_text.delta", "rs_1", " the tool."),
		added(msgAdded),
		event("response.content_part.added", `"item_id":"msg_1","part":{"type":"output_text","text":""}`),
		delta("response.output_text.delta", "msg_1", "Checking."),
		added(fcAdded),
		delta("response.function_call_arguments.delta", "fc_1", `{"city":`),
		delta("response.function_call_arguments.delta", "fc_1", `"Paris"}`),
	}
	routerClose = []string{
		itemDone(rsDone),
		event("response.output_text.done", `"item_id":"msg_1","text":"Checking."`),
		event("response.content_part.done", `"item_id":"msg_1","part":{"type":"output_text","annotations":[],"logprobs":[],"text":"Checking."}`),
		itemDone(msgDone),
		itemDone(fcDone),
		terminal("response.completed", "completed", usageJSON, rsDone, msgDone, fcDone),
	}
)

func TestStreamRecorded(t *testing.T) {
	cases := []struct {
		name  string
		trace string
	}{
		{"text", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Reasoning{ID:"rs_l0DH4feO0lEFnP6fEYo84ApdoR5iC4Ct", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
			2 response.content_part.added 0 0 rs_l0DH4feO0lEFnP6fEYo84ApdoR5iC4Ct protocol.ReasoningText{Text:""}
			3-64 response.reasoning.delta 0 0 rs_l0DH4feO0lEFnP6fEYo84ApdoR5iC4Ct, 62 deltas
			65 response.output_item.added 1 protocol.Message{ID:"msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
			66 response.content_part.added 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes protocol.OutputText{Text:""}
			67 response.output_text.delta 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes "Paris"
			68 response.output_text.delta 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes ","
			69 response.output_text.delta 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes " Lyon"
			70 response.output_text.delta 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes ","
			71 response.output_text.delta 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes " Marseille"
			72 response.reasoning.done 0 0 rs_l0DH4feO0lEFnP6fEYo84ApdoR5iC4Ct "The user asks: \"Name three French cities, comma-separated, nothing else.\" The developer says: \"Answer with the names alone.\" So we should output three French city names, separated by commas, no extra text. That's fine. No policy violation. Provide e.g., \"Paris, Lyon, Marseille\"."
			73 response.content_part.done 0 0 rs_l0DH4feO0lEFnP6fEYo84ApdoR5iC4Ct protocol.ReasoningText{Text:"The user asks: \"Name three French cities, comma-separated, nothing else.\" The developer says: \"Answer with the names alone.\" So we should output three French city names, separated by commas, no extra text. That's fine. No policy violation. Provide e.g., \"Paris, Lyon, Marseille\"."}
			74 response.output_item.done 0 protocol.Reasoning{ID:"rs_l0DH4feO0lEFnP6fEYo84ApdoR5iC4Ct", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"The user asks: \"Name three French cities, comma-separated, nothing else.\" The developer says: \"Answer with the names alone.\" So we should output three French city names, separated by commas, no extra text. That's fine. No policy violation. Provide e.g., \"Paris, Lyon, Marseille\"."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"go-ai:responses/v1:eyJlbmNyeXB0ZWRfY29udGVudCI6IiIsImNvbnRlbnQiOlsiVGhlIHVzZXIgYXNrczogXCJOYW1lIHRocmVlIEZyZW5jaCBjaXRpZXMsIGNvbW1hLXNlcGFyYXRlZCwgbm90aGluZyBlbHNlLlwiIFRoZSBkZXZlbG9wZXIgc2F5czogXCJBbnN3ZXIgd2l0aCB0aGUgbmFtZXMgYWxvbmUuXCIgU28gd2Ugc2hvdWxkIG91dHB1dCB0aHJlZSBGcmVuY2ggY2l0eSBuYW1lcywgc2VwYXJhdGVkIGJ5IGNvbW1hcywgbm8gZXh0cmEgdGV4dC4gVGhhdCdzIGZpbmUuIE5vIHBvbGljeSB2aW9sYXRpb24uIFByb3ZpZGUgZS5nLiwgXCJQYXJpcywgTHlvbiwgTWFyc2VpbGxlXCIuIl19"}
			75 response.output_text.done 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes "Paris, Lyon, Marseille"
			76 response.content_part.done 1 0 msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes protocol.OutputText{Text:"Paris, Lyon, Marseille"}
			77 response.output_item.done 1 protocol.Message{ID:"msg_0mLiut2ZpnKtRAzKjma1E1QuXzYvHzes", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Paris, Lyon, Marseille"}}}
			78 response.completed 2 items`},
		{"tool_call", `
			0 response.created in_progress
			1 response.output_item.added 0 protocol.Reasoning{ID:"rs_Ez5hgLi2ioU8m83JLwF86wnX6VoPCBdR", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
			2 response.content_part.added 0 0 rs_Ez5hgLi2ioU8m83JLwF86wnX6VoPCBdR protocol.ReasoningText{Text:""}
			3-16 response.reasoning.delta 0 0 rs_Ez5hgLi2ioU8m83JLwF86wnX6VoPCBdR, 14 deltas
			17 response.output_item.added 1 protocol.FunctionCall{ID:"fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD", Status:"in_progress", CallID:"call_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD", Name:"get_weather", Arguments:""}
			18 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "{"
			19 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "\n  \""
			20 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "city"
			21 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "\":"
			22 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD " \""
			23 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "Paris"
			24 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "\""
			25 response.function_call_arguments.delta 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "\n}"
			26 response.reasoning.done 0 0 rs_Ez5hgLi2ioU8m83JLwF86wnX6VoPCBdR "User asks current weather in Paris. Need to call get_weather function."
			27 response.content_part.done 0 0 rs_Ez5hgLi2ioU8m83JLwF86wnX6VoPCBdR protocol.ReasoningText{Text:"User asks current weather in Paris. Need to call get_weather function."}
			28 response.output_item.done 0 protocol.Reasoning{ID:"rs_Ez5hgLi2ioU8m83JLwF86wnX6VoPCBdR", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"User asks current weather in Paris. Need to call get_weather function."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"go-ai:responses/v1:eyJlbmNyeXB0ZWRfY29udGVudCI6IiIsImNvbnRlbnQiOlsiVXNlciBhc2tzIGN1cnJlbnQgd2VhdGhlciBpbiBQYXJpcy4gTmVlZCB0byBjYWxsIGdldF93ZWF0aGVyIGZ1bmN0aW9uLiJdfQ=="}
			29 response.function_call_arguments.done 1 fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD "{\n  \"city\": \"Paris\"\n}"
			30 response.output_item.done 1 protocol.FunctionCall{ID:"fc_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD", Status:"completed", CallID:"call_TVKSdkeSZt62DYlf3kxA8ZIG66tWeCdD", Name:"get_weather", Arguments:"{\n  \"city\": \"Paris\"\n}"}
			31 response.completed 2 items`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := string(aitest.ReadTestdata(t, tc.name+".sse"))
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			assertTrace(t, r.events, tc.trace)
			want := terminalResponse(t, body)
			aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
			assertDoneItems(t, r.events, want)
		})
	}
}

func assertDoneItems(t *testing.T, events []protocol.Event, resp *protocol.Response) {
	t.Helper()
	for _, e := range events {
		done, ok := e.(protocol.OutputItemDone)
		if !ok {
			continue
		}
		if done.OutputIndex >= len(resp.Output) {
			t.Errorf("output_item.done %d is past the response's %d items", done.OutputIndex, len(resp.Output))
			continue
		}
		want := resp.Output[done.OutputIndex]
		if !reflect.DeepEqual(done.Item, want) {
			t.Errorf("output_item.done %d = %#v\nwant the response's %#v", done.OutputIndex, done.Item, want)
		}
	}
}

var specStream = []string{
	`{"type":"response.created","sequence_number":3,"response":{"id":"resp_1","object":"response","status":"in_progress","output":[]}}`,
	`{"type":"response.in_progress","sequence_number":4,"response":{"id":"resp_1","object":"response","status":"in_progress","output":[]}}`,
	`{"type":"response.output_item.added","sequence_number":5,"output_index":0,"item":{"type":"reasoning","id":"rs_1","status":"in_progress","summary":[],"content":[]}}`,
	`{"type":"response.content_part.added","sequence_number":6,"output_index":0,"item_id":"rs_1","content_index":0,"part":{"type":"reasoning_text","text":""}}`,
	`{"type":"response.reasoning.delta","sequence_number":7,"output_index":0,"item_id":"rs_1","content_index":0,"delta":"Need"}`,
	`{"type":"response.reasoning.delta","sequence_number":9,"output_index":0,"item_id":"rs_1","content_index":0,"delta":" the tool."}`,
	`{"type":"response.reasoning.done","sequence_number":10,"output_index":0,"item_id":"rs_1","content_index":0,"text":"Need the tool."}`,
	`{"type":"response.content_part.done","sequence_number":11,"output_index":0,"item_id":"rs_1","content_index":0,"part":{"type":"reasoning_text","text":"Need the tool."}}`,
	`{"type":"response.output_item.done","sequence_number":12,"output_index":0,"item":{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"Tool."}],"content":[{"type":"reasoning_text","text":"Need the tool."}],"encrypted_content":"enc"}}`,
	`{"type":"response.output_item.added","sequence_number":13,"output_index":2,"item":{"type":"function_call","id":"fc_1","status":"in_progress","call_id":"call_1","name":"get_weather","arguments":""}}`,
	`{"type":"response.output_item.added","sequence_number":14,"output_index":1,"item":{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[]}}`,
	`{"type":"response.content_part.added","sequence_number":15,"output_index":1,"item_id":"msg_1","content_index":0,"part":{"type":"output_text","text":"","annotations":[]}}`,
	`{"type":"response.function_call_arguments.delta","sequence_number":16,"output_index":2,"item_id":"fc_1","delta":"{\"city\":"}`,
	`{"type":"response.output_text.delta","sequence_number":17,"output_index":1,"item_id":"msg_1","content_index":0,"delta":"Checking.","logprobs":[]}`,
	`{"type":"response.function_call_arguments.delta","sequence_number":18,"output_index":2,"item_id":"fc_1","delta":"\"Paris\"}"}`,
	`{"type":"response.output_text.done","sequence_number":19,"output_index":1,"item_id":"msg_1","content_index":0,"text":"Checking.","logprobs":[]}`,
	`{"type":"response.content_part.done","sequence_number":20,"output_index":1,"item_id":"msg_1","content_index":0,"part":{"type":"output_text","text":"Checking.","annotations":[]}}`,
	`{"type":"response.content_part.added","sequence_number":21,"output_index":1,"item_id":"msg_1","content_index":1,"part":{"type":"refusal","refusal":""}}`,
	`{"type":"response.refusal.delta","sequence_number":22,"output_index":1,"item_id":"msg_1","content_index":1,"delta":"No."}`,
	`{"type":"response.refusal.done","sequence_number":23,"output_index":1,"item_id":"msg_1","content_index":1,"refusal":"No."}`,
	`{"type":"response.content_part.done","sequence_number":24,"output_index":1,"item_id":"msg_1","content_index":1,"part":{"type":"refusal","refusal":"No."}}`,
	`{"type":"response.function_call_arguments.done","sequence_number":25,"output_index":2,"item_id":"fc_1","arguments":"{\"city\":\"Paris\"}"}`,
	`{"type":"response.output_item.done","sequence_number":26,"output_index":2,"item":{"type":"function_call","id":"fc_1","status":"completed","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}}`,
	`{"type":"response.output_item.done","sequence_number":27,"output_index":1,"item":{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Checking.","annotations":[]},{"type":"refusal","refusal":"No."}]}}`,
	compact(`{"type":"response.completed","sequence_number":28,"response":{"id":"resp_1","object":"response","status":"completed","output":[
		{"type":"reasoning","id":"rs_1","status":"completed","summary":[{"type":"summary_text","text":"Tool."}],"content":[{"type":"reasoning_text","text":"Need the tool."}],"encrypted_content":"enc"},
		{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[{"type":"output_text","text":"Checking.","annotations":[]},{"type":"refusal","refusal":"No."}]},
		{"type":"function_call","id":"fc_1","status":"completed","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"Paris\"}"}],` + usageJSON + `}}`),
}

func compact(s string) string {
	v := jsontext.Value(s)
	if err := v.Compact(); err != nil {
		panic(err)
	}
	return string(v)
}

func TestStreamSpec(t *testing.T) {
	body := stream(specStream...)
	r := decodeStream(t, body)
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	if got, want := len(r.events), len(specStream)-1; got != want {
		t.Errorf("%d events, want the %d sent, response.in_progress aside", got, want)
	}
	assertTrace(t, r.events, `
		0 response.created in_progress
		1 response.output_item.added 0 protocol.Reasoning{ID:"rs_1", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
		2 response.content_part.added 0 0 rs_1 protocol.ReasoningText{Text:""}
		3-4 response.reasoning.delta 0 0 rs_1, 2 deltas
		5 response.reasoning.done 0 0 rs_1 "Need the tool."
		6 response.content_part.done 0 0 rs_1 protocol.ReasoningText{Text:"Need the tool."}
		7 response.output_item.done 0 protocol.Reasoning{ID:"rs_1", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"Need the tool."}}, Summary:[]protocol.SummaryText{protocol.SummaryText{Text:"Tool."}}, EncryptedContent:"`+seal(t, "enc", "Need the tool.")+`"}
		8 response.output_item.added 2 protocol.FunctionCall{ID:"fc_1", Status:"in_progress", CallID:"call_1", Name:"get_weather", Arguments:""}
		9 response.output_item.added 1 protocol.Message{ID:"msg_1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
		10 response.content_part.added 1 0 msg_1 protocol.OutputText{Text:""}
		11 response.function_call_arguments.delta 2 fc_1 "{\"city\":"
		12 response.output_text.delta 1 0 msg_1 "Checking."
		13 response.function_call_arguments.delta 2 fc_1 "\"Paris\"}"
		14 response.output_text.done 1 0 msg_1 "Checking."
		15 response.content_part.done 1 0 msg_1 protocol.OutputText{Text:"Checking."}
		16 response.content_part.added 1 1 msg_1 protocol.Refusal{Refusal:""}
		17 response.refusal.delta 1 1 msg_1 "No."
		18 response.refusal.done 1 1 msg_1 "No."
		19 response.content_part.done 1 1 msg_1 protocol.Refusal{Refusal:"No."}
		20 response.function_call_arguments.done 2 fc_1 "{\"city\":\"Paris\"}"
		21 response.output_item.done 2 protocol.FunctionCall{ID:"fc_1", Status:"completed", CallID:"call_1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
		22 response.output_item.done 1 protocol.Message{ID:"msg_1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Checking."}, protocol.Refusal{Refusal:"No."}}}
		23 response.completed 3 items`)
	want := terminalResponse(t, body)
	aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
	assertDoneItems(t, r.events, want)
}

func TestStreamFills(t *testing.T) {
	body := stream(slices.Concat(routerOpen, routerClose)...)
	r := decodeStream(t, body)
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	assertTrace(t, r.events, `
		0 response.created in_progress
		1 response.output_item.added 0 protocol.Reasoning{ID:"rs_1", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
		2 response.content_part.added 0 0 rs_1 protocol.ReasoningText{Text:""}
		3-4 response.reasoning.delta 0 0 rs_1, 2 deltas
		5 response.output_item.added 1 protocol.Message{ID:"msg_1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
		6 response.content_part.added 1 0 msg_1 protocol.OutputText{Text:""}
		7 response.output_text.delta 1 0 msg_1 "Checking."
		8 response.output_item.added 2 protocol.FunctionCall{ID:"fc_1", Status:"in_progress", CallID:"call_1", Name:"get_weather", Arguments:""}
		9 response.function_call_arguments.delta 2 fc_1 "{\"city\":"
		10 response.function_call_arguments.delta 2 fc_1 "\"Paris\"}"
		11 response.reasoning.done 0 0 rs_1 "Need the tool."
		12 response.content_part.done 0 0 rs_1 protocol.ReasoningText{Text:"Need the tool."}
		13 response.output_item.done 0 protocol.Reasoning{ID:"rs_1", Status:"completed", Content:[]protocol.ReasoningText{protocol.ReasoningText{Text:"Need the tool."}}, Summary:[]protocol.SummaryText(nil), EncryptedContent:"`+seal(t, "", "Need the tool.")+`"}
		14 response.output_text.done 1 0 msg_1 "Checking."
		15 response.content_part.done 1 0 msg_1 protocol.OutputText{Text:"Checking."}
		16 response.output_item.done 1 protocol.Message{ID:"msg_1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Checking."}}}
		17 response.function_call_arguments.done 2 fc_1 "{\"city\":\"Paris\"}"
		18 response.output_item.done 2 protocol.FunctionCall{ID:"fc_1", Status:"completed", CallID:"call_1", Name:"get_weather", Arguments:"{\"city\":\"Paris\"}"}
		19 response.completed 3 items`)
	want := terminalResponse(t, body)
	aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
	assertDoneItems(t, r.events, want)

	bare := slices.Concat(routerOpen, []string{itemDone(rsDone), itemDone(msgDone), itemDone(fcDone), routerClose[len(routerClose)-1]})
	got := decodeStream(t, stream(bare...))
	if got.err != nil {
		t.Fatalf("stream error: %v", got.err)
	}
	if trace(got.events) != trace(r.events) {
		t.Errorf("events of a message closed at its output_item.done alone:\n%s\nwant:\n%s", trace(got.events), trace(r.events))
	}
}

func TestStreamUsage(t *testing.T) {
	r := decodeStream(t, stream(created, terminal("response.completed", "completed", usageJSON)))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	completed, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted)
	if !ok {
		t.Fatalf("last event = %#v, want response.completed", r.events[len(r.events)-1])
	}
	want := protocol.Usage{InputTokens: 7, OutputTokens: 5, TotalTokens: 12,
		InputTokensDetails:  protocol.InputTokensDetails{CachedTokens: 3},
		OutputTokensDetails: protocol.OutputTokensDetails{ReasoningTokens: 2}}
	if completed.Response.Usage != want {
		t.Errorf("Usage = %+v, want %+v", completed.Response.Usage, want)
	}
}

func TestStreamTerminal(t *testing.T) {
	text := []string{
		created,
		added(msgAdded),
		event("response.content_part.added", `"item_id":"msg_1","part":{"type":"output_text","text":""}`),
		delta("response.output_text.delta", "msg_1", "Checking."),
	}
	incompleteMsg := `{"type":"message","id":"msg_1","status":"incomplete","role":"assistant","content":[{"type":"output_text","text":"Checking."}]}`
	cases := []struct {
		name   string
		events []string
		want   string
	}{
		{"incomplete", slices.Concat(text, []string{itemDone(incompleteMsg),
			terminal("response.incomplete", "incomplete", `"incomplete_details":{"reason":"max_output_tokens"},`+usageJSON, incompleteMsg)}),
			"response.incomplete"},
		{"failed with the message open", slices.Concat(text, []string{
			terminal("response.failed", "failed", `"error":{"code":"server_error","message":"The model crashed."},`+usageJSON,
				`{"type":"message","id":"msg_1","status":"in_progress","role":"assistant","content":[{"type":"output_text","text":"Checking."}]}`)}),
			"response.failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := stream(tc.events...)
			r := decodeStream(t, body)
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			if got := trace(r.events[len(r.events)-1:]); !strings.Contains(got, tc.want) {
				t.Errorf("last event = %s, want %s", got, tc.want)
			}
			aitest.AssertResponse(t, aitest.Fold(t, r.events), terminalResponse(t, body))
		})
	}
}

func TestStreamIgnores(t *testing.T) {
	events := slices.Concat(routerOpen, routerClose)
	want := decodeStream(t, stream(events...))
	if want.err != nil {
		t.Fatalf("stream error: %v", want.err)
	}
	cases := []struct {
		name  string
		extra string
	}{
		{"response.in_progress", inProgress},
		{"response.queued", event("response.queued", `"response":{"id":"resp_1","status":"queued"}`)},
		{"a summary part added", event("response.reasoning_summary_part.added", `"item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":""}`)},
		{"a summary delta", event("response.reasoning_summary_text.delta", `"item_id":"rs_1","output_index":0,"summary_index":0,"delta":"Tool."`)},
		{"a summary done", event("response.reasoning_summary_text.done", `"item_id":"rs_1","output_index":0,"summary_index":0,"text":"Tool."`)},
		{"a summary part done", event("response.reasoning_summary_part.done", `"item_id":"rs_1","output_index":0,"summary_index":0,"part":{"type":"summary_text","text":"Tool."}`)},
		{"an annotation", event("response.output_text.annotation.added", `"item_id":"msg_1","output_index":1,"content_index":0,"annotation_index":0,"annotation":{"type":"url_citation","url":"https://example.com"}`)},
		{"an unknown event", event("response.flourish", `"item_id":"msg_1","delta":{"x":1}`)},
		{"an unknown event naming an item never added", event("response.flourish", `"item_id":"msg_9","output_index":9`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withExtra := slices.Concat([]string{tc.extra}, routerOpen[:4], []string{tc.extra}, routerOpen[4:], routerClose[:2], []string{tc.extra}, routerClose[2:])
			got := decodeStream(t, stream(withExtra...))
			if got.err != nil {
				t.Fatalf("stream error: %v", got.err)
			}
			if trace(got.events) != trace(want.events) {
				t.Errorf("events:\n%s\nwant those without it:\n%s", trace(got.events), trace(want.events))
			}
		})
	}
}

func TestStreamErrorFrame(t *testing.T) {
	const (
		spec  = `{"type":"error","sequence_number":9,"error":{"type":"invalid_request_error","code":"model_not_found","message":"no such model","param":"model"}}`
		null  = `{"type":"error","error":{"type":"server_error","code":null,"message":"Overloaded","param":null}}`
		llama = `{"error":{"code":500,"message":"context shift is disabled","type":"server_error"}}`
	)
	notFound := protocol.ErrorPayload{Type: "invalid_request_error", Code: "model_not_found", Message: "no such model", Param: "model"}
	overloaded := protocol.ErrorPayload{Type: "server_error", Message: "Overloaded"}
	contextShift := protocol.ErrorPayload{Type: "server_error", Code: "500", Message: "context shift is disabled"}
	openOutput := []protocol.Item{
		protocol.Reasoning{ID: "rs_1", Status: protocol.StatusInProgress, Content: []protocol.ReasoningText{{Text: "Need the tool."}}},
		protocol.Message{ID: "msg_1", Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Checking."}}},
		protocol.FunctionCall{ID: "fc_1", Status: protocol.StatusInProgress, CallID: "call_1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
	}
	closedOutput := slices.Clone(openOutput)
	closedOutput[0] = protocol.Reasoning{ID: "rs_1", Status: protocol.StatusCompleted, Content: []protocol.ReasoningText{{Text: "Need the tool."}}, EncryptedContent: seal(t, "", "Need the tool.")}
	cases := []struct {
		name    string
		events  []string
		payload protocol.ErrorPayload
		output  []protocol.Item
		trace   string
	}{
		{name: "the spec's, first", events: []string{spec}, payload: notFound, trace: `
			0 response.created in_progress
			1 error protocol.ErrorPayload{Type:"invalid_request_error", Code:"model_not_found", Message:"no such model", Param:"model"}
			2 response.failed 0 items`},
		{name: "llama.cpp's, first", events: []string{llama}, payload: contextShift, trace: `
			0 response.created in_progress
			1 error protocol.ErrorPayload{Type:"server_error", Code:"500", Message:"context shift is disabled", Param:""}
			2 response.failed 0 items`},
		{name: "the spec's, of null code and param, after response.created", events: []string{created, null}, payload: overloaded},
		{name: "the spec's, with items open", events: slices.Concat(routerOpen, []string{spec}), payload: notFound, output: openOutput},
		{name: "llama.cpp's, with items open", events: slices.Concat(routerOpen, []string{llama}), payload: contextShift, output: openOutput},
		{name: "llama.cpp's, with an item done", events: slices.Concat(routerOpen, routerClose[:1], []string{llama}), payload: contextShift, output: closedOutput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.events...))
			if r.err != nil {
				t.Fatalf("stream error = %v, want the stream closed with response.failed", r.err)
			}
			if tc.trace != "" {
				assertTrace(t, r.events, tc.trace)
			}
			n := len(r.events)
			if n < 2 {
				t.Fatalf("%d events, want the error event and response.failed last", n)
			}
			e, ok := r.events[n-2].(protocol.ErrorEvent)
			if !ok || e.Error != tc.payload {
				t.Errorf("next to last event = %#v, want the error event of %#v", r.events[n-2], tc.payload)
			}
			failed, ok := r.events[n-1].(protocol.ResponseFailed)
			if !ok {
				t.Fatalf("last event = %#v, want response.failed", r.events[n-1])
			}
			want := &protocol.Response{
				Output: tc.output,
				Status: protocol.StatusFailed,
				Error:  &protocol.ResponseError{Code: tc.payload.Code, Message: tc.payload.Message},
			}
			if tc.events[0] == created {
				want.ID, want.Model = "resp_1", "m-1"
			}
			aitest.AssertResponse(t, failed.Response, want)
			aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
			if before := aitest.Fold(t, r.events[:n-2]); !reflect.DeepEqual(before.Output, tc.output) {
				t.Errorf("output before the frame = %#v\nwant the failed response's %#v", before.Output, tc.output)
			}

			decoder := responses.New(responses.Options{}).NewStreamDecoder()
			for _, data := range tc.events {
				if _, err := decoder.Decode(format.WireEvent{Data: []byte(data)}); err != nil {
					t.Fatalf("Decode: %v", err)
				}
			}
			after := terminal("response.failed", "failed", `"error":{"code":"server_error","message":"x"}`)
			if events, err := decoder.Decode(format.WireEvent{Data: []byte(after)}); err == nil || len(events) > 0 {
				t.Errorf("Decode after the error frame = %v, %v; want no events and the stream ended", events, err)
			}
			if events, err := decoder.End(); err != nil || len(events) > 0 {
				t.Errorf("End after the error frame = %v, %v; want nothing", events, err)
			}
		})
	}
}

func TestStreamNotAnErrorFrame(t *testing.T) {
	r := decodeStream(t, stream(created,
		event("response.flourish", `"error":{"message":"not a frame"}`),
		`{"error":"a string"}`,
		terminal("response.completed", "completed", usageJSON)))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	if _, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted); !ok {
		t.Errorf("last event = %#v, want response.completed", r.events[len(r.events)-1])
	}
}

func TestStreamEnd(t *testing.T) {
	cases := []struct {
		name     string
		events   []string
		complete bool
	}{
		{"response.completed", slices.Concat(routerOpen, routerClose), true},
		{"end with every item done", slices.Concat(routerOpen, routerClose[:len(routerClose)-1]), false},
		{"end with items open", routerOpen, false},
		{"end after response.created", []string{created}, false},
		{"empty stream", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := decodeStream(t, stream(tc.events...))
			if !tc.complete {
				if !errors.Is(r.err, io.ErrUnexpectedEOF) {
					t.Errorf("stream error = %v, want io.ErrUnexpectedEOF", r.err)
				}
				return
			}
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			if _, ok := r.events[len(r.events)-1].(protocol.ResponseCompleted); !ok {
				t.Errorf("last event = %#v, want response.completed", r.events[len(r.events)-1])
			}
		})
	}
	decoder := responses.New(responses.Options{}).NewStreamDecoder()
	for _, data := range slices.Concat(routerOpen, routerClose) {
		if _, err := decoder.Decode(format.WireEvent{Data: []byte(data)}); err != nil {
			t.Fatalf("Decode(%s): %v", data, err)
		}
	}
	if events, err := decoder.End(); events != nil || err != nil {
		t.Errorf("End = %v, %v; want nothing", events, err)
	}
}

func TestStreamReasoningAddedEncrypted(t *testing.T) {
	rsWhole := `{"type":"reasoning","id":"rs_1","summary":[],"content":[],"encrypted_content":"whole"}`
	body := stream(created,
		added(`{"type":"reasoning","id":"rs_1","summary":[],"content":[],"encrypted_content":"part"}`),
		itemDone(rsWhole),
		added(msgAdded),
		event("response.content_part.added", `"item_id":"msg_1","part":{"type":"output_text","text":""}`),
		delta("response.output_text.delta", "msg_1", "Checking."),
		itemDone(msgDone),
		terminal("response.completed", "completed", usageJSON, rsWhole, msgDone))
	r := decodeStream(t, body)
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	assertTrace(t, r.events, `
		0 response.created in_progress
		1 response.output_item.added 0 protocol.Reasoning{ID:"rs_1", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
		2 response.output_item.done 0 protocol.Reasoning{ID:"rs_1", Status:"completed", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:"`+seal(t, "whole")+`"}
		3 response.output_item.added 1 protocol.Message{ID:"msg_1", Status:"in_progress", Role:"assistant", Content:[]protocol.ContentPart(nil)}
		4 response.content_part.added 1 0 msg_1 protocol.OutputText{Text:""}
		5 response.output_text.delta 1 0 msg_1 "Checking."
		6 response.output_text.done 1 0 msg_1 "Checking."
		7 response.content_part.done 1 0 msg_1 protocol.OutputText{Text:"Checking."}
		8 response.output_item.done 1 protocol.Message{ID:"msg_1", Status:"completed", Role:"assistant", Content:[]protocol.ContentPart{protocol.OutputText{Text:"Checking."}}}
		9 response.completed 2 items`)
	want := terminalResponse(t, body)
	aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
	assertDoneItems(t, r.events, want)
}

func TestStreamReasoningAddedSummary(t *testing.T) {
	rsSummed := `{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"Tool."}],"content":[]}`
	body := stream(created, added(rsSummed), itemDone(rsSummed),
		terminal("response.completed", "completed", usageJSON, rsSummed))
	r := decodeStream(t, body)
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	assertTrace(t, r.events, `
		0 response.created in_progress
		1 response.output_item.added 0 protocol.Reasoning{ID:"rs_1", Status:"in_progress", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText(nil), EncryptedContent:""}
		2 response.output_item.done 0 protocol.Reasoning{ID:"rs_1", Status:"completed", Content:[]protocol.ReasoningText(nil), Summary:[]protocol.SummaryText{protocol.SummaryText{Text:"Tool."}}, EncryptedContent:"`+seal(t, "")+`"}
		3 response.completed 1 items`)
	want := terminalResponse(t, body)
	aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
	assertDoneItems(t, r.events, want)
}

func TestStreamRefusesEvent(t *testing.T) {
	msg := []string{created, added(msgAdded)}
	part := event("response.content_part.added", `"item_id":"msg_1","part":{"type":"output_text","text":""}`)
	text := slices.Concat(msg, []string{part, delta("response.output_text.delta", "msg_1", "Hi")})
	call := []string{created, added(fcAdded), delta("response.function_call_arguments.delta", "fc_1", "{}")}
	completedEmpty := terminal("response.completed", "completed", usageJSON)
	cases := []struct {
		name   string
		events []string
		want   string
	}{
		{"not JSON", []string{created, `{"type":`}, "decode event"},
		{"an item before response.created", []string{added(msgAdded)}, "response.output_item.added before response.created"},
		{"a delta before response.created", []string{delta("response.output_text.delta", "msg_1", "Hi")}, "response.output_text.delta before response.created"},
		{"a second response.created", []string{created, created}, "a second response.created"},
		{"an item at an index added", []string{created,
			event("response.output_item.added", `"output_index":0,"item":`+msgAdded),
			event("response.output_item.added", `"output_index":0,"item":`+fcAdded)}, "output 0 was already added"},
		{"an item at a negative index", []string{created, event("response.output_item.added", `"output_index":-1,"item":`+msgAdded)}, "output -1 added"},
		{"an item under an id added", []string{created, added(msgAdded), added(msgAdded)}, `item "msg_1" was already added`},
		{"an item to skip below an item added", []string{created,
			event("response.output_item.added", `"output_index":1,"item":`+msgAdded),
			event("response.output_item.added", `"output_index":0,"item":{"type":"web_search_call","id":"ws_1","status":"in_progress"}`)},
			`output 0 of type "web_search_call", which the format skips, is added after output 1`},
		{"a message added with content", []string{created, added(msgDone)}, `message "msg_1" is added with content`},
		{"a reasoning item added with content", []string{created, added(rsDone)}, `reasoning item "rs_1" is added with content`},
		{"a reasoning item added with content and encrypted_content", []string{created, added(`{"type":"reasoning","id":"rs_1","summary":[],"content":[{"type":"reasoning_text","text":"Need"}],"encrypted_content":"part"}`)}, `reasoning item "rs_1" is added with content`},
		{"a call added with arguments", []string{created, added(fcDone)}, `function call "fc_1" is added with arguments`},
		{"a delta of an item never added", []string{created, delta("response.output_text.delta", "msg_9", "Hi")}, `item "msg_9" wasn't added`},
		{"a delta at an index never added", []string{created, event("response.output_text.delta", `"output_index":3,"item_id":"msg_1","content_index":0,"delta":"Hi"`)}, "output 3 wasn't added"},
		{"an item_id not the output_index's", slices.Concat(msg, []string{event("response.content_part.added", `"output_index":0,"item_id":"msg_2","part":{"type":"output_text","text":""}`)}), `output 0 is item "msg_1", not "msg_2"`},
		{"a delta naming no item", slices.Concat(text, []string{event("response.output_text.delta", `"delta":"Hi"`)}), "names no item"},
		{"a message's delta of no part", slices.Concat(msg, []string{delta("response.output_text.delta", "msg_1", "Hi")}), "output 0 content 0 wasn't added"},
		{"a part out of order", slices.Concat(msg, []string{event("response.content_part.added", `"item_id":"msg_1","content_index":1,"part":{"type":"output_text","text":""}`)}), "output 0 content 1 added, want content 0 next"},
		{"a second part with the first open", slices.Concat(text, []string{part}), "output 0 content 1 added while content 0 is open"},
		{"a reasoning part of a message", slices.Concat(msg, []string{event("response.content_part.added", `"item_id":"msg_1","part":{"type":"reasoning_text","text":""}`)}), `content 0 has type "reasoning_text"`},
		{"a text delta of a reasoning item", []string{created, added(rsAdded), delta("response.output_text.delta", "rs_1", "Hi")}, `content 0 has type "output_text"`},
		{"a refusal delta of a text part", slices.Concat(text, []string{delta("response.refusal.delta", "msg_1", "No.")}), "output 0 content 0 is output_text, not refusal"},
		{"a content part of a call", slices.Concat(call, []string{event("response.content_part.added", `"item_id":"fc_1","part":{"type":"output_text","text":""}`)}), "output 0 is a function call"},
		{"arguments of a message", slices.Concat(text, []string{delta("response.function_call_arguments.delta", "msg_1", "{}")}), "not a function call"},
		{"a text done not what the deltas made", slices.Concat(text, []string{event("response.output_text.done", `"item_id":"msg_1","text":"Hello"`)}), `text "Hello" isn't the text its deltas made, "Hi"`},
		{"a delta after the text's done", slices.Concat(text, []string{event("response.output_text.done", `"item_id":"msg_1","content_index":0,"text":"Hi"`), event("response.output_text.delta", `"item_id":"msg_1","content_index":0,"delta":"!"`)}), "output 0 content 0's text is done"},
		{"a part done not the part streamed", slices.Concat(text, []string{event("response.content_part.done", `"item_id":"msg_1","part":{"type":"output_text","text":"Hello"}`)}), `is done with text "Hello", not the text streamed, "Hi"`},
		{"a part done of another type", slices.Concat(text, []string{event("response.content_part.done", `"item_id":"msg_1","part":{"type":"refusal","refusal":"Hi"}`)}), "is done as refusal, added as output_text"},
		{"a part done twice", slices.Concat(text, []string{event("response.content_part.done", `"item_id":"msg_1","content_index":0,"part":{"type":"output_text","text":"Hi"}`), event("response.content_part.done", `"item_id":"msg_1","content_index":0,"part":{"type":"output_text","text":"Hi"}`)}), "output 0 content 0 is done"},
		{"a part done never added", slices.Concat(msg, []string{event("response.content_part.done", `"item_id":"msg_1","part":{"type":"output_text","text":""}`)}), "output 0 content -1 wasn't added"},
		{"arguments done not what the deltas made", slices.Concat(call, []string{event("response.function_call_arguments.done", `"item_id":"fc_1","arguments":"{\"a\":1}"`)}), `arguments "{\"a\":1}" aren't the arguments their deltas made, "{}"`},
		{"an arguments delta after their done", slices.Concat(call, []string{event("response.function_call_arguments.done", `"item_id":"fc_1","arguments":"{}"`), delta("response.function_call_arguments.delta", "fc_1", " ")}), "output 0's arguments are done"},
		{"an item done of another type", slices.Concat(text, []string{itemDone(`{"type":"function_call","id":"msg_1","call_id":"c","name":"n","arguments":"Hi"}`)}), "is done where a message was added"},
		{"an item done not the parts streamed", slices.Concat(text, []string{itemDone(msgDone)}), `content 0 protocol.OutputText{Text:"Checking."} isn't the part streamed`},
		{"an item done of more parts than streamed", []string{created, added(rsAdded), itemDone(rsDone)}, "the item has 1 content parts, want the 0 streamed"},
		{"a call done not the arguments streamed", slices.Concat(call, []string{itemDone(fcDone)}), `arguments "{\"city\":\"Paris\"}" aren't the arguments streamed, "{}"`},
		{"a call done of another name", slices.Concat(call, []string{itemDone(`{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_time","arguments":"{}"}`)}), "call \"call_1\" of get_time, is done where"},
		{"an item done of a part its item doesn't take", slices.Concat(text, []string{itemDone(`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"reasoning_text","text":"Hi"}]}`)}), `message content 0 has type "reasoning_text", which a message doesn't take`},
		{"an item done of a skipped part in place of the part streamed", slices.Concat(text, []string{itemDone(`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"input_text","text":"Hi"}]}`)}), "the item has 0 content parts, want the 1 streamed"},
		{"an item done of a type the format skips", slices.Concat(text, []string{itemDone(`{"type":"web_search_call","id":"msg_1","status":"completed"}`)}), `item of type "web_search_call" is done where a protocol.Message was added`},
		{"an event of an item done", slices.Concat(text, []string{itemDone(`{"type":"message","id":"msg_1","role":"assistant","content":[{"type":"output_text","text":"Hi"}]}`), delta("response.output_text.delta", "msg_1", "!")}), "output 0 is done"},
		{"response.completed with an item open", slices.Concat(text, []string{completedEmpty}), "response.completed: output 0 isn't done"},
		{"response.completed with an index skipped", []string{created, event("response.output_item.added", `"output_index":1,"item":`+fcAdded), event("response.function_call_arguments.done", `"output_index":1,"item_id":"fc_1","arguments":""`),
			event("response.output_item.done", `"output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"get_weather","arguments":""}`), completedEmpty}, "response.completed: output 0 wasn't added"},
		{"response.completed of other output", slices.Concat(routerOpen, routerClose[:len(routerClose)-1], []string{terminal("response.completed", "completed", usageJSON, rsDone, msgDone)}), "the response has 2 output items, want the 3 streamed"},
		{"response.completed of other items", slices.Concat(routerOpen, routerClose[:len(routerClose)-1], []string{terminal("response.completed", "completed", usageJSON, msgDone, rsDone, fcDone)}), `the response's output 0 is item "msg_1", not "rs_1"`},
		{"response.completed of another status", []string{created, terminal("response.completed", "incomplete", usageJSON)}, "response.completed's response has status incomplete"},
		{"response.completed of a response in progress", []string{created, terminal("response.completed", "in_progress", usageJSON)}, `response has status "in_progress"`},
		{"a terminal event of no response", []string{created, event("response.completed", "")}, "response.completed has no response"},
		{"an event after the terminal event", []string{created, completedEmpty, inProgress}, "event after the end of the stream"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decoder := responses.New(responses.Options{}).NewStreamDecoder()
			var err error
			for _, data := range tc.events {
				if _, err = decoder.Decode(format.WireEvent{Data: []byte(data)}); err != nil {
					break
				}
			}
			if err == nil || !strings.HasPrefix(err.Error(), "responses: stream: ") || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Decode error = %v, want one containing %q", err, tc.want)
			}
			if _, err := decoder.Decode(format.WireEvent{Data: []byte(inProgress)}); err == nil {
				t.Error("Decode after an error = nil, want the stream broken")
			}
		})
	}
}

func TestStreamDecodeErrorIsNoEndOfStream(t *testing.T) {
	_, err := responses.New(responses.Options{}).NewStreamDecoder().Decode(format.WireEvent{Data: []byte(`{"type":`)})
	if err == nil || !strings.Contains(err.Error(), "decode event") || errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("Decode error = %v, want a decode error that doesn't match io.ErrUnexpectedEOF", err)
	}
}
