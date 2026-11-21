package integration_test

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/standards-lab/go-ai"
	"github.com/standards-lab/go-ai/format"
	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/protocol"
)

type profile struct {
	Completions completions.Options `json:"completions,omitzero,case:strict"`
	Messages    messages.Options    `json:"messages,omitzero,case:strict"`
	Responses   responses.Options   `json:"responses,omitzero,case:strict"`
}

func profileVariable(baseVariable string) string {
	return strings.TrimSuffix(baseVariable, "_BASE_URL") + "_PROFILE"
}

func readProfile(baseVariable string, getenv func(string) string) (profile, error) {
	var p profile
	variable := profileVariable(baseVariable)
	v := getenv(variable)
	if v == "" {
		return p, nil
	}
	if err := json.Unmarshal([]byte(v), &p, json.RejectUnknownMembers(true)); err != nil {
		return profile{}, fmt.Errorf("%s: %w", variable, err)
	}
	return p, nil
}

// See docs/design.md, "Implementation notes".
type skipDeclared struct {
	format.Format
	t        testing.TB
	variable string
}

func (f skipDeclared) EncodeRequest(req *protocol.Request, header http.Header) ([]byte, error) {
	body, err := f.Format.EncodeRequest(req, header)
	f.skipIfDeclared(err)
	return body, err
}

func (f skipDeclared) EncodeStreamRequest(req *protocol.Request, header http.Header) ([]byte, error) {
	body, err := f.Format.EncodeStreamRequest(req, header)
	f.skipIfDeclared(err)
	return body, err
}

func (f skipDeclared) skipIfDeclared(err error) {
	f.t.Helper()
	u, ok := errors.AsType[*format.UnsupportedError](err)
	if !ok || !u.Declared {
		return
	}
	gap := u.Feature
	if u.Value != "" {
		gap += " " + u.Value
	}
	f.t.Skipf("%s declares the gap %q of the %s format, so the request is refused before sending: %v",
		f.variable, gap, u.Format, err)
}

