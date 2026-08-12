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
// It is not cosmetic, and the method matters as much as the number, so both are
// here rather than only on a bead the ledger keeps machine-local. Measured on
// the development machine 2026-08-12: the file held 9,280 project entries, of
// which 9,150 named directories that no longer exist — 98.6% by entry count and
// 95% of the 1.8 MB file by BYTES, because the dead weight is mostly the
// pathnames used as keys (a 32-byte value under a key averaging 137
// characters).
//
// The byte share was measured rather than estimated: re-serialize the parsed
// file with the two-space indent it already uses, then re-serialize it without
// the dead entries and take the difference. An earlier estimate summed key and
// value lengths and gave 85%, because it dropped the indentation and per-entry
// framing — which fall almost entirely on the dead entries, those being almost
// the whole file.
//
// The cost was taken over 20 iterations of the real writer cycle — ReadFile,
// Unmarshal, set the key, MarshalIndent, temp write, Sync, Rename, then fsync
// the parent directory — against the live file and against a small 101 KB
// config: 46.5 ms versus 6.9 ms, a factor of 6.7. Pruning the dead entries
// leaves about 87 KB, so the small file is about the size a pruned copy of this
// one would be and the pair reads as a before and after.
//
// All of that runs inside the exclusive lock, with a four-attempt retry budget
// above it, and the bounded acquire ahead of it is what times out when the lock
// is contended. An earlier round of the same growth reached 8 MB and starved a
// worker with a lock-acquire timeout.
//
// Several places in this tree quote a measurement of this one file:
// internal/testhelpers/hermetic, internal/harness/claude/trustisolation_test.go,
// internal/workspace/claudetrust_wm040b.go, and the 2026-06-09 postmortem. Do
// not reconcile their PERCENTAGES by assuming the file grew. The first two count
// only dead paths under the Go test temp directory — 4,459 of 6,426 — while this
// comment counts every directory that no longer exists, 9,150 of 9,280. On this
// same date the temp-only count was 6,291 of 9,280. So the distance between 69%
// and 98.6% is the definition changing, not the file growing. The SIZES are a
// different matter: the postmortem's 8.6 MB and 36.6k keys are this file before
// somebody pruned it to 1.9 MB, and that one is a real change in the file.
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
	if strings.HasPrefix(cfgPath, filepath.Join(home, ".claude")+string(os.PathSeparator)) {
		t.Errorf("HARMONIK_CLAUDE_CONFIG_PATH = %q, which is inside the operator's Claude state", cfgPath)
	}
}
