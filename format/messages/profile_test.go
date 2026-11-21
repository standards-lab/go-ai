package messages_test

import (
	"encoding/json/v2"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/internal/aitest"
	"github.com/standards-lab/go-ai/protocol"
)

func encodings(opts messages.Options) map[string]func(*protocol.Request, http.Header) ([]byte, error) {
	f := messages.New(opts)
	return map[string]func(*protocol.Request, http.Header) ([]byte, error){
		"EncodeRequest":       f.EncodeRequest,
		"EncodeStreamRequest": f.EncodeStreamRequest,
	}
}

func effortRequest(e protocol.ReasoningEffort) *protocol.Request {
	req := ask(text(protocol.RoleUser, "hi"))
	req.Reasoning = protocol.ReasoningParam{Effort: e}
	return req
}

func TestProfileSendsUnlistedEffort(t *testing.T) {
	refused := messages.Options{RefusedEfforts: []protocol.ReasoningEffort{
		protocol.ReasoningEffortNone, protocol.ReasoningEffortXHigh, protocol.ReasoningEffortMax,
	}}
	cases := []struct {
		name   string
		opts   messages.Options
		effort protocol.ReasoningEffort
		want   string
	}{
		{"no effort", refused, "", `{}`},
		{"low", refused, protocol.ReasoningEffortLow,
			`{"thinking":{"type":"adaptive"},"output_config":{"effort":"low"}}`},
		{"high", refused, protocol.ReasoningEffortHigh,
			`{"thinking":{"type":"adaptive"},"output_config":{"effort":"high"}}`},
		{"none under the zero profile", messages.Options{}, protocol.ReasoningEffortNone,
			`{"thinking":{"type":"disabled"}}`},
		{"xhigh under the zero profile", messages.Options{}, protocol.ReasoningEffortXHigh,
			`{"thinking":{"type":"adaptive"},"output_config":{"effort":"xhigh"}}`},
	}
	for _, tc := range cases {
		for name, encode := range encodings(tc.opts) {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				body, err := encode(effortRequest(tc.effort), make(http.Header))
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				aitest.AssertJSON(t, effortMembers(t, body), tc.want)
			})
		}
	}
}

func effortMembers(t *testing.T, body []byte) []byte {
	t.Helper()
	var m struct {
		Thinking     any `json:"thinking,omitzero"`
		OutputConfig any `json:"output_config,omitzero"`
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

func TestProfileRefuses(t *testing.T) {
	every := messages.Options{RefusedEfforts: []protocol.ReasoningEffort{
		protocol.ReasoningEffortNone, protocol.ReasoningEffortMinimal,
		protocol.ReasoningEffortXHigh, protocol.ReasoningEffortMax,
	}}
	noMax := effortRequest(protocol.ReasoningEffortXHigh)
	noMax.MaxOutputTokens = 0
	audio := effortRequest(protocol.ReasoningEffortMax)
	audio.Input = []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{
		protocol.InputAudio{Data: []byte("RIFF"), Format: protocol.AudioFormatWAV},
	}}}
	declared := func(e string) format.UnsupportedError {
		return format.UnsupportedError{Format: "messages", Feature: "reasoning effort", Value: e, Declared: true}
	}
	cases := []struct {
		name    string
		opts    messages.Options
		req     *protocol.Request
		want    format.UnsupportedError
		message string
	}{
		{"refused effort", messages.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortXHigh}},
			effortRequest(protocol.ReasoningEffortXHigh), declared("xhigh"),
			"messages: reasoning effort xhigh is refused by the deployment, as its profile declares"},
		{"refused effort none", every, effortRequest(protocol.ReasoningEffortNone), declared("none"),
			"messages: reasoning effort none is refused by the deployment, as its profile declares"},
		{"refused effort max", every, effortRequest(protocol.ReasoningEffortMax), declared("max"),
			"messages: reasoning effort max is refused by the deployment, as its profile declares"},
		{"refused effort ahead of no max output tokens", every, noMax, declared("xhigh"),
			"messages: reasoning effort xhigh is refused by the deployment, as its profile declares"},
		{"refused effort ahead of audio input", every, audio, declared("max"),
			"messages: reasoning effort max is refused by the deployment, as its profile declares"},
		{"listed effort minimal stays the spec's gap", every, effortRequest(protocol.ReasoningEffortMinimal),
			format.UnsupportedError{Format: "messages", Feature: "reasoning effort", Value: "minimal"},
			"messages: reasoning effort minimal has no Messages form"},
	}
	for _, tc := range cases {
		for name, encode := range encodings(tc.opts) {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				if err := tc.req.Validate(); err != nil {
					t.Fatalf("Validate: %v", err)
				}
				header := make(http.Header)
				body, err := encode(tc.req, header)
				if err == nil {
					t.Fatalf("%s = %s, nil; want a refusal", name, body)
				}
				aitest.AssertUnsupported(t, err, tc.want, tc.message)
				if len(header) != 0 {
					t.Errorf("header = %v, want none set for a refused request", header)
				}
			})
		}
	}
}

func TestNewCopiesRefusedEfforts(t *testing.T) {
	efforts := []protocol.ReasoningEffort{protocol.ReasoningEffortXHigh}
	f := messages.New(messages.Options{RefusedEfforts: efforts})
	efforts[0] = protocol.ReasoningEffortLow
	if body, err := f.EncodeRequest(effortRequest(protocol.ReasoningEffortXHigh), make(http.Header)); err == nil {
		t.Errorf("EncodeRequest = %s, nil; want xhigh refused", body)
	}
}

var strict = json.RejectUnknownMembers(true)

func TestOptionsDecode(t *testing.T) {
	var got messages.Options
	if err := json.Unmarshal([]byte(`{"refused_efforts":["none","xhigh","max"]}`), &got, strict); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortXHigh, protocol.ReasoningEffortMax}
	if !slices.Equal(got.RefusedEfforts, want) {
		t.Errorf("RefusedEfforts = %q, want %q", got.RefusedEfforts, want)
	}
}

func TestOptionsDecodeRefuses(t *testing.T) {
	cases := []struct {
		name string
		in   string
		opts []json.Options
		want string
	}{
		{"unknown member", `{"version":"2023-06-01"}`, nil, "version"},
		{"another format's member", `{"no_allowed_tools":true}`, nil, "no_allowed_tools"},
		{"Go field name", `{"RefusedEfforts":["xhigh"]}`, nil, "RefusedEfforts"},
		{"Go field name, case-insensitive decoder", `{"RefusedEfforts":["xhigh"]}`,
			[]json.Options{json.MatchCaseInsensitiveNames(true)}, "RefusedEfforts"},
		{"another case", `{"Refused_Efforts":["xhigh"]}`, nil, "Refused_Efforts"},
		{"another case, case-insensitive decoder", `{"Refused_Efforts":["xhigh"]}`,
			[]json.Options{json.MatchCaseInsensitiveNames(true)}, "Refused_Efforts"},
		{"mistyped effort", `{"refused_efforts":["xhgih"]}`, nil, `unknown reasoning effort "xhgih"`},
		{"empty effort", `{"refused_efforts":[""]}`, nil, `unknown reasoning effort ""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got messages.Options
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
		opts messages.Options
		want string
	}{
		{"zero", messages.Options{}, `{}`},
		{"every field", messages.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortNone}},
			`{"refused_efforts":["none"]}`},
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
