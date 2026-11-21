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

type role string

const (
	agent         role = "AI_AGENT_BASE_URL"
	vision        role = "AI_VISION_BASE_URL"
	audio         role = "AI_AUDIO_BASE_URL"
	embedding     role = "AI_EMBEDDING_BASE_URL"
	transcription role = "AI_TRANSCRIPTION_BASE_URL"
)

var roles = []role{agent, vision, audio, embedding, transcription}

var formats = []string{"completions", "messages", "responses"}

const answerBound = "1024"

func runOn(t *testing.T, r role, env []string, args ...string) processtest.Result {
	t.Helper()
	res := integration.RunEnv(t, append(integration.Endpoint(t, string(r)), env...), args...)
	integration.SkipDeclared(t, string(r), res)
	return res
}

func eachFormat(t *testing.T, scenario func(t *testing.T, formatEnv string)) {
	t.Helper()
	for _, f := range formats {
		t.Run(f, func(t *testing.T) { scenario(t, "INFER_FORMAT="+f) })
	}
}

func eachChat(t *testing.T, scenario func(t *testing.T, formatEnv, streamFlag string)) {
	t.Helper()
	eachFormat(t, func(t *testing.T, formatEnv string) {
		for _, stream := range []bool{true, false} {
			flag := fmt.Sprintf("--stream=%t", stream)
			t.Run(strings.TrimPrefix(flag, "--"), func(t *testing.T) { scenario(t, formatEnv, flag) })
		}
	})
}

func wantCode(t *testing.T, res processtest.Result, code int) {
	t.Helper()
	if res.Code != code {
		t.Fatalf("code = %d, want %d; stdout = %q, stderr = %q", res.Code, code, res.Stdout, res.Stderr)
	}
}

type summary struct {
	status         string
	reasoningItems int
}

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

func TestModels(t *testing.T) {
	for _, r := range roles {
		t.Run(string(r), func(t *testing.T) {
			res := runOn(t, r, nil, "models")

			wantCode(t, res, process.ExitOK)
			id, rest, found := strings.Cut(res.Stdout, "\n")
			switch {
			case strings.TrimSpace(id) == "" || !found:
				t.Errorf("stdout = %q, want a model ID line", res.Stdout)
			case rest != "" && integration.Model(string(r)) == "":
				t.Errorf("stdout = %q, want exactly one model ID line", res.Stdout)
			}
			if res.Stderr != "" {
				t.Errorf("stderr = %q, want empty", res.Stderr)
			}
		})
	}
}
