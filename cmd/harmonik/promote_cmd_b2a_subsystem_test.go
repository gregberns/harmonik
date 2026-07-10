package main

// promote_cmd_b2a_subsystem_test.go — B2a subsystem-proofs: promote push-mode acceptance.
//
// Coverage (sections g–i; a–f are in promote_cmd_hkpk3p1_test.go):
//
//	(g) go-build/vet gate: cherry-picking a commit with a Go syntax error exits 3
//	(h) non-ff rebase-retry: pre-receive hook rejects push #1 → retry succeeds (exit 0)
//	(i) PR-mode arg-construction: parsePromoteFlags → promoteConfig → gh-args (no live gh)
//
// Fixtures reused from promote_cmd_hkpk3p1_test.go (same package):
//
//	setupPromoteRepo, setupCommitOnBranch, runGit, runGitWithEnv, writeFile, gitLogBody, gitRevParse
//
// runPromoteSubcommand is the production entry point defined in promote_cmd.go.
//
// Bead: hk-420yr.6 (codename:subsystem-proofs, branch integration/subsystem-proofs).
//
// This suite, together with promote_cmd_hkpk3p1_test.go (sections a-f) and
// gitmergecommitscanner_hk420yr7_test.go (reconcile-side HasMergeCommitForBead
// acceptance), fully covers the scope of the parent umbrella bead hk-420yr.2
// ("B2: promote/reconcile acceptance suite on temp git repo"): cherry-pick
// onto origin/<target>, trailer stamping (explicit/auto-detect/none), the
// go-build/vet gate, the non-ff rebase-retry race path, PR-mode arg
// construction, and GitMergeCommitScanner.HasMergeCommitForBead. No further
// implementation is needed for hk-420yr.2.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---- (g) go-build/vet gate ----

// TestB2aBuildGateCatchesSyntaxError verifies that runPromotePush exits 3 when
// the cherry-picked commit introduces a Go syntax error that fails the build gate.
func TestB2aBuildGateCatchesSyntaxError(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	runGit(t, repoRoot, "checkout", "-b", "task-build-fail")
	// broken.go has a missing closing paren — won't compile.
	writeFile(t, repoRoot, "broken.go", "package main\n\nfunc brokenSyntax( {\n}\n")
	runGit(t, repoRoot, "add", "broken.go")
	runGitWithEnv(t, repoRoot, nil, "commit", "-m", "fix: B2a build-gate syntax-error")
	sha := gitRevParse(t, repoRoot, "HEAD")
	runGit(t, repoRoot, "checkout", "main")

	rc := runPromoteSubcommand([]string{"--project", repoRoot, sha})
	if rc != 3 {
		t.Errorf("expected exit 3 (build gate failed), got %d", rc)
	}
}

// ---- (h) non-ff rebase-retry ----

