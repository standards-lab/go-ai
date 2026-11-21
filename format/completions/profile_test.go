package completions_test

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

const (
	getWeatherWire = `{"type":"function","function":{"name":"get_weather","description":"Get the current weather in a city.",
		"parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]},"strict":true}}`
	getTimeWire = `{"type":"function","function":{"name":"get_time"}}`
)

func encodings(opts completions.Options) map[string]func(*protocol.Request, http.Header) ([]byte, error) {
	f := completions.New(opts)
	return map[string]func(*protocol.Request, http.Header) ([]byte, error){
		"EncodeRequest":       f.EncodeRequest,
		"EncodeStreamRequest": f.EncodeStreamRequest,
	}
}

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

func TestProfileSends(t *testing.T) {
	allowed := func(mode protocol.ToolChoiceMode, names ...string) protocol.ToolChoice {
		c := protocol.AllowedToolChoice{Mode: mode}
		for _, n := range names {
			c.Tools = append(c.Tools, protocol.FunctionToolChoice{Name: n})
		}
		return c
	}
	cases := []struct {
		name        string
		opts        completions.Options
		req         *protocol.Request
		unvalidated bool
		want        string
	}{
		{"allowed_tools narrowed to its tool, mode auto", completions.Options{NoAllowedTools: true},
			withTools(allowed(protocol.ToolChoiceAuto, "get_time")), false,
			`{"tools":[` + getTimeWire + `],"tool_choice":"auto"}`},
		{"allowed_tools narrowed to its tool, mode required", completions.Options{NoAllowedTools: true},
			withTools(allowed(protocol.ToolChoiceRequired, "get_time")), false,
			`{"tools":[` + getTimeWire + `],"tool_choice":"required"}`},
		{"allowed_tools narrowed in the request's order", completions.Options{NoAllowedTools: true},
			withTools(allowed(protocol.ToolChoiceAuto, "get_time", "get_weather")), false,
			`{"tools":[` + getWeatherWire + `,` + getTimeWire + `],"tool_choice":"auto"}`},
		{"allowed tool the request doesn't carry sends nothing", completions.Options{NoAllowedTools: true},
			withTools(allowed(protocol.ToolChoiceAuto, "get_date", "get_time")), true,
			`{"tools":[` + getTimeWire + `],"tool_choice":"auto"}`},
		{"allowed_tools of mode auto narrowed under NoToolChoiceRequired",
			completions.Options{NoAllowedTools: true, NoToolChoiceRequired: true},
			withTools(allowed(protocol.ToolChoiceAuto, "get_weather")), false,
			`{"tools":[` + getWeatherWire + `],"tool_choice":"auto"}`},
		{"allowed_tools sent whole under NoToolChoiceRequired alone", completions.Options{NoToolChoiceRequired: true},
			withTools(allowed(protocol.ToolChoiceAuto, "get_time")), false,
			`{` + toolsWire + `,"tool_choice":{"type":"allowed_tools","allowed_tools":{"mode":"auto","tools":[` + getTimeWire + `]}}}`},
		{"tool choice of another mode under NoToolChoiceRequired", completions.Options{NoToolChoiceRequired: true},
			withTools(protocol.ToolChoiceAuto), false,
			`{` + toolsWire + `,"tool_choice":"auto"}`},
		{"named tool choice under every field",
			completions.Options{NoAllowedTools: true, NoToolChoiceRequired: true},
			withTools(protocol.FunctionToolChoice{Name: "get_time"}), false,
			`{` + toolsWire + `,"tool_choice":{"type":"function","function":{"name":"get_time"}}}`},
		{"tool choice required under NoAllowedTools alone", completions.Options{NoAllowedTools: true},
			withTools(protocol.ToolChoiceRequired), false,
			`{` + toolsWire + `,"tool_choice":"required"}`},
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

func TestProfileSendsUnlistedEffort(t *testing.T) {
	refused := []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortXHigh}
	opts := completions.Options{RefusedEfforts: refused}
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
					ReasoningEffort *string `json:"reasoning_effort"`
				}
				if err := json.Unmarshal(body, &got); err != nil {
					t.Fatal(err)
				}
				switch {
				case effort == "" && got.ReasoningEffort != nil:
					t.Errorf("reasoning_effort = %q, want none sent", *got.ReasoningEffort)
				case effort != "" && (got.ReasoningEffort == nil || *got.ReasoningEffort != string(effort)):
					t.Errorf("reasoning_effort = %v, want %q", got.ReasoningEffort, effort)
				}
			})
		}
	}
}

