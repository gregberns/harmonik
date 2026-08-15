package daemon_test

// sandboxscratchtmpdir_test.go — the sandboxed child gets a writable temp
// directory, and it is one the profile grants
// (hk-sandbox-no-writable-tmpdir-7484h).
//
// # What went wrong
//
// srt pointed the child's TMPDIR at its own default, /tmp/claude, whatever ran
// inside, and the profile granted that path. So the child could write a temp
// file. What it did not have was a temp directory of its OWN: every concurrent
// run on the box spooled into the one directory srt named.
//
// Then a live Pi run did its work correctly, composed a correct commit message,
// and tried to write it to /tmp/commit-msg.txt — this project requires
// `git commit -F <file>`, and the instruction named that literal path. Nothing
// granted it. The run got EPERM, tried `sudo`, was refused again, and left a
// finished edit unstaged. Every run that reached the commit step took that path.
// The instruction is fixed in internal/workspace buildAgentTaskContent. These
// tests hold the other half: the run gets a scratch directory of its own, and
// the profile grants it.
//
// # What these tests defend
//
//   - The profile grants the run's own scratch directory — "its own" checked by
//     deriving the expectation from the run's worktree, not by listing paths it
//     must not be — and the grant as a whole still refuses the world-shared temp
//     roots.
//   - The wrap that launches the child names that SAME directory as the child's
//     TMPDIR, and the directory exists on disk before the child starts. The
//     cross-check — the TMPDIR value must appear in the profile's write grant —
//     is the assertion that catches the original defect, because the two facts
//     were produced by different code and nothing compared them.
//   - Under a real srt, a shell inside the sandbox can write a commit-message
//     file to $TMPDIR, and STILL cannot write to /tmp. The second half is what
//     stops "give it a temp dir" from being read as "open /tmp".

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

// scratchProfileInput builds the profile input for a local pi run the way the
// launch path does, rooted at a real directory on disk.
//
// EvalSymlinks is not cosmetic: on macOS the per-test temp root lives under
// /var/folders, and /var is a symlink to /private/var. Production worktrees sit
// under the repo and have no such indirection, so resolving here keeps the test
// measuring the sandbox rather than the symlink.
func scratchProfileInput(t *testing.T) daemon.SandboxProfileInput {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp root: %v", err)
	}
	return daemon.SandboxProfileInput{
		WorktreePath:      root,
		GitDir:            filepath.Join(root, ".git"),
		RunID:             "scratch-tmpdir-run",
		DaemonSockPath:    filepath.Join(root, "daemon.sock"),
		AllowLocalBinding: true,
	}
}

// scratchPiSandboxConfig is the live deployment's gate config: srt backend, pi
// in the harness list.
func scratchPiSandboxConfig() projectconfig.SandboxConfig {
	return projectconfig.SandboxConfig{
		Backend:   "srt",
		Harnesses: []string{"pi"},
		Network:   projectconfig.SandboxNetworkConfig{AllowLocalBinding: true},
	}
}

// profileAllowWrite decodes the generated profile and returns filesystem.allowWrite.
func profileAllowWrite(t *testing.T, in daemon.SandboxProfileInput) []string {
	t.Helper()
	raw, err := daemon.GenerateSandboxProfile(in)
	if err != nil {
		t.Fatalf("GenerateSandboxProfile: %v", err)
	}
	var settings struct {
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
		} `json:"filesystem"`
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("decode profile %s: %v", raw, err)
	}
	return settings.Filesystem.AllowWrite
}

