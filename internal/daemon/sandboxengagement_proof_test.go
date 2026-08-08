package daemon_test

// sandboxengagement_proof_test.go — behavioural pin on the srt sandbox
// engagement gate (sandboxgate.go verifySandboxEngaged, srtEngagementCanaryPath).
//
// The gate exists because srt's own exit code is not evidence the sandbox
// applied: under fork saturation sandbox_init can silently fail while srt still
// exits 0. The gate therefore runs a canary probe and demands TWO independent
// signals before it lets a real agent launch:
//
//   - srt itself reported a non-zero exit, AND
//   - the canary write never reached disk.
//
// Either signal alone is the shape of "srt exited but the sandbox never
// applied", and the caller must treat a non-nil return as fatal.
//
// Both functions were mutated on 2026-08-04 and the whole suite stayed green.
// verifySandboxEngaged was made to always report the sandbox engaged, which is
// the gate failing OPEN — an agent runs unsandboxed and nothing sees it. These
// tests drive the four combinations of the two signals through a stub srt
// binary, so the gate cannot fail open again without one of them going red.
//
// The stub is a shell script, so the tests need no macOS Seatbelt, no real srt,
// and no fork-saturation timing. spawn.SrtBinary is honored by the production
// code for exactly this reason.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// engagementProbeStub writes an executable stand-in for the srt binary and
// returns its path. The production probe invokes it as
//
//	<stub> --settings <profilePath> -c <script>
//
// so the script the sandbox is supposed to deny arrives in $4. A stub that runs
// $4 models a sandbox that never applied. A stub that does not run it models a
// sandbox that denied the write.
func engagementProbeStub(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "srt-stub")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil { //nolint:gosec // G306: the stub must be executable
		t.Fatalf("write srt stub: %v", err)
	}
	return path
}

// engagementFixture builds a valid spawn config plus the canary path the probe
// will try to write. The profile input carries the four required fields, so a
// refusal in these tests always comes from the engagement decision and never
// from profile generation.
func engagementFixture(t *testing.T, stubBody string) (spawn *daemon.ExportedSrtSpawnConfig, attemptCounterPath string) {
	t.Helper()
	projectDir := t.TempDir()
	worktree := filepath.Join(projectDir, ".harmonik", "worktrees", "run-a")
	if err := os.MkdirAll(worktree, 0o700); err != nil {
		t.Fatalf("make worktree: %v", err)
	}
	runID := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	spawn = &daemon.ExportedSrtSpawnConfig{
		SrtBinary: engagementProbeStub(t, stubBody),
		ProfileInput: daemon.SandboxProfileInput{
			WorktreePath:   worktree,
			GitDir:         filepath.Join(projectDir, ".git"),
			RunID:          runID,
			DaemonSockPath: filepath.Join(projectDir, ".harmonik", "daemon.sock"),
		},
	}
	return spawn, daemon.ExportedSrtEngagementCanaryPath(projectDir, runID)
}

// Stub bodies, one per combination of the two signals the gate reads.
const (
	// The sandbox denied the canary write and srt reported the failure. This is
	// the only combination that proves engagement.
	stubDeniedWriteAndFailed = `echo "sandbox_init: deny file-write-create" >&2
exit 1`

	// The sandbox never applied: the child ran unsandboxed, the canary write
	// landed, and srt still exited 0.
	stubWroteAndExitedZero = `sh -c "$4"
exit 0`

	// The canary write landed but srt reported a non-zero exit. Trusting srt's
	// exit code alone reads this as engaged, which is the failure the gate was
	// written to stop.
	stubWroteButExitedNonZero = `sh -c "$4"
exit 1`

	// Nothing was written but srt exited 0. Trusting the canary alone reads this
	// as engaged, and it is just as unproven.
	stubWroteNothingAndExitedZero = `exit 0`
)

