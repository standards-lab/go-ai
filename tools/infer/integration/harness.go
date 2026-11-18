package integration

import (
	"encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
	"github.com/standards-lab/go-core/process/processtest"

	"github.com/standards-lab/go-ai/format/completions"
	"github.com/standards-lab/go-ai/format/messages"
	"github.com/standards-lab/go-ai/format/responses"
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
// unset, INFER_MODEL set to AI_<ROLE>_MODEL's when it is set and non-empty,
// for an endpoint whose model list isn't one model, such as a cloud
// account's that lists its whole catalog, and INFER_PROFILE set to
// AI_<ROLE>_PROFILE's, as [Profile] checks it, when it is set and
// non-empty, failing t when it doesn't decode. Unset, the run sets no model,
// so infer sends to the one model the endpoint lists, and no profile, so
// infer speaks each format's spec. A model and a profile are no secret, so
// each is an override, shown in the run's logged shell line. It maps the
// role's credentials onto infer's as well, each only when set and
// non-empty: AI_<ROLE>_TOKEN onto INFER_TOKEN and AI_<ROLE>_HEADER onto
// INFER_HEADER. Those it sets in the test's own environment, with
// t.Setenv, which the program inherits, rather than as overrides, since a
// run logs its overrides in the shell line that replays it: a scenario's
// log, and so its failure, shows its base URL and model but never a token
// or header value. It logs the credentials it mapped by their variables'
// names alone. Main cleared every INFER_ variable before the suite ran, so
// with neither set the program reads neither; t.Setenv panics in a
// parallel test, which no scenario is.
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
	overrides := []string{"INFER_BASE_URL=" + base}
	if model := Model(baseVariable); model != "" {
		overrides = append(overrides, "INFER_MODEL="+model)
	}
	profile, err := Profile(baseVariable)
	if err != nil {
		t.Fatal(err)
	}
	if profile != "" {
		overrides = append(overrides, "INFER_PROFILE="+profile)
	}
	return overrides
}

// Model returns the model AI_<ROLE>_MODEL names for the role whose API
// root variable is baseVariable, such as AI_AGENT_MODEL for
// AI_AGENT_BASE_URL, which [Endpoint] maps onto INFER_MODEL, or "" when it
// is unset or empty.
func Model(baseVariable string) string {
	return os.Getenv(strings.TrimSuffix(baseVariable, "_BASE_URL") + "_MODEL")
}

// profile is the shape of a deployment's profile, as infer's --profile and
// INFER_PROFILE take it: a JSON object keyed by format name, each member
// that go-ai format's Options under the snake_case, case-strict names their
// tags give them.
type profile struct {
	Completions completions.Options `json:"completions,omitzero,case:strict"`
	Messages    messages.Options    `json:"messages,omitzero,case:strict"`
	Responses   responses.Options   `json:"responses,omitzero,case:strict"`
}

// Profile returns the profile AI_<ROLE>_PROFILE holds for the role whose
// API root variable is baseVariable, such as AI_AGENT_PROFILE for
// AI_AGENT_BASE_URL, which [Endpoint] maps onto INFER_PROFILE, or "" when it
// is unset or empty. It decodes it first as infer does, and as go-ai's
// integration tier does, with encoding/json/v2 and
// json.RejectUnknownMembers, so a malformed profile, an unknown format name
// or field, a Go field name, or a value a field refuses, is an error naming
// the variable ahead of the decoder's, rather than infer's usage error
// naming its own --profile and INFER_PROFILE. A profile is no secret, so the
// error may show it.
func Profile(baseVariable string) (string, error) {
	variable := strings.TrimSuffix(baseVariable, "_BASE_URL") + "_PROFILE"
	v := os.Getenv(variable)
	if v == "" {
		return "", nil
	}
	var p profile
	if err := json.Unmarshal([]byte(v), &p, json.RejectUnknownMembers(true)); err != nil {
		return "", fmt.Errorf("%s: %w", variable, err)
	}
	return v, nil
}

// declaredSuffix ends infer's error line for a request the deployment's
// profile refuses before sending, a go-ai *format.UnsupportedError of
// Declared true: chat names the setting that declared the gap after the
// format's reason, which names the gap's feature and value.
const declaredSuffix = "; declared by --profile (INFER_PROFILE)"

// SkipDeclared skips t when res, a run on the endpoint of the role whose
// API root variable is baseVariable, is infer's refusal of a request the
// role's profile, AI_<ROLE>_PROFILE, declares a gap of: a usage error,
// exit 2, whose error line ends in "; declared by --profile
// (INFER_PROFILE)". The profile says the deployment can't serve that
// request, so the scenario has nothing to assert of it; the skip names the
// variable and quotes the line, which names the gap, its feature and
// value, as in "--reasoning: responses: reasoning effort none is refused
// by the deployment, as its profile declares". It is the suite's one skip
// rule, and every scenario gets it, as each runs on an endpoint through
// one helper that calls it. A refusal of the format spec's own carries no
// such suffix, so its scenario goes on to fail on the code.
func SkipDeclared(t testing.TB, baseVariable string, res processtest.Result) {
	t.Helper()
	if res.Code != process.ExitUsage {
		return
	}
	for line := range strings.Lines(res.Stderr) {
		if line = strings.TrimSuffix(line, "\n"); strings.HasSuffix(line, declaredSuffix) {
			variable := strings.TrimSuffix(baseVariable, "_BASE_URL") + "_PROFILE"
			t.Skipf("%s, passed as INFER_PROFILE, declares the request's gap: %s", variable, line)
			return
		}
	}
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
