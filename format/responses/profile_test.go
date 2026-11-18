package responses_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

// The tests here hold the format to its Options, the profile of the deployment it targets: what
// each field refuses, narrows, and leaves as the spec's form, and how Options decodes from JSON.

// timeTool is a function tool of a name alone, on the wire as timeToolJSON.
var timeTool = protocol.FunctionTool{Name: "get_time"}

const timeToolJSON = `{"type":"function","name":"get_time","strict":false}`

// encodings are the format's two encodings of a request under opts, unstreamed and streamed.
func encodings(opts responses.Options) map[string]func(*protocol.Request, http.Header) ([]byte, error) {
	f := responses.New(opts)
	return map[string]func(*protocol.Request, http.Header) ([]byte, error){
		"EncodeRequest":       f.EncodeRequest,
		"EncodeStreamRequest": f.EncodeStreamRequest,
	}
}

// toolMembers returns body's tools and tool_choice members alone, as a JSON object, so a test can
// compare them across both encodings, whose bodies differ in their stream members.
func toolMembers(t *testing.T, body []byte) []byte {
	t.Helper()
	var m struct {
		Tools      jsontext.Value `json:"tools,omitzero"`
		ToolChoice jsontext.Value `json:"tool_choice,omitzero"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body %s: %v", body, err)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// allowed is an allowed_tools choice of mode and the tools of names.
func allowed(mode protocol.ToolChoiceMode, names ...string) protocol.AllowedToolChoice {
	c := protocol.AllowedToolChoice{Mode: mode}
	for _, n := range names {
		c.Tools = append(c.Tools, protocol.FunctionToolChoice{Name: n})
	}
	return c
}

// TestProfileSends wants each request a profile lets through sent as the profile has it: the
// spec's form where it declares nothing of the request, and an allowed_tools choice narrowed under
// NoAllowedTools, whatever its mode.
func TestProfileSends(t *testing.T) {
	narrowing := responses.Options{NoAllowedTools: true}
	cases := []struct {
		name string
		opts responses.Options
		req  *protocol.Request
		// unvalidated is set for a request Validate refuses, which only a direct caller of the
		// format can give it.
		unvalidated bool
		want        string
	}{
		{"allowed_tools narrowed to its tool, mode auto", narrowing,
			withTools(allowed(protocol.ToolChoiceAuto, "get_time"), weatherTool, timeTool), false,
			`{"tools":[` + timeToolJSON + `],"tool_choice":"auto"}`},
		{"allowed_tools narrowed to its tool, mode required", narrowing,
			withTools(allowed(protocol.ToolChoiceRequired, "get_time"), weatherTool, timeTool), false,
			`{"tools":[` + timeToolJSON + `],"tool_choice":"required"}`},
		{"allowed_tools narrowed to its tool, mode none", narrowing,
			withTools(allowed(protocol.ToolChoiceNone, "get_time"), weatherTool, timeTool), false,
			`{"tools":[` + timeToolJSON + `],"tool_choice":"none"}`},
		{"allowed_tools narrowed in the request's order", narrowing,
			withTools(allowed(protocol.ToolChoiceAuto, "get_time", "get_weather"), weatherTool, timeTool), false,
			`{"tools":[` + weatherToolJSON + `,` + timeToolJSON + `],"tool_choice":"auto"}`},
		{"allowed tool the request doesn't carry sends nothing", narrowing,
			withTools(allowed(protocol.ToolChoiceAuto, "get_date", "get_time"), weatherTool, timeTool), true,
			`{"tools":[` + timeToolJSON + `],"tool_choice":"auto"}`},
		{"allowed_tools sent whole under the zero profile", responses.Options{},
			withTools(allowed(protocol.ToolChoiceRequired, "get_time"), weatherTool, timeTool), false,
			`{"tools":[` + weatherToolJSON + `,` + timeToolJSON + `],
				"tool_choice":{"type":"allowed_tools","tools":[{"type":"function","name":"get_time"}],"mode":"required"}}`},
		{"named tool choice under NoAllowedTools", narrowing,
			withTools(protocol.FunctionToolChoice{Name: "get_time"}, weatherTool, timeTool), false,
			`{"tools":[` + weatherToolJSON + `,` + timeToolJSON + `],"tool_choice":{"type":"function","name":"get_time"}}`},
		{"tool choice required under NoAllowedTools", narrowing,
			withTools(protocol.ToolChoiceRequired, weatherTool, timeTool), false,
			`{"tools":[` + weatherToolJSON + `,` + timeToolJSON + `],"tool_choice":"required"}`},
	}
	for _, tc := range cases {
		for name, encode := range encodings(tc.opts) {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				if err := tc.req.Validate(); (err != nil) != tc.unvalidated {
					t.Fatalf("Validate = %v, want an error %t", err, tc.unvalidated)
				}
				body, err := encode(tc.req, make(http.Header))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				aitest.AssertJSON(t, toolMembers(t, body), tc.want)
			})
		}
	}
}

// TestProfileSendsUnlistedEffort wants every effort RefusedEfforts doesn't list sent by its name,
// and a request of no effort sent with none, whatever the set.
func TestProfileSendsUnlistedEffort(t *testing.T) {
	refused := []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortXHigh}
	opts := responses.Options{RefusedEfforts: refused}
	for _, effort := range []protocol.ReasoningEffort{"", protocol.ReasoningEffortLow, protocol.ReasoningEffortHigh} {
		for name, encode := range encodings(opts) {
			t.Run(name+"/"+string(effort), func(t *testing.T) {
				req := &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
					Reasoning: protocol.ReasoningParam{Effort: effort}}
				body, err := encode(req, make(http.Header))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				var got struct {
					Reasoning *struct {
						Effort string `json:"effort"`
					} `json:"reasoning"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatal(err)
				}
				switch {
				case effort == "" && got.Reasoning != nil:
					t.Errorf("reasoning = %+v, want none sent", *got.Reasoning)
				case effort != "" && (got.Reasoning == nil || got.Reasoning.Effort != string(effort)):
					t.Errorf("reasoning = %+v, want effort %q", got.Reasoning, effort)
				}
			})
		}
	}
}

