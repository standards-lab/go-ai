package integration_test

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"

	"github.com/standards-lab/go-ai/tools/infer/integration"
)

type logRecorder struct {
	testing.TB
	mu   sync.Mutex
	text strings.Builder
}

func (r *logRecorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.text.WriteString(s + "\n")
}

func (r *logRecorder) Log(args ...any) {
	r.Helper()
	r.record(fmt.Sprint(args...))
	r.TB.Log(args...)
}

func (r *logRecorder) Logf(format string, args ...any) {
	r.Helper()
	r.record(fmt.Sprintf(format, args...))
	r.TB.Logf(format, args...)
}

func (r *logRecorder) Fatalf(format string, args ...any) {
	r.Helper()
	r.record(fmt.Sprintf(format, args...))
	r.TB.Fatalf(format, args...)
}

func (r *logRecorder) Skipf(format string, args ...any) {
	r.Helper()
	r.record(fmt.Sprintf(format, args...))
	r.TB.Skipf(format, args...)
}

func (r *logRecorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.text.String()
}

const (
	tokenSecret  = "t0ken-secret"
	headerSecret = "head3r-secret"
)

func unsetInfer(t *testing.T) {
	t.Helper()
	for _, name := range []string{"INFER_TOKEN", "INFER_HEADER"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEndpoint_MapsCredentials(t *testing.T) {
	unsetInfer(t)
	header := "api-key: " + headerSecret + "\nx-tenant: docs"
	t.Setenv("AI_TEST_BASE_URL", "http://localhost:8000/v1")
	t.Setenv("AI_TEST_TOKEN", tokenSecret)
	t.Setenv("AI_TEST_HEADER", header)
	t.Setenv("AI_TEST_MODEL", "")
	t.Setenv("AI_TEST_PROFILE", "")
	rec := &logRecorder{TB: t}

	overrides := integration.Endpoint(rec, "AI_TEST_BASE_URL")

	if want := []string{"INFER_BASE_URL=http://localhost:8000/v1"}; !slices.Equal(overrides, want) {
		t.Errorf("overrides = %q, want %q", overrides, want)
	}
	if got := os.Getenv("INFER_TOKEN"); got != tokenSecret {
		t.Errorf("INFER_TOKEN = %q, want AI_TEST_TOKEN's", got)
	}
	if got := os.Getenv("INFER_HEADER"); got != header {
		t.Errorf("INFER_HEADER = %q, want AI_TEST_HEADER's", got)
	}
	logged := rec.String()
	for _, want := range []string{"INFER_TOKEN set from AI_TEST_TOKEN", "INFER_HEADER set from AI_TEST_HEADER"} {
		if !strings.Contains(logged, want) {
			t.Errorf("log = %q, want it to say %q", logged, want)
		}
	}
	for _, secret := range []string{tokenSecret, headerSecret} {
		if strings.Contains(logged, secret) || strings.Contains(strings.Join(overrides, " "), secret) {
			t.Errorf("log = %q, overrides = %q, want neither to show %q", logged, overrides, secret)
		}
	}
}

func TestEndpoint_NoCredentials(t *testing.T) {
	unsetInfer(t)
	t.Setenv("AI_TEST_BASE_URL", "http://localhost:8000/v1")
	t.Setenv("AI_TEST_TOKEN", "")
	t.Setenv("AI_TEST_HEADER", "")
	t.Setenv("AI_TEST_MODEL", "")
	t.Setenv("AI_TEST_PROFILE", "")
	rec := &logRecorder{TB: t}

	overrides := integration.Endpoint(rec, "AI_TEST_BASE_URL")

	if want := []string{"INFER_BASE_URL=http://localhost:8000/v1"}; !slices.Equal(overrides, want) {
		t.Errorf("overrides = %q, want %q, no model", overrides, want)
	}

	for _, name := range []string{"INFER_TOKEN", "INFER_HEADER"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Errorf("%s = %q, want it unset", name, v)
		}
	}
	if logged := rec.String(); logged != "" {
		t.Errorf("log = %q, want nothing", logged)
	}
}

func TestEndpoint_MapsModel(t *testing.T) {
	unsetInfer(t)
	t.Setenv("AI_TEST_BASE_URL", "http://localhost:8000/v1")
	t.Setenv("AI_TEST_TOKEN", tokenSecret)
	t.Setenv("AI_TEST_HEADER", "api-key: "+headerSecret)
	t.Setenv("AI_TEST_MODEL", "gpt-5-mini")
	t.Setenv("AI_TEST_PROFILE", "")
	rec := &logRecorder{TB: t}

	overrides := integration.Endpoint(rec, "AI_TEST_BASE_URL")

	if want := []string{"INFER_BASE_URL=http://localhost:8000/v1", "INFER_MODEL=gpt-5-mini"}; !slices.Equal(overrides, want) {
		t.Errorf("overrides = %q, want %q", overrides, want)
	}
	for _, secret := range []string{tokenSecret, headerSecret} {
		if strings.Contains(rec.String(), secret) || strings.Contains(strings.Join(overrides, " "), secret) {
			t.Errorf("log = %q, overrides = %q, want neither to show %q", rec.String(), overrides, secret)
		}
	}
}

const vllmAgent = `{"completions":{"refused_efforts":["none","max"],"no_allowed_tools":true},` +
	`"responses":{"refused_efforts":["xhigh"],"resend_reasoning_text":true},"messages":{"refused_efforts":["none"]}}`

func TestEndpoint_MapsProfile(t *testing.T) {
	unsetInfer(t)
	t.Setenv("AI_TEST_BASE_URL", "http://localhost:8000/v1")
	t.Setenv("AI_TEST_TOKEN", "")
	t.Setenv("AI_TEST_HEADER", "")
	t.Setenv("AI_TEST_MODEL", "gpt-oss-120b")
	t.Setenv("AI_TEST_PROFILE", vllmAgent)
	t.Setenv("AI_OTHER_PROFILE", `{"messages":{"refused_efforts":["max"]}}`)

	overrides := integration.Endpoint(t, "AI_TEST_BASE_URL")

	want := []string{"INFER_BASE_URL=http://localhost:8000/v1", "INFER_MODEL=gpt-oss-120b", "INFER_PROFILE=" + vllmAgent}
	if !slices.Equal(overrides, want) {
		t.Errorf("overrides = %q, want %q", overrides, want)
	}
}

func TestProfile(t *testing.T) {
	for _, tc := range []struct{ name, v string }{
		{"unset", ""},
		{"empty object", "{}"},
		{"one member", `{"responses":{"resend_reasoning_text":true}}`},
		{"every member", vllmAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AI_TEST_PROFILE", tc.v)

			got, err := integration.Profile("AI_TEST_BASE_URL")

			if err != nil {
				t.Fatalf("Profile: %v", err)
			}
			if got != tc.v {
				t.Errorf("Profile = %q, want %q", got, tc.v)
			}
		})
	}
}

