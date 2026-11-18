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

// This file is untagged, so the unit tier runs its tests: the reading of a role's profile and the
// skip of a request it declares a gap of, which every tagged scenario runs through, are checked
// hermetically on every pull request.

// profile is a role's deployment profile, as AI_<ROLE>_PROFILE holds it: a JSON object keyed by
// format name, each member that format's Options under the snake_case, case-strict names their
// tags give them, the shape infer's --profile and INFER_PROFILE take. A member absent is that
// format's zero Options, its spec's form, and so is the whole profile when the variable is unset.
type profile struct {
	Completions completions.Options `json:"completions,omitzero,case:strict"`
	Messages    messages.Options    `json:"messages,omitzero,case:strict"`
	Responses   responses.Options   `json:"responses,omitzero,case:strict"`
}

// profileVariable is the variable that holds the profile of the role whose API root variable is
// baseVariable: AI_<ROLE>_PROFILE, such as AI_AGENT_PROFILE for AI_AGENT_BASE_URL.
func profileVariable(baseVariable string) string {
	return strings.TrimSuffix(baseVariable, "_BASE_URL") + "_PROFILE"
}

// readProfile returns the profile AI_<ROLE>_PROFILE holds, read through getenv, for the role
// whose API root variable is baseVariable, or the zero profile when it is unset or empty. It is
// decoded as infer decodes INFER_PROFILE, with encoding/json/v2 and json.RejectUnknownMembers, so
// an unknown format name or field, a Go field name, or a value a field refuses, such as an effort
// protocol.ReasoningEffort doesn't name, is an error naming the variable ahead of the decoder's.
// A profile is no secret, so the error may show it.
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

// skipDeclared is a format.Format that skips its scenario's test when the format it wraps
// refuses a request as a gap of the role's profile, a *format.UnsupportedError of Declared true:
// the profile says the deployment can't serve that request, so the scenario has nothing to
// assert of it. It is the tier's one skip rule, and every scenario gets it, as endpoint builds
// each format wrapped in one. A refusal of the spec's own, Declared false, is returned as it
// came, so the scenario fails on it. Each refusal happens as the request is encoded, before
// anything is sent, on the goroutine that called Generate, GenerateStream's iterator, or
// Transcribe, which is the test's.
type skipDeclared struct {
	format.Format
	// t is the scenario's test, which a declared refusal skips.
	t testing.TB
	// variable is the role's AI_<ROLE>_PROFILE, which the skip names as the gap's source.
	variable string
}

// EncodeRequest encodes req as the wrapped format does, skipping t on a declared refusal.
func (f skipDeclared) EncodeRequest(req *protocol.Request, header http.Header) ([]byte, error) {
	body, err := f.Format.EncodeRequest(req, header)
	f.skipIfDeclared(err)
	return body, err
}

// EncodeStreamRequest encodes req as the wrapped format does, skipping t on a declared refusal.
func (f skipDeclared) EncodeStreamRequest(req *protocol.Request, header http.Header) ([]byte, error) {
	body, err := f.Format.EncodeStreamRequest(req, header)
	f.skipIfDeclared(err)
	return body, err
}

// skipIfDeclared skips t when err is a declared refusal, naming the profile's variable, the
// format, and the gap, its Feature and its Value, with the refusal's account.
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

// A role's AI_<ROLE>_PROFILE decodes to each format's Options, a member absent its zero Options;
// the variable unset or empty, or another role's alone, is the zero profile.
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

// A profile that doesn't decode strictly is an error naming the role's variable: not JSON, an
// unknown format name, an unknown field, a Go field name, and an effort the protocol lacks.
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

// skipRecorder is a testing.TB that records a Skipf call's text instead of skipping, so a test
// reads whether, and how, skipDeclared would skip.
type skipRecorder struct {
	testing.TB
	skipped string
}

func (r *skipRecorder) Skipf(format string, args ...any) {
	r.skipped = fmt.Sprintf(format, args...)
}

// effortRequest is a request of effort e, which a profile may refuse.
func effortRequest(e protocol.ReasoningEffort) *protocol.Request {
	return &protocol.Request{
		Model:           "m",
		Input:           []protocol.Item{protocol.Message{Role: protocol.RoleUser, Content: []protocol.ContentPart{protocol.InputText{Text: "hi"}}}},
		Reasoning:       protocol.ReasoningParam{Effort: e},
		MaxOutputTokens: 64,
	}
}

// A request the profile declares a gap of skips, streamed or not, naming the variable, the
// format, and the gap; one the spec refuses, Declared false, and one the format sends, don't.
func TestSkipDeclared(t *testing.T) {
	refusesNone := responses.New(responses.Options{RefusedEfforts: []protocol.ReasoningEffort{protocol.ReasoningEffortNone}})
	for _, tc := range []struct {
		name   string
		effort protocol.ReasoningEffort
		// skip is what the skip's text holds, or empty for no skip.
		skip []string
	}{
		{"declared", protocol.ReasoningEffortNone, []string{"AI_AGENT_PROFILE", "the responses format", `"reasoning effort none"`}},
		{"the spec's", protocol.ReasoningEffortMinimal, nil},
		{"sent", protocol.ReasoningEffortLow, nil},
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

				if tc.skip == nil {
					if rec.skipped != "" {
						t.Errorf("skipped %q, want no skip", rec.skipped)
					}
					u, ok := errors.AsType[*format.UnsupportedError](err)
					switch {
					case tc.effort == protocol.ReasoningEffortMinimal && (!ok || u.Declared):
						t.Errorf("err = %v, want the spec's refusal returned", err)
					case tc.effort == protocol.ReasoningEffortLow && err != nil:
						t.Errorf("err = %v, want the request encoded", err)
					}
					return
				}
				for _, want := range tc.skip {
					if !strings.Contains(rec.skipped, want) {
						t.Errorf("skipped %q, want it to name %s", rec.skipped, want)
					}
				}
			})
		}
	}
}

// Through a Client, a declared refusal skips the scenario's test from inside Generate and from
// inside GenerateStream's iterator, the test's own goroutine, and nothing reaches the endpoint.
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