// TestProfileRefuses wants each request a profile declares a gap refused with a declared
// *format.UnsupportedError, ahead of the format's other refusals, and an effort the spec lacks
// refused as the spec's gap though the profile lists it.
func TestProfileRefuses(t *testing.T) {
	every := responses.Options{
		RefusedEfforts: []protocol.ReasoningEffort{
			protocol.ReasoningEffortNone, protocol.ReasoningEffortMinimal,
			protocol.ReasoningEffortXHigh, protocol.ReasoningEffortMax,
		},
		NoAllowedTools:      true,
		ResendReasoningText: true,
	}
	effort := func(e protocol.ReasoningEffort) *protocol.Request {
		return &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
			Reasoning: protocol.ReasoningParam{Effort: e}}
	}
	cases := []struct {
		name    string
		opts    responses.Options
		req     *protocol.Request
		want    format.UnsupportedError
		message string
	}{
		{"refused effort", responses.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortXHigh}},
			effort(protocol.ReasoningEffortXHigh),
			format.UnsupportedError{Format: "responses", Feature: "reasoning effort", Value: "xhigh", Declared: true},
			"responses: reasoning effort xhigh is refused by the deployment, as its profile declares"},
		{"refused effort none", every, effort(protocol.ReasoningEffortNone),
			format.UnsupportedError{Format: "responses", Feature: "reasoning effort", Value: "none", Declared: true},
			"responses: reasoning effort none is refused by the deployment, as its profile declares"},
		{"refused effort ahead of a stateful native member", every,
			withNative(effort(protocol.ReasoningEffortXHigh), `{"previous_response_id":"resp_1"}`),
			format.UnsupportedError{Format: "responses", Feature: "reasoning effort", Value: "xhigh", Declared: true},
			"responses: reasoning effort xhigh is refused by the deployment, as its profile declares"},
		{"listed effort minimal stays the spec's gap", every, effort(protocol.ReasoningEffortMinimal),
			format.UnsupportedError{Format: "responses", Feature: "reasoning effort", Value: "minimal"},
			"responses: reasoning effort minimal has no Open Responses form"},
		{"listed effort max stays the spec's gap", every, effort(protocol.ReasoningEffortMax),
			format.UnsupportedError{Format: "responses", Feature: "reasoning effort", Value: "max"},
			"responses: reasoning effort max has no Open Responses form"},
	}
	for _, tc := range cases {
		for name, encode := range encodings(tc.opts) {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				if err := tc.req.Validate(); err != nil {
					t.Fatalf("Validate: %v", err)
				}
				body, err := encode(tc.req, make(http.Header))
				if err == nil {
					t.Fatalf("%s = %s, nil; want a refusal", name, body)
				}
				aitest.AssertUnsupported(t, err, tc.want, tc.message)
			})
		}
	}
}