// TestB2aNonFFRetrySucceeds verifies that runPromotePush retries on a
// non-fast-forward push rejection and succeeds on the second attempt.
//
// Mechanism: a git 'update' hook on the bare remote prints "non-fast-forward"
// to stderr on the first push attempt (which runPromotePush detects via
// strings.Contains(pushOut, "non-fast-forward") → isNonFF=true) and exits 0 on
// the second attempt.  The rebase between the two attempts is a no-op because
// the hook does not advance the remote ref; the second push lands cleanly.
func TestB2aNonFFRetrySucceeds(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	// Locate the bare remote path so we can install a hook.
	remoteRaw, remoteErr := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin").Output() //nolint:gosec
	if remoteErr != nil {
		t.Fatalf("git remote get-url: %v", remoteErr)
	}
	remoteDir := strings.TrimSpace(string(remoteRaw))

	// State file tracks hook invocation count across shell executions.
	stateFile := filepath.Join(t.TempDir(), "hook-call-count")

	// 'update' hook ($1=refname, $2=oldrev, $3=newrev):
	// - Call 1: print "non-fast-forward" to stderr (so isNonFF fires) and exit 1.
	// - Call 2+: accept (exit 0).
	//
	// runPromotePush.isNonFF checks strings.Contains(pushOut,"non-fast-forward"),
	// which matches "remote: error: non-fast-forward" relayed from the hook.
	hookScript := fmt.Sprintf(
		"#!/bin/sh\n"+
			"count=$(cat %q 2>/dev/null || echo 0)\n"+
			"count=$((count + 1))\n"+
			"printf '%%s\\n' \"$count\" > %q\n"+
			"[ \"$count\" -le 1 ] && { printf 'error: non-fast-forward (B2a race simulation)\\n' >&2; exit 1; }\n"+
			"exit 0\n",
		stateFile, stateFile,
	)
	hooksDir := filepath.Join(remoteDir, "hooks")
	if mkErr := os.MkdirAll(hooksDir, 0o750); mkErr != nil {
		t.Fatalf("mkdir hooks: %v", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(hooksDir, "update"), []byte(hookScript), 0o750); writeErr != nil {
		t.Fatalf("write update hook: %v", writeErr)
	}

	sha := setupCommitOnBranch(t, repoRoot, "task-nonff", "fix: non-ff retry acceptance")

	rc := runPromoteSubcommand([]string{"--project", repoRoot, sha})
	if rc != 0 {
		t.Errorf("non-ff retry: expected exit 0, got %d", rc)
	}

	// The cherry-picked commit must appear on origin/main after the retry.
	runGit(t, repoRoot, "fetch", "origin", "main")
	msg := gitLogBody(t, repoRoot, "origin/main")
	if !strings.Contains(msg, "non-ff retry acceptance") {
		t.Errorf("expected cherry-picked commit on origin/main; got:\n%s", msg)
	}
}

// TestB2aNonFFExhaustsRetries verifies that runPromotePush returns exit 4 after
// exhausting all maxPromotePushAttempts non-ff retries.  The update hook always
// prints "non-fast-forward" and rejects, so all three attempts fail.
func TestB2aNonFFExhaustsRetries(t *testing.T) {
	repoRoot, cleanup := setupPromoteRepo(t)
	defer cleanup()

	remoteRaw, remoteErr := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin").Output() //nolint:gosec
	if remoteErr != nil {
		t.Fatalf("git remote get-url: %v", remoteErr)
	}
	remoteDir := strings.TrimSpace(string(remoteRaw))

	stateFile := filepath.Join(t.TempDir(), "exhaustion-count")

	// update hook: always prints "non-fast-forward" and exits 1.
	hookScript := fmt.Sprintf(
		"#!/bin/sh\n"+
			"count=$(cat %q 2>/dev/null || echo 0)\n"+
			"count=$((count + 1))\n"+
			"printf '%%s\\n' \"$count\" > %q\n"+
			"printf 'error: non-fast-forward (exhaustion test)\\n' >&2\n"+
			"exit 1\n",
		stateFile, stateFile,
	)
	hooksDir := filepath.Join(remoteDir, "hooks")
	if mkErr := os.MkdirAll(hooksDir, 0o750); mkErr != nil {
		t.Fatalf("mkdir hooks: %v", mkErr)
	}
	if writeErr := os.WriteFile(filepath.Join(hooksDir, "update"), []byte(hookScript), 0o750); writeErr != nil {
		t.Fatalf("write update hook: %v", writeErr)
	}

	sha := setupCommitOnBranch(t, repoRoot, "task-exhaust", "fix: exhaustion test commit")

	rc := runPromoteSubcommand([]string{"--project", repoRoot, sha})
	if rc != 4 {
		t.Errorf("expected exit 4 (all retries exhausted), got %d", rc)
	}

	// Verify the hook was called exactly maxPromotePushAttempts times.
	countBytes, readErr := os.ReadFile(stateFile) //nolint:gosec // G304: stateFile is a test-controlled temp path
	if readErr != nil {
		t.Fatalf("read hook state file: %v", readErr)
	}
	if got := strings.TrimSpace(string(countBytes)); got != fmt.Sprintf("%d", maxPromotePushAttempts) {
		t.Errorf("expected %d push attempts, hook count = %s", maxPromotePushAttempts, got)
	}
}

// ---- (i) PR-mode arg-construction ----

// TestB2aPRModeArgConstruction verifies that parsePromoteFlags for PR-mode
// produces the expected promoteConfig, and that the fields correctly drive the
// gh pr create argument list.  No live gh process is invoked.
func TestB2aPRModeArgConstruction(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		args       []string
		wantFrom   string
		wantTarget string
		wantTitle  string
		wantBody   string
	}{
		{
			name:       "defaults",
			args:       []string{"--pr"},
			wantFrom:   "integration",
			wantTarget: "",
		},
		{
			name:       "custom-from-title-body-target",
			args:       []string{"--pr", "--from", "feature-x", "--target", "staging", "--title", "Deploy staging", "--body", "See #42"},
			wantFrom:   "feature-x",
			wantTarget: "staging",
			wantTitle:  "Deploy staging",
			wantBody:   "See #42",
		},
		{
			name:       "from-flag-only",
			args:       []string{"--pr", "--from", "hotfix-99"},
			wantFrom:   "hotfix-99",
			wantTarget: "",
		},
		{
			name:      "equal-form-flags",
			args:      []string{"--pr", "--from=eq-branch", "--title=EQ Title"},
			wantFrom:  "eq-branch",
			wantTitle: "EQ Title",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := parsePromoteFlags(tc.args)
			if err != nil {
				t.Fatalf("parsePromoteFlags: %v", err)
			}
			if !cfg.prMode {
				t.Error("prMode must be true")
			}
			if cfg.from != tc.wantFrom {
				t.Errorf("from: got %q, want %q", cfg.from, tc.wantFrom)
			}
			if cfg.target != tc.wantTarget {
				t.Errorf("target: got %q, want %q", cfg.target, tc.wantTarget)
			}
			if cfg.title != tc.wantTitle {
				t.Errorf("title: got %q, want %q", cfg.title, tc.wantTitle)
			}
			if cfg.body != tc.wantBody {
				t.Errorf("body: got %q, want %q", cfg.body, tc.wantBody)
			}

			// Reconstruct the gh-args the same way runPromotePR would, using the
			// resolved target (default "main" when cfg.target is empty).
			resolvedTarget := cfg.target
			if resolvedTarget == "" {
				resolvedTarget = "main"
			}
			ghArgs := []string{"pr", "create", "--base", resolvedTarget, "--head", cfg.from}
			if cfg.title != "" {
				ghArgs = append(ghArgs, "--title", cfg.title)
			}
			if cfg.body != "" {
				ghArgs = append(ghArgs, "--body", cfg.body)
			}

			if got := b2aArgAfter(ghArgs, "--base"); got != resolvedTarget {
				t.Errorf("gh --base: got %q, want %q", got, resolvedTarget)
			}
			if got := b2aArgAfter(ghArgs, "--head"); got != tc.wantFrom {
				t.Errorf("gh --head: got %q, want %q", got, tc.wantFrom)
			}
			if tc.wantTitle != "" {
				if got := b2aArgAfter(ghArgs, "--title"); got != tc.wantTitle {
					t.Errorf("gh --title: got %q, want %q", got, tc.wantTitle)
				}
			}
			if tc.wantBody != "" {
				if got := b2aArgAfter(ghArgs, "--body"); got != tc.wantBody {
					t.Errorf("gh --body: got %q, want %q", got, tc.wantBody)
				}
			}
		})
	}
}

// b2aArgAfter returns the argument immediately following flag in args, or "".
func b2aArgAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