func TestReadProfile(t *testing.T) {
	for _, tc := range []struct {
		name string
		vars map[string]string
		want profile
	}{
		{"unset", map[string]string{"AI_VISION_PROFILE": `{"messages":{"refused_efforts":["max"]}}`}, profile{}},
		{"empty", map[string]string{"AI_AGENT_PROFILE": ""}, profile{}},
		{"empty object", map[string]string{"AI_AGENT_PROFILE": "{}"}, profile{}},
		{
			name: "one member",
			vars: map[string]string{"AI_AGENT_PROFILE": `{"responses":{"resend_reasoning_text":true}}`},
			want: profile{Responses: responses.Options{ResendReasoningText: true}},
		},
		{
			name: "every member",
			vars: map[string]string{"AI_AGENT_PROFILE": `{"completions":{"refused_efforts":["none","max"],"no_allowed_tools":true},` +
				`"responses":{"refused_efforts":["xhigh"],"no_allowed_tools":true,"resend_reasoning_text":true},` +
				`"messages":{"refused_efforts":["none"]}}`},
			want: profile{
				Completions: completions.Options{
					RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortNone, protocol.ReasoningEffortMax},
					NoAllowedTools: true,
				},
				Responses: responses.Options{
					RefusedEfforts:      []protocol.ReasoningEffort{protocol.ReasoningEffortXHigh},
					NoAllowedTools:      true,
					ResendReasoningText: true,
				},
				Messages: messages.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortNone}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readProfile("AI_AGENT_BASE_URL", env(tc.vars))
			if err != nil {
				t.Fatalf("readProfile: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("readProfile = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestReadProfile_Malformed(t *testing.T) {
	for _, tc := range []struct{ name, v string }{
		{"not JSON", `{"completions":`},
		{"unknown format", `{"chat":{}}`},
		{"unknown field", `{"messages":{"no_allowed_tools":true}}`},
		{"Go field name", `{"responses":{"ResendReasoningText":true}}`},
		{"unknown effort", `{"completions":{"refused_efforts":["extreme"]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readProfile("AI_AGENT_BASE_URL", env(map[string]string{"AI_AGENT_PROFILE": tc.v}))
			if err == nil {
				t.Fatal("readProfile succeeded, want it refused")
			}
			if !strings.HasPrefix(err.Error(), "AI_AGENT_PROFILE: ") {
				t.Errorf("err = %q, want it to name AI_AGENT_PROFILE first", err)
			}
		})
	}
}

type skipRecorder struct {
	testing.TB
	skipped string
}

func (r *skipRecorder) Skipf(format string, args ...any) {
	r.skipped = fmt.Sprintf(format, args...)
}

func effortRequest(e protocol.ReasoningEffort) *protocol.Request {
	return &protocol.Request{
		Model:           "m",
		Input:           []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: "hi"}}}},
		Reasoning:       protocol.ReasoningParam{Effort: e},
		MaxOutputTokens: 64,
	}
}

func TestSkipDeclared(t *testing.T) {
	refusesNone := responses.New(responses.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortNone}})
	for _, tc := range []struct {
		name        string
		effort      protocol.ReasoningEffort
		skip        []string
		specRefusal bool
	}{
		{"declared", protocol.ReasoningEffortNone, []string{"AI_AGENT_PROFILE", "the responses format", `"reasoning effort none"`}, false},
		{"the spec's", protocol.ReasoningEffortMinimal, nil, true},
		{"sent", protocol.ReasoningEffortLow, nil, false},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", tc.name, stream), func(t *testing.T) {
				rec := &skipRecorder{TB: t}
				f := skipDeclared{Format: refusesNone, t: rec, variable: "AI_AGENT_PROFILE"}
				encode := f.EncodeRequest
				if stream {
					encode = f.EncodeStreamRequest
				}

				_, err := encode(effortRequest(tc.effort), make(http.Header))

				if tc.skip != nil {
					for _, want := range tc.skip {
						if !strings.Contains(rec.skipped, want) {
							t.Errorf("skipped %q, want it to name %s", rec.skipped, want)
						}
					}
					return
				}
				if rec.skipped != "" {
					t.Errorf("skipped %q, want no skip", rec.skipped)
				}
				if tc.specRefusal {
					if u, ok := errors.AsType[*format.UnsupportedError](err); !ok || u.Declared {
						t.Errorf("err = %v, want the spec's refusal returned", err)
					}
				} else if err != nil {
					t.Errorf("err = %v, want the request encoded", err)
				}
			})
		}
	}
}

func TestSkipDeclared_ThroughClient(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected request", http.StatusTeapot)
	}))
	defer srv.Close()
	refusesNone := completions.New(completions.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortNone}})
	for name, send := range map[string]func(t *testing.T, c *ai.Client){
		"Generate": func(t *testing.T, c *ai.Client) {
			_, err := c.Generate(t.Context(), effortRequest(protocol.ReasoningEffortNone))
			t.Fatalf("Generate returned %v, want the test skipped", err)
		},
		"GenerateStream": func(t *testing.T, c *ai.Client) {
			for _, err := range c.GenerateStream(t.Context(), effortRequest(protocol.ReasoningEffortNone)) {
				t.Fatalf("GenerateStream yielded %v, want the test skipped", err)
			}
			t.Fatal("GenerateStream ended, want the test skipped")
		},
	} {
		var scenario *testing.T
		t.Run(name, func(t *testing.T) {
			scenario = t
			wire := skipDeclared{Format: refusesNone, t: t, variable: "AI_AGENT_PROFILE"}
			c, err := newClient("AI_AGENT_BASE_URL", srv.URL+"/v1", wire, ai.Options{}, env(nil))
			if err != nil {
				t.Fatalf("newClient: %v", err)
			}
			send(t, c)
		})
		if !scenario.Skipped() {
			t.Errorf("%s: the scenario wasn't skipped", name)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("the endpoint got %d requests, want none", n)
	}
}
