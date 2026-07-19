package main

// promote_cmd.go — `harmonik promote` subcommand.
//
// Implements two promotion modes:
//
//  1. push-mode: `harmonik promote <sha>...`
//     Cherry-picks the given reviewed SHA(s) onto the target branch in a temp
//     worktree, runs a build gate, and pushes race-safely with up to 3 non-ff
//     rebase retries. Formalises the captain bypass-SOP.
//
//  2. PR-mode: `harmonik promote --pr`
//     Opens a PR from --from (default "integration") onto the target branch via
//     `gh pr create`. Never pushes directly to the target. Required when the
//     target branch is protected.
//
// Protection gate (fail-closed, both modes):
//
//	If the resolved target branch is present in the project's protect_branches
//	(from .harmonik/branching.yaml or --protect-branch flags), push-mode is
//	refused with a clear message directing the operator to --pr.
//
// Exit codes:
//
//	0   success
//	1   argument / flag / config error
//	2   conflict during cherry-pick
//	3   build gate failed
//	4   push failed (all retries exhausted)
//	5   protection gate: push-mode refused on protected branch
//
// Spec ref: specs/promote.md.
// Bead ref: hk-pk3p1 (promote push-mode + PR-mode, reconciles hk-gax8v).

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gregberns/harmonik/internal/branching"
)

// beadIDInSubjectRE matches a harmonik bead ID parenthetical anywhere in a
// commit subject, e.g. "(hk-abc123)".  Used to auto-detect the bead ID from
// the source commit when --bead is not explicitly provided.
var beadIDInSubjectRE = regexp.MustCompile(`\((hk-[a-z0-9]+)\)`)

// extractBeadIDFromSubject returns the last bead ID found in subject as
// "(hk-xxx)", or "" if none is present.
func extractBeadIDFromSubject(subject string) string {
	matches := beadIDInSubjectRE.FindAllStringSubmatch(subject, -1)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1][1]
}

const promoteUsage = `harmonik promote — promote work toward the target branch

USAGE
  harmonik promote <sha>...         push-mode: cherry-pick SHA(s) -> target, build-gate, push
  harmonik promote --pr             PR-mode:   open a PR from --from to target via 'gh pr create'

FLAGS
  --project <dir>     Project root (default: cwd or $HARMONIK_PROJECT)
  --target <branch>   Target branch (default: branching.yaml lands_on, else "main")
  --bead <id>         Bead ID to stamp as Harmonik-Bead-ID trailer on cherry-picked commits
                      (enables reconcile auto-close; auto-detected from subject "(hk-xxx)" if absent)
  --pr                PR-mode; mutually exclusive with positional SHA arguments
  --from <branch>     PR-mode: head branch for the PR (default: "integration")
  --title <text>      PR-mode: PR title (passthrough to gh pr create)
  --body <text>       PR-mode: PR body  (passthrough to gh pr create)
  --dry-run           Print planned actions without mutating anything

PROTECTION GATE
  If the resolved target is in the project's protect_branches list, push-mode
  is refused fail-closed. Use --pr to open a pull request instead.

EXIT CODES
  0   Success
  1   Argument / flag / config error
  2   Cherry-pick conflict
  3   Build gate failed
  4   Push failed (all retries exhausted)
  5   Push-mode refused: target branch is protected

EXAMPLES
  harmonik promote abc1234
  harmonik promote abc1234 def5678
  harmonik promote --target integration abc1234
  harmonik promote --pr
  harmonik promote --pr --from integration --title "Sprint 23"
  harmonik promote --dry-run abc1234
`

const maxPromotePushAttempts = 3

