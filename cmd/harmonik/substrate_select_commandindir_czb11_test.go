package main

// substrate_select_commandindir_czb11_test.go — hk-czb11 PRODUCTION-WIRING guard.
// The composition-root codexWorkerRoutingRunner is what codexdriver.Options.Runner
// actually holds, so it MUST satisfy codexdriver.RemoteCwdRunner — otherwise the
// driver's spawn type-assert fails against the router and falls back to setting a
// LOCAL exec.Cmd.Dir to the REMOTE worktree path (fork/exec-ENOENT). The
// driver-level test injects a fake runner and cannot catch this router gap.

import (
	"context"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/codexdriver"
)

func TestCodexRouter_CommandInDir_czb11(t *testing.T) {
	// Compile-time + runtime proof: the production router IS a RemoteCwdRunner.
	var _ codexdriver.RemoteCwdRunner = (*codexWorkerRoutingRunner)(nil)
	if _, ok := interface{}(&codexWorkerRoutingRunner{}).(codexdriver.RemoteCwdRunner); !ok {
		t.Fatal("codexWorkerRoutingRunner must satisfy codexdriver.RemoteCwdRunner (else the driver falls back to a local cmd.Dir = remote path)")
	}

	const remoteCwd = "/box-b/.harmonik/worktrees/run-czb11"

	t.Run("ssh worker: remote cd, local cmd.Dir unset", func(t *testing.T) {
		r := &codexWorkerRoutingRunner{}
		r.setRegistry(oneWorkerRegistry(true))
		cmd := r.CommandInDir(context.Background(), remoteCwd, "codex", "app-server")
		if cmd.Args[0] != "ssh" {
			t.Fatalf("enabled ssh worker: expected ssh routing, got %v", cmd.Args)
		}
		if cmd.Dir != "" {
			t.Errorf("ssh path: local cmd.Dir = %q; want UNSET (remote cwd applied via cd on the worker) — hk-czb11", cmd.Dir)
		}
		remote := cmd.Args[len(cmd.Args)-1]
		if !strings.Contains(remote, "cd ") || !strings.Contains(remote, " && exec ") {
			t.Errorf("ssh remote command missing `cd … && exec …`: %q", remote)
		}
		if !strings.Contains(remote, remoteCwd) {
			t.Errorf("ssh remote command does not cd into the remote cwd %q: %q", remoteCwd, remote)
		}
	})

	t.Run("local fallback: cmd.Dir set locally", func(t *testing.T) {
		// No requireBoundary, no registry → LocalRunner. The worktree cwd is a
		// LOCAL path, so CommandInDir MUST set exec.Cmd.Dir (spawn leaves it unset
		// on the RemoteCwdRunner branch).
		r := &codexWorkerRoutingRunner{}
		cmd := r.CommandInDir(context.Background(), remoteCwd, "codex", "app-server")
		if cmd.Args[0] == "ssh" {
			t.Fatalf("local fallback: expected local exec, got ssh: %v", cmd.Args)
		}
		if cmd.Dir != remoteCwd {
			t.Errorf("local path: cmd.Dir = %q; want the local cwd %q", cmd.Dir, remoteCwd)
		}
	})

	t.Run("fail-closed: refusal argv0 (no ssh route)", func(t *testing.T) {
		r := &codexWorkerRoutingRunner{requireBoundary: true}
		cmd := r.CommandInDir(context.Background(), remoteCwd, "codex", "app-server")
		if cmd.Args[0] != refusedIsolationBoundaryArgv0 {
			t.Fatalf("fail-closed: expected refusal argv0 %q, got %v", refusedIsolationBoundaryArgv0, cmd.Args)
		}
	})
}
