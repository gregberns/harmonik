package main

// reconcile.go — `harmonik reconcile` subcommand implementation.
//
// # Purpose (hk-lgtq2)
//
// Runs the Cat 3a / Cat 3c on-demand reconciler against the project's bead
// ledger. This is the minimum deliverable from hk-lgtq2: an operator-facing
// command that detects and closes beads that are in_progress despite their
// implementation having already landed in git.
//
// The dogfood trigger: hk-iuaed.4 was IN_PROGRESS despite landing at 9779f72
// (6 commits prior). Had to manually `br close` before hk-iuaed.6 became ready.
// `harmonik reconcile` automates this step.
//
// # What it does
//
//  1. Lists all beads currently in coarse status `in_progress`.
//  2. For each, scans git log for a commit bearing the trailer
//     `Harmonik-Bead-ID: <bead_id>` on the target branch (default: main).
//  3. If found, the bead is closed via `br close <bead_id>` (Cat 3c auto-resolve).
//  4. Reports counts to stdout and exits 0 on success.
//
// # Exit codes
//
//	0  — success (all subsumed beads closed; zero matches is also success)
//	1  — argument / validation / adapter error
//	2  — at least one close write failed (partial reconciliation)
//
// # Grammar (OQ-RC-005 tracking operator-CLI grammar)
//
//	harmonik reconcile [--project DIR] [--target-branch BRANCH]
//
// Spec refs:
//   - specs/reconciliation/spec.md §8.6 Cat 3c — "inverse premature-close".
//   - specs/reconciliation/spec.md §4.5 RC-020a — on-demand dispatch trigger.
//
// Bead ref: hk-lgtq2.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// runReconcileSubcommand implements `harmonik reconcile [--project DIR] [--target-branch BRANCH]`.
// subArgs is os.Args[2:] (everything after "reconcile").
func runReconcileSubcommand(subArgs []string) int {
	// --- Parse flags ---

	projectDirFlag := ""
	targetBranchFlag := ""
	for i := 0; i < len(subArgs); i++ {
		switch {
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
		case strings.HasPrefix(subArgs[i], "-"):
			fmt.Fprintf(os.Stderr, "harmonik reconcile: unknown flag %q\n", subArgs[i])
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik reconcile: unexpected positional argument %q\n", subArgs[i])
			fmt.Fprintln(os.Stderr, "usage: harmonik reconcile [--project DIR] [--target-branch BRANCH]")
			return 1
		}
	}

	// --- Resolve project directory ---

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

	// --- Resolve br binary ---

	brPath, brErr := exec.LookPath("br")
	if brErr != nil {
		fmt.Fprintln(os.Stderr, "harmonik reconcile: 'br' not found on PATH — bead ledger required")
		return 1
	}

	// --- Construct adapter ---

	adapter, adapterErr := brcli.NewForProject(brPath, projectDir)
	if adapterErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik reconcile: cannot initialise brcli adapter: %v\n", adapterErr)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// --- List in_progress beads ---

	beads, listErr := adapter.ListInFlightBeads(ctx)
	if listErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik reconcile: cannot list in_progress beads: %v\n", listErr)
		return 1
	}
	if len(beads) == 0 {
		fmt.Fprintln(os.Stderr, "harmonik reconcile: no in_progress beads — nothing to reconcile")
		return 0
	}
	fmt.Fprintf(os.Stderr, "harmonik reconcile: found %d in_progress bead(s), scanning git log for subsumed beads...\n", len(beads))

	// --- Cat 3c: detect and close subsumed beads ---

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

		// Cat 3c: implementation has landed. Close the bead.
		fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — merge commit found on %s (subsumed); closing...\n", bead.BeadID, targetBranchFlag)
		if closeErr := adapter.SweepCloseBead(ctx, timeoutCfg, bead.BeadID); closeErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — close failed: %v\n", bead.BeadID, closeErr)
			failed++
			continue
		}
		fmt.Fprintf(os.Stderr, "harmonik reconcile: bead %s — closed (Cat 3c auto-resolve)\n", bead.BeadID)
		closed++
	}

	// --- Report ---

	fmt.Fprintf(os.Stderr, "harmonik reconcile: done — closed=%d skipped=%d failed=%d\n", closed, skipped, failed)
	if failed > 0 {
		return 2
	}
	return 0
}