// runPromoteSubcommand dispatches `harmonik promote [flags] [sha...]`.
// subArgs is os.Args[2:].
func runPromoteSubcommand(subArgs []string) int {
	if len(subArgs) == 0 || subArgs[0] == "--help" || subArgs[0] == "-h" {
		fmt.Print(promoteUsage) //nolint:forbidigo // help to stdout
		return 0
	}

	cfg, parseErr := parsePromoteFlags(subArgs)
	if parseErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote: %v\n", parseErr)
		return 1
	}

	// Resolve project root.
	projectDir := cfg.projectDir
	if projectDir == "" {
		if v := os.Getenv("HARMONIK_PROJECT"); v != "" {
			projectDir = v
		} else {
			wd, wdErr := os.Getwd()
			if wdErr != nil {
				fmt.Fprintf(os.Stderr, "harmonik promote: cannot determine working directory: %v\n", wdErr)
				return 1
			}
			projectDir = wd
		}
	}
	absProject, absErr := filepath.Abs(projectDir)
	if absErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote: cannot resolve project path %q: %v\n", projectDir, absErr)
		return 1
	}
	projectDir = absProject

	// Load branching config and resolve target.
	branchingDefaults, branchingErr := branching.Load(projectDir)
	if branchingErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote: load .harmonik/branching.yaml: %v\n", branchingErr)
		return 1
	}

	target := cfg.target
	if target == "" && branchingDefaults.LandsOn != "" {
		target = branchingDefaults.LandsOn
	}
	if target == "" {
		target = "main"
	}

	// Merge protect_branches: yaml then flag overrides.
	protectBranches := branchingDefaults.ProtectBranches
	if len(cfg.protectBranches) > 0 {
		protectBranches = cfg.protectBranches
	}

	// Protection gate: refuse push-mode for protected branches.
	if !cfg.prMode {
		for _, pb := range protectBranches {
			if target == pb {
				fmt.Fprintf(os.Stderr,
					"harmonik promote: target %q is a protected branch; use 'harmonik promote --pr' to open a pull request instead\n",
					target)
				return 5
			}
		}
	}

	ctx := context.Background()

	if cfg.prMode {
		return runPromotePR(ctx, projectDir, target, cfg)
	}
	return runPromotePush(ctx, projectDir, target, cfg)
}

// promoteConfig holds parsed flags for the promote subcommand.
type promoteConfig struct {
	projectDir      string
	target          string
	beadID          string // optional: stamp Harmonik-Bead-ID trailer on cherry-picked commits
	prMode          bool
	from            string
	title           string
	body            string
	dryRun          bool
	shas            []string
	protectBranches []string // from --protect-branch flags (operator override)
}

// parsePromoteFlags parses promote subcommand flags from args (os.Args[2:]).
func parsePromoteFlags(args []string) (promoteConfig, error) {
	var cfg promoteConfig
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			// handled before parsePromoteFlags is called
		case arg == "--project" && i+1 < len(args):
			i++
			cfg.projectDir = args[i]
		case strings.HasPrefix(arg, "--project="):
			cfg.projectDir = strings.TrimPrefix(arg, "--project=")
		case arg == "--target" && i+1 < len(args):
			i++
			cfg.target = args[i]
		case strings.HasPrefix(arg, "--target="):
			cfg.target = strings.TrimPrefix(arg, "--target=")
		case arg == "--bead" && i+1 < len(args):
			i++
			cfg.beadID = args[i]
		case strings.HasPrefix(arg, "--bead="):
			cfg.beadID = strings.TrimPrefix(arg, "--bead=")
		case arg == "--pr":
			cfg.prMode = true
		case arg == "--from" && i+1 < len(args):
			i++
			cfg.from = args[i]
		case strings.HasPrefix(arg, "--from="):
			cfg.from = strings.TrimPrefix(arg, "--from=")
		case arg == "--title" && i+1 < len(args):
			i++
			cfg.title = args[i]
		case strings.HasPrefix(arg, "--title="):
			cfg.title = strings.TrimPrefix(arg, "--title=")
		case arg == "--body" && i+1 < len(args):
			i++
			cfg.body = args[i]
		case strings.HasPrefix(arg, "--body="):
			cfg.body = strings.TrimPrefix(arg, "--body=")
		case arg == "--dry-run":
			cfg.dryRun = true
		case arg == "--protect-branch" && i+1 < len(args):
			i++
			cfg.protectBranches = append(cfg.protectBranches, args[i])
		case strings.HasPrefix(arg, "--protect-branch="):
			cfg.protectBranches = append(cfg.protectBranches, strings.TrimPrefix(arg, "--protect-branch="))
		case strings.HasPrefix(arg, "-"):
			return promoteConfig{}, fmt.Errorf("unknown flag %q", arg)
		default:
			cfg.shas = append(cfg.shas, arg)
		}
	}

	// Mutual-exclusion: --pr and positional SHA args.
	if cfg.prMode && len(cfg.shas) > 0 {
		return promoteConfig{}, fmt.Errorf("--pr and positional SHA arguments are mutually exclusive")
	}
	// push-mode requires at least one SHA.
	if !cfg.prMode && len(cfg.shas) == 0 {
		return promoteConfig{}, fmt.Errorf("push-mode requires at least one SHA argument (or use --pr for PR-mode)")
	}

	// Default --from for PR-mode.
	if cfg.from == "" {
		cfg.from = "integration"
	}

	return cfg, nil
}

