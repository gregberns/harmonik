package daemon

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

			if strings.Contains(got, "/quit") {
				for i, line := range strings.Split(got, "\n") {
					if strings.Contains(line, "/quit") {
						t.Errorf("%s agent-task.md line %d instructs a slash command this harness does not have: %q",
							tc.name, i+1, line)
					}
				}
			}

			if !strings.Contains(got, "Refs: <bead-id>") {
				t.Errorf("%s agent-task.md never names the Refs: trailer as the completion signal", tc.name)
			}
		})
	}
}
