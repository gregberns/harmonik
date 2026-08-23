package workspace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWM013b_LeaseReleaseOnTerminalTransitions verifies that the lease-lock is
// removed on every terminal workspace transition, gated on the per-terminal-path
// durability rules, and that the workspace-local lease_released JSONL marker is
// written and fsynced before the lock is removed.
//
// Spec ref: workspace-model.md §4.3 WM-013b — "The workspace manager MUST release
// the lease (remove the lease-lock file) on every terminal workspace transition:
// entering merged (§7.1) or discarded (§7.1). … Across all terminal paths, the
// workspace-local lease_released JSONL marker MUST be written before the
// lease-lock file is removed."
func TestWM013b_LeaseReleaseOnTerminalTransitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		reason      string
		description string
	}{
		{
			name:        "merged",
			reason:      "merged",
			description: "Release MUST occur AFTER workspace_merge_status with status=merged flushed to durable events journal (class F per EV-015).",
		},
		{
			name:        "run_failed",
			reason:      "run_failed",
			description: "Release MUST occur AFTER run_failed event (class F per EV) flushed. run_failed is the durable terminal marker for a failed run.",
		},
		{
			name:        "post_escalation",
			reason:      "post_escalation",
			description: "Release MUST occur AFTER workspace-local durability marker written and fsynced to .harmonik/events/workspace-<workspace_id>.jsonl.",
		},
		{
			name:        "verdict_driven",
			reason:      "verdict_driven",
			description: "Release MUST occur AFTER reconciliation_verdict_executed flushed per EV AND workspace-local lease_released marker written and fsynced.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo, sha := tempRepo(t)
			reasonPad := tc.reason
			for len(reasonPad) < 8 {
				reasonPad += "0"
			}
			runID := "0196a1b2-c3d4-713b-8a1b-" + leaseFixtureSanitizeRunID(reasonPad[:8]) + "0000"
			branch := "run/" + runID
			worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
			workspaceID := "ws-" + runID

			if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			//nolint:gosec // G204: git command and worktree paths are controlled by this test fixture.
			cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
			cmd.Dir = repo
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git worktree add: %v\n%s", err, out)
			}

			leaseLockPath := LeaseLockPath(worktreePath)
			leaseFixtureWriteLockAtomic(t, leaseLockPath, leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

			if _, err := os.Stat(leaseLockPath); err != nil {
				t.Fatalf("WM-013b[%s]: lease-lock absent before terminal transition: %v", tc.name, err)
			}

			if err := WriteLeaseReleasedMarker(worktreePath, runID, workspaceID, tc.reason); err != nil {
				t.Fatalf("WM-013b[%s]: WriteLeaseReleasedMarker: %v", tc.name, err)
			}

			eventsFile := WorkspaceLocalEventsPath(worktreePath, workspaceID)
			markerData := mustReadFile(t, eventsFile)

			lines := strings.Split(strings.TrimRight(string(markerData), "\n"), "\n")
			if len(lines) < 1 || lines[0] == "" {
				t.Fatalf("WM-013b[%s]: events JSONL is empty", tc.name)
			}
			var marker struct {
				Event       string `json:"event"`
				RunID       string `json:"run_id"`
				WorkspaceID string `json:"workspace_id"`
				Reason      string `json:"reason"`
				ReleasedAt  string `json:"released_at"`
			}
			if err := json.Unmarshal([]byte(lines[0]), &marker); err != nil {
				t.Fatalf("WM-013b[%s]: json.Unmarshal marker: %v\nline: %s", tc.name, err, lines[0])
			}
			if marker.Event != "lease_released" {
				t.Errorf("WM-013b[%s]: marker.event = %q, want %q", tc.name, marker.Event, "lease_released")
			}
			if marker.RunID != runID {
				t.Errorf("WM-013b[%s]: marker.run_id = %q, want %q", tc.name, marker.RunID, runID)
			}
			if marker.WorkspaceID != workspaceID {
				t.Errorf("WM-013b[%s]: marker.workspace_id = %q, want %q", tc.name, marker.WorkspaceID, workspaceID)
			}
			if marker.Reason != tc.reason {
				t.Errorf("WM-013b[%s]: marker.reason = %q, want %q", tc.name, marker.Reason, tc.reason)
			}
			if _, parseErr := time.Parse(time.RFC3339, marker.ReleasedAt); parseErr != nil {
				t.Errorf("WM-013b[%s]: marker.released_at %q not RFC 3339: %v", tc.name, marker.ReleasedAt, parseErr)
			}

			if err := ReleaseLeaseLock(leaseLockPath); err != nil {
				t.Fatalf("WM-013b[%s]: ReleaseLeaseLock: %v", tc.name, err)
			}

			if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
				t.Errorf("WM-013b[%s]: lease-lock still present after release; want absent", tc.name)
			}

			if _, err := os.Stat(eventsFile); err != nil {
				t.Errorf("WM-013b[%s]: events JSONL absent after lock release; want present: %v", tc.name, err)
			}

			if err := ReleaseLeaseLock(leaseLockPath); err != nil {
				t.Errorf("WM-013b[%s]: ReleaseLeaseLock idempotent second call: %v", tc.name, err)
			}
		})
	}
}