// TestSandboxEngagement_ADeniedWriteAndAFailedProbeAreTheOnlyProofOfEngagement
// is the positive control. Without it every refusal test below is vacuous: a
// gate that refused everything would satisfy them all.
func TestSandboxEngagement_ADeniedWriteAndAFailedProbeAreTheOnlyProofOfEngagement(t *testing.T) {
	t.Parallel()

	spawn, canary := engagementFixture(t, stubDeniedWriteAndFailed)

	if err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canary, nil); err != nil {
		t.Fatalf("verify = %v, want nil.\n"+
			"The probe saw the canary write denied AND srt report the failure. That is the sandbox working. "+
			"A gate that refuses this refuses every sandboxed run, so no run would ever launch.", err)
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Errorf("canary %s survived the probe (stat err = %v), want it removed — "+
			"a leaked canary in the project root taints the next run's reading", canary, err)
	}
}

// TestSandboxEngagement_AWriteThatReachesDiskIsRefusedEvenWhenSrtReportsFailure
// pins the half of the rule that srt's exit code cannot supply. srt exiting
// non-zero says the process ended badly. It does not say the sandbox applied.
// The canary landing on disk says it did not, and that outranks the exit code.
func TestSandboxEngagement_AWriteThatReachesDiskIsRefusedEvenWhenSrtReportsFailure(t *testing.T) {
	t.Parallel()

	spawn, canary := engagementFixture(t, stubWroteButExitedNonZero)

	err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canary, nil)
	if err == nil {
		t.Fatalf("verify = nil, want a refusal.\n"+
			"The probe's write reached disk at %s, so the sandbox did not isolate the child. "+
			"srt's non-zero exit is not evidence of engagement — reading it alone lets a real agent launch unsandboxed.", canary)
	}
	if _, statErr := os.Stat(canary); !os.IsNotExist(statErr) {
		t.Errorf("canary %s survived the probe (stat err = %v), want it removed after every attempt", canary, statErr)
	}
}

// TestSandboxEngagement_ASilentExitZeroIsRefusedEvenWhenNothingWasWritten pins
// the other half. A probe that wrote nothing proves nothing on its own: srt
// exiting 0 with no denial is exactly what a child that never ran looks like.
func TestSandboxEngagement_ASilentExitZeroIsRefusedEvenWhenNothingWasWritten(t *testing.T) {
	t.Parallel()

	spawn, canary := engagementFixture(t, stubWroteNothingAndExitedZero)

	if err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canary, nil); err == nil {
		t.Fatal("verify = nil, want a refusal.\n" +
			"srt exited 0 and reported no denial. Reading the absent canary alone treats a probe that never ran as a sandbox that engaged.")
	}
}

// TestSandboxEngagement_AnUnsandboxedProbeThatWritesAndExitsZeroIsRefused is the
// plain apply failure the gate was written for: srt exits 0, the child ran
// unsandboxed, and the write the sandbox had to deny reached disk.
func TestSandboxEngagement_AnUnsandboxedProbeThatWritesAndExitsZeroIsRefused(t *testing.T) {
	t.Parallel()

	spawn, canary := engagementFixture(t, stubWroteAndExitedZero)

	var logged []string
	logf := func(format string, args ...any) { logged = append(logged, format) }

	if err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canary, logf); err == nil {
		t.Fatal("verify = nil, want a refusal — srt exited 0 and the child's write landed, so no sandbox was applied")
	}
	if len(logged) == 0 {
		t.Error("the probe logged nothing across the whole retry budget; the operator gets no account of why the launch was refused")
	}
}

// TestSandboxEngagement_AOneOffApplyFailureIsRetriedRatherThanTreatedAsFatal
// pins the retry budget. A single apply failure under host saturation is
// transient and must not fail a launch, so the probe samples more than once. A
// budget of one would make the gate flake red under the exact host conditions
// it was written for.
func TestSandboxEngagement_AOneOffApplyFailureIsRetriedRatherThanTreatedAsFatal(t *testing.T) {
	t.Parallel()

	counter := filepath.Join(t.TempDir(), "attempts")
	// Attempt one fails open. Every later attempt shows a working sandbox.
	body := `counter='` + counter + `'
n=$(cat "$counter" 2>/dev/null || echo 0)
n=$((n+1))
printf '%s' "$n" > "$counter"
if [ "$n" -eq 1 ]; then sh -c "$4"; exit 0; fi
exit 1`

	spawn, canary := engagementFixture(t, body)

	if err := daemon.ExportedVerifySandboxEngaged(context.Background(), spawn, canary, nil); err != nil {
		t.Fatalf("verify = %v, want nil — one transient apply failure must be retried, not treated as fatal", err)
	}
	got, readErr := os.ReadFile(counter) //nolint:gosec // G304: counter is t.TempDir-derived
	if readErr != nil {
		t.Fatalf("read attempt counter: %v", readErr)
	}
	if string(got) == "1" {
		t.Errorf("the probe ran %s attempt, want more than one — a single sample cannot tell a transient apply failure from a real one", got)
	}
}

