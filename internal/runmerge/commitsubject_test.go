package runmerge_test

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

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	validateScript := filepath.Join(repoRoot, "scripts", "validate-commit-msg.sh")

	if _, err := os.Stat(validateScript); err != nil {
		t.Fatalf("validate-commit-msg.sh not found at %s: %v", validateScript, err)
	}

	const sampleRunID = "019f555e-bd64-7ebd-bf9e-37155c93c095"
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

			msgPath := filepath.Join(t.TempDir(), "commit-msg")
			if err := os.WriteFile(msgPath, []byte(tc.msg), 0o600); err != nil {
				t.Fatalf("write commit message: %v", err)
			}

			cmd := exec.CommandContext(t.Context(), "bash", validateScript, msgPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Errorf("validate-commit-msg.sh rejected %q:\n%s", tc.name, out)
			}
		})
	}
}
