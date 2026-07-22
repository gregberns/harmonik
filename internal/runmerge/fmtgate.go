package runmerge

// fmtgate.go — the prepare-phase gofumpt/gci format gate and its auto-fix
// commit (hk-k1hn, hk-0lrt). Runs OUTSIDE the merge exclusion domain (RSM-017).
//
// Carved out of internal/daemon/workloop.go by P2 unit E5 RT13 (pure move).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// runMergeFmtGate runs the gofumpt/gci fmt gate on the merged tree — the
// prepare-phase fmt gate (RSM-017: OUTSIDE the exclusion domain). It wraps
// runMergeFmtCheck, which auto-fixes and commits format drift into the worktree.
// Returns (nil, newRunTip) where newRunTip is non-empty when the auto-fix
// advanced the worktree HEAD; (outcome, "") on a terminal fmt failure.
func runMergeFmtGate(ctx context.Context, wtPath, projectDir string, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (outcome *Outcome, newRunTip string) {
	buildDir := projectDir
	if _, statErr := os.Stat(wtPath); statErr == nil {
		buildDir = wtPath
	}
	if _, goModErr := os.Stat(filepath.Join(buildDir, "go.mod")); goModErr != nil {
		return nil, ""
	}
	return runMergeFmtCheck(ctx, buildDir, projectDir, runID, beadID, bus)
}

// runMergeFmtCheck detects formatting drift (gofumpt, gci) on buildDir before
// the push and auto-heals it when an isolated worktree is available.
//
// Auto-heal path (buildDir != projectDir — worktree exists): runs gofumpt -w
// and/or gci write to reformat in place, then stages and commits the changes
// in the worktree and advances refs/heads/<targetBranch> to the new tip so the
// caller's push step picks up the format commit.
//
// Fallback path (buildDir == projectDir — worktree already removed): rolls
// back the update-ref and emits merge_build_failed, same as the original
// reject behaviour, because committing in the live project directory is unsafe.
//
// Fail-open: if either tool binary is absent in projectDir/.tools/ the check
// is silently skipped (non-Go repos, bare test fixtures, CI without tools).
//
// Beads: hk-k1hn (original gate), hk-0lrt (auto-heal).
// runMergeFmtCheck runs gofumpt+gci on the merged tree in buildDir and, when the
// worktree is isolated (buildDir != projectDir), auto-fixes drift and commits it
// onto the run-branch. It is the prepare-phase fmt gate (RSM-017): it runs
// OUTSIDE the merge exclusion domain and never advances the target ref — the
// post-fmt worktree HEAD is returned as the new run-branch tip, and the commit
// phase's update-ref advances the target to it.
//
// Returns (nil, newRunTip) on success — newRunTip is the post-fmt worktree HEAD
// when the auto-fix committed, else ""; (outcome, "") on a terminal fmt failure.
func runMergeFmtCheck(ctx context.Context, buildDir, projectDir string, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (outcome *Outcome, newRunTip string) {
	// Auto-format is only safe when an isolated worktree is available.
	canAutoFmt := buildDir != projectDir
	needsCommit := false

	gofumptBin := filepath.Join(projectDir, ".tools", "gofumpt")
	gciBin := filepath.Join(projectDir, ".tools", "gci")
	_, gofumptAvail := os.Stat(gofumptBin)
	_, gciAvail := os.Stat(gciBin)
	mod := readGoModule(buildDir)

	const maxFmtIter = 5
	for i := range maxFmtIter {
		iterDirty, out := runFmtPassesOnce(ctx, buildDir, gofumptBin, gciBin, mod,
			gofumptAvail == nil, gciAvail == nil, canAutoFmt, runID, beadID, bus)
		if out != nil {
			return out, ""
		}
		needsCommit = needsCommit || iterDirty

		if !iterDirty {
			break
		}
		if i == maxFmtIter-1 {
			emitMergeBuildFailed(ctx, bus, runID, beadID,
				errors.New("gofumpt+gci did not converge after "+fmt.Sprint(maxFmtIter)+" passes"), nil)
			return &Outcome{
				Success: false,
				Reason:  "merge_fmt_failed: gofumpt+gci did not converge after " + fmt.Sprint(maxFmtIter) + " passes (check gci local-prefix config vs module path)",
			}, ""
		}
	}

	if !needsCommit {
		return nil, ""
	}
	return commitFmtChanges(ctx, buildDir, runID, beadID, bus)
}

// runFmtPassesOnce runs one gofumpt then one gci pass over buildDir (each gated
// on tool availability), returning (dirty, nil) when either reformatted the tree
// and (false, outcome) on the first terminal failure.
func runFmtPassesOnce(ctx context.Context, buildDir, gofumptBin, gciBin, mod string, gofumptAvail, gciAvail, canAutoFmt bool, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (dirty bool, out *Outcome) {
	if gofumptAvail {
		d, o := fmtGofumptPass(ctx, buildDir, gofumptBin, canAutoFmt, runID, beadID, bus)
		if o != nil {
			return false, o
		}
		dirty = dirty || d
	}
	if gciAvail && mod != "" {
		d, o := fmtGciPass(ctx, buildDir, gciBin, mod, canAutoFmt, runID, beadID, bus)
		if o != nil {
			return false, o
		}
		dirty = dirty || d
	}
	return dirty, nil
}

// fmtGofumptPass runs one gofumpt pass over buildDir. It returns (dirty, nil)
// when files were reformatted (auto-fix) and (false, outcome) on a terminal
// failure. When canAutoFmt is false, unformatted files are a terminal failure.
func fmtGofumptPass(ctx context.Context, buildDir, gofumptBin string, canAutoFmt bool, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (bool, *Outcome) {
	listCmd := exec.CommandContext(ctx, gofumptBin, "-l", ".")
	listCmd.Dir = buildDir
	out, err := listCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return false, nil
	}
	if !canAutoFmt {
		msg := "gofumpt: unformatted files (run 'make fmt' to fix):\n" + strings.TrimRight(string(out), "\n")
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
		return false, &Outcome{Success: false, Reason: "merge_fmt_failed (gofumpt): " + strings.TrimRight(string(out), "\n")}
	}
	fmtCmd := exec.CommandContext(ctx, gofumptBin, "-w", ".")
	fmtCmd.Dir = buildDir
	if fmtErr := fmtCmd.Run(); fmtErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New("gofumpt -w: "+fmtErr.Error()), nil)
		return false, &Outcome{Success: false, Reason: "merge_fmt_failed (gofumpt -w): " + fmtErr.Error()}
	}
	return true, nil
}

// fmtGciPass runs one gci import-order pass over buildDir, with the same
// (dirty, outcome) contract as fmtGofumptPass.
func fmtGciPass(ctx context.Context, buildDir, gciBin, mod string, canAutoFmt bool, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (bool, *Outcome) {
	diffCmd := exec.CommandContext(ctx, gciBin, "diff", "-s", "standard", "-s", "default", "-s", "prefix("+mod+")", ".") //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	diffCmd.Dir = buildDir
	out, err := diffCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return false, nil
	}
	if !canAutoFmt {
		msg := "gci: import order drift (run 'make fmt' to fix):\n" + strings.TrimRight(string(out), "\n")
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
		return false, &Outcome{Success: false, Reason: "merge_fmt_failed (gci): import order drift detected"}
	}
	writeCmd := exec.CommandContext(ctx, gciBin, "write", "-s", "standard", "-s", "default", "-s", "prefix("+mod+")", ".") //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	writeCmd.Dir = buildDir
	if writeErr := writeCmd.Run(); writeErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New("gci write: "+writeErr.Error()), nil)
		return false, &Outcome{Success: false, Reason: "merge_fmt_failed (gci write): " + writeErr.Error()}
	}
	return true, nil
}

