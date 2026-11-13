package app_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/standards-lab/go-cli-sdk"
	"github.com/standards-lab/go-core/graph"
	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
	"github.com/standards-lab/go-ai/tools/infer/internal/app"
	"github.com/standards-lab/go-ai/tools/infer/internal/apptest"
)

// probe adds a leaf at a's root that declares node and writes its value,
// formatted with %T for the format node and %q otherwise, to stdout, so a
// test observes the setting a run resolves.
func probe[T any](a *app.App, node *graph.Node[T], verb string) {
	a.Root().Add((&cli.Command{
		Name:    "probe",
		Summary: "Write a node's value",
		Run: func(_ context.Context, inv *cli.Invocation) error {
			_, err := fmt.Fprintf(inv.Stdout, verb, inv.Get(node))
			return err
		},
	}).Use(node))
}

// The format is completions when unset, else INFER_FORMAT's, else
// --format's when given, wherever on the command line; each maps to its
// format package's format.
func TestFormat_FlagOverVariable(t *testing.T) {
	want := map[string]string{
		"completions": fmt.Sprintf("%T", completions.New()),
		"messages":    fmt.Sprintf("%T", messages.New()),
		"responses":   fmt.Sprintf("%T", responses.New(responses.Options{})),
	}
	for _, tc := range []struct {
		name string
		env  string
		args []string
		want string
	}{
		{"default", "", []string{"probe"}, "completions"},
		{"variable", "messages", []string{"probe"}, "messages"},
		{"flag before the command", "messages", []string{"--format", "responses", "probe"}, "responses"},
		{"flag after the command", "messages", []string{"probe", "--format=completions"}, "completions"},
		{"flag over an unknown variable", "bogus", []string{"probe", "--format", "messages"}, "messages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, map[string]string{"INFER_FORMAT": tc.env})
			probe(h.app, h.app.Nodes().Format, "%T")

			r := h.run(tc.args...)

			wantCode(t, r, process.ExitOK)
			if r.stdout != want[tc.want] {
				t.Errorf("format = %s, want %s", r.stdout, want[tc.want])
			}
		})
	}
}

// The model is empty when unset, else INFER_MODEL's, else --model's when
// given, even given empty.
func TestModel_FlagOverVariable(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  string
		args []string
		want string
	}{
		{"unset", "", []string{"probe"}, ""},
		{"variable", "agent", []string{"probe"}, "agent"},
		{"flag before the command", "agent", []string{"--model", "vision", "probe"}, "vision"},
		{"flag after the command", "agent", []string{"probe", "--model", "vision"}, "vision"},
		{"flag given empty", "agent", []string{"probe", "--model="}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, map[string]string{"INFER_MODEL": tc.env})
			probe(h.app, h.app.Nodes().Model, "%q")

			r := h.run(tc.args...)

			wantCode(t, r, process.ExitOK)
			if want := fmt.Sprintf("%q", tc.want); r.stdout != want {
				t.Errorf("model = %s, want %s", r.stdout, want)
			}
		})
	}
}

// The base URL is INFER_BASE_URL's, or --base-url's when given, wherever on
// the command line: the request goes to the endpoint the flag names, and
// the variable's receives none.
func TestBaseURL_FlagOverVariable(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(flag string) []string
	}{
		{"variable", func(string) []string { return []string{"models"} }},
		{"flag before the command", func(u string) []string { return []string{"--base-url", u, "models"} }},
		{"flag after the command", func(u string) []string { return []string{"models", "--base-url=" + u} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fromVar := apptest.Serve(t, apptest.Models("from-variable"))
			fromFlag := apptest.Serve(t, apptest.Models("from-flag"))

			r := runApp(t, map[string]string{"INFER_BASE_URL": fromVar.URL}, tc.args(fromFlag.URL)...)

			wantCode(t, r, process.ExitOK)
			want, sent, unsent := "from-flag\n", fromFlag, fromVar
			if tc.name == "variable" {
				want, sent, unsent = "from-variable\n", fromVar, fromFlag
			}
			if r.stdout != want {
				t.Errorf("stdout = %q, want %q", r.stdout, want)
			}
			if n := len(sent.Requests()); n != 1 {
				t.Errorf("chosen endpoint received %d requests, want 1", n)
			}
			wantNoRequest(t, unsent)
		})
	}
}

// Each refused setting exits 2 before any request, its error reported
// once on stderr, naming the flag and its variable. The endpoint a valid
// setting would reach receives nothing.
func TestRefusals(t *testing.T) {
	e := apptest.Serve(t, apptest.Models("agent"))
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
		want string // the error, after the command's label
	}{
		{
			name: "no base URL",
			args: []string{"models"},
			want: "client: no base URL: set --base-url or INFER_BASE_URL",
		},
		{
			name: "base URL given empty",
			env:  map[string]string{"INFER_BASE_URL": e.URL},
			args: []string{"models", "--base-url="},
			want: "client: no base URL: set --base-url or INFER_BASE_URL",
		},
		{
			name: "base URL with a query",
			env:  map[string]string{"INFER_BASE_URL": e.URL + "?key=1"},
			args: []string{"models"},
			want: "client: --base-url (INFER_BASE_URL): ai: base URL",
		},
		{
			name: "relative base URL from the flag",
			env:  map[string]string{"INFER_BASE_URL": e.URL},
			args: []string{"models", "--base-url", "localhost/v1"},
			want: "client: --base-url (INFER_BASE_URL): ai: base URL",
		},
		{
			name: "unknown format variable",
			env:  map[string]string{"INFER_BASE_URL": e.URL, "INFER_FORMAT": "chat"},
			args: []string{"models"},
			want: `format: unknown format "chat": --format (INFER_FORMAT) is completions, messages, or responses`,
		},
		{
			name: "unknown format flag",
			env:  map[string]string{"INFER_BASE_URL": e.URL, "INFER_FORMAT": "messages"},
			args: []string{"--format", "Responses", "models"},
			want: `format: unknown format "Responses": --format (INFER_FORMAT) is completions, messages, or responses`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runApp(t, tc.env, tc.args...)

			wantCode(t, r, process.ExitUsage)
			if want := "infer models: " + tc.want; !strings.HasPrefix(r.stderr, want) {
				t.Errorf("stderr = %q, want it to start with %q", r.stderr, want)
			}
			if n := strings.Count(r.stderr, "infer models: "); n != 1 {
				t.Errorf("stderr = %q, want the error reported once", r.stderr)
			}
			if r.stdout != "" {
				t.Errorf("stdout = %q, want empty", r.stdout)
			}
			wantNoRequest(t, e)
		})
	}
}
