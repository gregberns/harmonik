package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/schedule"
)

func captureSchedIO(t *testing.T, fn func() int) (out string, code int) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	code = fn()
	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	bOut, _ := io.ReadAll(rOut)
	bErr, _ := io.ReadAll(rErr)
	_ = rOut.Close()
	_ = rErr.Close()
	return string(bOut) + string(bErr), code
}

func TestRunScheduleSubcommand_Routing(t *testing.T) {
	tests := []struct {
		name     string
		subArgs  []string
		wantCode int
		wantSub  string
	}{
		{"empty prints usage", nil, 0, "harmonik schedule"},
		{"--help prints usage", []string{"--help"}, 0, "VERBS"},
		{"-h prints usage", []string{"-h"}, 0, "VERBS"},
		{"unknown verb is exit 2", []string{"frobnicate"}, 2, "unrecognised verb"},
		{"add with no flags is a plain arg error, not a routing error", []string{"add"}, 1, "--id is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, code := captureSchedIO(t, func() int { return runScheduleSubcommand(tc.subArgs) })
			if code != tc.wantCode {
				t.Fatalf("code = %d, want %d (out=%q)", code, tc.wantCode, out)
			}
			if !strings.Contains(out, tc.wantSub) {
				t.Errorf("output %q must contain %q", out, tc.wantSub)
			}
		})
	}
}

func TestResolveScheduleStore(t *testing.T) {
	dir := t.TempDir()
	store, code := resolveScheduleStore(dir)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if store == nil {
		t.Fatal("store is nil on success")
	}
	if got := store.List(); len(got) != 0 {
		t.Errorf("fresh store List() = %d jobs, want 0", len(got))
	}
}

func TestRunScheduleAdd_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"missing id", []string{"--schedule", "every@5m", "--action", "command", "--", "echo"}, "--id is required"},
		{"missing schedule", []string{"--id", "j", "--action", "command", "--", "echo"}, "--schedule is required"},
		{"malformed schedule", []string{"--id", "j", "--schedule", "weekly@x", "--action", "command", "--", "echo"}, "unsupported schedule kind"},
		{"command action with no argv", []string{"--id", "j", "--schedule", "every@5m", "--action", "command"}, "requires `-- <argv...>`"},
		{"spawn-crew missing crew and queue", []string{"--id", "j", "--schedule", "every@5m", "--action", "spawn-crew"}, "requires --crew and --queue"},
		{"unknown action kind", []string{"--id", "j", "--schedule", "every@5m", "--action", "teleport"}, "--action must be"},
		{"bad overlap policy", []string{"--id", "j", "--schedule", "every@5m", "--action", "command", "--overlap-policy", "maybe", "--", "echo"}, "--overlap-policy must be"},
		{"bad catchup", []string{"--id", "j", "--schedule", "every@5m", "--action", "command", "--catchup", "sometimes", "--", "echo"}, "--catchup must be"},
		{"bad catchup-window duration", []string{"--id", "j", "--schedule", "every@5m", "--action", "command", "--catchup-window", "soon", "--", "echo"}, "is not a valid duration"},
		{"unexpected argument", []string{"--id", "j", "--bogus"}, "unexpected argument"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out, code := captureSchedIO(t, func() int { return runScheduleAdd(tc.args) })
			if code != 1 {
				t.Fatalf("code = %d, want 1 (out=%q)", code, out)
			}
			if !strings.Contains(out, tc.wantSub) {
				t.Errorf("output %q must contain %q", out, tc.wantSub)
			}
		})
	}
}

func TestRunScheduleAdd_Command_Persists(t *testing.T) {
	dir := t.TempDir()
	args := []string{
		"--id", "rotate", "--schedule", "every@5m", "--action", "command",
		"--overlap-policy", "skip", "--catchup", "off", "--catchup-window", "1h",
		"--project", dir, "--", "echo", "hello",
	}
	out, code := captureSchedIO(t, func() int { return runScheduleAdd(args) })
	if code != 0 {
		t.Fatalf("code = %d, want 0 (out=%q)", code, out)
	}
	if !strings.Contains(out, "added: rotate (every@5m, action=command, enabled)") {
		t.Errorf("unexpected add output %q", out)
	}
	store, c := resolveScheduleStore(dir)
	if c != 0 {
		t.Fatalf("resolve after add: code %d", c)
	}
	j, ok := store.Get("rotate")
	if !ok {
		t.Fatal("job rotate not persisted")
	}
	if j.Action.Kind != schedule.ActionKindCommand || strings.Join(j.Action.Argv, " ") != "echo hello" {
		t.Errorf("persisted action = %+v, want command echo hello", j.Action)
	}
	if !j.Enabled {
		t.Error("newly added job should be enabled")
	}
}