func TestProfile_Malformed(t *testing.T) {
	for _, tc := range []struct{ name, v string }{
		{"not JSON", `{"completions":`},
		{"unknown format", `{"chat":{}}`},
		{"unknown field", `{"messages":{"no_allowed_tools":true}}`},
		{"Go field name", `{"responses":{"ResendReasoningText":true}}`},
		{"unknown effort", `{"completions":{"refused_efforts":["extreme"]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AI_TEST_PROFILE", tc.v)

			_, err := integration.Profile("AI_TEST_BASE_URL")

			if err == nil {
				t.Fatal("Profile succeeded, want it refused")
			}
			if !strings.HasPrefix(err.Error(), "AI_TEST_PROFILE: ") {
				t.Errorf("err = %q, want it to name AI_TEST_PROFILE first", err)
			}
		})
	}
}

func TestSkipDeclared(t *testing.T) {
	const declared = "infer chat: --reasoning: responses: reasoning effort none is refused by the deployment, as its profile declares; declared by --profile (INFER_PROFILE)"
	for _, tc := range []struct {
		name string
		res  processtest.Result
		skip bool
	}{
		{"declared", processtest.Result{Code: 2, Stderr: declared + "\nUsage: infer chat [flags] PROMPT\n"}, true},
		{"declared, last line", processtest.Result{Code: 2, Stderr: declared}, true},
		{"the spec's", processtest.Result{Code: 2, Stderr: "infer chat: --reasoning: responses: reasoning effort minimal has no responses form\n"}, false},
		{"another code", processtest.Result{Code: 1, Stderr: declared + "\n"}, false},
		{"completed", processtest.Result{Code: 0, Stdout: "Paris\n"}, false},
	} {
		var sub testing.TB
		rec := &logRecorder{}
		t.Run(tc.name, func(t *testing.T) {
			sub, rec.TB = t, t
			integration.SkipDeclared(rec, "AI_TEST_BASE_URL", tc.res)
		})
		if skipped := sub.Skipped(); skipped != tc.skip {
			t.Errorf("%s: skipped = %t, want %t", tc.name, skipped, tc.skip)
		}
		if !tc.skip {
			continue
		}
		for _, want := range []string{"AI_TEST_PROFILE", "reasoning effort none is refused by the deployment"} {
			if !strings.Contains(rec.String(), want) {
				t.Errorf("%s: skip = %q, want it to name %q", tc.name, rec.String(), want)
			}
		}
	}
}
