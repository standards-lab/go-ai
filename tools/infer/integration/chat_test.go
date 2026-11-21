//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
)

func TestChat(t *testing.T) {
	eachChat(t, func(t *testing.T, formatEnv, streamFlag string) {
		res := runOn(t, agent, []string{formatEnv}, "chat", streamFlag,
			"--instructions", "Answer in one word.", "--reasoning", "low", "--max-tokens", answerBound,
			"What is the capital of France?")

		wantCode(t, res, process.ExitOK)
		if !strings.Contains(strings.ToLower(res.Stdout), "paris") {
			t.Errorf("stdout = %q, want the answer naming Paris", res.Stdout)
		}
		s := chatSummary(t, res.Stderr)
		if s.status != "completed" {
			t.Errorf("status = %q, want completed", s.status)
		}
		if s.reasoningItems < 1 {
			t.Errorf("reasoning items = %d, want at least 1 at --reasoning low", s.reasoningItems)
		}
	})
}

func TestChatIncompleteAtMaxTokens(t *testing.T) {
	eachChat(t, func(t *testing.T, formatEnv, streamFlag string) {
		res := runOn(t, agent, []string{formatEnv}, "chat", streamFlag, "--max-tokens", "16",
			"Write a long essay on the history of Paris.")

		wantCode(t, res, process.ExitFailure)
		if s := chatSummary(t, res.Stderr); s.status != "incomplete" {
			t.Errorf("status = %q, want incomplete", s.status)
		}
		if want := "infer chat: incomplete: max_output_tokens\n"; !strings.HasSuffix(res.Stderr, want) {
			t.Errorf("stderr = %q, want it to end with %q", res.Stderr, want)
		}
	})
}

// See docs/design.md, "Implementation notes".
func TestChatReasoningEffort(t *testing.T) {
	eachChat(t, func(t *testing.T, formatEnv, streamFlag string) {
		noneRan := false
		for _, c := range []struct {
			effort  string
			atLeast int
			atMost  int
		}{
			{effort: "none", atLeast: 0, atMost: 0},
			{effort: "low", atLeast: 1, atMost: -1},
		} {
			t.Run(c.effort, func(t *testing.T) {
				res := runOn(t, vision, []string{formatEnv}, "chat", streamFlag,
					"--instructions", "Answer in one word.", "--reasoning", c.effort, "--max-tokens", answerBound,
					"What is the capital of France?")
				if c.effort == "none" {
					noneRan = true
				}
				atLeast := c.atLeast
				if c.effort == "low" && !noneRan {
					atLeast = 0
				}

				wantCode(t, res, process.ExitOK)
				if !strings.Contains(strings.ToLower(res.Stdout), "paris") {
					t.Errorf("stdout = %q, want the answer naming Paris", res.Stdout)
				}
				s := chatSummary(t, res.Stderr)
				if s.status != "completed" {
					t.Errorf("status = %q, want completed", s.status)
				}
				if s.reasoningItems < atLeast || (c.atMost >= 0 && s.reasoningItems > c.atMost) {
					t.Errorf("reasoning items = %d at --reasoning %s, want %d to %d (-1: no bound)",
						s.reasoningItems, c.effort, atLeast, c.atMost)
				}
			})
		}
	})
}
