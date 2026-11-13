package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process/processtest"
)

// envPrefix opens the name of every variable the program reads.
const envPrefix = "INFER_"

// Main is the suite's TestMain: it unsets every INFER_ variable the suite
// inherited, so each run reads only the program's defaults and the
// overrides its test passes, then builds cmd/infer once, with the race
// detector so the program runs under it too, runs the tests, and removes
// the build.
func Main(m *testing.M) {
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, envPrefix) {
			_ = os.Unsetenv(name)
		}
	}
	processtest.Main(m, "./cmd/infer")
}

// Run runs the built program once with args, the arguments without the
// program name, on empty standard input, and returns its output and exit
// code. A nonzero code is a result, not a failure.
func Run(t testing.TB, args ...string) processtest.Result {
	t.Helper()
	return processtest.Run(t, processtest.Cmd{Args: args})
}

// RunEnv runs the built program once as Run does, with env, KEY=VALUE
// pairs, added to the environment Main cleared of INFER_ variables, so env
// alone configures it.
func RunEnv(t testing.TB, env []string, args ...string) processtest.Result {
	t.Helper()
	return processtest.Run(t, processtest.Cmd{Args: args, Env: env})
}

// BaseURL returns the API root the environment variable named variable
// holds, such as AI_AGENT_BASE_URL, for a scenario to map onto
// INFER_BASE_URL. It skips t, naming variable, when it is unset or empty,
// so a scenario runs only where its endpoint is configured.
func BaseURL(t testing.TB, variable string) string {
	t.Helper()
	base := os.Getenv(variable)
	if base == "" {
		t.Skipf("%s is unset: set it to the endpoint's API root, such as http://localhost:8000/v1", variable)
	}
	return base
}

// Endpoint returns the overrides that point a run at the endpoint of the
// role whose API root variable is baseVariable, such as AI_AGENT_BASE_URL:
// INFER_BASE_URL set to [BaseURL]'s, which skips t when the variable is
// unset. It maps the role's credentials onto infer's as well, each only
// when set and non-empty: AI_<ROLE>_TOKEN onto INFER_TOKEN and
// AI_<ROLE>_HEADER onto INFER_HEADER. Those it sets in the test's own
// environment, with t.Setenv, which the program inherits, rather than as
// overrides, since a run logs its overrides in the shell line that replays
// it: a scenario's log, and so its failure, shows its base URL but never a
// token or header value. It logs the credentials it mapped by their
// variables' names alone. Main cleared every INFER_ variable before the
// suite ran, so with neither set the program reads neither; t.Setenv
// panics in a parallel test, which no scenario is.
func Endpoint(t testing.TB, baseVariable string) []string {
	t.Helper()
	base := BaseURL(t, baseVariable)
	role := strings.TrimSuffix(baseVariable, "_BASE_URL")
	for _, name := range []string{"TOKEN", "HEADER"} {
		from, to := role+"_"+name, envPrefix+name
		if v := os.Getenv(from); v != "" {
			t.Setenv(to, v)
			t.Logf("%s set from %s; its value isn't shown", to, from)
		}
	}
	return []string{"INFER_BASE_URL=" + base}
}

// mediaDir is the directory of the media the suite sends, from the suite's
// package directory, where go test runs it: go-ai's own integration
// testdata, whose README gives each file's provenance. It is read in place
// rather than copied, so the repository holds one copy and one account of
// where it came from; the tier runs from a checkout of go-ai, where the
// directory is always present beside tools/infer.
const mediaDir = "../../../integration/testdata"

// Media returns the absolute path of the integration medium name, such as
// shapes.png, failing t when it can't be found. The path is absolute
// because the program runs in the module root, not in the suite's
// directory.
func Media(t testing.TB, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(mediaDir, name))
	if err != nil {
		t.Fatalf("media %s: %v", name, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("media %s: %v", name, err)
	}
	return path
}