// TestNewCopiesRefusedEfforts wants a change to the caller's RefusedEfforts after New to leave
// the format's profile as it was.
func TestNewCopiesRefusedEfforts(t *testing.T) {
	efforts := []protocol.ReasoningEffort{protocol.ReasoningEffortXHigh}
	f := responses.New(responses.Options{RefusedEfforts: efforts})
	efforts[0] = protocol.ReasoningEffortLow
	req := &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
		Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortXHigh}}
	if body, err := f.EncodeRequest(req, make(http.Header)); err == nil {
		t.Errorf("EncodeRequest = %s, nil; want xhigh refused", body)
	}
}

// strict is the decoding a profile's loader would use: an unknown member is an error.
var strict = json.RejectUnknownMembers(true)

func TestOptionsDecode(t *testing.T) {
	var got responses.Options
	in := `{"refused_efforts":["none","xhigh"],"no_allowed_tools":true,"resend_reasoning_text":true}`
	if err := json.Unmarshal([]byte(in), &got, strict); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := responses.Options{
		RefusedEfforts:      []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortXHigh},
		NoAllowedTools:      true,
		ResendReasoningText: true,
	}
	if !slices.Equal(got.RefusedEfforts, want.RefusedEfforts) ||
		got.NoAllowedTools != want.NoAllowedTools || got.ResendReasoningText != want.ResendReasoningText {
		t.Errorf("Options = %+v, want %+v", got, want)
	}
}

// TestOptionsDecodeRefuses wants strict decoding to refuse an unknown member, completions'
// no_tool_choice_required among them, a member spelled as the Go field or in another case, even
// where the decoder matches names case-insensitively, and an effort the protocol doesn't define.
func TestOptionsDecodeRefuses(t *testing.T) {
	cases := []struct {
		name string
		in   string
		opts []json.Options
		want string
	}{
		{"unknown member", `{"no_parallel_tool_calls":true}`, nil, "no_parallel_tool_calls"},
		{"another format's member", `{"no_tool_choice_required":true}`, nil, "no_tool_choice_required"},
		{"Go field name", `{"ResendReasoningText":true}`, nil, "ResendReasoningText"},
		{"Go field name, case-insensitive decoder", `{"ResendReasoningText":true}`,
			[]json.Options{json.MatchCaseInsensitiveNames(true)}, "ResendReasoningText"},
		{"another case", `{"No_Allowed_Tools":true}`, nil, "No_Allowed_Tools"},
		{"another case, case-insensitive decoder", `{"No_Allowed_Tools":true}`,
			[]json.Options{json.MatchCaseInsensitiveNames(true)}, "No_Allowed_Tools"},
		{"mistyped effort", `{"refused_efforts":["xhgih"]}`, nil, `unknown reasoning effort "xhgih"`},
		{"empty effort", `{"refused_efforts":[""]}`, nil, `unknown reasoning effort ""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got responses.Options
			err := json.Unmarshal([]byte(tc.in), &got, append([]json.Options{strict}, tc.opts...)...)
			if err == nil {
				t.Fatalf("Unmarshal(%s) = %+v, nil; want an error", tc.in, got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Unmarshal error = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

// TestOptionsEncode wants Options encoded under its JSON names, each zero field omitted, so the
// zero profile encodes as an empty object.
func TestOptionsEncode(t *testing.T) {
	cases := []struct {
		name string
		opts responses.Options
		want string
	}{
		{"zero", responses.Options{}, `{}`},
		{"every field", responses.Options{
			RefusedEfforts:      []protocol.ReasoningEffort{protocol.ReasoningEffortNone},
			NoAllowedTools:      true,
			ResendReasoningText: true,
		}, `{"refused_efforts":["none"],"no_allowed_tools":true,"resend_reasoning_text":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			aitest.AssertJSON(t, b, tc.want)
		})
	}
}
