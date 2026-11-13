//go:build integration

package integration_test

import (
	"strings"
	"testing"

	"github.com/standards-lab/go-core/process"
)

// TestChat asks the agent a one-word question with a reasoning effort, in
// each format, streamed and unstreamed, and wants the answer on stdout, a
// completed summary on stderr, and the reply's reasoning counted there. The
// agent's model always reasons, so TestChatReasoningEffort, not this, shows
// an effort reaching the model.
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

// TestChatIncompleteAtMaxTokens asks the agent for a long answer under
// --max-tokens 16, go-ai's least bound, in each format, streamed and
// unstreamed, and wants the reply incomplete for max_output_tokens: exit 1,
// the summary's status incomplete, and the error naming the reason, last on
// stderr. The bound reaching the model is what ends the reply early.
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

// TestChatReasoningEffort asks TestChat's question at --reasoning none and
// at --reasoning low, in each format, streamed and unstreamed, and wants the
// answer each time, no reasoning item at none, and at least one at low, so
// the zero shows the effort reached the model rather than a model that
// never reasons.
//
// It runs on the vision endpoint, not the agent's: the agent's model,
// gpt-oss-120b, speaks Harmony, which always reasons. vLLM 0.33.0 refuses
// its reasoning_effort none with a 400 through Chat Completions and Open
// Responses, and through Messages takes thinking {"type":"disabled"} and
// reasons anyway. The vision model, Gemma 4, honors the effort in all three
// formats.
func TestChatReasoningEffort(t *testing.T) {
	eachChat(t, func(t *testing.T, formatEnv, streamFlag string) {
		for _, c := range []struct {
			effort  string
			atLeast int // the fewest reasoning items wanted
			atMost  int // the most, or -1 for no bound
		}{
			{effort: "none", atLeast: 0, atMost: 0},
			{effort: "low", atLeast: 1, atMost: -1},
		} {
			t.Run(c.effort, func(t *testing.T) {
				res := runOn(t, vision, []string{formatEnv}, "chat", streamFlag,
					"--instructions", "Answer in one word.", "--reasoning", c.effort, "--max-tokens", answerBound,
					"What is the capital of France?")

				wantCode(t, res, process.ExitOK)
				if !strings.Contains(strings.ToLower(res.Stdout), "paris") {
					t.Errorf("stdout = %q, want the answer naming Paris", res.Stdout)
				}
				s := chatSummary(t, res.Stderr)
				if s.status != "completed" {
					t.Errorf("status = %q, want completed", s.status)
				}
				if s.reasoningItems < c.atLeast || (c.atMost >= 0 && s.reasoningItems > c.atMost) {
					t.Errorf("reasoning items = %d at --reasoning %s, want %d to %d (-1: no bound)",
						s.reasoningItems, c.effort, c.atLeast, c.atMost)
				}
			})
		}
	})
}
