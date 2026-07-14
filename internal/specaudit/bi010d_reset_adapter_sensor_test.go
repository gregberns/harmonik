//go:build specaudit

package specaudit_test

// hk-iuaed.6 — Sensor 1: BI-010d reset-op-routes-through-adapter corpus scan.
//
// Spec ref: specs/beads-integration.md §4.4 BI-010d; §4.8 (Adapter).
//
// BI-010d states that the only permitted write op that transitions a bead from
// in_progress → open during orphan-sweep is the §4.8 adapter's ResetBead
// method. No production code outside internal/brcli/ may issue this write
// directly via os/exec (bypassing the adapter and the BI-030 intent-log
// protocol).
//
// # Audit frame
//
// This sensor performs a corpus-wide scan of all non-test Go source files
// outside internal/brcli/ and asserts that none of them contain a direct
// os/exec site that issues `br update ... --status open`. A violation means
// the reset write is bypassing the §4.8 adapter, which would skip BI-030
// intent-log discipline and break BI-031 crash-recovery.
//
// Detection heuristic: the sensor looks for the Go source string pattern
// `"--status", "open"` (exact token pair as passed to exec.Command /
// exec.CommandContext) in production files outside internal/brcli/. This
// pattern only arises when explicitly constructing a subprocess argv whose
// 'update' subcommand passes --status open as separate args — the exact shape
// a direct bypass would take.
//
// BI-010d governs only the in_progress → open RESET transition, which is the
// `br update ... --status open` write. Lines whose argv carries a non-update
// br subcommand are NOT resets and are excluded:
//   - "list"   — a READ (e.g. `br list --status=open -q`); transitions nothing.
//   - "create" — mints a NEW bead in the open state; it is not an
//     in_progress → open reset of an existing bead.
// Excluding these subcommand tokens keeps the heuristic scoped to the actual
// bypass shape and avoids firing on reads and creates (hk-feow8).
//
// True negatives: git status calls never carry `"open"` as a separate arg.
// tmux / ps calls do not use `"--status"`. The heuristic is tightly scoped.
//
// # Failure modes
//
//   - A new production Go file outside internal/brcli/ has been added that
//     contains `"--status", "open"` — a direct subprocess bypass of ResetBead.
//   - An existing file has been modified to include such a bypass.
//
// # Helper prefix
//
// All package-level identifiers in this file use the bi010dAdpt prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// bi010dAdptRepoRoot resolves the repository root from the test file's path.
// The test file lives at internal/specaudit/...; the repo root is two
// directories up.
func bi010dAdptRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("bi010dAdptRepoRoot: runtime.Caller(0) failed")
	}
	// thisFile: .../internal/specaudit/bi010d_reset_adapter_sensor_test.go
	return filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
}

// bi010dAdptIsExcluded reports whether a path should be excluded from the scan.
// Excluded: files in internal/brcli/ (the legitimate adapter site), _test.go
// files (test fixtures may construct argv for verification), and vendor/.
func bi010dAdptIsExcluded(repoRoot, path string) bool {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return false
	}
	// Normalize to forward slashes for cross-platform matching.
	rel = filepath.ToSlash(rel)
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return true
	case strings.HasPrefix(rel, "vendor/"):
		return true
	case strings.HasPrefix(rel, "internal/brcli/"):
		// Legitimate site: the adapter itself calls RunWithDBLockedRetry with
		// "--status", "open" for ResetBead and ReopenBead. Not a bypass.
		return true
	}
	return false
}

// bi010dAdptFileViolates reports whether the Go source file at path contains
// the direct-bypass pattern: the string `"--status", "open"` (two adjacent
// quoted tokens that together form the br --status open argv pair) in a
// non-comment, non-blank line.
//
// The scan is line-level; it does not parse the AST. Comments starting with
// "//" are excluded from matching to reduce false positives from inline
// explanatory text.
func bi010dAdptFileViolates(path string) ([]string, error) {
	//nolint:gosec // G304: path is derived from filepath.Walk over the repo; not user input.
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var violations []string
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		// Skip blank lines and single-line comments.
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		// The bypass pattern: exec.Command[Context] called with "--status"
		// and "open" as adjacent string args. We look for the literal token
		// pair `"--status", "open"` which is the only production shape for
		// a direct br update --status open subprocess call.
		if strings.Contains(line, `"--status", "open"`) ||
			strings.Contains(line, `"--status=open"`) {
			// BI-010d targets only the `br update ... --status open` RESET.
			// Lines whose argv carries a non-update br subcommand ("list" is a
			// read; "create" mints a new open bead) are not resets — skip them.
			if strings.Contains(line, `"list"`) || strings.Contains(line, `"create"`) {
				continue
			}
			violations = append(violations, fmt.Sprintf("line %d: %s", lineNo, strings.TrimSpace(line)))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return violations, nil
}

// TestBI010d_ResetRoutesViaAdapter is the corpus-scan binding test for
// hk-iuaed.6 Sensor 1.
//
// Asserts that no production Go file outside internal/brcli/ contains a
// direct os/exec site issuing `br update ... --status open`, which would
// bypass the §4.8 adapter's ResetBead and skip BI-030 intent-log discipline.
//
// Spec ref: beads-integration.md §4.4 BI-010d; §4.8.
func TestBI010d_ResetRoutesViaAdapter(t *testing.T) {
	t.Parallel()

	repoRoot := bi010dAdptRepoRoot(t)

	type violation struct {
		file  string
		lines []string
	}
	var found []violation

	walkErr := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// Skip hidden dirs, vendor, and build output.
			if name == "vendor" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if bi010dAdptIsExcluded(repoRoot, path) {
			return nil
		}
		violations, scanErr := bi010dAdptFileViolates(path)
		if scanErr != nil {
			t.Logf("bi010dAdpt: scan error for %s: %v (skipping)", path, scanErr)
			return nil
		}
		if len(violations) > 0 {
			rel, _ := filepath.Rel(repoRoot, path)
			found = append(found, violation{file: rel, lines: violations})
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("bi010dAdpt: WalkDir: %v", walkErr)
	}

	if len(found) == 0 {
		t.Logf("BI-010d sensor 1 PASS — no direct br --status open bypass found outside internal/brcli/")
		return
	}

	for _, v := range found {
		for _, l := range v.lines {
			t.Errorf(
				"BI-010d violation: %s:%s\n"+
					"  detail: direct os/exec site issuing br --status open detected outside internal/brcli/.\n"+
					"  fix:    route the reset write through brcli.Adapter.ResetBead (§4.8) to preserve\n"+
					"          BI-030 intent-log discipline and BI-031 crash-recovery semantics.",
				v.file, l,
			)
		}
	}
}
