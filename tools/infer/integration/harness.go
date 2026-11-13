package integration

import (
	"os"
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
