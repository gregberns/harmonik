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

// fmtToolWords recovers what a format tool said about its own failure.
//
// The two probe passes read the tool's standard OUTPUT to decide whether the
// tree is dirty, so they must not fold its standard error into that stream. A
// tool that exits zero and writes a warning — a deprecation notice, a skipped
// file — would then read as drift, and the gate would format a clean tree and
// try to commit it with nothing staged. That kills a merge over a warning,
// which is the same fault this bead exists to remove.
//
// Standard error is still worth reading once the tool has FAILED, and it
// survives on the error the failed run returned. Bead: hk-7neu1.
func fmtToolWords(err error, out []byte) []byte {
	// Standard error comes first. On a failure that is where the diagnostic is,
	// and standard output still holds the list of files that need formatting.
	// Print the list alone and the operator reads a drift report that names a
	// file which merely needs formatting, and never sees the file that broke.
	// That is a confident wrong answer, which is worse than a bare exit status.
	var words []string
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if trimmed := strings.TrimRight(string(exitErr.Stderr), "\n"); trimmed != "" {
			words = append(words, trimmed)
		}
	}
	if trimmed := strings.TrimRight(string(out), "\n"); trimmed != "" {
		words = append(words, trimmed)
	}
	if len(words) == 0 {
		return nil
	}
	return []byte(strings.Join(words, "\n"))
}

// fmtToolFailure builds the reason text for a format tool that failed, and it
// keeps the tool's own words. The gate used to throw that output away, so an
// operator was left with nothing to read but the exit status.
func fmtToolFailure(prefix string, err error, out []byte) string {
	reason := prefix + err.Error()
	if words := fmtToolWords(err, out); len(words) > 0 {
		reason += "\n" + string(words)
	}
	return reason
}

func fmtGofumptPass(ctx context.Context, buildDir, gofumptBin string, canAutoFmt bool, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (bool, *Outcome) {
	// Listing the unformatted files reads the tree and changes nothing, so a
	// stopped child can run again.
	out, err := gitprobe.CommandOutput(ctx, buildDir, gofumptBin, "-l", ".")
	if err != nil {
		// An error here is a real failure, not an absent tool: the caller has
		// already checked that gofumpt is there. Reading it as "everything is
		// formatted" let a stopped child pass the gate.
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New("gofumpt -l: "+err.Error()), fmtToolWords(err, out))
		return false, &Outcome{Success: false, Reason: fmtToolFailure("merge_fmt_failed (gofumpt -l): ", err, out)}
	}
	if strings.TrimSpace(string(out)) == "" {
		return false, nil
	}
	if !canAutoFmt {
		msg := "gofumpt: unformatted files (run 'make fmt' to fix):\n" + strings.TrimRight(string(out), "\n")
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
		return false, &Outcome{Success: false, Reason: "merge_fmt_failed (gofumpt): " + strings.TrimRight(string(out), "\n")}
	}
	// Formatting is idempotent, so a stopped child can run again.
	fmtOut, fmtErr := gitprobe.CommandCombinedOutput(ctx, buildDir, gofumptBin, "-w", ".")
	if fmtErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New("gofumpt -w: "+fmtErr.Error()), fmtOut)
		return false, &Outcome{Success: false, Reason: fmtToolFailure("merge_fmt_failed (gofumpt -w): ", fmtErr, fmtOut)}
	}
	return true, nil
}

func fmtGciPass(ctx context.Context, buildDir, gciBin, mod string, canAutoFmt bool, runID core.RunID, beadID core.BeadID, bus handlercontract.EventEmitter) (bool, *Outcome) {
	// The diff reads the tree and changes nothing, so a stopped child can run
	// again.
	out, err := gitprobe.CommandOutput(ctx, buildDir, gciBin, "diff", "-s", "standard", "-s", "default", "-s", "prefix("+mod+")", ".")
	if err != nil {
		// The caller has already checked that gci is there, so this is a real
		// failure. Reading it as "the imports are in order" let a stopped child
		// pass the gate.
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New("gci diff: "+err.Error()), fmtToolWords(err, out))
		return false, &Outcome{Success: false, Reason: fmtToolFailure("merge_fmt_failed (gci diff): ", err, out)}
	}
	if strings.TrimSpace(string(out)) == "" {
		return false, nil
	}
	if !canAutoFmt {
		msg := "gci: import order drift (run 'make fmt' to fix):\n" + strings.TrimRight(string(out), "\n")
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New(msg), nil)
		return false, &Outcome{Success: false, Reason: "merge_fmt_failed (gci): import order drift detected"}
	}
	// Rewriting the import order is idempotent, so a stopped child can run again.
	writeOut, writeErr := gitprobe.CommandCombinedOutput(ctx, buildDir, gciBin, "write", "-s", "standard", "-s", "default", "-s", "prefix("+mod+")", ".")
	if writeErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, errors.New("gci write: "+writeErr.Error()), writeOut)
		return false, &Outcome{Success: false, Reason: fmtToolFailure("merge_fmt_failed (gci write): ", writeErr, writeOut)}
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
	// Staging sets the index to an exact state, so a stopped child can run again.
	if addOut, addErr := gitprobe.CombinedOutput(ctx, buildDir, "add", "-A"); addErr != nil {
		emitMergeBuildFailed(ctx, bus, runID, beadID, addErr, addOut)
		return &Outcome{Success: false, Reason: "merge_fmt_failed (git add): " + addErr.Error()}, ""
	}

	// A commit does not mean the same thing twice, so it does not go through the
	// shared retry. runCommitOnce reads HEAD around the one attempt and runs the
	// commit again only when HEAD did not move.
	commitOut, commitErr := runCommitOnce(ctx, buildDir, func() ([]byte, error) {
		//nolint:gosec // G204: fixed git binary; the commit message is daemon-generated from a typed BeadID and a constant template.
		commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", fmtGateCommitMessage(beadID))
		commitCmd.Dir = buildDir
		return commitCmd.CombinedOutput()
	})
	if commitErr != nil {
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
