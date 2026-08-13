package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dispatch"
)

func TestPreflightDispatchReplayAllowsEmptyAuthorityWithoutReader(t *testing.T) {
	if err := preflightDispatchReplayWithReader(t.Context(), nil, nil); err != nil {
		t.Fatalf("empty replay preflight = %v", err)
	}
}

func TestPreflightDispatchReplayRefusesActionBeforeExecutorExists(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		intent.Binding.RunID.String(): replayPlanPreparedFacts(intent, dispatch.QueueOfferable),
	}}
	err := preflightDispatchReplayWithReader(t.Context(), []dispatch.Intent{intent}, reader)
	if err == nil || !strings.Contains(err.Error(), "executor is not configured for 1 action") {
		t.Fatalf("replay preflight error = %v", err)
	}
}

func TestPreflightDispatchReplayFailsClosedBeforeExecutorOnRepair(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		intent.Binding.RunID.String(): replayPlanPreparedFacts(intent, dispatch.QueueConflict),
	}}
	err := preflightDispatchReplayWithReader(t.Context(), []dispatch.Intent{intent}, reader)
	if err == nil || !strings.Contains(err.Error(), "requires repair") {
		t.Fatalf("replay repair preflight error = %v", err)
	}
}

func TestStartupReconcileRefusesActionableReplayBeforeOrphanSweep(t *testing.T) {
	projectDir := t.TempDir()
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	writeReplayReaderIntent(t, projectDir, intent)
	writeReplayReaderQueue(t, projectDir, intent, false)
	brPath := filepath.Join(t.TempDir(), "br")
	callsPath := filepath.Join(t.TempDir(), "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> '` + callsPath + `'
if [ "$1" = "--version" ]; then
  echo "br test"
  exit 0
fi
printf '%s\n' '[{"id":"hk-replay-owner","title":"replay","issue_type":"task","status":"open"}]'
`
	if err := os.WriteFile(brPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	bs := &bootState{cfg: Config{ProjectDir: projectDir, BrPath: brPath}}
	err := bs.runStartupReconcile(t.Context(), time.Now(), "main")
	if err == nil || !strings.Contains(err.Error(), "executor is not configured for 1 action") {
		t.Fatalf("runStartupReconcile() error = %v", err)
	}
	calls, readErr := os.ReadFile(callsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(calls), " list ") || strings.Contains(string(calls), "list --") {
		t.Fatalf("orphan sweep reached Beads list before replay refusal:\n%s", calls)
	}
}
