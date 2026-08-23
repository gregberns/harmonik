package daemon

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

func ensureBeadsMergeDriver(ctx context.Context, projectDir string) {
	start := time.Now()

	checkCmd := exec.CommandContext(ctx, "git", "-C", projectDir,
		"config", "--local", "merge."+beadsMergeDriverName+".driver")
	out, err := checkCmd.Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		return
	}

	nameCmd := exec.CommandContext(ctx, "git", "-C", projectDir,
		"config", "--local",
		"merge."+beadsMergeDriverName+".name",
		beadsMergeDriverLabel)
	if nameErr := nameCmd.Run(); nameErr != nil {
		slog.WarnContext(ctx, "beads-union driver: could not set merge name",
			"driver", beadsMergeDriverName, "error", nameErr)
	}

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