func TestRunScheduleAdd_SpawnCrew_Persists(t *testing.T) {
	dir := t.TempDir()
	args := []string{
		"--id", "nightly", "--schedule", "daily@02:00 UTC", "--action", "spawn-crew",
		"--crew", "owl", "--queue", "night", "--mission", "/tmp/m.md", "--project", dir,
	}
	out, code := captureSchedIO(t, func() int { return runScheduleAdd(args) })
	if code != 0 {
		t.Fatalf("code = %d, want 0 (out=%q)", code, out)
	}
	if !strings.Contains(out, "added: nightly (daily@02:00 UTC, action=spawn-crew, enabled)") {
		t.Errorf("unexpected add output %q", out)
	}
	store, _ := resolveScheduleStore(dir)
	j, ok := store.Get("nightly")
	if !ok {
		t.Fatal("job nightly not persisted")
	}
	if j.Action.Crew != "owl" || j.Action.Queue != "night" || j.Action.Mission != "/tmp/m.md" {
		t.Errorf("persisted spawn-crew action = %+v", j.Action)
	}
}

func seedJob(t *testing.T, dir, id string) {
	t.Helper()
	args := []string{"--id", id, "--schedule", "every@5m", "--action", "command", "--project", dir, "--", "echo", id}
	_, code := captureSchedIO(t, func() int { return runScheduleAdd(args) })
	if code != 0 {
		t.Fatalf("seed %q failed with code %d", id, code)
	}
}

func TestRunScheduleList(t *testing.T) {
	dir := t.TempDir()

	out, code := captureSchedIO(t, func() int { return runScheduleList([]string{"--project", dir}) })
	if code != 0 || !strings.Contains(out, "(no scheduled jobs)") {
		t.Fatalf("empty list: code=%d out=%q", code, out)
	}

	seedJob(t, dir, "alpha")

	out, code = captureSchedIO(t, func() int { return runScheduleList([]string{"--project", dir}) })
	if code != 0 || !strings.Contains(out, "alpha") || !strings.Contains(out, "command: echo alpha") {
		t.Fatalf("human list: code=%d out=%q", code, out)
	}

	out, code = captureSchedIO(t, func() int { return runScheduleList([]string{"--json", "--project", dir}) })
	if code != 0 || !strings.Contains(out, `"id":"alpha"`) {
		t.Fatalf("json list: code=%d out=%q", code, out)
	}

	out, code = captureSchedIO(t, func() int { return runScheduleList([]string{"--bogus"}) })
	if code != 1 || !strings.Contains(out, "unexpected argument") {
		t.Fatalf("bad-arg list: code=%d out=%q", code, out)
	}
}

func TestRunScheduleRemove(t *testing.T) {
	dir := t.TempDir()

	out, code := captureSchedIO(t, func() int { return runScheduleRemove([]string{"ghost", "--project", dir}) })
	if code != 1 || !strings.Contains(out, "no such job \"ghost\"") {
		t.Fatalf("remove absent: code=%d out=%q", code, out)
	}

	seedJob(t, dir, "beta")
	out, code = captureSchedIO(t, func() int { return runScheduleRemove([]string{"beta", "--project", dir}) })
	if code != 0 || !strings.Contains(out, "removed: beta") {
		t.Fatalf("remove present: code=%d out=%q", code, out)
	}
	store, _ := resolveScheduleStore(dir)
	if _, ok := store.Get("beta"); ok {
		t.Error("job beta still present after remove")
	}
}

func TestRunScheduleEnableDisable(t *testing.T) {
	dir := t.TempDir()
	seedJob(t, dir, "gamma")

	out, code := captureSchedIO(t, func() int { return runScheduleEnableDisable([]string{"gamma", "--project", dir}, false) })
	if code != 0 || !strings.Contains(out, "disabled: gamma") {
		t.Fatalf("disable: code=%d out=%q", code, out)
	}
	store, _ := resolveScheduleStore(dir)
	if j, _ := store.Get("gamma"); j.Enabled {
		t.Error("job should be disabled after disable")
	}

	out, code = captureSchedIO(t, func() int { return runScheduleEnableDisable([]string{"gamma", "--project", dir}, true) })
	if code != 0 || !strings.Contains(out, "enabled: gamma") {
		t.Fatalf("enable: code=%d out=%q", code, out)
	}
	store, _ = resolveScheduleStore(dir)
	if j, _ := store.Get("gamma"); !j.Enabled {
		t.Error("job should be enabled after enable")
	}

	out, code = captureSchedIO(t, func() int { return runScheduleEnableDisable([]string{"ghost", "--project", dir}, true) })
	if code != 1 || !strings.Contains(out, "no such job \"ghost\"") {
		t.Fatalf("enable absent: code=%d out=%q", code, out)
	}
}

func TestRunScheduleRunNow(t *testing.T) {
	dir := t.TempDir()

	out, code := captureSchedIO(t, func() int { return runScheduleRunNow([]string{"ghost", "--project", dir}) })
	if code != 1 || !strings.Contains(out, "no such job \"ghost\"") {
		t.Fatalf("run-now absent: code=%d out=%q", code, out)
	}

	seedJob(t, dir, "delta")
	out, code = captureSchedIO(t, func() int { return runScheduleRunNow([]string{"delta", "--project", dir}) })
	if code != 0 || !strings.Contains(out, "run-now requested: delta") {
		t.Fatalf("run-now present: code=%d out=%q", code, out)
	}
}

func TestScheduleUsage(t *testing.T) {
	out, code := captureSchedIO(t, func() int { scheduleUsage(); return 0 })
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, want := range []string{"USAGE", "VERBS", "add", "run-now", "EXIT CODES"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage text missing %q", want)
		}
	}
}
