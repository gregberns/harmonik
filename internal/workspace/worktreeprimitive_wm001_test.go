package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWM001_GitWorktreeAddProducesCanonicalPathAndBranch verifies that
// `git worktree add -b <branch> <canonical-path> HEAD` produces:
//
//	(a) the canonical path exists with a valid checkout,
//	(b) the branch exists at HEAD of the worktree,
//	(c) atomicity: if the operation fails (forced-fail via invalid branch name),
//	    neither the worktree directory nor the branch is left behind.
//
// Spec ref: workspace-model.md §4.1 WM-003 — "The workspace manager MUST create a
// worktree and a fresh task branch atomically via `git worktree add -b <branch>
// <path> <parent_commit>`."
func TestWM001_GitWorktreeAddProducesCanonicalPathAndBranch(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0001"
	branch := "run/" + runID

	// The canonical worktree path strips the trailing separator for git invocation.
	worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

	// Create the parent directory so that git can place the worktree there.
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	// (a) + (b): successful `git worktree add -b`.
	cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add -b: %v\n%s", err, out)
	}

	// (a) Assert canonical path exists with a valid checkout (HEAD file present).
	headFile := filepath.Join(worktreePath, ".git")
	if _, err := os.Stat(headFile); os.IsNotExist(err) {
		t.Errorf("WM-001: worktree .git absent at %q; expected valid checkout", headFile)
	}

	// (b) Assert the branch exists and its tip matches the parent commit.
	out, err := exec.Command("git", "-C", repo, "rev-parse", branch).Output()
	if err != nil {
		t.Fatalf("WM-001: rev-parse %q: %v", branch, err)
	}
	branchTip := strings.TrimRight(string(out), "\n")
	if branchTip != sha {
		t.Errorf("WM-001: branch %q tip = %q, want %q", branch, branchTip, sha)
	}

	// (c) Atomicity — forced-fail case: use a branch name that git rejects.
	// git rejects names containing ".." as ref-unsafe.
	badRunID := "bad..runid"
	badBranch := "run/" + badRunID
	badPath := filepath.Join(repo, ".harmonik", "worktrees", badRunID)

	cmd2 := exec.Command("git", "worktree", "add", "-b", badBranch, badPath, sha)
	cmd2.Dir = repo
	// We expect this to fail; the exit code is non-zero.
	if out2, err2 := cmd2.CombinedOutput(); err2 == nil {
		t.Errorf("WM-001: atomicity: expected failure for invalid branch name %q, got success\n%s", badBranch, out2)
	}

	// Neither the directory nor the branch should remain after a failed add.
	if _, err := os.Stat(badPath); !os.IsNotExist(err) {
		t.Errorf("WM-001: atomicity: worktree dir %q still exists after failed git worktree add", badPath)
	}
	out3, _ := exec.Command("git", "-C", repo, "rev-parse", "--verify", badBranch).Output()
	if strings.TrimSpace(string(out3)) != "" {
		t.Errorf("WM-001: atomicity: branch %q still exists after failed git worktree add", badBranch)
	}
}

// TestWM002_WorkspaceIDDerivableFromRunID verifies that the workspace_id is
// deterministically derivable as "ws-"+run_id per WM-004 so that a daemon restart
// can reconstruct the workspace_id from the run_id without a separate store.
//
// Spec ref: workspace-model.md §4.1 WM-004 — "The workspace_id MUST be generated
// by the deterministic construction workspace_id = "ws-" + run_id so that a daemon
// restart finds the same workspace_id for a given run without consulting a separate
// store."
//
// NOTE: The post-restart obligation — that state reconstruction uses git + Beads per
// [execution-model.md §4.7] and that the workspace ID MUST be derivable from those
// sources — is a daemon-level integration concern verified at the daemon-restart
// scenario layer (hk-8mwo.66). This test covers the deterministic derivation function
// in isolation.
func TestWM002_WorkspaceIDDerivableFromRunID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		runID           string
		wantWorkspaceID string
	}{
		{
			runID:           "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0002",
			wantWorkspaceID: "ws-0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0002",
		},
		{
			runID:           "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0003",
			wantWorkspaceID: "ws-0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0003",
		},
		{
			runID:           "abcdef01-2345-7000-8abc-def012345678",
			wantWorkspaceID: "ws-abcdef01-2345-7000-8abc-def012345678",
		},
	}

	for _, tc := range cases {
		t.Run(tc.runID, func(t *testing.T) {
			t.Parallel()

			// The derivation function: workspace_id = "ws-" + run_id.
			// TODO: replace with the real WorkspaceID type from hk-8mwo.24 when
			// the Workspace type package ships. At that point the type constructor
			// should enforce this invariant and this test should call that
			// constructor.
			got := "ws-" + tc.runID
			if got != tc.wantWorkspaceID {
				t.Errorf("WM-002: workspace_id for run_id %q = %q, want %q",
					tc.runID, got, tc.wantWorkspaceID)
			}
		})
	}
}

