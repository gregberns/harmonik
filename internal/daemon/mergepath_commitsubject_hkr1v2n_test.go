package daemon_test

// mergepath_commitsubject_hkr1v2n_test.go — GATE-0: table-driven assertion that
// every commit subject produced by the daemon merge path passes the repo's
// scripts/validate-commit-msg.sh gate.
//
// Three merge-path auto-commits are covered:
//
//   1. commitResidualDelta     — "chore: residual iteration delta [<runID>]"
//   2. stripRunContextFromMerge — "chore: strip run-context from merge (hk-4je)"
//   3. runMergeFmtCheck (hk-9k24q) — "chore: auto-format via gofumpt+gci"
//
// hk-2jeel (65cfb767) landed these commit subjects BEFORE the qa-execution gate
// existed, so this test is the missing e2e coverage for the fmt-gate class.
//
// Operator-mandated GATE-0.
// Bead: hk-r1v2n.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestMergePathCommitSubjects_hkr1v2n asserts that every commit message emitted
// by the daemon merge path passes scripts/validate-commit-msg.sh (exit 0).
func TestMergePathCommitSubjects_hkr1v2n(t *testing.T) {
	t.Parallel()

	// Locate the repo root from this source file's path so the test works in
	// any worktree, not just the canonical checkout.
	_, thisFile, _, _ := runtime.Caller(0)
	// thisFile: .../internal/daemon/mergepath_commitsubject_hkr1v2n_test.go
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	validateScript := filepath.Join(repoRoot, "scripts", "validate-commit-msg.sh")

	if _, err := os.Stat(validateScript); err != nil {
		t.Fatalf("validate-commit-msg.sh not found at %s: %v", validateScript, err)
	}

	// sampleRunID is a representative UUIDv7 string used in the
	// commitResidualDelta subject.  The subject is ≤72 chars for any UUID.
	const sampleRunID = "019f555e-bd64-7ebd-bf9e-37155c93c095"
	// sampleBeadID is a representative bead ID used in the fmt-check subject.
	const sampleBeadID = "hk-r1v2n"

	cases := []struct {
		name string
		msg  string // full commit message written to the temp file
	}{
		{
			name: "commitResidualDelta",
			msg: fmt.Sprintf(
				"chore: residual iteration delta [%s]\n\nTrivial: true",
				sampleRunID,
			),
		},
		{
			name: "stripRunContextFromMerge",
			msg: "chore: strip run-context from merge (hk-4je)\n\n" +
				"Remove .harmonik/run-context/** that was force-committed by CHB-023 for\n" +
				"crash-recovery (EM-031). The files remain valid on the run-branch reflog;\n" +
				"they must not land on the merge target.\n" +
				"Trivial: true",
		},
		{
			name: "runMergeFmtCheck_hk9k24q",
			msg: fmt.Sprintf(
				"chore: auto-format via gofumpt+gci\n\nRefs: %s\nTrivial: true",
				sampleBeadID,
			),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f, err := os.CreateTemp(t.TempDir(), "commit-msg-*")
			if err != nil {
				t.Fatalf("create temp file: %v", err)
			}
			if _, err := f.WriteString(tc.msg); err != nil {
				f.Close()
				t.Fatalf("write commit message: %v", err)
			}
			f.Close()

			cmd := exec.Command("bash", validateScript, f.Name())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("validate-commit-msg.sh rejected %q:\n%s", tc.name, out)
			}
		})
	}
}
