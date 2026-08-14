package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/queue"
)

func TestPreflightDispatchReplayAllowsEmptyAuthorityWithoutReader(t *testing.T) {
	steps, err := preflightDispatchReplayWithReader(t.Context(), nil, nil)
	if err != nil {
		t.Fatalf("empty replay preflight = %v", err)
	}
	if len(steps) != 0 {
		t.Fatalf("empty replay preflight steps = %v", steps)
	}
}

func TestPreflightDispatchReplayReturnsActionForExecutor(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		intent.Binding.RunID.String(): replayPlanPreparedFacts(intent, dispatch.QueueOfferable),
	}}
	steps, err := preflightDispatchReplayWithReader(t.Context(), []dispatch.Intent{intent}, reader)
	if err != nil {
		t.Fatalf("replay preflight error = %v", err)
	}
	if len(steps) != 1 || steps[0].Action != dispatch.ReplayReservation {
		t.Fatalf("replay preflight steps = %+v", steps)
	}
}

func TestPreflightDispatchReplayFailsClosedBeforeExecutorOnRepair(t *testing.T) {
	intent := replayOwnershipIntent(t, dispatch.PhasePrepared)
	reader := &replayPlanFactReader{facts: map[string]dispatch.ReplayFacts{
		intent.Binding.RunID.String(): replayPlanPreparedFacts(intent, dispatch.QueueConflict),
	}}
	_, err := preflightDispatchReplayWithReader(t.Context(), []dispatch.Intent{intent}, reader)
	if err == nil || !strings.Contains(err.Error(), "requires repair") {
		t.Fatalf("replay repair preflight error = %v", err)
	}
}

func TestStartupReconcileReplaysReservationBeforeOrphanSweep(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "durable progress") {
		t.Fatalf("runStartupReconcile() error = %v", err)
	}
	assertReplayReservationQueue(t, projectDir, intent, 1)
	calls, readErr := os.ReadFile(callsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(calls), " list ") || strings.Contains(string(calls), "list --") {
		t.Fatalf("orphan sweep reached Beads list before replay refusal:\n%s", calls)
	}
}

func TestExecuteDispatchReplayPlanRejectsUnsupportedWaveBeforeWrite(t *testing.T) {
	projectDir := t.TempDir()
	first := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second := replayOwnershipIntent(t, dispatch.PhasePrepared)
	second.Binding.RunID[15]++
	writeReplayReaderQueue(t, projectDir, first, false)
	steps := []dispatchReplayStep{
		{Intent: first, Action: dispatch.ReplayReservation},
		{Intent: second, Action: dispatch.ReplayClaim},
	}
	err := executeDispatchReplayPlan(t.Context(), steps, dispatchReplayExecutor{projectDir: projectDir})
	if err == nil || !strings.Contains(err.Error(), "does not support action") {
		t.Fatalf("execute replay plan error = %v", err)
	}
	durable, loadErr := queue.Load(t.Context(), projectDir, first.Binding.QueueName)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if item := durable.Groups[0].Items[0]; item.Status != queue.ItemStatusPending || item.RunID != nil || item.Attempts != 0 {
		t.Fatalf("unsupported wave changed queue item = %+v", item)
	}
}
