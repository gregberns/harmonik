package daemon

// beadsmergedriver.go — daemon startup auto-config for the beads-union git driver.
//
// # Why
//
// .gitattributes marks .beads/issues.jsonl with merge=beads-union, but git only
// invokes the driver when the corresponding merge.beads-union.driver entry exists
// in the repo's .git/config. Without it, git falls back to the default (lossy)
// text merge on the JSONL file.
//
// This pre-flight writes the driver entry once per clone, eliminating the manual
// `git config` step documented in .gitattributes. It runs at daemon startup so
// the driver is registered before any merge that the daemon might trigger.
//
// # Policy
//
// Check `git config --local merge.beads-union.driver`; if absent or empty, run:
//
//	git config --local merge.beads-union.name "Bead Ledger Union Merge"
//	git config --local merge.beads-union.driver "harmonik beads-merge %O %A %B %P"
//
// Both calls are non-fatal: a failure is logged as a warning and the daemon
// continues. The driver is still invoked by git when harmonik is on PATH, so
// the pre-flight is purely a convenience / correctness safety net.
//
// # Config escape hatch
//
// Config.SkipBeadsMergeDriverConfig = true disables the pre-flight entirely.
// Intended for unit tests that operate on temp directories without a real
// git repository.
//
// Bead ref: hk-r0y1o.

import (
	"context"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

const (
	beadsMergeDriverName   = "beads-union"
	beadsMergeDriverLabel  = "Bead Ledger Union Merge"
	beadsMergeDriverDriver = "harmonik beads-merge %O %A %B %P"
)

// ensureBeadsMergeDriver registers merge.beads-union.{name,driver} in the
// repo's .git/config if the driver entry is absent. Non-fatal.
func ensureBeadsMergeDriver(ctx context.Context, projectDir string) {
	start := time.Now()

	// Check whether the driver is already configured.
	checkCmd := exec.CommandContext(ctx, "git", "-C", projectDir,
		"config", "--local", "merge."+beadsMergeDriverName+".driver")
	out, err := checkCmd.Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		// Already set — nothing to do.
		return
	}

	// Register the human-readable name first (cosmetic; failure is non-fatal).
	nameCmd := exec.CommandContext(ctx, "git", "-C", projectDir,
		"config", "--local",
		"merge."+beadsMergeDriverName+".name",
		beadsMergeDriverLabel)
	if nameErr := nameCmd.Run(); nameErr != nil {
		slog.WarnContext(ctx, "beads-union driver: could not set merge name",
			"driver", beadsMergeDriverName, "error", nameErr)
	}

	// Register the driver invocation.
	driverCmd := exec.CommandContext(ctx, "git", "-C", projectDir,
		"config", "--local",
		"merge."+beadsMergeDriverName+".driver",
		beadsMergeDriverDriver)
	if driverErr := driverCmd.Run(); driverErr != nil {
		slog.WarnContext(ctx, "beads-union driver: could not set merge driver",
			"driver", beadsMergeDriverName, "error", driverErr)
		return
	}

	slog.InfoContext(ctx, "beads-union driver: registered in .git/config",
		"driver", beadsMergeDriverName, "elapsed_ms", time.Since(start).Milliseconds())
}
