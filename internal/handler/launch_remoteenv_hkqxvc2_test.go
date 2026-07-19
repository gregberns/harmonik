package handler_test

// launch_remoteenv_hkqxvc2_test.go — hk-qxvc2: handler.Launch's remote-cwd exec
// path must deliver spec.Env to the remote agent via an `env KEY=VAL … binary args`
// argv prefix (ssh does NOT forward the local process env). Asserts CommandInDir is
// called with name=="env", the surviving CLAUDE_CONFIG_DIR sits BEFORE the binary,
// and the empty deny-list override CLAUDE_CODE_OAUTH_TOKEN= is NOT forwarded.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func TestHandler_Launch_RemoteCwd_ForwardsEnvViaArgv_hkqxvc2(t *testing.T) {
	t.Parallel()

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl, handlercontract.NewAdapterRegistry())

	rr := &recordingRemoteRunner{}
	const remoteWorkDir = "/box-b/.harmonik/worktrees/run-qxvc2/wt"
	const configDir = "CLAUDE_CONFIG_DIR=/box-b/.harmonik/worktrees/run-qxvc2/wt/.harmonik/claude-config"
	spec := handler.LaunchSpec{
		Binary:  "claude",
		Args:    []string{"--print"},
		Env:     []string{configDir, "CLAUDE_CODE_OAUTH_TOKEN="},
		WorkDir: remoteWorkDir,
		Role:    "implementer",
		Runner:  rr,
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err == nil {
		select {
		case <-watcher.Done():
		case <-t.Context().Done():
		}
		if sess != nil {
			_ = sess.Wait(t.Context()) //nolint:errcheck // cleanup
		}
	}

	rr.mu.Lock()
	defer rr.mu.Unlock()

	if rr.inDirCalls != 1 {
		t.Fatalf("expected 1 CommandInDir call, got %d", rr.inDirCalls)
	}
	if rr.inDirName != "env" {
		t.Fatalf("CommandInDir name = %q; want \"env\" (env-prefix argv) — hk-qxvc2", rr.inDirName)
	}

	binIdx, cfgIdx := -1, -1
	for i, tok := range rr.inDirArgs {
		switch tok {
		case "claude":
			binIdx = i
		case configDir:
			cfgIdx = i
		}
		if tok == "CLAUDE_CODE_OAUTH_TOKEN=" {
			t.Errorf("empty deny-list override CLAUDE_CODE_OAUTH_TOKEN= was forwarded to the remote argv %#v — hk-qxvc2", rr.inDirArgs)
		}
	}
	if cfgIdx < 0 {
		t.Fatalf("CLAUDE_CONFIG_DIR not forwarded in remote argv %#v", rr.inDirArgs)
	}
	if binIdx < 0 {
		t.Fatalf("binary \"claude\" not present in remote argv %#v", rr.inDirArgs)
	}
	if cfgIdx >= binIdx {
		t.Errorf("CLAUDE_CONFIG_DIR at %d must precede binary at %d in %#v", cfgIdx, binIdx, rr.inDirArgs)
	}
}