func TestProfileRefuses(t *testing.T) {
	everyField := completions.Options{
		RefusedEfforts:       []protocol.ReasoningEffort{protocol.ReasoningEffortMax},
		NoToolChoiceRequired: true,
		NoAllowedTools:       true,
	}
	modeNone := protocol.AllowedToolChoice{Tools: []protocol.FunctionToolChoice{{Name: "get_time"}}, Mode: protocol.ToolChoiceNone}
	modeRequired := protocol.AllowedToolChoice{Tools: []protocol.FunctionToolChoice{{Name: "get_time"}}, Mode: protocol.ToolChoiceRequired}
	cases := []struct {
		name    string
		opts    completions.Options
		req     *protocol.Request
		want    format.UnsupportedError
		message string
	}{
		{"refused effort", completions.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortMinimal, protocol.ReasoningEffortXHigh}},
			&protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
				Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortXHigh}},
			format.UnsupportedError{Format: "completions", Feature: "reasoning effort", Value: "xhigh", Declared: true},
			"completions: reasoning effort xhigh is refused by the deployment, as its profile declares"},
		{"refused effort ahead of a refused tool choice", everyField,
			func() *protocol.Request {
				r := withTools(protocol.ToolChoiceRequired)
				r.Reasoning.Effort = protocol.ReasoningEffortMax
				return r
			}(),
			format.UnsupportedError{Format: "completions", Feature: "reasoning effort", Value: "max", Declared: true},
			"completions: reasoning effort max is refused by the deployment, as its profile declares"},
		{"tool_choice required", completions.Options{NoToolChoiceRequired: true}, withTools(protocol.ToolChoiceRequired),
			format.UnsupportedError{Format: "completions", Feature: "tool choice", Value: "required", Declared: true},
			"completions: tool choice: tool_choice required is refused by the deployment, as its profile declares"},
		{"allowed_tools of mode required", completions.Options{NoToolChoiceRequired: true}, withTools(modeRequired),
			format.UnsupportedError{Format: "completions", Feature: "tool choice", Value: "required", Declared: true},
			"completions: tool choice: allowed_tools of mode required is refused by the deployment, as its profile declares"},
		{"allowed_tools of mode required, narrowed", everyField, withTools(modeRequired),
			format.UnsupportedError{Format: "completions", Feature: "tool choice", Value: "required", Declared: true},
			"completions: tool choice: tool_choice required is refused by the deployment, as its profile declares"},
		{"allowed_tools of mode none under every field", everyField, withTools(modeNone),
			format.UnsupportedError{Format: "completions", Feature: "allowed_tools mode", Value: "none"},
			"completions: tool choice: allowed_tools of mode none has no Chat Completions form"},
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

func TestNewCopiesRefusedEfforts(t *testing.T) {
	efforts := []protocol.ReasoningEffort{protocol.ReasoningEffortXHigh}
	f := completions.New(completions.Options{RefusedEfforts: efforts})
	efforts[0] = protocol.ReasoningEffortLow
	req := &protocol.Request{Model: "m", Input: []protocol.Item{text(protocol.RoleUser, "hi")},
		Reasoning: protocol.ReasoningParam{Effort: protocol.ReasoningEffortXHigh}}
	if body, err := f.EncodeRequest(req, make(http.Header)); err == nil {
		t.Errorf("EncodeRequest = %s, nil; want xhigh refused", body)
	}
}

var strict = json.RejectUnknownMembers(true)

func TestOptionsDecode(t *testing.T) {
	var got completions.Options
	in := `{"refused_efforts":["none","minimal","xhigh","max"],"no_tool_choice_required":true,"no_allowed_tools":true}`
	if err := json.Unmarshal([]byte(in), &got, strict); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := completions.Options{
		RefusedEfforts: []protocol.ReasoningEffort{
			protocol.ReasoningEffortNone, protocol.ReasoningEffortMinimal,
			protocol.ReasoningEffortXHigh, protocol.ReasoningEffortMax,
		},
		NoToolChoiceRequired: true,
		NoAllowedTools:       true,
	}
	if !slices.Equal(got.RefusedEfforts, want.RefusedEfforts) ||
		got.NoToolChoiceRequired != want.NoToolChoiceRequired || got.NoAllowedTools != want.NoAllowedTools {
		t.Errorf("Options = %+v, want %+v", got, want)
	}
}

func TestOptionsDecodeRefuses(t *testing.T) {
	cases := []struct {
		name string
		in   string
		opts []json.Options
		want string
	}{
		{"unknown member", `{"no_parallel_tool_calls":true}`, nil, "no_parallel_tool_calls"},
		{"Go field name", `{"NoAllowedTools":true}`, nil, "NoAllowedTools"},
		{"Go field name, case-insensitive decoder", `{"RefusedEfforts":["max"]}`,
			[]json.Options{json.MatchCaseInsensitiveNames(true)}, "RefusedEfforts"},
		{"another case", `{"No_Tool_Choice_Required":true}`, nil, "No_Tool_Choice_Required"},
		{"another case, case-insensitive decoder", `{"No_Tool_Choice_Required":true}`,
			[]json.Options{json.MatchCaseInsensitiveNames(true)}, "No_Tool_Choice_Required"},
		{"mistyped effort", `{"refused_efforts":["xhgih"]}`, nil, `unknown reasoning effort "xhgih"`},
		{"empty effort", `{"refused_efforts":[""]}`, nil, `unknown reasoning effort ""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got completions.Options
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

func TestOptionsEncode(t *testing.T) {
	cases := []struct {
		name string
		opts completions.Options
		want string
	}{
		{"zero", completions.Options{}, `{}`},
		{"every field", completions.Options{
			RefusedEfforts:       []protocol.ReasoningEffort{protocol.ReasoningEffortNone},
			NoToolChoiceRequired: true,
			NoAllowedTools:       true,
		}, `{"refused_efforts":["none"],"no_tool_choice_required":true,"no_allowed_tools":true}`},
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
