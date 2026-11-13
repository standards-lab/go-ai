//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"

	"github.com/standards-lab/go-ai/tools/infer/integration"
)

// The baseline command tree as a running binary: main hands the program its
// arguments and its process streams, and exits with the code the run
// returns. The commands an application adds take their own cases beside
// these.

// version prints the main module's version, one line, to stdout.
func TestCLI_Version(t *testing.T) {
	res := integration.Run(t, "version")

	if res.Code != process.ExitOK {
		t.Fatalf("code = %d, want %d; stderr = %q", res.Code, process.ExitOK, res.Stderr)
	}
	line, rest, found := strings.Cut(res.Stdout, "\n")
	if line == "" || !found || rest != "" {
		t.Errorf("stdout = %q, want one non-empty line", res.Stdout)
	}
	if res.Stderr != "" {
		t.Errorf("stderr = %q, want empty", res.Stderr)
	}
}

// The root's help, asked for or given by no command at all, exits with the
// usage code on stdout and lists version.
func TestCLI_RootHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			res := integration.Run(t, args...)

			if res.Code != process.ExitUsage {
				t.Errorf("code = %d, want %d; stderr = %q", res.Code, process.ExitUsage, res.Stderr)
			}
			if !strings.Contains(res.Stdout, "version") {
				t.Errorf("stdout = %q, want the root help listing version", res.Stdout)
			}
			if res.Stderr != "" {
				t.Errorf("stderr = %q, want empty", res.Stderr)
			}
		})
	}
}

// An unknown command is a usage error: the usage code, and the error and
// usage on stderr alone.
func TestCLI_UnknownCommand(t *testing.T) {
	res := integration.Run(t, "bogus")

	if res.Code != process.ExitUsage {
		t.Errorf("code = %d, want %d", res.Code, process.ExitUsage)
	}
	if want := `infer: unknown command "bogus"`; !strings.HasPrefix(res.Stderr, want) {
		t.Errorf("stderr = %q, want it to start with %q", res.Stderr, want)
	}
	if !strings.Contains(res.Stderr, "version") {
		t.Errorf("stderr = %q, want the usage listing version", res.Stderr)
	}
	if res.Stdout != "" {
		t.Errorf("stdout = %q, want empty", res.Stdout)
	}
}
