package main

// init_goalkeeper_schedule_hkz25w_test.go — unit tests for seedGoalKeeperSchedule
// (flywheel FW5, hk-z25w).
//
// Contract:
//   - seedGoalKeeperSchedule writes a "goal-keeper" ScheduledJob (every@1h,
//     command action, Enabled:true) to .harmonik/schedules.json.
//   - Idempotent: a second call with force=false skips and returns 0.
//   - force=true overwrites the existing job.
//   - The seeded Argv contains "--project <projectDir>".

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/schedule"
)

func TestSeedGoalKeeperSchedule_Seeds(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	code := seedGoalKeeperSchedule(dir, false, &out, &errBuf)
	if code != 0 {
		t.Fatalf("seedGoalKeeperSchedule returned %d; stderr: %s", code, errBuf.String())
	}

	store := schedule.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("load schedules: %v", err)
	}
	job, ok := store.Get("goal-keeper")
	if !ok {
		t.Fatal("goal-keeper job not found in schedules.json")
	}
	if job.Schedule.Kind != schedule.ScheduleKindEvery {
		t.Errorf("schedule kind = %q, want %q", job.Schedule.Kind, schedule.ScheduleKindEvery)
	}
	if job.Schedule.Interval == "" {
		t.Error("schedule interval is empty")
	}
	if job.Action.Kind != schedule.ActionKindCommand {
		t.Errorf("action kind = %q, want %q", job.Action.Kind, schedule.ActionKindCommand)
	}
	if len(job.Action.Argv) == 0 || job.Action.Argv[0] != "harmonik" {
		t.Errorf("action argv[0] = %q, want \"harmonik\"", firstOrEmpty(job.Action.Argv))
	}
	foundProject := false
	for i, arg := range job.Action.Argv {
		if arg == "--project" && i+1 < len(job.Action.Argv) && job.Action.Argv[i+1] == dir {
			foundProject = true
			break
		}
	}
	if !foundProject {
		t.Errorf("argv does not contain '--project %s'; got %v", dir, job.Action.Argv)
	}
	if !job.Enabled {
		t.Error("job.Enabled is false, want true")
	}
}

func TestSeedGoalKeeperSchedule_IdempotentSkip(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	if code := seedGoalKeeperSchedule(dir, false, &out, &errBuf); code != 0 {
		t.Fatalf("first call returned %d; stderr: %s", code, errBuf.String())
	}

	// Tamper with the job so we can verify the second call leaves it unchanged.
	store := schedule.NewStore(dir)
	_ = store.Load()
	original, _ := store.Get("goal-keeper")

	out.Reset()
	errBuf.Reset()
	if code := seedGoalKeeperSchedule(dir, false, &out, &errBuf); code != 0 {
		t.Fatalf("second call returned %d; stderr: %s", code, errBuf.String())
	}
	_ = store.Load()
	afterSkip, _ := store.Get("goal-keeper")
	if afterSkip.Schedule.Interval != original.Schedule.Interval {
		t.Errorf("job changed on idempotent skip: interval %q → %q", original.Schedule.Interval, afterSkip.Schedule.Interval)
	}
}

func TestSeedGoalKeeperSchedule_ForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	harmonikDir := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, 0o755); err != nil {
		t.Fatal(err)
	}

	var out, errBuf bytes.Buffer
	if code := seedGoalKeeperSchedule(dir, false, &out, &errBuf); code != 0 {
		t.Fatalf("first call: %d; stderr: %s", code, errBuf.String())
	}
	out.Reset()
	errBuf.Reset()
	// force=true must succeed (re-add / overwrite).
	if code := seedGoalKeeperSchedule(dir, true, &out, &errBuf); code != 0 {
		t.Fatalf("force call: %d; stderr: %s", code, errBuf.String())
	}
	store := schedule.NewStore(dir)
	_ = store.Load()
	if _, ok := store.Get("goal-keeper"); !ok {
		t.Fatal("goal-keeper job missing after force overwrite")
	}
}

func firstOrEmpty(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
