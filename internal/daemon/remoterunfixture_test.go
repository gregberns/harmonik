package daemon

// remoterunfixture_test.go — the shared fixture for a test that drives the real
// beadRunOne down the REMOTE path.
//
// # Why this file exists
//
// A remote run needs five things a local run does not: a repository whose
// daemon-socket path fits the platform limit, an ssh that answers, a reverse
// tunnel that is not a real ssh forward, a worker with a reserved slot, and a
// bead plus a run environment. Two files grew their own copy of all five within
// one commit of each other, and the copies agreed in shape and differed only in
// spelling. Eight more resources get tests like these next, and each will copy
// whichever shape it finds first.
//
// So the shape lives here once. A test file keeps only what it varies.
//
// # What a caller still owns
//
// The parts that are the test rather than the setting: the bead ledger and the
// event bus it reads its assertions out of, the worktree factory, the adapter
// registry, and whatever seam its own claim is about. remotefixParams fills the
// fields that are the same in every remote fixture and leaves the rest at their
// zero value for the caller to set by name.
//
// Helper names in this package are package-scoped, so grep for a name before you
// add one here. Two files that add the same helper in separate worktrees merge
// cleanly and then fail to build.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/runloop"
	tunnelpkg "github.com/gregberns/harmonik/internal/transport/tunnel"
	"github.com/gregberns/harmonik/internal/workers"
)

// remotefixWorker is the worker the outer dispatch loop pre-selects. Handing a
// pre-selected worker to beadRunOne is what makes a run remote.
//
// The host is unroutable on purpose. The ssh shim below answers every remote
// command, so a run that escaped the shim fails loudly rather than reaching a
// real machine.
var remotefixWorker = workers.Worker{
	Name:     "remotefix-worker",
	Host:     "remotefix.invalid",
	Enabled:  true,
	MaxSlots: 1,
	RepoPath: "/tmp/remotefix-worker-repo",
}

// remotefixReserveWorker builds a one-slot registry and reserves that slot, which
// is the state the outer dispatch loop hands beadRunOne.
func remotefixReserveWorker(t *testing.T) (*workers.Registry, *workers.Worker) {
	t.Helper()
	reg := workers.NewRegistry(workers.Config{Workers: []workers.Worker{remotefixWorker}})
	preSelected := reg.SelectWorker()
	if preSelected == nil {
		t.Fatal("remotefixReserveWorker: SelectWorker returned nil, so the run would not be remote")
	}
	if got := reg.InFlight(); got != 1 {
		t.Fatalf("remotefixReserveWorker: InFlight = %d, want 1", got)
	}
	return reg, preSelected
}

// remotefixRepo returns a one-commit git repository whose
// <dir>/.harmonik/daemon.sock path fits the platform's socket-path limit.
//
// t.TempDir is not used. Its path carries the test's name, which on a machine
// with a long temp prefix pushes the socket past the limit. The run plan then
// refuses for the hook-socket reason, and a test that meant to measure something
// further down the run measures that refusal instead.
func remotefixRepo(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hkremfix")
	if err != nil {
		t.Fatalf("remotefixRepo: MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			t.Logf("remotefixRepo: cleanup %s: %v", dir, rmErr)
		}
	})
	if lenErr := lifecycle.ValidateSocketPathLength(hooksockPath(dir)); lenErr != nil {
		t.Fatalf("remotefixRepo: the fixture socket path is already too long, so this test would "+
			"refuse before it reached what it means to measure: %v", lenErr)
	}
	hooksockGitInit(t, dir)
	return dir
}

// remotefixSSHShim puts a recording ssh first on PATH and returns the path of the
// file it appends one line to per call.
//
// Every ssh a remote run makes is a subprocess resolved through PATH — the tmux
// SSHRunner and the tunnel alike — so the shim is the one place that sees all of
// them. It keeps a test that regressed off any network, and its log is how a test
// asserts which round trips a run did or did not make.
//
// exitCode is what the shim returns. Pass 0 for a fixture that must reach the
// launch, because the tunnel readiness probe runs over this shim and a non-zero
// exit refuses the run.
func remotefixSSHShim(t *testing.T, exitCode int) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "ssh-calls.log")
	shim := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logPath + "\nexit " + strconv.Itoa(exitCode) + "\n"
	// 0o700 rather than 0o600: the shim is put on PATH and must be executable.
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte(shim), 0o700); err != nil { //nolint:gosec // G306: an exec shim in a per-test temp dir must carry the execute bit
		t.Fatalf("remotefixSSHShim: write shim: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

// remotefixSSHCalls returns the lines the shim recorded, or nothing when no ssh
// ran at all.
func remotefixSSHCalls(t *testing.T, logPath string) []string {
	t.Helper()
	raw, err := os.ReadFile(logPath) //nolint:gosec // a path the fixture just created under its own temp dir
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("remotefixSSHCalls: read %s: %v", logPath, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// remotefixTunnelSeam replaces the reverse-tunnel process builder for the length
// of the test and restores it after.
//
// The seam is a package-level variable in internal/transport/tunnel, so a test
// that swaps it MUST NOT be parallel.
func remotefixTunnelSeam(t *testing.T, build func(ctx context.Context, name string, args ...string) *exec.Cmd) {
	t.Helper()
	orig := tunnelpkg.ReverseTunnelRunner
	t.Cleanup(func() { tunnelpkg.ReverseTunnelRunner = orig })
	tunnelpkg.ReverseTunnelRunner = build
}

// remotefixIdleTunnel is a reverse tunnel that starts, stays up, and carries
// nothing. A fixture that must reach the launch needs a live process for the run
// to hold and then kill.
func remotefixIdleTunnel(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "sleep 300")
}

// remotefixParams fills the work-loop fields that are the same in every remote
// fixture. The caller sets the rest by name.
//
// The values here are settings rather than subjects. /bin/sh with a stub script
// stands in for the agent, one concurrent run keeps the dispatch gate out of the
// way, and main is the branch the run plan resolves against.
//
// AdapterRegistry2 is deliberately NOT set. It decides how the readiness phase
// ends, which is the subject of some of these tests rather than a setting, and it
// MUST be non-nil, so a caller that forgets it fails loudly rather than
// inheriting somebody else's readiness behaviour.
func remotefixParams(t *testing.T, projectDir string) WorkLoopDepsParams {
	t.Helper()
	return WorkLoopDepsParams{
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  t.TempDir(),
		MaxConcurrent: 1,
		TargetBranch:  "main",
	}
}

// remotefixBead returns an open task bead with no labels and no body, which is
// the plainest thing the run plan can resolve.
func remotefixBead(id core.BeadID, title string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:   id,
		Title:    title,
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
	}
}

// remotefixRunEnv builds the run environment for one fresh run of bead.
//
// deps.runEnv takes eleven positional arguments, ten of which every fixture here
// leaves at their zero value. Spelling them out per file is how a fixture ends up
// depending on an argument it never meant to set.
func remotefixRunEnv(deps workLoopDeps, bead core.BeadRecord) runloop.RunEnv {
	return deps.runEnv(core.RunID(uuid.New()), bead, "", nil, nil, 0, "", "", nil, false, "", core.AgentType(""))
}
