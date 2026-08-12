package daemon_test

// claudetrustisolation_test.go — the sensor for a defect that has now been
// fixed three times in this tree, in three different packages.
//
// # What goes wrong
//
// Booting a daemon builds a Claude launch spec, and that seeds a folder-trust
// entry into Claude Code's user-level config so the trust dialog cannot block a
// daemon-spawned pane. The write is a read-modify-write of the WHOLE file under
// an exclusive lock, and it is additive: nothing removes the entry when the
// worktree goes away. Point that at the operator's real ~/.claude.json from a
// test binary and every throwaway worktree leaves a permanent record behind.
//
// It is not cosmetic. Measured on the development machine: 9,280 project
// entries, 9,150 of them naming directories that no longer exist, 85% of a
// 1.8 MB file. The read-modify-write every spawn performs took 46.5 ms against
// that file and 6.9 ms against a pruned one, all of it inside the exclusive
// lock, and the bounded acquire ahead of it is what times out when the lock is
// contended. An earlier round of the same growth reached 8 MB and starved a
// worker with a lock-acquire timeout.
//
// # Why a test and not a comment
//
// hermetic.Main redirects the config for the whole binary, and that is the
// protection. But the variable it sets is process-wide, so any test that
// redirects it for its own purposes and then UNSETS it on cleanup removes the
// protection for everything that runs afterwards. That is exactly what
// scenariotest.RunConcurrentMerge did: os.Setenv paired with
// t.Cleanup(os.Unsetenv) rather than t.Setenv, which restores. The package
// TestMain already documents the rule those helpers have to follow; nothing
// enforced it.
//
// # Why this test is parallel, which is the load-bearing part
//
// t.Parallel is not decoration here. A parallel test pauses and is resumed only
// after every non-parallel top-level test in the package has finished. The
// helpers that redirect this variable are all non-parallel, by their own
// contract. So a parallel guard runs strictly AFTER them, which is the only
// point where a cleanup that unset the variable is observable. A serial guard
// would race the very tests it is watching and would pass or fail on ordering.
//
// The callers live behind the scenario build tag, so under `make fast` this
// guard asserts that hermetic.Main ran at all. It is under `make full` that it
// sees the state the defect produced.
//
// Refs: hk-85pqo. Companion guard:
// internal/harness/claude/trustisolation_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeConfigStaysIsolatedFromTheOperatorHome fails if anything in this
// package's test binary has taken away the redirect hermetic.Main installed.
func TestClaudeConfigStaysIsolatedFromTheOperatorHome(t *testing.T) {
	t.Parallel()

	cfgPath := os.Getenv("HARMONIK_CLAUDE_CONFIG_PATH")
	if cfgPath == "" {
		t.Fatal("HARMONIK_CLAUDE_CONFIG_PATH is unset by the time the parallel tests run, so every " +
			"daemon boot after that point locks and rewrites the operator's real ~/.claude.json and " +
			"leaves a trust entry per throwaway worktree behind. hermetic.Main sets this variable " +
			"once for the whole binary; a helper that redirects it must use t.Setenv, which RESTORES " +
			"the previous value, not os.Setenv paired with os.Unsetenv, which removes it (hk-85pqo)")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	if cfgPath == filepath.Join(home, ".claude.json") {
		t.Fatalf("HARMONIK_CLAUDE_CONFIG_PATH = %q, which is the operator's real config", cfgPath)
	}
	if strings.HasPrefix(cfgPath, filepath.Join(home, ".claude")) {
		t.Errorf("HARMONIK_CLAUDE_CONFIG_PATH = %q, which is inside the operator's Claude state", cfgPath)
	}
}
