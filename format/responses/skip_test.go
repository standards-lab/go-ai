package responses_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	searchAdded = `{"type":"web_search_call","id":"ws_1","status":"in_progress"}`
	searchDone  = `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"Paris weather"}}`
	sticker     = `{"type":"acme:sticker","name":"wave"}`
)

func TestDecodeSkips(t *testing.T) {
	msgWith := `{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[` + sticker +
		`,{"type":"output_text","text":"Checking."}]}`
	with, err := decode(`{"id":"resp_1","model":"m-1","status":"completed","output":[` +
		strings.Join([]string{rsDone, searchDone, msgWith, fcDone, searchDone}, ",") + `],` + usageJSON + `}`)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	want, err := decode(`{"id":"resp_1","model":"m-1","status":"completed","output":[` +
		strings.Join([]string{rsDone, msgDone, fcDone}, ",") + `],` + usageJSON + `}`)
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	aitest.AssertResponse(t, with, want)
}

func skipStream(indexed, skipped bool) []string {
	at := func(output, content int) string {
		switch {
		case !indexed:
			return ""
		case content < 0:
			return fmt.Sprintf(`"output_index":%d,`, output)
		}
		return fmt.Sprintf(`"output_index":%d,"content_index":%d,`, output, content)
	}
	msg, call, text := 1, 2, 0
	if skipped {
		msg, call, text = 2, 3, 1
	}
	events := []string{
		created,
		event("response.output_item.added", at(0, -1)+`"item":`+rsAdded),
		event("response.content_part.added", at(0, 0)+`"item_id":"rs_1","part":{"type":"reasoning_text","text":""}`),
		event("response.reasoning.delta", at(0, 0)+`"item_id":"rs_1","delta":"Need the tool."`),
		event("response.reasoning.done", at(0, 0)+`"item_id":"rs_1","text":"Need the tool."`),
		event("response.content_part.done", at(0, 0)+`"item_id":"rs_1","part":{"type":"reasoning_text","text":"Need the tool."}`),
		event("response.output_item.done", at(0, -1)+`"item":`+rsDone),
	}
	if skipped {
		events = append(events,
			event("response.output_item.added", at(1, -1)+`"item":`+searchAdded),
			event("response.content_part.added", at(1, 0)+`"item_id":"ws_1","part":{"type":"output_text","text":""}`),
			event("response.output_text.delta", at(1, 0)+`"item_id":"ws_1","delta":"Searching."`),
			event("response.content_part.done", at(1, 0)+`"item_id":"ws_1","part":{"type":"output_text","text":"Searching."}`),
			event("response.function_call_arguments.delta", at(1, -1)+`"item_id":"ws_1","delta":"{}"`),
			event("response.output_item.done", at(1, -1)+`"item":`+searchDone))
	}
	events = append(events,
		event("response.output_item.added", at(msg, -1)+`"item":`+msgAdded),
		event("response.output_item.added", at(call, -1)+`"item":`+fcAdded))
	if skipped {
		events = append(events,
			event("response.content_part.added", at(msg, 0)+`"item_id":"msg_1","part":`+sticker),
			event("response.output_text.delta", at(msg, 0)+`"item_id":"msg_1","delta":"wave"`),
			event("response.content_part.done", at(msg, 0)+`"item_id":"msg_1","part":`+sticker))
	}
	msgDoneWith := msgDone
	if skipped {
		msgDoneWith = `{"type":"message","id":"msg_1","status":"completed","role":"assistant","content":[` + sticker +
			`,{"type":"output_text","annotations":[],"logprobs":[],"text":"Checking."}]}`
	}
	output := []string{rsDone, msgDoneWith, fcDone}
	if skipped {
		output = []string{rsDone, searchDone, msgDoneWith, fcDone}
	}
	return append(events,
		event("response.content_part.added", at(msg, text)+`"item_id":"msg_1","part":{"type":"output_text","text":""}`),
		event("response.function_call_arguments.delta", at(call, -1)+`"item_id":"fc_1","delta":"{\"city\":\"Paris\"}"`),
		event("response.output_text.delta", at(msg, text)+`"item_id":"msg_1","delta":"Checking."`),
		event("response.output_text.done", at(msg, text)+`"item_id":"msg_1","text":"Checking."`),
		event("response.content_part.done", at(msg, text)+`"item_id":"msg_1","part":{"type":"output_text","text":"Checking."}`),
		event("response.output_item.done", at(msg, -1)+`"item":`+msgDoneWith),
		event("response.function_call_arguments.done", at(call, -1)+`"item_id":"fc_1","arguments":"{\"city\":\"Paris\"}"`),
		event("response.output_item.done", at(call, -1)+`"item":`+fcDone),
		terminal("response.completed", "completed", usageJSON, output...))
}

