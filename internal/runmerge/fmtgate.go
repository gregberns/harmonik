package runmerge

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

func runMergeFmtCheck(ctx context.Context, buildDir, projectDir string, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (outcome *Outcome, newRunTip string) {
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

// fmtGateCommitMessage is the message commitFmtChanges commits. Named so the
// commit-message gate test reads what production writes — see
// stripRunContextCommitMessage for why that matters.
func fmtGateCommitMessage(beadID core.BeadID) string {
	return fmt.Sprintf("chore: auto-format via gofumpt+gci\n\nRefs: %s\nTrivial: true", beadID)
}

func commitFmtChanges(ctx context.Context, buildDir string, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (outcome *Outcome, newRunTip string) {
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = buildDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, addErr, addOut)
		return &Outcome{Success: false, Reason: "merge_fmt_failed (git add): " + addErr.Error()}, ""
	}

	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", fmtGateCommitMessage(beadID)) //nolint:gosec // G204: fixed git/go binary with controlled args (config target branch, git SHAs, module path) — not user input
	commitCmd.Dir = buildDir
	if commitOut, commitErr := commitCmd.CombinedOutput(); commitErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, commitErr, commitOut)
		return &Outcome{Success: false, Reason: "merge_fmt_failed (fmt commit): " + commitErr.Error()}, ""
	}

	if newTip, rerr := gitprobe.RevParse(ctx, buildDir, "HEAD"); rerr == nil {
		return nil, newTip
	}
	return nil, ""
}

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
