package daemon_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

type r0DecisionOracle struct {
	Name                   string   `json:"name"`
	Success                bool     `json:"success"`
	CompletionReason       string   `json:"completion_reason"`
	NeedsAttention         bool     `json:"needs_attention"`
	OrderedCycleEvents     []string `json:"ordered_cycle_events"`
	LocalArchiveIterations []int    `json:"local_archive_iterations"`
}

type r0Oracle struct {
	Decisions []r0DecisionOracle `json:"decisions"`
}

func loadR0Oracle(t *testing.T) r0Oracle {
	t.Helper()
	body, err := os.ReadFile("../runloop/testdata/reviewloop_r0_oracle.json")
	if err != nil {
		t.Fatalf("read R0 review-loop oracle: %v", err)
	}
	var oracle r0Oracle
	if err := json.Unmarshal(body, &oracle); err != nil {
		t.Fatalf("decode R0 review-loop oracle: %v", err)
	}
	return oracle
}

// TestR0ReviewLoopDecisionOracle binds every decision-table row to the shipped
// review-loop entry point. The JSON is therefore a cross-package fixture, not a
// self-validating description: R1 must produce these same results, ordered
// externally visible events, and local archive placements.
func TestR0ReviewLoopDecisionOracle(t *testing.T) {
	wantNames := []string{
		"approve",
		"flagless-request-changes",
		"block",
		"actionable-request-changes-at-cap",
		"unchanged-head-after-request-changes",
		"typed-phase-error",
	}
	oracle := loadR0Oracle(t)
	gotNames := make([]string, 0, len(oracle.Decisions))
	seen := make(map[string]struct{}, len(oracle.Decisions))

	for _, row := range oracle.Decisions {
		row := row
		if _, duplicate := seen[row.Name]; duplicate {
			t.Fatalf("duplicate decision oracle row %q", row.Name)
		}
		seen[row.Name] = struct{}{}
		gotNames = append(gotNames, row.Name)

		t.Run(row.Name, func(t *testing.T) {
			t.Parallel()
			projectDir, wtPath, parentSHA := rlcFixtureSetup(t)
			scriptPath := r0DecisionScript(t, row.Name, wtPath)
			collector := &stubEventCollector{}
			deps := daemon.ExportedWorkLoopDeps(daemon.WorkLoopDepsParams{
				BrAdapter:           &stubBeadLedger{},
				Bus:                 collector,
				ProjectDir:          projectDir,
				HandlerBinary:       "/bin/sh",
				HandlerArgs:         []string{scriptPath},
				IntentLogDir:        filepath.Join(projectDir, ".harmonik", "beads-intents"),
				AdapterRegistry2:    NewSealedAdapterRegistryForTest(t),
				WorkflowModeDefault: core.WorkflowModeReviewLoop,
			})

			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			defer cancel()
			result := daemon.ExportedRunReviewLoop(
				ctx, deps, rlFixtureRunID(t), core.BeadID("r0-"+row.Name),
				wtPath, parentSHA,
			)

			if result.Success != row.Success ||
				result.CompletionReason != row.CompletionReason ||
				result.NeedsAttention != row.NeedsAttention {
				t.Errorf("result = {success:%t completion_reason:%q needs_attention:%t}; want {%t %q %t}; summary=%q",
					result.Success, result.CompletionReason, result.NeedsAttention,
					row.Success, row.CompletionReason, row.NeedsAttention, result.Summary)
			}
			rlAssertEventSubsequence(t, collector.eventTypes(), row.OrderedCycleEvents)
			for _, iteration := range row.LocalArchiveIterations {
				archive := filepath.Join(wtPath, ".harmonik", fmt.Sprintf("review.iter-%d.json", iteration))
				if _, err := os.Stat(archive); err != nil {
					t.Errorf("local archive iteration %d missing at %s: %v", iteration, archive, err)
				}
			}
		})
	}
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("decision oracle rows = %v; want %v", gotNames, wantNames)
	}
}

func r0DecisionScript(t *testing.T, name, wtPath string) string {
	t.Helper()
	switch name {
	case "approve":
		return rlFixtureHandlerScript(t, wtPath, []string{"APPROVE"})
	case "flagless-request-changes":
		return r0FlaglessHandlerScript(t, wtPath)
	case "block":
		return rlFixtureHandlerScript(t, wtPath, []string{"BLOCK"})
	case "actionable-request-changes-at-cap":
		return rlFixtureHandlerScript(t, wtPath, []string{
			"REQUEST_CHANGES", "REQUEST_CHANGES", "REQUEST_CHANGES",
		})
	case "unchanged-head-after-request-changes":
		return rlcFixtureNoProgressScript(t, wtPath)
	case "typed-phase-error":
		return rlcFixtureMalformedVerdictScript(t, wtPath)
	default:
		t.Fatalf("no executable fixture for R0 decision row %q", name)
		return ""
	}
}