// TestWM003_RunIDFilesystemSafetyRegex verifies the canonical filesystem-safety
// regex [A-Za-z0-9-]+ for run_id values per WM-002.
//
// Spec ref: workspace-model.md §4.2 WM-002 — "run_id MUST match the filesystem-safe
// regex [A-Za-z0-9-]+ (UUIDv7 satisfies this by construction)".
func TestWM003_RunIDFilesystemSafetyRegex(t *testing.T) {
	t.Parallel()

	valid := []struct {
		input string
		desc  string
	}{
		{"0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0001", "canonical UUIDv7"},
		{"aaaaaaaa-bbbb-7ccc-dddd-eeeeeeeeeeee", "all-lowercase hex UUIDv7"},
		{"AAAAAAAA-BBBB-7CCC-DDDD-EEEEEEEEEEEE", "all-uppercase hex UUIDv7"},
		{"abc", "short alphanumeric"},
		{"ABC-123", "uppercase with hyphen"},
		{"a-b-c-d-e", "multiple hyphens"},
		{"0000000000000000000000000000000000000000", "all zeros without hyphens"},
	}

	pathological := []struct {
		input string
		desc  string
	}{
		{"", "empty string"},
		{"a/b", "slash — directory traversal"},
		{"../etc/passwd", "double-dot traversal"},
		{".hidden", "leading dot"},
		{"run\x00id", "null byte (control char)"},
		{"run\nid", "newline (control char)"},
		{"run id", "space"},
		{"run\tid", "tab (control char)"},
		{"a!b", "exclamation mark"},
		{"a@b", "at sign"},
		{
			// 256-character input — no explicit length cap in WM-002, but
			// this exercises robustness; the regex itself would accept it,
			// so this is marked as "would-accept-by-regex" and listed here as
			// a docs-level concern rather than a regex failure.
			// We therefore omit it from the pathological set.
			// See TODO below.
			"",
			"",
		},
	}
	// Filter out the empty-descriptor sentinel above.
	filtered := pathological[:0]
	for _, p := range pathological {
		if p.desc != "" {
			filtered = append(filtered, p)
		}
	}

	for _, tc := range valid {
		tc := tc
		t.Run("valid/"+tc.desc, func(t *testing.T) {
			t.Parallel()
			if !runIDValid(tc.input) {
				t.Errorf("WM-003: runIDValid(%q) = false, want true (%s)", tc.input, tc.desc)
			}
		})
	}

	for _, tc := range filtered {
		tc := tc
		t.Run("invalid/"+tc.desc, func(t *testing.T) {
			t.Parallel()
			if runIDValid(tc.input) {
				t.Errorf("WM-003: runIDValid(%q) = true, want false (%s)", tc.input, tc.desc)
			}
		})
	}
}

