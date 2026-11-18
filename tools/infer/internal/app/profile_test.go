package app_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
)

// refusesHigh is a profile whose completions member refuses the effort
// high, so a chat at --reasoning high through completions is refused
// before it is sent when the profile reaches the format.
const refusesHigh = `{"completions":{"refused_efforts":["high"]}}`

// The format in use is built with its member of the profile: an effort the
// profile refuses is refused before any request, exit 2, the endpoint
// receiving nothing, through chat's request check and so through its
// client. A profile from --profile wins over INFER_PROFILE's, wherever on
// the command line; a profile with no member for the format in use, or
// none at all, is the format's spec, which sends the effort.
func TestProfile_ReachesTheFormat(t *testing.T) {
	for _, tc := range []struct {
		name    string
		env     string
		args    []string
		refused bool
	}{
		{"unset or empty", "", nil, false},
		{"variable", refusesHigh, nil, true},
		{"flag before the command", "", []string{"--profile", refusesHigh}, true},
		{"flag after the command", "", []string{"chat", "--profile=" + refusesHigh}, true},
		{"flag over the variable", refusesHigh, []string{"--profile", `{"completions":{}}`}, false},
		{"flag given empty over the variable", refusesHigh, []string{"--profile="}, false},
		{"flag over a malformed variable", `{"bogus":{}}`, []string{"--profile", refusesHigh}, true},
		{"another format's member alone", `{"messages":{"refused_efforts":["high"]},"responses":{"refused_efforts":["high"]}}`, nil, false},
		{"empty object", `{}`, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := completedStream(t, "agent")
			env := agent(e)
			env["INFER_PROFILE"] = tc.env
			args := tc.args
			if len(args) == 0 || args[0] != "chat" {
				args = append(args, "chat")
			}

			r := runApp(t, env, append(args, "--reasoning", "high", "hello")...)

			if !tc.refused {
				wantCode(t, r, process.ExitOK)
				if body := oneBody(t, e, completionsRoute); body["reasoning_effort"] != "high" {
					t.Errorf("body = %v, want reasoning_effort high", body)
				}
				return
			}
			wantCode(t, r, process.ExitUsage)
			if !strings.Contains(r.stderr, "reasoning effort") || strings.Count(r.stderr, "infer chat: ") != 1 {
				t.Errorf("stderr = %q, want one infer chat error refusing the reasoning effort", r.stderr)
			}
			wantNoRequest(t, e)
		})
	}
}

// Each format in use is built with its own member: through messages and
// responses, an effort their member refuses is refused, and the same
// profile's completions member reaches no other format.
func TestProfile_EachFormatItsMember(t *testing.T) {
	const profile = `{"completions":{"refused_efforts":["low"]},"messages":{"refused_efforts":["high"]},"responses":{"refused_efforts":["medium"]}}`
	for _, format := range []string{"messages", "responses"} {
		t.Run(format, func(t *testing.T) {
			e := completedStream(t, "agent")
			env := agent(e)
			env["INFER_FORMAT"] = format
			env["INFER_PROFILE"] = profile
			refused := map[string]string{"messages": "high", "responses": "medium"}[format]

			r := runApp(t, env, "chat", "--max-tokens", "64", "--reasoning", refused, "hello")

			wantCode(t, r, process.ExitUsage)
			if !strings.Contains(r.stderr, "reasoning effort") {
				t.Errorf("stderr = %q, want the reasoning effort refused", r.stderr)
			}
			wantNoRequest(t, e)
		})
	}
}

// A profile that doesn't decode strictly as go-ai's Options, keyed by
// format name, is a usage error, exit 2, before any request, naming the
// flag and its variable ahead of the decoder's error; every member is
// checked, not only the format in use's. Chat and transcribe read it.
func TestProfile_Refusals(t *testing.T) {
	clip := writeClip(t, "clip.wav")
	for _, tc := range []struct {
		name    string
		profile string
		detail  string // a part of the decoder's error the message carries
	}{
		{"not JSON", `{"completions":`, "unexpected EOF"},
		{"not an object", `["completions"]`, "array"},
		{"unknown top-level key", `{"chat":{}}`, `"chat"`},
		{"format name in another case", `{"Completions":{}}`, `"Completions"`},
		{"unknown member field", `{"completions":{"refused_effort":["none"]}}`, `"refused_effort"`},
		{"Go field name", `{"completions":{"RefusedEfforts":["none"]}}`, `"RefusedEfforts"`},
		{"unknown field of a format not in use", `{"responses":{"no_tool_choice_required":true}}`, `"no_tool_choice_required"`},
		{"mistyped effort", `{"completions":{"refused_efforts":["extreme"]}}`, `"extreme"`},
		{"effort not a string", `{"messages":{"refused_efforts":[1]}}`, "number"},
		{"mistyped flag", `{"responses":{"resend_reasoning_text":"yes"}}`, "string"},
	} {
		for _, args := range [][]string{{"chat", "hello"}, {"transcribe", clip}} {
			t.Run(tc.name+" "+args[0], func(t *testing.T) {
				e := completedStream(t, "agent")
				env := agent(e)
				env["INFER_PROFILE"] = tc.profile

				r := runApp(t, env, args...)

				wantCode(t, r, process.ExitUsage)
				label := "infer " + args[0] + ": "
				first, _, _ := strings.Cut(r.stderr, "\n")
				if !strings.HasPrefix(first, label+"profiled format: --profile (INFER_PROFILE): ") || !strings.Contains(first, tc.detail) {
					t.Errorf("stderr = %q, want %q naming --profile (INFER_PROFILE) and %s", r.stderr, label, tc.detail)
				}
				if strings.Count(r.stderr, label) != 1 {
					t.Errorf("stderr = %q, want the error reported once", r.stderr)
				}
				wantNoRequest(t, e)
			})
		}
	}
}

// The flag's malformed profile is refused as the variable's is, naming
// both.
func TestProfile_FlagRefused(t *testing.T) {
	e := completedStream(t, "agent")

	r := runApp(t, agent(e), "chat", "--profile", `{"completions":{"bogus":true}}`, "hello")

	wantCode(t, r, process.ExitUsage)
	if want := "infer chat: profiled format: --profile (INFER_PROFILE): "; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	wantNoRequest(t, e)
}

// models and embed send no request a profile changes, so they read none: a
// malformed INFER_PROFILE, or --profile, fails neither.
func TestProfile_IgnoredByModelsAndEmbed(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"models", []string{"models"}, "agent\n"},
		{"models with the flag", []string{"--profile", "{", "models"}, "agent\n"},
		{"embed", []string{"embed", "hello"}, "[1]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := embedEndpoint(t, []string{"agent"}, []float32{1})
			env := agent(e)
			env["INFER_PROFILE"] = `{"bogus":{"RefusedEfforts":"extreme"}}`

			r := runApp(t, env, tc.args...)

			wantCode(t, r, process.ExitOK)
			if r.stdout != tc.want {
				t.Errorf("stdout = %q, want %q", r.stdout, tc.want)
			}
		})
	}
}