// TestWM013b_MarkerWrittenBeforeUnlink verifies the ordering invariant:
// workspace-local lease_released JSONL marker MUST be written before lease-lock
// is removed. Tests the crash-recovery scenario where marker is present + lock
// is present (daemon crashed after marker write, before unlink).
//
// Spec ref: workspace-model.md §4.3 WM-013b — "if the workspace manager crashes
// after writing the marker but before unlink, startup reconciliation observes a
// present lock + marker combination and completes the release by unlinking the
// lock (idempotent replay)."
func TestWM013b_MarkerWrittenBeforeUnlink(t *testing.T) {
	t.Parallel()

	t.Run("crash-after-marker-before-unlink-idempotent-replay", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713b-8a1b-crashrecover1"
		runID = leaseFixtureSanitizeRunID(runID)
		branch := "run/" + runID
		worktreePath := filepath.Join(repo, ".harmonik", "worktrees", runID)
		workspaceID := "ws-" + runID

		if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		//nolint:gosec // G204: git command and worktree paths are controlled by this test fixture.
		cmd := exec.CommandContext(t.Context(), "git", "worktree", "add", "-b", branch, worktreePath, sha)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v\n%s", err, out)
		}

		leaseLockPath := LeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath, leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

		if err := WriteLeaseReleasedMarker(worktreePath, runID, workspaceID, "post_escalation"); err != nil {
			t.Fatalf("WM-013b: WriteLeaseReleasedMarker: %v", err)
		}

		if _, err := os.Stat(leaseLockPath); err != nil {
			t.Fatalf("WM-013b: crash state: lease-lock absent; want present: %v", err)
		}
		eventsFile := WorkspaceLocalEventsPath(worktreePath, workspaceID)
		if _, err := os.Stat(eventsFile); err != nil {
			t.Fatalf("WM-013b: crash state: events JSONL absent; want present: %v", err)
		}

		if err := ReleaseLeaseLock(leaseLockPath); err != nil {
			t.Fatalf("WM-013b: idempotent replay ReleaseLeaseLock: %v", err)
		}

		if _, err := os.Stat(leaseLockPath); !os.IsNotExist(err) {
			t.Errorf("WM-013b: idempotent replay: lock still present; want absent")
		}
		if _, err := os.Stat(eventsFile); err != nil {
			t.Errorf("WM-013b: idempotent replay: events JSONL absent; want present: %v", err)
		}
	})
}

func leaseFixtureSanitizeRunID(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result[i] = c
		} else {
			result[i] = '-'
		}
	}
	return string(result)
}