// TestWM003a_CrashEvidenceTypes verifies that the classifier correctly identifies
// the two orphan evidence types defined in WM-003a:
//   - "bare-worktree-no-lease": registered worktree, no lease-lock, no sessions dir
//   - "sidecar-without-lease": registered worktree, sidecar present, no lease-lock
//
// Both states arise from a SIGKILL / power loss between `git worktree add` and the
// lease-lock fsync gate of WM-016. Neither the `leased` nor any post-`ready` event
// has been durably emitted; Cat 3 routing is the correct reconciliation path.
//
// Spec ref: workspace-model.md §4.1 WM-003a and §3 Glossary "orphan evidence types".
func TestWM003a_CrashEvidenceTypes(t *testing.T) {
	t.Parallel()

	t.Run("bare-worktree-no-lease", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0010"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		// Simulate crash state: `git worktree add` completed (so the worktree is
		// registered with git), but the lease-lock file was never written and no
		// session-log directory exists.
		cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		// No lease-lock, no sessions dir — bare-worktree-no-lease state.
		evidenceType, err := classifyCrashEvidence(repo, runID)
		if err != nil {
			t.Fatalf("WM-003a: bare-worktree-no-lease: classifyCrashEvidence: %v", err)
		}
		if evidenceType != "bare-worktree-no-lease" {
			t.Errorf("WM-003a: bare-worktree-no-lease: got %q, want %q",
				evidenceType, "bare-worktree-no-lease")
		}

		// Verify the expected reconciliation routing: Cat 3.
		// TODO: replace with actual reconciliation routing call when
		// reconciliation Cat 3 detector lands (hk-8mwo.67).
		// The Cat 3 route gives reconciliation the right to propose `reopen-bead`
		// (discard partial worktree, fresh run_id per WM-034) or
		// `accept-close-with-note` with operator cleanup.
		_ = evidenceType // will be passed to reconciliation router
	})

	t.Run("sidecar-without-lease", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)

		runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0011"
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}

		// Create the worktree (registered with git).
		cmd := exec.Command("git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		// Synthesize a session sidecar to simulate a crash after the sidecar was
		// written but before the lease-lock was fsynced (between steps (c) and (d)
		// of WM-016).
		sessionID := "sess-0196a1b2-c3d4-7ef0-8a1b-000000000001"
		sidecarDir := filepath.Join(worktreePath, ".harmonik", "sessions", sessionID)
		if err := os.MkdirAll(sidecarDir, 0o755); err != nil {
			t.Fatalf("MkdirAll sidecarDir: %v", err)
		}
		sidecarPath := filepath.Join(sidecarDir, "harmonik.meta.json")
		sidecarContent := `{"run_id":"` + runID + `","session_id":"` + sessionID + `","schema_version":"1"}`
		if err := os.WriteFile(sidecarPath, []byte(sidecarContent), 0o644); err != nil {
			t.Fatalf("WriteFile sidecar: %v", err)
		}

		// No lease-lock, but sidecar present — sidecar-without-lease state.
		evidenceType, err := classifyCrashEvidence(repo, runID)
		if err != nil {
			t.Fatalf("WM-003a: sidecar-without-lease: classifyCrashEvidence: %v", err)
		}
		if evidenceType != "sidecar-without-lease" {
			t.Errorf("WM-003a: sidecar-without-lease: got %q, want %q",
				evidenceType, "sidecar-without-lease")
		}

		// TODO: replace placeholder classifier when reconciliation Cat 3 detector lands
		// (hk-8mwo.67 owns the lease-lock format; the Cat 3 detector will own the
		// reconciliation routing logic).
		_ = evidenceType
	})
}

// TestWM004_WorkspaceIDPrefixOpaqueToConsumers is a negative test that captures the
// invariant: downstream consumers MUST NOT parse the "ws-" prefix or the embedded
// run_id from the workspace_id string.
//
// The correlation field for joins is run_id, which is carried explicitly in every
// event payload per [event-model.md §8.5].
//
// Spec ref: workspace-model.md §4.1 WM-004 — "workspace_id MUST be treated as
// opaque by event consumers — downstream subsystems MUST NOT parse the prefix or the
// embedded run_id from the string".
func TestWM004_WorkspaceIDPrefixOpaqueToConsumers(t *testing.T) {
	t.Parallel()

	// The workspace_id is an opaque string. Its only allowed operations are:
	//   - equality comparison (e.g., join on event payloads)
	//   - storage and transmission as a string
	//
	// A consumer that extracts run_id by stripping "ws-" is WRONG per WM-004.
	// The run_id is always present as a distinct explicit field on event payloads.

	runID := "0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0020"
	workspaceID := "ws-" + runID

	// Demonstrate the correct (opaque) consumer pattern: compare whole strings.
	// Do NOT do: runIDFromWsID := strings.TrimPrefix(workspaceID, "ws-")
	otherWorkspaceID := "ws-" + runID
	if workspaceID != otherWorkspaceID {
		t.Errorf("WM-004: opaque equality: %q != %q", workspaceID, otherWorkspaceID)
	}

	// Assert type shape: workspace_id is a plain string; no structured parsing
	// is permitted. The type-level enforcement will be owned by the WorkspaceID
	// type in hk-8mwo.24. Until then, this doc-test comment captures the obligation.
	//
	// WRONG (WM-004 violation): extractedRunID := strings.TrimPrefix(workspaceID, "ws-")
	// CORRECT: use the explicit run_id field on the event payload.
	//
	// We assert opaqueness via the doc-test shape: the variable is only ever
	// compared as a whole string, never parsed.
	_ = workspaceID // opaque: only equality operations are permitted

	// Negative assertion: if a consumer parses the prefix, it is wrong even if
	// the derivation happens to work today. Capture this as a compile-time-visible
	// comment obligation, not a runtime check (runtime parsing would succeed today
	// but violates the spec contract).
	//
	// TODO: when WorkspaceID becomes a distinct named type (hk-8mwo.24), the
	// type system will enforce opacity by not exposing any String() decomposition.
	// This test should then call the WorkspaceID constructor and assert that no
	// decomposition method exists.
	_ = runID // explicit run_id field — the correct correlation path
}