// TestSrtEngagementCanaryPath_TheProbeWritesDirectlyUnderTheProjectRoot pins
// WHERE the probe writes. The path only proves anything because the profile
// never grants write access to it. The profile's write set covers the run's
// worktree and specific git-internal paths, never the project root itself, so a
// file sitting directly under the project root is guaranteed to be denied.
func TestSrtEngagementCanaryPath_TheProbeWritesDirectlyUnderTheProjectRoot(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	got := daemon.ExportedSrtEngagementCanaryPath(projectDir, "run-a")

	if got == "" {
		t.Fatal("canary path is empty; the probe would write to the current directory, and a write that lands anywhere the sandbox permits proves nothing")
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("canary path %q is relative; the probe resolves it against whatever directory srt runs in", got)
	}
	if dir := filepath.Dir(got); dir != projectDir {
		t.Errorf("canary path %q sits in %q, want it directly under the project root %q.\n"+
			"Anywhere deeper risks landing inside the worktree, which the profile DOES grant write access to. "+
			"A denied write there would be indistinguishable from a granted one.", got, dir, projectDir)
	}
}

// TestSrtEngagementCanaryPath_TwoConcurrentRunsProbeSeparateFiles pins the run
// identity in the name. Two runs starting together share the project root. One
// run's leaked canary read as another run's proof of a failed sandbox would
// refuse a launch that was fine, or worse, clear one that was not.
func TestSrtEngagementCanaryPath_TwoConcurrentRunsProbeSeparateFiles(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	first := daemon.ExportedSrtEngagementCanaryPath(projectDir, "run-a")
	second := daemon.ExportedSrtEngagementCanaryPath(projectDir, "run-b")

	if first == second {
		t.Fatalf("both runs probe %q; concurrent runs would overwrite and misread each other's canary", first)
	}
	if !strings.Contains(first, "run-a") {
		t.Errorf("canary path %q does not carry the run id; an operator cannot tell whose leftover probe file this is", first)
	}
}

// TestSrtEngagementCanaryPath_TheCanaryIsOutsideEveryPathTheProfileGrants is the
// invariant the two tests above only approximate. It reads the write set out of
// the profile the real spawn will use and demands the canary fall outside all of
// it. A canary inside a granted path would be written by a fully working
// sandbox, and the gate would refuse every run.
func TestSrtEngagementCanaryPath_TheCanaryIsOutsideEveryPathTheProfileGrants(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	runID := "run-a"
	worktree := filepath.Join(projectDir, ".harmonik", "worktrees", runID)

	profile, err := daemon.GenerateSandboxProfile(daemon.SandboxProfileInput{
		WorktreePath:   worktree,
		GitDir:         filepath.Join(projectDir, ".git"),
		RunID:          runID,
		BranchName:     "run/" + runID,
		DaemonSockPath: filepath.Join(projectDir, ".harmonik", "daemon.sock"),
	})
	if err != nil {
		t.Fatalf("generate profile: %v", err)
	}
	var decoded struct {
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
		} `json:"filesystem"`
	}
	if jsonErr := json.Unmarshal(profile, &decoded); jsonErr != nil {
		t.Fatalf("decode profile: %v", jsonErr)
	}
	if len(decoded.Filesystem.AllowWrite) == 0 {
		t.Fatal("the profile grants no write access at all; this test would pass for any canary path")
	}

	canary := daemon.ExportedSrtEngagementCanaryPath(projectDir, runID)
	for _, granted := range decoded.Filesystem.AllowWrite {
		if canary == granted || strings.HasPrefix(canary, strings.TrimSuffix(granted, "/")+string(filepath.Separator)) {
			t.Errorf("canary %q falls under granted write path %q.\n"+
				"A working sandbox would ALLOW that write, so the probe could never prove denial and the gate would refuse every run.",
				canary, granted)
		}
	}
}