// runPromotePush implements push-mode: cherry-pick SHA(s) into a temp worktree
// at origin/<target>, run build gate, race-safe push (up to 3 retries on non-ff).
func runPromotePush(ctx context.Context, projectDir, target string, cfg promoteConfig) int {
	if cfg.dryRun {
		fmt.Printf("harmonik promote (dry-run): would cherry-pick %s onto %q in a temp worktree\n",
			strings.Join(cfg.shas, " "), target)
		if cfg.beadID != "" {
			fmt.Printf("harmonik promote (dry-run): would stamp Harmonik-Bead-ID: %s trailer on cherry-picked commit(s)\n", cfg.beadID)
		} else {
			fmt.Printf("harmonik promote (dry-run): would auto-detect bead ID from commit subject (hk-xxx) and stamp Harmonik-Bead-ID trailer if found\n")
		}
		fmt.Printf("harmonik promote (dry-run): would run: go build ./... && go vet ./...\n")
		fmt.Printf("harmonik promote (dry-run): would push: git push origin HEAD:%s (with up to %d non-ff retries)\n",
			target, maxPromotePushAttempts)
		return 0
	}

	// Step 1: fetch origin/<target> to get the latest remote tip.
	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", target)
	fetchCmd.Dir = projectDir
	if fetchOut, fetchErr := fetchCmd.CombinedOutput(); fetchErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote: git fetch origin %s: %v\n%s\n", target, fetchErr, fetchOut)
		return 1
	}

	// Resolve the remote tip SHA.
	remoteRef := "refs/remotes/origin/" + target
	remoteRevCmd := exec.CommandContext(ctx, "git", "rev-parse", remoteRef)
	remoteRevCmd.Dir = projectDir
	remoteRevOut, remoteRevErr := remoteRevCmd.Output()
	if remoteRevErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote: git rev-parse %s: %v\n", remoteRef, remoteRevErr)
		return 1
	}
	remoteTip := strings.TrimSpace(string(remoteRevOut))

	// Step 2: create a temp worktree rooted at the remote tip (detached HEAD).
	tmpDir, tmpErr := os.MkdirTemp("", "hk-promote-*")
	if tmpErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote: MkdirTemp: %v\n", tmpErr)
		return 1
	}
	// Always clean up the temp worktree on exit. If `git worktree remove`
	// fails (e.g. a dirty tree from an aborted cherry-pick), removing the
	// directory alone leaves a stale registration behind, so follow up with
	// `git worktree prune` to drop it.
	defer func() {
		rmCmd := exec.Command("git", "worktree", "remove", "--force", tmpDir) //nolint:gosec
		rmCmd.Dir = projectDir
		if rmErr := rmCmd.Run(); rmErr != nil {
			_ = os.RemoveAll(tmpDir) //nolint:errcheck // best-effort cleanup of a temp worktree dir; nothing to recover on failure
			pruneCmd := exec.CommandContext(ctx, "git", "worktree", "prune")
			pruneCmd.Dir = projectDir
			_ = pruneCmd.Run() //nolint:errcheck // best-effort stale-registration prune; nothing to recover on failure
		}
	}()

	addCmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", tmpDir, remoteTip)
	addCmd.Dir = projectDir
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote: git worktree add: %v\n%s\n", addErr, addOut)
		return 1
	}

	// Step 3: cherry-pick each SHA in order (-x records provenance).
	for _, sha := range cfg.shas {
		cpCmd := exec.CommandContext(ctx, "git", "cherry-pick", "-x", sha)
		cpCmd.Dir = tmpDir
		if cpOut, cpErr := cpCmd.CombinedOutput(); cpErr != nil {
			// Abort the cherry-pick to leave the worktree clean for removal.
			abortCmd := exec.CommandContext(ctx, "git", "cherry-pick", "--abort")
			abortCmd.Dir = tmpDir
			_ = abortCmd.Run()
			fmt.Fprintf(os.Stderr, "harmonik promote: cherry-pick %s failed (conflict or error):\n%s\n", sha, cpOut)
			return 2
		}

		// Step 3a: stamp Harmonik-Bead-ID trailer so 'harmonik reconcile' can
		// auto-close the salvaged bead.  Without this trailer the cherry-picked
		// commit is a plain commit that reconcile.go:GitMergeCommitScanner cannot
		// match, leaving the bead stranded in_progress forever (hk-53p3).
		//
		// hk-tnui (accepted exception): Reviewed-By / Review-Verdict trailers are
		// NOT re-stamped here. git cherry-pick -x preserves the full source-commit
		// message, so any trailers already present on the source commit are
		// inherited verbatim. When the source commit lacks trailers (older runs
		// before hk-dyim, or DOT runs before hk-tnui), the cherry-pick also lacks
		// them — re-synthesising a verdict with no backing workspace file would
		// produce misleading audit data. Accept this as a known gap: future
		// promotes of daemon-merged commits will carry trailers automatically.
		beadID := cfg.beadID
		if beadID == "" {
			// Auto-detect from the source commit's subject "(hk-xxx)" parenthetical.
			subjectCmd := exec.CommandContext(ctx, "git", "-C", projectDir, "log", "-1", "--format=%s", sha)
			if subjectOut, subjectErr := subjectCmd.Output(); subjectErr == nil {
				beadID = extractBeadIDFromSubject(strings.TrimSpace(string(subjectOut)))
			}
		}
		if beadID != "" {
			amendCmd := exec.CommandContext(ctx, "git", "commit", "--amend", "--no-edit",
				"--trailer", "Harmonik-Bead-ID: "+beadID)
			amendCmd.Dir = tmpDir
			if amendOut, amendErr := amendCmd.CombinedOutput(); amendErr != nil {
				// Non-fatal: push proceeds; warn that reconcile auto-close will not work.
				fmt.Fprintf(os.Stderr,
					"harmonik promote: warning: failed to stamp Harmonik-Bead-ID: %s trailer on cherry-pick of %s: %v\n%s\n",
					beadID, sha, amendErr, amendOut)
			} else {
				fmt.Fprintf(os.Stderr, "harmonik promote: stamped Harmonik-Bead-ID: %s trailer on cherry-pick of %s\n", beadID, sha)
			}
		}
	}

	// Step 4 + retry loop: build gate → race-safe push.
	for attempt := 1; attempt <= maxPromotePushAttempts; attempt++ {
		// Build gate: go build + go vet (only when go.mod is present).
		if _, goModErr := os.Stat(filepath.Join(tmpDir, "go.mod")); goModErr == nil {
			for _, buildArgs := range [][]string{
				{"build", "./..."},
				{"vet", "./..."},
			} {
				gateCmd := exec.CommandContext(ctx, "go", buildArgs...)
				gateCmd.Dir = tmpDir
				if gateOut, gateErr := gateCmd.CombinedOutput(); gateErr != nil {
					fmt.Fprintf(os.Stderr, "harmonik promote: build gate failed (go %s):\n%s\n",
						buildArgs[0], gateOut)
					return 3
				}
			}
		}

		// Race-safe push: HEAD → target.
		pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "HEAD:"+target)
		pushCmd.Dir = tmpDir
		pushOut, pushErr := pushCmd.CombinedOutput()
		if pushErr == nil {
			// Print the pushed tip.
			tipCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
			tipCmd.Dir = tmpDir
			if tipOut, tipErr := tipCmd.Output(); tipErr == nil {
				fmt.Printf("harmonik promote: pushed %s SHA(s) to %s — tip %s\n",
					strings.Join(cfg.shas, " "), target, strings.TrimSpace(string(tipOut)))
			}
			return 0
		}

		pushOutStr := string(pushOut)
		isNonFF := strings.Contains(pushOutStr, "non-fast-forward") ||
			strings.Contains(pushOutStr, "[rejected]")

		if !isNonFF || attempt >= maxPromotePushAttempts {
			fmt.Fprintf(os.Stderr, "harmonik promote: push failed (attempt %d/%d): %v\n%s\n",
				attempt, maxPromotePushAttempts, pushErr, pushOut)
			return 4
		}

		// Non-ff: fetch, rebase cherry-picks onto new remote tip, retry.
		fmt.Fprintf(os.Stderr, "harmonik promote: non-fast-forward push (attempt %d/%d); fetching and rebasing\n",
			attempt, maxPromotePushAttempts)

		retryFetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin", target)
		retryFetchCmd.Dir = projectDir
		if retryFetchOut, retryFetchErr := retryFetchCmd.CombinedOutput(); retryFetchErr != nil {
			fmt.Fprintf(os.Stderr, "harmonik promote: fetch for retry failed: %v\n%s\n", retryFetchErr, retryFetchOut)
			return 4
		}

		// Rebase the cherry-picks in the temp worktree onto the updated remote tip.
		// git rebase in detached HEAD mode: rebase current commits onto origin/<target>.
		rebaseCmd := exec.CommandContext(ctx, "git", "rebase", "origin/"+target)
		rebaseCmd.Dir = tmpDir
		if rebaseOut, rebaseErr := rebaseCmd.CombinedOutput(); rebaseErr != nil {
			abortCmd := exec.CommandContext(ctx, "git", "rebase", "--abort")
			abortCmd.Dir = tmpDir
			_ = abortCmd.Run()
			fmt.Fprintf(os.Stderr, "harmonik promote: rebase conflict on push retry %d: %v\n%s\n",
				attempt, rebaseErr, rebaseOut)
			return 4
		}
		// Loop: re-run build gate + push with the rebased tree.
	}

	// Should not be reached due to loop logic above.
	fmt.Fprintln(os.Stderr, "harmonik promote: push failed after all retries")
	return 4
}

// runPromotePR implements PR-mode: open a PR via `gh pr create`.
func runPromotePR(ctx context.Context, projectDir, target string, cfg promoteConfig) int {
	// Verify gh is on PATH.
	ghPath, ghErr := exec.LookPath("gh")
	if ghErr != nil {
		fmt.Fprintln(os.Stderr, "harmonik promote --pr: 'gh' not found on PATH; install the GitHub CLI to use PR-mode")
		return 1
	}

	ghArgs := []string{"pr", "create", "--base", target, "--head", cfg.from}
	if cfg.title != "" {
		ghArgs = append(ghArgs, "--title", cfg.title)
	}
	if cfg.body != "" {
		ghArgs = append(ghArgs, "--body", cfg.body)
	}

	if cfg.dryRun {
		fmt.Printf("harmonik promote --pr (dry-run): would run: %s %s\n", ghPath, strings.Join(ghArgs, " "))
		return 0
	}

	ghCmd := exec.CommandContext(ctx, ghPath, ghArgs...) //nolint:gosec
	ghCmd.Dir = projectDir
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr
	if ghErr := ghCmd.Run(); ghErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik promote --pr: gh pr create failed: %v\n", ghErr)
		return 1
	}
	return 0
}
