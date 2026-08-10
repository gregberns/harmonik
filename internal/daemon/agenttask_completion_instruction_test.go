package daemon

// agenttask_completion_instruction_test.go — the task file a one-shot harness
// receives must not ask it to run a slash command.
//
// Pi and codex have no REPL and no slash commands. Their process ends when the
// turn ends, and the Refs: trailer on the commit is what tells the daemon the
// work is done. The task file used to end with the claude instruction anyway —
// "You MUST run /quit as your final action" — because the section was rendered
// from a constant rather than from the launching harness.
//
// A pi agent read it. It had already done the task and committed correctly, and
// it obeyed with the only tool it has: `echo "/quit" | pbcopy`. The session
// stayed alive, the daemon killed it 158 seconds in, and the run was recorded as
// a structural crash whose reason named a claude crash on a run that never
// involved claude. The commit was stranded on run/<run_id>.
//
// The test drives the real launch-spec builder, not the renderer, because the
// renderer was never the whole defect: it also has to be told which harness it
// is rendering for. Asserting on the file the daemon actually wrote covers both
// halves.
//
// Bead: hk-quit-instruction-not-portable-ms55w.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/harness/codex"
	"github.com/gregberns/harmonik/internal/harness/pi"
	"github.com/gregberns/harmonik/internal/harness/shared"
	"github.com/gregberns/harmonik/internal/workspace"
)

func TestOneShotHarnessTaskFileNeverAsksForASlashCommand(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name      string
		agentType core.AgentType
		harness   func(t *testing.T) handlercontract.Harness
		rc        func(wt string) shared.LaunchCtx
	}{
		{
			name:      "codex",
			agentType: core.AgentTypeCodex,
			harness: func(*testing.T) handlercontract.Harness {
				return codex.NewHarness("", "")
			},
			rc: func(wt string) shared.LaunchCtx {
				return shared.LaunchCtx{
					BeadID:          "hk-completion-codex",
					WorkspacePath:   wt,
					Phase:           "implementer-initial",
					IterationCount:  1,
					BeadTitle:       "codex completion instruction",
					BeadDescription: "Do the work.",
					Model:           "o4-mini",
				}
			},
		},
		{
			name:      "pi",
			agentType: core.AgentTypePi,
			harness: func(t *testing.T) handlercontract.Harness {
				t.Setenv("OPENROUTER_API_KEY", "sk-test-completion")
				return pi.NewHarness("pi", "openrouter", "openrouter/qwen/qwen3-coder", "OPENROUTER_API_KEY", "", "", "")
			},
			rc: func(wt string) shared.LaunchCtx {
				return shared.LaunchCtx{
					BeadID:          "hk-completion-pi",
					WorkspacePath:   wt,
					Phase:           "implementer-initial",
					IterationCount:  1,
					BeadTitle:       "pi completion instruction",
					BeadDescription: "Do the work.",
					HandlerBinary:   "pi",
					Provider:        "openrouter",
					Model:           "openrouter/qwen/qwen3-coder",
					APIKeyEnv:       "OPENROUTER_API_KEY",
					BaseURL:         "http://dgx.local:8080/v1",
					API:             "openai",
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := tc.harness(t)
			if got := h.Completion(); got != handlercontract.CompletionProcessExit {
				t.Fatalf("%s Completion() = %v; want CompletionProcessExit — this test is about one-shot harnesses", tc.name, got)
			}

			wt := t.TempDir()
			if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o750); err != nil {
				t.Fatalf("mkdir .harmonik: %v", err)
			}
			rc := tc.rc(wt)
			rc.RunID = z8ekRunID(t)

			if _, _, err := buildCodexRoutedLaunchSpec(ctx, rc, h, tc.agentType); err != nil {
				t.Fatalf("buildCodexRoutedLaunchSpec(%s): %v", tc.name, err)
			}

			content, err := os.ReadFile(workspace.AgentTaskPath(wt))
			if err != nil {
				t.Fatalf("read agent-task.md: %v", err)
			}
			got := string(content)

			// The instruction the pi agent obeyed. It must not appear anywhere
			// in the file — not in the Session Completion section, and not in
			// the Bead Lifecycle section's one-line summary of the job.
			if strings.Contains(got, "/quit") {
				for i, line := range strings.Split(got, "\n") {
					if strings.Contains(line, "/quit") {
						t.Errorf("%s agent-task.md line %d instructs a slash command this harness does not have: %q",
							tc.name, i+1, line)
					}
				}
			}

			// The completion signal that IS real for these harnesses. Without
			// it the file would pass the check above by saying nothing at all
			// about how to finish.
			if !strings.Contains(got, "Refs: <bead-id>") {
				t.Errorf("%s agent-task.md never names the Refs: trailer as the completion signal", tc.name)
			}
		})
	}
}
