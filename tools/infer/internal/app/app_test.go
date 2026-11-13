package app_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/internal/app"
	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// unreadable is an environment no command could build from: an
// unparsable shutdown timeout and an unknown format. A run under it that
// succeeds, or fails only as a usage error of its own, read no
// configuration.
func unreadable(baseURL string) map[string]string {
	return map[string]string{
		"INFER_SHUTDOWN_TIMEOUT": "not-a-duration",
		"INFER_FORMAT":           "bogus",
		"INFER_BASE_URL":         baseURL,
		"INFER_TOKEN":            "secret",
	}
}

// result is what one in-process run of the App returned and wrote, and
// the names of the nodes its Build began.
type result struct {
	code           int
	stdout, stderr string
	built          []string
}

// harness is an App over fresh buffers, with the recorder of the nodes its
// runs build, ready for a test to add a probe command before it runs.
type harness struct {
	app            *app.App
	stdout, stderr *bytes.Buffer
	built          *apptest.Recorder[graph.Ref]
}

// newHarness gives the test env, through apptest.Env, and returns an App
// over fresh buffers and empty standard input.
func newHarness(t *testing.T, env map[string]string) *harness {
	t.Helper()
	return newHarnessIn(t, env, "")
}

// newHarnessIn is [newHarness] with stdin as standard input.
func newHarnessIn(t *testing.T, env map[string]string, stdin string) *harness {
	t.Helper()
	apptest.Env(t, env)
	h := &harness{stdout: new(bytes.Buffer), stderr: new(bytes.Buffer)}
	h.app = app.New(cli.Streams{Stdin: strings.NewReader(stdin), Stdout: h.stdout, Stderr: h.stderr})
	h.built = apptest.Builds(h.app)
	return h
}

// run runs h's App once with args and returns what it did.
func (h *harness) run(args ...string) result {
	code := h.app.Run(context.Background(), args)
	return result{code: code, stdout: h.stdout.String(), stderr: h.stderr.String(), built: apptest.Names(h.built.Log())}
}

// runApp runs a fresh App once with env and args.
func runApp(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()
	return newHarness(t, env).run(args...)
}

// wantCode fails t unless r exited with code.
func wantCode(t *testing.T, r result, code int) {
	t.Helper()
	if r.code != code {
		t.Errorf("code = %d, want %d; stdout = %q, stderr = %q", r.code, code, r.stdout, r.stderr)
	}
}

// wantNothingBuilt fails t if r's run built any node.
func wantNothingBuilt(t *testing.T, r result) {
	t.Helper()
	if len(r.built) != 0 {
		t.Errorf("nodes built = %q, want none", r.built)
	}
}

// wantNoRequest fails t if e received any request.
func wantNoRequest(t *testing.T, e *apptest.Endpoint) {
	t.Helper()
	if reqs := e.Requests(); len(reqs) != 0 {
		t.Errorf("endpoint received %d requests, want none: first %s %s", len(reqs), reqs[0].Method, reqs[0].Path)
	}
}

func TestRun_RootHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"-h"}, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			e := apptest.Serve(t, apptest.Models("agent"))

			r := runApp(t, unreadable(e.URL), args...)

			wantCode(t, r, process.ExitUsage)
			for _, want := range []string{"version", "models", "embed", "chat", "transcribe", "--base-url", "INFER_BASE_URL", "--format", "INFER_FORMAT", "--model", "INFER_MODEL",
				"--retries", "INFER_RETRIES", "the first included", "--retry-base-delay", "INFER_RETRY_BASE_DELAY", "--retry-max-delay", "INFER_RETRY_MAX_DELAY"} {
				if !strings.Contains(r.stdout, want) {
					t.Errorf("stdout = %q, want the root help naming %s", r.stdout, want)
				}
			}
			if strings.Contains(r.stdout, "--token") {
				t.Errorf("stdout = %q, want no token flag", r.stdout)
			}
			if r.stderr != "" {
				t.Errorf("stderr = %q, want empty", r.stderr)
			}
			wantNothingBuilt(t, r)
			wantNoRequest(t, e)
		})
	}
}

// A command's help builds nothing and sends nothing either.
func TestRun_ModelsHelp(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, unreadable(e.URL), "models", "--help")

	wantCode(t, r, process.ExitUsage)
	if !strings.Contains(r.stdout, "--base-url") {
		t.Errorf("stdout = %q, want models' help naming the root flags", r.stdout)
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}

func TestRun_UnknownCommand(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, unreadable(e.URL), "bogus")

	wantCode(t, r, process.ExitUsage)
	if want := `infer: unknown command "bogus"`; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}

// A positional argument to models is a usage error, reported once, before
// anything is built or sent.
func TestRun_ModelsTakesNoArguments(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL}, "models", "extra")

	wantCode(t, r, process.ExitUsage)
	if !strings.HasPrefix(r.stderr, "infer models: ") || strings.Count(r.stderr, "infer models: ") != 1 {
		t.Errorf("stderr = %q, want one error labelled infer models", r.stderr)
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}

func TestRun_Version(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, unreadable(e.URL), "version")

	wantCode(t, r, process.ExitOK)
	// A test binary carries no main module version.
	if r.stdout != "(devel)\n" {
		t.Errorf("stdout = %q, want %q", r.stdout, "(devel)\n")
	}
	if r.stderr != "" {
		t.Errorf("stderr = %q, want empty", r.stderr)
	}
	wantNothingBuilt(t, r)
	wantNoRequest(t, e)
}

// New builds nothing: the graph freezes on its first Build and panics on a
// Replace after it, so Halt's Replace succeeding proves New never built.
func TestNew_IsCold(t *testing.T) {
	h := newHarness(t, unreadable("http://127.0.0.1:1/v1"))

	if h.stdout.Len() != 0 || h.stderr.Len() != 0 {
		t.Errorf("New() wrote %q, %q, want nothing", h.stdout, h.stderr)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Replace after New() panicked: %v; want the graph unbuilt", r)
		}
	}()
	apptest.Halt(h.app)
}

func TestRun_InvalidShutdownTimeoutFailsTheCommand(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))

	r := runApp(t, map[string]string{"INFER_BASE_URL": e.URL, "INFER_SHUTDOWN_TIMEOUT": "not-a-duration"}, "models")

	wantCode(t, r, process.ExitFailure)
	if want := "infer models: lifecycle config: INFER_SHUTDOWN_TIMEOUT"; !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	wantNoRequest(t, e)
}

// A halted run of models fails its Build before anything starts, so it
// sends nothing.
func TestRun_HaltStopsTheCommand(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))
	h := newHarness(t, map[string]string{"INFER_BASE_URL": e.URL})
	apptest.Halt(h.app)

	r := h.run("models")

	wantCode(t, r, process.ExitFailure)
	if want := "infer models: lifecycle config: " + apptest.ErrHalted.Error(); !strings.HasPrefix(r.stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want empty", r.stdout)
	}
	if !slices.Contains(r.built, "lifecycle config") {
		t.Errorf("nodes built = %q, want the lifecycle config among them", r.built)
	}
	wantNoRequest(t, e)
}