// TestSandboxProfileGrantsTheRunAWritableScratchDirectory pins the first half of
// the promise: the run has somewhere to write that is not the worktree checkout
// and not one harness's hardcoded path.
func TestSandboxProfileGrantsTheRunAWritableScratchDirectory(t *testing.T) {
	in := scratchProfileInput(t)
	allowWrite := profileAllowWrite(t, in)

	scratch := daemon.SandboxScratchDir(in.WorktreePath)
	if !slices.Contains(allowWrite, scratch) {
		t.Fatalf("profile grants no scratch directory: allowWrite=%v, want it to contain %q.\n"+
			"Without this a run that writes an ordinary temp file is refused, which is how a "+
			"finished commit was thrown away (hk-sandbox-no-writable-tmpdir-7484h).", allowWrite, scratch)
	}

	// The paired negative, DERIVED from this run's own worktree rather than
	// listed. "Per-run" means "inside the one directory this run owns", and
	// containment is the whole of that property. A list of forbidden literals is
	// a weaker claim wearing the same words: it refuses the temp roots whoever
	// wrote the list thought of, and a scratch directory moved to any other
	// host-shared path — a fixed /var/folders/harmonik, an $XDG_RUNTIME_DIR, a
	// sibling run's worktree — walks straight through it.
	if !isUnderDir(t, in.WorktreePath, scratch) {
		t.Errorf("the granted scratch directory %q is not under this run's worktree %q.\n"+
			"It must be the run's own: a shared path is written into by every concurrent run at once, "+
			"and it outlives the worktree whose removal was supposed to take it away (hk-guapd).",
			scratch, in.WorktreePath)
	}

	// Containment in the worktree is necessary and not sufficient. Two
	// directories under it are the worktree's own and must never become scratch
	// space. A scratch directory at <worktree>/.git/tmp satisfies every
	// assertion above — it is this run's, it is no shared temp root — while
	// spooling the agent's temp files in among the object store; one at
	// <worktree>/tmp satisfies them while putting those files in the agent's own
	// `git status`, where they become part of its change. Both survived a
	// mutation run against this file. So name the directory that already carries
	// this run's state and is already gitignored, and assert containment in THAT.
	stateDir := filepath.Join(in.WorktreePath, ".harmonik")
	if !isUnderDir(t, stateDir, scratch) {
		t.Errorf("the granted scratch directory %q is not under the run's gitignored state directory %q.\n"+
			"A temp directory inside the checkout puts the agent's scratch files in its own `git status`; "+
			"one inside .git puts them beside the object store. Neither is somewhere to spool a commit message.",
			scratch, stateDir)
	}

	// And the grant AS A WHOLE opens no world-shared temp root. This is a claim
	// about the other entries, not about the scratch directory: srt expands every
	// allowWrite entry recursively, so one such line would leave the containment
	// above true and meaningless.
	for _, shared := range []string{"/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp", "/"} {
		if slices.Contains(allowWrite, shared) {
			t.Errorf("profile grants the world-shared temp root %q — the scratch directory must be per-run, "+
				"not a blanket temp grant (hk-guapd)", shared)
		}
	}
}