// commitFmtChanges stages and commits the gofumpt/gci auto-fix onto the
// run-branch in buildDir and returns (nil, newRunTip) — the post-fmt worktree
// HEAD the commit phase advances the target to — or (outcome, "") on failure.
func commitFmtChanges(ctx context.Context, buildDir string, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (outcome *Outcome, newRunTip string) {
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = buildDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, addErr, addOut)
		return &Outcome{Success: false, Reason: "merge_fmt_failed (git add): " + addErr.Error()}, ""
	}

	commitMsg := fmt.Sprintf("chore: auto-format via gofumpt+gci\n\nRefs: %s\nTrivial: true", beadID)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg) //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	commitCmd.Dir = buildDir
	if commitOut, commitErr := commitCmd.CombinedOutput(); commitErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, commitErr, commitOut)
		return &Outcome{Success: false, Reason: "merge_fmt_failed (fmt commit): " + commitErr.Error()}, ""
	}

	// Return the post-fmt worktree HEAD; the commit phase advances the target to it.
	if newTip, rerr := gitprobe.RevParse(ctx, buildDir, "HEAD"); rerr == nil {
		return nil, newTip
	}
	return nil, ""
}

// readGoModule parses the first "module <path>" directive from dir/go.mod.
// Returns empty string on any error.
func readGoModule(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			return fields[1]
		}
	}
	return ""
}