func TestStreamSkips(t *testing.T) {
	for _, indexed := range []bool{true, false} {
		t.Run(fmt.Sprintf("indexed=%t", indexed), func(t *testing.T) {
			r := decodeStream(t, stream(skipStream(indexed, true)...))
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			without := decodeStream(t, stream(skipStream(indexed, false)...))
			if without.err != nil {
				t.Fatalf("stream error without the skipped: %v", without.err)
			}
			if got, want := trace(r.events), trace(without.events); got != want {
				t.Errorf("events:\n%s\nwant those of the stream without the skipped:\n%s", got, want)
			}
			want, err := decode(`{"id":"resp_1","model":"m-1","status":"completed","output":[` +
				strings.Join([]string{rsDone, msgDone, fcDone}, ",") + `],` + usageJSON + `}`)
			if err != nil {
				t.Fatalf("DecodeResponse: %v", err)
			}
			rs := want.Output[0].(protocol.Reasoning)
			rs.Status = protocol.StatusCompleted
			want.Output[0] = rs
			aitest.AssertResponse(t, aitest.Fold(t, r.events), want)
			aitest.AssertResponse(t, aitest.Fold(t, r.events), terminalResponse(t, stream(skipStream(indexed, true)...)))
			assertDoneItems(t, r.events, want)
		})
	}
}

func TestStreamSkipsInErrorFrame(t *testing.T) {
	events := skipStream(true, true)
	cut := 0
	for i, e := range events {
		if strings.Contains(e, `"delta":"Checking."`) {
			cut = i + 1
		}
	}
	r := decodeStream(t, stream(append(events[:cut:cut], `{"error":{"code":500,"message":"down","type":"server_error"}}`)...))
	if r.err != nil {
		t.Fatalf("stream error: %v", r.err)
	}
	failed, ok := r.events[len(r.events)-1].(protocol.ResponseFailed)
	if !ok {
		t.Fatalf("last event = %#v, want response.failed", r.events[len(r.events)-1])
	}
	want := []protocol.Item{
		protocol.Reasoning{ID: "rs_1", Status: protocol.StatusCompleted, Content: []protocol.ReasoningText{{Text: "Need the tool."}}, EncryptedContent: seal(t, "", "Need the tool.")},
		protocol.Message{ID: "msg_1", Status: protocol.StatusInProgress, Role: protocol.RoleAssistant, Content: []protocol.ContentPart{protocol.OutputText{Text: "Checking."}}},
		protocol.FunctionCall{ID: "fc_1", Status: protocol.StatusInProgress, CallID: "call_1", Name: "get_weather", Arguments: `{"city":"Paris"}`},
	}
	if !reflect.DeepEqual(failed.Response.Output, want) {
		t.Errorf("failed output = %#v\nwant %#v", failed.Response.Output, want)
	}
	aitest.AssertResponse(t, aitest.Fold(t, r.events), failed.Response)
}

func TestStreamIDModelAndCachedTokens(t *testing.T) {
	cases := []struct {
		file             string
		id, createdModel string
		model            string
		cached           int
	}{
		{"text.sse", "resp_aYvSTHsSVs2yfZm0VPGa8FEUaMiecrOM", "", "ggml-org/gpt-oss-120b-GGUF:MXFP4", 60},
		{"tool_call.sse", "resp_A6oBzAY0HnVhHgkH6anQP6XrjNeCjEfP", "", "ggml-org/gpt-oss-120b-GGUF:MXFP4", 56},
		{"vllm/text.sse", "resp_a43daa87edf03692", "nvidia/Gemma-4-26B-A4B-NVFP4", "nvidia/Gemma-4-26B-A4B-NVFP4", 0},
		{"vllm/reasoning.sse", "resp_8b42ec261b53911c", "openai/gpt-oss-120b", "openai/gpt-oss-120b", 64},
		{"vllm/tool_call.sse", "resp_a16c2587aad31d97", "openai/gpt-oss-120b", "openai/gpt-oss-120b", 128},
		{"vllm/length.sse", "resp_9faecf8a33be2c6d", "nvidia/Gemma-4-26B-A4B-NVFP4", "nvidia/Gemma-4-26B-A4B-NVFP4", 0},
		{"foundry/text.sse", "resp_05cbb2e980ddadaa016afc4f6c5ffc819582b24575daffbc29", "gpt-5-mini", "gpt-5-mini", 0},
		{"foundry/reasoning.sse", "resp_0b0d1234b4344875016afc4f6fd5ec8190b9aabf76ccdec416", "gpt-5-mini", "gpt-5-mini", 0},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			r := decodeStream(t, string(aitest.ReadTestdata(t, tc.file)))
			if r.err != nil {
				t.Fatalf("stream error: %v", r.err)
			}
			created, ok := r.events[0].(protocol.ResponseCreated)
			if !ok {
				t.Fatalf("first event = %#v, want response.created", r.events[0])
			}
			if created.Response.ID != tc.id || created.Response.Model != tc.createdModel {
				t.Errorf("response.created ID, Model = %q, %q; want %q, %q", created.Response.ID, created.Response.Model, tc.id, tc.createdModel)
			}
			got := aitest.Fold(t, r.events)
			if got.ID != tc.id || got.Model != tc.model || got.Usage.InputTokensDetails.CachedTokens != tc.cached {
				t.Errorf("folded ID, Model, CachedTokens = %q, %q, %d; want %q, %q, %d",
					got.ID, got.Model, got.Usage.InputTokensDetails.CachedTokens, tc.id, tc.model, tc.cached)
			}
		})
	}
}