// isUnderDir reports whether path lies strictly inside dir. It compares the
// relative path rather than a string prefix, so a sibling directory whose name
// merely starts with dir's ("<worktree>-2") is not mistaken for a child.
func isUnderDir(t *testing.T, dir, path string) bool {
	t.Helper()
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// TestSandboxWrapPointsTheChildTmpdirAtAGrantedScratchDirectory is the
// assertion that catches the original defect. The profile said what the run may
// write; the wrap decided where the child's temp files go; nothing compared the
// two, and they disagreed.
func TestSandboxWrapPointsTheChildTmpdirAtAGrantedScratchDirectory(t *testing.T) {
	in := scratchProfileInput(t)
	spawn := daemon.ExportedSandboxSpawnForRun(scratchPiSandboxConfig(), core.AgentType("pi"), in)
	if spawn == nil {
		t.Fatalf("gate returned nil spawn — a pi run under backend=srt must be wrapped")
	}

	_, _, extraEnv, err := daemon.ExportedSandboxWrapExecArgv(spawn, "pi", []string{"--mode", "json"})
	if err != nil {
		t.Fatalf("exec wrap: %v", err)
	}

	const key = "CLAUDE_CODE_TMPDIR="
	var childTmpDir string
	for _, kv := range extraEnv {
		if after, ok := strings.CutPrefix(kv, key); ok {
			childTmpDir = after
		}
	}
	if childTmpDir == "" {
		t.Fatalf("the wrap sets no child temp directory: env=%v.\n"+
			"srt reads CLAUDE_CODE_TMPDIR from its own environment and injects TMPDIR into the "+
			"sandboxed child; with it unset the child gets /tmp/claude, one harness's path.", extraEnv)
	}

	if !slices.Contains(profileAllowWrite(t, in), childTmpDir) {
		t.Fatalf("the child's TMPDIR %q is not in the profile's write grant %v — "+
			"the child is pointed at a directory it may not write", childTmpDir, profileAllowWrite(t, in))
	}

	// A granted path is not a directory. srt stats the child's TMPDIR before any
	// sandboxed process runs, and the first tool to open a missing one gets
	// ENOENT rather than a refusal it can read.
	info, statErr := os.Stat(childTmpDir)
	if statErr != nil {
		t.Fatalf("the child's TMPDIR %q does not exist on disk: %v", childTmpDir, statErr)
	}
	if !info.IsDir() {
		t.Fatalf("the child's TMPDIR %q is not a directory", childTmpDir)
	}
}

// removeDeniedCanary clears the /tmp canary the sandbox must never let the
// child create. A leftover from an earlier run would make the denial assertion
// fire on evidence this test did not produce, so a removal failure that is not
// "already absent" fails the test rather than being swallowed.
func removeDeniedCanary(t *testing.T) {
	t.Helper()
	if err := os.Remove(deniedCanaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clear the /tmp denial canary %s: %v", deniedCanaryPath, err)
	}
}

// deniedCanaryPath is a path under /tmp this test owns. The sandbox must refuse
// it; if the write grant ever widens to /tmp, the file appears and the test says
// so.
//
// The suffix makes the path this PROCESS's own, and it is load-bearing. The name
// used to be a fixed literal, so two `go test ./internal/daemon` runs at once on
// one box shared a single file: either run's cleanup deleted the other run's
// canary, and a real sandbox escape then read as a pass. srtEngagementCanaryPath
// in sandboxgate.go appends the RunID for the same reason. A test has no RunID,
// so it uses its own process ID and start time.
var deniedCanaryPath = fmt.Sprintf("/tmp/harmonik-scratch-tmpdir-denied-canary-%d-%d.txt",
	os.Getpid(), time.Now().UnixNano())

// TestAnUnwrappedLaunchSetsNoTmpdirForTheChild pins the premise the
// implementer's instructions have to survive: on a launch the gate declines to
// wrap, harmonik contributes NO environment, so the child has no TMPDIR from
// anyone.
//
// The gate declines on three live paths — a non-srt backend, a harness absent
// from sandbox.harnesses, and every remote run (DaemonSockPath is a tcp://
// reverse tunnel there). Nothing downstream fills the gap: handler.go assigns
// cmd.Env = spec.Env outright, pi's buildPiEnv emits no TMPDIR, and
// RemoteExecArgv rebuilds the remote `env K=V …` prefix from that same explicit
// slice over a non-login shell.
//
// This test exists because every OTHER test here asserts the sandboxed path,
// and that gap is exactly how an instruction reading "$TMPDIR/commit-msg.txt"
// — which expands to "/commit-msg.txt" when TMPDIR is empty — got as far as
// review. The instruction's own defence is
// TestCommitMessagePathIsWritableWhateverTmpdirTheRunHas in internal/workspace.
func TestAnUnwrappedLaunchSetsNoTmpdirForTheChild(t *testing.T) {
	base := scratchProfileInput(t)

	remoteIn := base
	remoteIn.DaemonSockPath = "tcp://127.0.0.1:53211"

	for _, tc := range []struct {
		name      string
		cfg       projectconfig.SandboxConfig
		agentType core.AgentType
		in        daemon.SandboxProfileInput
	}{
		{"backend_is_not_srt", projectconfig.SandboxConfig{Backend: "none", Harnesses: []string{"pi"}}, core.AgentType("pi"), base},
		{"harness_not_in_the_sandbox_list", scratchPiSandboxConfig(), core.AgentType("claude-code"), base},
		{"remote_run_reaches_the_daemon_over_a_tunnel", scratchPiSandboxConfig(), core.AgentType("pi"), remoteIn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spawn := daemon.ExportedSandboxSpawnForRun(tc.cfg, tc.agentType, tc.in)
			if spawn != nil {
				t.Fatalf("the gate wrapped a launch it must decline")
			}

			bin, args, extraEnv, err := daemon.ExportedSandboxWrapExecArgv(spawn, "pi", []string{"--mode", "json"})
			if err != nil {
				t.Fatalf("exec wrap: %v", err)
			}
			// The paired positive: the launch is untouched, not merely env-free.
			// An assertion that only says "no TMPDIR" is satisfied for free by a
			// wrap that failed to do anything at all.
			if bin != "pi" || !slices.Equal(args, []string{"--mode", "json"}) {
				t.Fatalf("an unwrapped launch must pass argv through unchanged, got %q %v", bin, args)
			}
			for _, kv := range extraEnv {
				if strings.HasPrefix(kv, "TMPDIR=") || strings.HasPrefix(kv, "CLAUDE_CODE_TMPDIR=") {
					t.Fatalf("an unwrapped launch contributed %q; the agent's instructions assume it does not "+
						"and carry their own default", kv)
				}
			}
			if len(extraEnv) != 0 {
				t.Fatalf("an unwrapped launch must contribute no environment, got %v", extraEnv)
			}
		})
	}
}