func r0FlaglessHandlerScript(t *testing.T, wtPath string) string {
	t.Helper()
	wtpEsc := strings.ReplaceAll(wtPath, "'", "'\\''")
	script := fmt.Sprintf(`#!/bin/sh
set -e
WTP='%s'
WS="${HARMONIK_WORKSPACE_PATH:-$WTP}"
CNT_FILE="$WTP/.harmonik/rl_count"
if [ ! -f "$CNT_FILE" ]; then printf '0' > "$CNT_FILE"; fi
CNT=$(cat "$CNT_FILE")
CNT=$((CNT + 1))
printf '%%d' "$CNT" > "$CNT_FILE"
if [ $((CNT %% 2)) -eq 0 ]; then
  mkdir -p "$WS/.harmonik"
  printf '{"schema_version":1,"verdict":"REQUEST_CHANGES","flags":[],"notes":"no actionable flags"}' > "$WS/.harmonik/review.json"
else
  printf '%%d' "$CNT" > "$WS/impl_iter_$CNT.txt"
  git -C "$WS" add "impl_iter_$CNT.txt" >/dev/null 2>&1
  git -C "$WS" -c user.email=test@harmonik.local -c user.name="Test" commit -m "impl iter $CNT" --no-gpg-sign >/dev/null 2>&1
fi
exit 0
`, wtpEsc)
	path := filepath.Join(t.TempDir(), "r0_flagless_handler.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write R0 flagless handler: %v", err)
	}
	return path
}

// TestR0CHBCheckpointRoundTripAndIdempotence executes the existing CHB-023
// durability seam: a successful return is recoverable from git, and repeating
// the same checkpoint is a no-op rather than another commit.
func TestR0CHBCheckpointRoundTripAndIdempotence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	runID := rl3FixtureRunID(t)
	rl3FixtureGitRepo(t, dir, runID.String())
	const sessionID = "r0-chb-checkpoint-session"

	sha, skipped, err := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, sessionID)
	if err != nil || skipped || sha == "" {
		t.Fatalf("first checkpoint = (sha=%q skipped=%t err=%v), want durable commit", sha, skipped, err)
	}
	if got := rl3FixtureReadContextFileFromGit(t, dir, runID); got != sessionID {
		t.Fatalf("recovered session ID = %q; want %q", got, sessionID)
	}
	secondSHA, secondSkipped, err := daemon.ExportedPersistClaudeSessionID(t.Context(), dir, runID, sessionID)
	if err != nil || !secondSkipped {
		t.Fatalf("repeat checkpoint = (sha=%q skipped=%t err=%v), want idempotent skip", secondSHA, secondSkipped, err)
	}
}

// TestR0HookSessionCloseRejectsLateArrival is the executable lifetime boundary
// available today: once a reviewer session is closed, a late hook event cannot
// re-enter the phase or update its outcome.
func TestR0HookSessionCloseRejectsLateArrival(t *testing.T) {
	t.Parallel()
	const runID, sessionID = "r0-run", "r0-reviewer"
	store := daemon.ExportedNewHookSessionStore()
	daemon.ExportedHookRegister(store, runID, sessionID)
	daemon.ExportedHookClose(store, runID, sessionID)

	status, _ := daemon.ExportedHookDispatch(store, daemon.HookRelayEnvelopeExported{
		Type:            "outcome_emitted",
		RunID:           runID,
		ClaudeSessionID: sessionID,
		Payload:         json.RawMessage(`{"kind":"WORK_COMPLETE","summary":"late"}`),
	})
	if status != "unknown_session" {
		t.Fatalf("late hook ACK status = %q; want unknown_session", status)
	}
	if got := daemon.ExportedHookLatestOutcome(store, runID, sessionID); got != nil {
		t.Fatalf("closed session retained late outcome: %s", string(*got))
	}
}

// TestR0TargetConformanceGaps keeps guarantees that lack a production seam
// separate from the green shipped oracle. Their owning slices remove the skip
// only after the required boundary exists.
func TestR0TargetConformanceGaps(t *testing.T) {
	t.Run("R2-checkpoint-crash-cut-and-ACK-runtime-order", func(t *testing.T) {
		t.Skip("R2: current tests can execute durable/idempotent persistence, but cannot crash between commit and version-selected ACK")
	})
	t.Run("R3-phase-close-is-joinable", func(t *testing.T) {
		t.Skip("R3: current review-loop uses deferred teardown and unjoinable auxiliary goroutines")
	})
	t.Run("R3-synthetic-heartbeat-is-liveness-not-work-progress", func(t *testing.T) {
		t.Skip("R3: WaitPostAgentReadyProgress currently treats every envelope, including daemon heartbeat, as progress")
	})
	t.Run("R4-archive-precedes-verdict-publication-and-cleanup", func(t *testing.T) {
		t.Skip("R4: current local path publishes reviewer_verdict before best-effort archive")
	})
	t.Run("R4-remote-review-artifacts-transfer-to-run-workspace", func(t *testing.T) {
		t.Skip("R4: current remote path skips run-workspace verdict archive")
	})
	t.Run("R5-f-event-error-blocks-terminal-effects", func(t *testing.T) {
		t.Skip("R5: current emitReviewLoopCycleComplete discards EmitWithRunID errors")
	})
}
