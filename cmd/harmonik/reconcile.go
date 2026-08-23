package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

func reconcileUsage(w io.Writer) error {
	if _, err := fmt.Fprint(w, `harmonik reconcile — close in_progress beads whose implementation has merged

USAGE
  harmonik reconcile [--project DIR] [--target-branch BRANCH] [--run RUN_ID]

FLAGS
  --project DIR           Project directory (default: current working directory)
  --target-branch BRANCH  Git branch to scan for merge commits (default: main)
  --run RUN_ID            Scope the scan to a single in-flight run ID (OQ-RC-005)

EXIT CODES
  0  All subsumed beads closed (zero matches is also success)
  1  Argument or adapter error
  2  At least one bead close failed (partial reconciliation)

EXAMPLES
  harmonik reconcile
  harmonik reconcile --target-branch develop
  harmonik reconcile --project /path/to/project --target-branch main
  harmonik reconcile --run 019e8273-753b-7f3a-bc25-798c33bb8e63
`); err != nil {
		return err
	}
	return nil
}

func runReconcileSubcommand(subArgs []string) int {
	return runReconcileSubcommandIO(subArgs, os.Stdout)
}

func runReconcileSubcommandIO(subArgs []string, stdout io.Writer) int {
	projectDirFlag := ""
	targetBranchFlag := ""
	runIDFlag := "" // OQ-RC-005: scope scan to a single run_id
	for i := 0; i < len(subArgs); i++ {
		switch {
		case subArgs[i] == "--help" || subArgs[i] == "-h":
			if err := reconcileUsage(stdout); err != nil {
				return 1
			}
			return 0
		case subArgs[i] == "--project" && i+1 < len(subArgs):
			i++
			projectDirFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--project="):
			projectDirFlag = strings.TrimPrefix(subArgs[i], "--project=")
		case subArgs[i] == "--target-branch" && i+1 < len(subArgs):
			i++
			targetBranchFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--target-branch="):
			targetBranchFlag = strings.TrimPrefix(subArgs[i], "--target-branch=")
		case subArgs[i] == "--run" && i+1 < len(subArgs):
			i++
			runIDFlag = subArgs[i]
		case strings.HasPrefix(subArgs[i], "--run="):
			runIDFlag = strings.TrimPrefix(subArgs[i], "--run=")
		case strings.HasPrefix(subArgs[i], "-"):
			fmt.Fprintf(os.Stderr, "harmonik reconcile: unknown flag %q\n", subArgs[i])
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik reconcile: unexpected positional argument %q\n", subArgs[i])
			fmt.Fprintln(os.Stderr, "usage: harmonik reconcile [--project DIR] [--target-branch BRANCH] [--run RUN_ID]")
			return 1
		}
	}

	if projectDirFlag == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik reconcile: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDirFlag = wd
	}

	projectDir := projectDirFlag
	if _, err := os.Stat(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik reconcile: project directory %q does not exist or is not accessible: %v\n", projectDir, err)
		return 1
	}

	if targetBranchFlag == "" {
		targetBranchFlag = "main"
	}

	brPath, brErr := exec.LookPath("br")
	if brErr != nil {
		fmt.Fprintln(os.Stderr, "harmonik reconcile: 'br' not found on PATH — bead ledger required")
		return 1
	}

	adapter, adapterErr := brcli.NewForProject(brPath, projectDir)
	if adapterErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik reconcile: cannot initialise brcli adapter: %v\n", adapterErr)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	beads, listErr := adapter.ListInFlightBeads(ctx)
	if listErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik reconcile: cannot list in_progress beads: %v\n", listErr)
		return 1
	}
	if len(beads) == 0 {
		fmt.Fprintln(os.Stderr, "harmonik reconcile: no in_progress beads — nothing to reconcile")
		return 0
	}

	if runIDFlag != "" {
		beads = filterBeadsByRunID(ctx, projectDir, runIDFlag, beads)
		if len(beads) == 0 {
			fmt.Fprintf(os.Stderr, "harmonik reconcile: run %s not found in queue or has no in_progress bead — nothing to reconcile\n", runIDFlag)
			return 0
		}
	}

	fmt.Fprintf(os.Stderr, "harmonik reconcile: found %d in_progress bead(s), scanning git log for subsumed beads...\n", len(beads))

	mergeScanner := lifecycle.GitMergeCommitScanner{
		ProjectDir:   projectDir,
		TargetBranch: targetBranchFlag,
	}

	var closed, skipped, failed int
	timeoutCfg := brcli.TimeoutConfig{} // zero = defaults apply

	for _, bead := range beads {
		merged, scanErr := mergeScanner.HasMergeCommitForBead(ctx, bead.BeadID)
		if scanErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — git scan error: %v (skipping)\n", bead.BeadID, scanErr)
			skipped++
			continue
		}
		if !merged {
			fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — no merge commit on %s (not subsumed; skipping)\n", bead.BeadID, targetBranchFlag)
			skipped++
			continue
		}

		fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — merge commit found on %s (subsumed); closing...\n", bead.BeadID, targetBranchFlag)
		if closeErr := adapter.SweepCloseBead(ctx, timeoutCfg, bead.BeadID); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — close failed: %v\n", bead.BeadID, closeErr)
			failed++
			continue
		}
		fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — closed (Cat 3c auto-resolve)\n", bead.BeadID)
		closed++
	}

	fmt.Fprintf(os.Stderr, "harmonik reconcile: done — closed=%d skipped=%d failed=%d\n", closed, skipped, failed)
	if failed > 0 {
		return 2
	}
	return 0
}

func filterBeadsByRunID(ctx context.Context, projectDir, runID string, beads []core.BeadRecord) []core.BeadRecord {
	q, loadErr := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if loadErr != nil || q == nil {
		return beads
	}

	matchedBeadIDs := make(map[core.BeadID]struct{})
	for gi := range q.Groups {
		for _, item := range q.Groups[gi].Items {
			if item.RunID != nil && *item.RunID == runID {
				matchedBeadIDs[item.BeadID] = struct{}{}
			}
		}
	}
	if len(matchedBeadIDs) == 0 {
		return nil
	}

	var filtered []core.BeadRecord
	for _, b := range beads {
		if _, ok := matchedBeadIDs[b.BeadID]; ok {
			filtered = append(filtered, b)
		}
	}
	return filtered
}