// TestSandboxedShellWritesItsCommitMessageToTmpdirAndStillCannotWriteTmp
// reproduces the reported failure against a real srt: a process inside the
// sandbox writes a commit-message file to $TMPDIR and it lands in the run's
// scratch directory, while the same process's write to /tmp is still refused.
//
// Skips when srt is absent. The unit tests above hold the contract in that case.
func TestSandboxedShellWritesItsCommitMessageToTmpdirAndStillCannotWriteTmp(t *testing.T) {
	if _, err := exec.LookPath("srt"); err != nil {
		t.Skip("srt binary not available; the in-sandbox write repro requires srt")
	}

	in := scratchProfileInput(t)
	spawn := daemon.ExportedSandboxSpawnForRun(scratchPiSandboxConfig(), core.AgentType("pi"), in)
	if spawn == nil {
		t.Fatalf("gate returned nil spawn — a pi run under backend=srt must be wrapped")
	}
	spawn.SrtBinary = "srt"

	removeDeniedCanary(t)
	t.Cleanup(func() { removeDeniedCanary(t) })

	// The script is a FILE run as `sh <path>`, not `sh -c <script>`: srt's own
	// CLI takes -c, and its option parser would consume a short flag meant for
	// the wrapped command (the same collision srt_pi_egress_e2e_test.go
	// documents for curl's -s/-o/-w).
	scriptPath := filepath.Join(in.WorktreePath, "write-commit-msg.sh")
	script := "printf 'docs(x): y' > \"$TMPDIR/commit-msg.txt\"\n" +
		"printf 'leaked' > " + deniedCanaryPath + "\n" +
		"exit 0\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		t.Fatalf("write probe script: %v", err)
	}

	bin, args, extraEnv, err := daemon.ExportedSandboxWrapExecArgv(spawn, "sh", []string{scriptPath})
	if err != nil {
		t.Fatalf("exec wrap: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...) //nolint:gosec // G204: argv is produced by the daemon's own wrap, not caller input.
	cmd.Env = append(os.Environ(), extraEnv...)
	out, runErr := cmd.CombinedOutput()

	wrote, readErr := os.ReadFile(filepath.Join(daemon.SandboxScratchDir(in.WorktreePath), "commit-msg.txt"))
	if readErr != nil {
		t.Fatalf("the sandboxed shell could not write a commit-message file to $TMPDIR: %v\n"+
			"srt exit=%v output=%q\n"+
			"This is the reported failure: the agent is told to commit with `git commit -F <file>` and "+
			"has nowhere to put the file (hk-sandbox-no-writable-tmpdir-7484h).",
			readErr, runErr, strings.TrimSpace(string(out)))
	}
	if string(wrote) != "docs(x): y" {
		t.Fatalf("commit-message file holds %q, want %q", wrote, "docs(x): y")
	}

	if _, statErr := os.Stat(deniedCanaryPath); statErr == nil {
		t.Fatalf("the sandboxed shell wrote %s — the scratch grant must be the run's own directory, "+
			"never /tmp. srt output=%q", deniedCanaryPath, strings.TrimSpace(string(out)))
	}
}
