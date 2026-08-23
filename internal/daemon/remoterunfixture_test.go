package daemon

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

var remotefixWorker = workers.Worker{
	Name:     "remotefix-worker",
	Host:     "remotefix.invalid",
	Enabled:  true,
	MaxSlots: 1,
	RepoPath: "/tmp/remotefix-worker-repo",
}

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

func remotefixSSHShim(t *testing.T, exitCode int) string {
	t.Helper()
	return remotefixSSHShimAnsweringHEAD(t, exitCode, "")
}

func remotefixSSHShimAnsweringHEAD(t *testing.T, exitCode int, wtPath string) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "ssh-calls.log")
	headAnswer := ""
	if wtPath != "" {
		headAnswer = "case \"$*\" in\n" +
			"  *rev-parse*) exec git -C " + wtPath + " rev-parse HEAD ;;\n" +
			"esac\n"
	}
	shim := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logPath + "\n" +
		headAnswer +
		"exit " + strconv.Itoa(exitCode) + "\n"
	if err := os.WriteFile(filepath.Join(binDir, "ssh"), []byte(shim), 0o700); err != nil { //nolint:gosec // G306: an exec shim in a per-test temp dir must carry the execute bit
		t.Fatalf("remotefixSSHShim: write shim: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

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

func remotefixTunnelSeam(t *testing.T, build func(ctx context.Context, name string, args ...string) *exec.Cmd) {
	t.Helper()
	orig := tunnelpkg.ReverseTunnelRunner
	t.Cleanup(func() { tunnelpkg.ReverseTunnelRunner = orig })
	tunnelpkg.ReverseTunnelRunner = build
}

func remotefixIdleTunnel(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", "sleep 300")
}

func remotefixParams(t *testing.T, projectDir string) TestRuntimeParams {
	t.Helper()
	return TestRuntimeParams{
		ProjectDir:    projectDir,
		HandlerBinary: "/bin/sh",
		HandlerArgs:   []string{"-c", "exit 0"},
		IntentLogDir:  t.TempDir(),
		MaxConcurrent: 1,
		TargetBranch:  "main",
	}
}

func remotefixBead(id core.BeadID, title string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:   id,
		Title:    title,
		BeadType: "task",
		Status:   core.CoarseStatusOpen,
	}
}

func remotefixRunEnv(deps testRuntime, bead core.BeadRecord) runloop.RunEnv {
	return deps.runEnv(core.RunID(uuid.New()), bead, "", "", core.AgentType(""))
}
