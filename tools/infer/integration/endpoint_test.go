//go:build integration

package integration_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
	"github.com/standards-lab/go-core/process/processtest"

	"github.com/standards-lab/go-ai/tools/infer/integration"
)

// role is an endpoint a scenario runs on, named by the environment variable
// that holds its API root, as go-ai's integration tier names it.
type role string

// The roles, each its variable.
const (
	agent         role = "AI_AGENT_BASE_URL"
	vision        role = "AI_VISION_BASE_URL"
	audio         role = "AI_AUDIO_BASE_URL"
	embedding     role = "AI_EMBEDDING_BASE_URL"
	transcription role = "AI_TRANSCRIPTION_BASE_URL"
)

// roles are every role, for the scenario that runs on each.
var roles = []role{agent, vision, audio, embedding, transcription}

// formats are the --format values, each a scenario that runs in every
// format runs through.
var formats = []string{"completions", "messages", "responses"}

// answerBound is the --max-tokens of a chat that wants an answer: a
// reasoning model reasons ahead of it, within the same bound. Messages
// requires one, so every chat scenario passes a bound.
const answerBound = "1024"

// runOn runs the built program once against r's endpoint, its API root
// mapped onto INFER_BASE_URL, with env added and no model set, so infer
// sends to the one model the endpoint lists. It skips t when r's variable
// is unset.
func runOn(t *testing.T, r role, env []string, args ...string) processtest.Result {
	t.Helper()
	base := integration.BaseURL(t, string(r))
	return integration.RunEnv(t, append([]string{"INFER_BASE_URL=" + base}, env...), args...)
}

// eachFormat runs scenario as a subtest of t for each of formats, named for
// the format, given the INFER_FORMAT setting that selects it.
func eachFormat(t *testing.T, scenario func(t *testing.T, formatEnv string)) {
	t.Helper()
	for _, f := range formats {
		t.Run(f, func(t *testing.T) { scenario(t, "INFER_FORMAT="+f) })
	}
}

// eachChat runs scenario as a subtest of t for each of formats, and in each
// both streamed and unstreamed, named as "completions/stream=true", given
// the INFER_FORMAT setting and the --stream flag that select them.
func eachChat(t *testing.T, scenario func(t *testing.T, formatEnv, streamFlag string)) {
	t.Helper()
	eachFormat(t, func(t *testing.T, formatEnv string) {
		for _, stream := range []bool{true, false} {
			flag := fmt.Sprintf("--stream=%t", stream)
			t.Run(strings.TrimPrefix(flag, "--"), func(t *testing.T) { scenario(t, formatEnv, flag) })
		}
	})
}

// wantCode fails t now unless res exited with code, so the assertions that
// follow read the output of the run they expect.
func wantCode(t *testing.T, res processtest.Result, code int) {
	t.Helper()
	if res.Code != code {
		t.Fatalf("code = %d, want %d; stdout = %q, stderr = %q", res.Code, code, res.Stdout, res.Stderr)
	}
}

// summary is chat's stderr summary, as its lines give it.
type summary struct {
	status         string
	reasoningItems int
}

// chatSummary returns the summary chat wrote to stderr, failing t unless
// stderr holds its status and reasoning item lines.
func chatSummary(t *testing.T, stderr string) summary {
	t.Helper()
	var s summary
	var haveStatus, haveItems bool
	for line := range strings.Lines(stderr) {
		line = strings.TrimSuffix(line, "\n")
		if v, ok := strings.CutPrefix(line, "status: "); ok {
			s.status, haveStatus = v, true
		}
		if v, ok := strings.CutPrefix(line, "reasoning items: "); ok {
			n, err := strconv.Atoi(v)
			if err != nil {
				t.Fatalf("reasoning items line %q: %v", line, err)
			}
			s.reasoningItems, haveItems = n, true
		}
	}
	if !haveStatus || !haveItems {
		t.Fatalf("stderr = %q, want the summary's status and reasoning items lines", stderr)
	}
	return s
}

// TestModels lists each role's endpoint and wants exactly one model ID, one
// line, as the one-model fallback of every other scenario needs.
func TestModels(t *testing.T) {
	for _, r := range roles {
		t.Run(string(r), func(t *testing.T) {
			res := runOn(t, r, nil, "models")

			wantCode(t, res, process.ExitOK)
			id, rest, found := strings.Cut(res.Stdout, "\n")
			if strings.TrimSpace(id) == "" || !found || rest != "" {
				t.Errorf("stdout = %q, want exactly one model ID line", res.Stdout)
			}
			if res.Stderr != "" {
				t.Errorf("stderr = %q, want empty", res.Stderr)
			}
		})
	}
}
