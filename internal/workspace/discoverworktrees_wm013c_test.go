package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// TestWM013c_DiscoverWorktrees verifies that DiscoverWorktrees performs the
// four startup discovery steps mandated by workspace-model.md §4.3 WM-013c:
//
//	(a) enumerate subdirectories matching the run_id regex,
//	(b) confirm registration in git via `git worktree list --porcelain`,
//	(c) read the lease-lock file to recover run_id/pid/created_at,
//	(d) stat ${path}/.harmonik/sessions/ to detect any started session.
func TestWM013c_DiscoverWorktrees(t *testing.T) {
	t.Parallel()

	t.Run("discovers-registered-worktrees-with-lease-lock", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0001"
		branch := TaskBranchName(runID)
		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}
		leaseLockPath := LeaseLockPath(worktreePath)
		leaseFixtureWriteLockAtomic(t, leaseLockPath,
			leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}

		dw := discovered[0]

		if dw.RunID != runID {
			t.Errorf("WM-013c: RunID = %q, want %q", dw.RunID, runID)
		}

		if !dw.RegisteredInGit {
			t.Errorf("WM-013c: RegisteredInGit = false, want true")
		}
		if dw.GitBranch != branch || dw.HeadCommit != sha {
			t.Errorf("WM-013c: registration = (%q, %q), want (%q, %q)", dw.GitBranch, dw.HeadCommit, branch, sha)
		}

		if dw.LeaseLock == nil {
			t.Fatalf("WM-013c: LeaseLock = nil, want non-nil")
		}
		if dw.LeaseLock.RunID != runID {
			t.Errorf("WM-013c: LeaseLock.RunID = %q, want %q", dw.LeaseLock.RunID, runID)
		}
		if dw.LeaseLock.PID != os.Getpid() {
			t.Errorf("WM-013c: LeaseLock.PID = %d, want %d", dw.LeaseLock.PID, os.Getpid())
		}

		if dw.HasSessionsDir {
			t.Errorf("WM-013c: HasSessionsDir = true, want false (no sessions created)")
		}
	})

	t.Run("step-d-true-when-sessions-dir-exists", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0002"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}

		worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())

		sessionsRoot := SessionLogRootPath(worktreePath)
		if err := os.MkdirAll(sessionsRoot, 0o700); err != nil {
			t.Fatalf("MkdirAll sessions root: %v", err)
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}
		if !discovered[0].HasSessionsDir {
			t.Errorf("WM-013c: HasSessionsDir = false, want true after sessions dir creation")
		}
		if discovered[0].HasExactSidecar {
			t.Error("WM-013c: empty sessions directory reported an exact sidecar")
		}
	})

	t.Run("orphan-directory-flagged-registered-false", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)
		orphanRunID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0003"

		orphanPath := filepath.Join(repo, ".harmonik", "worktrees", orphanRunID)
		if err := os.MkdirAll(orphanPath, 0o700); err != nil {
			t.Fatalf("MkdirAll orphan: %v", err)
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}
		if discovered[0].RegisteredInGit {
			t.Errorf("WM-013c: orphan directory marked RegisteredInGit = true, want false")
		}
	})

	t.Run("empty-root-returns-nil-slice", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Errorf("WM-013c: DiscoverWorktrees on absent root: want nil error, got %v", err)
		}
		if len(discovered) != 0 {
			t.Errorf("WM-013c: DiscoverWorktrees on absent root: want 0 results, got %d", len(discovered))
		}
	})

	t.Run("non-matching-directories-skipped", func(t *testing.T) {
		t.Parallel()

		repo, _ := tempRepo(t)

		worktreeRoot := filepath.Join(repo, ".harmonik", "worktrees")
		for _, name := range []string{".gitkeep", "not_valid", "also.invalid"} {
			if err := os.MkdirAll(filepath.Join(worktreeRoot, name), 0o700); err != nil {
				t.Fatalf("MkdirAll %q: %v", name, err)
			}
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 0 {
			t.Errorf("WM-013c: expected 0 discovered worktrees (all invalid names), got %d: %+v",
				len(discovered), discovered)
		}
	})

	t.Run("multiple-worktrees-discovered", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runIDs := []string{
			"0196a1b2-c3d4-713c-8a1b-2c3d4e5f0004",
			"0196a1b2-c3d4-713c-8a1b-2c3d4e5f0005",
		}

		for _, runID := range runIDs {
			if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
				t.Fatalf("CreateWorktree %q: %v", runID, err)
			}
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}

		if len(discovered) != 2 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 2", len(discovered))
		}

		seenIDs := map[string]bool{}
		for _, dw := range discovered {
			seenIDs[dw.RunID] = true
			if !dw.RegisteredInGit {
				t.Errorf("WM-013c: worktree %q: RegisteredInGit = false, want true", dw.RunID)
			}
		}
		for _, runID := range runIDs {
			if !seenIDs[runID] {
				t.Errorf("WM-013c: run_id %q not found in discovered worktrees", runID)
			}
		}
	})

	t.Run("run-id-valid-filters-regex", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			s    string
			want bool
		}{
			{"0196a1b2-c3d4-7ef0-8a1b-2c3d4e5f0001", true},
			{"abcdef", true},
			{"ABC-123", true},
			{"", false},
			{"has_underscore", false},
			{"has.dot", false},
			{"has space", false},
			{".hidden", false},
		}
		for _, tc := range cases {
			got := RunIDValid(tc.s)
			if got != tc.want {
				t.Errorf("RunIDValid(%q) = %v, want %v", tc.s, got, tc.want)
			}
		}
	})

	t.Run("worktree-path-matches-canonical-construction", func(t *testing.T) {
		t.Parallel()

		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0006"

		if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
			t.Fatalf("CreateWorktree: %v", err)
		}

		discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil {
			t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
		}
		if len(discovered) != 1 {
			t.Fatalf("WM-013c: discovered %d worktrees, want 1", len(discovered))
		}

		wantPath := WorktreePath(repo, runID, NoWorktreeRootOverride())
		if discovered[0].WorktreePath != wantPath {
			t.Errorf("WM-013c: WorktreePath = %q, want %q", discovered[0].WorktreePath, wantPath)
		}
	})
}

// TestWM013c_DiscoverWorktreesBranchConvention verifies that DiscoverWorktrees
// and CreateWorktree together satisfy the WM-013c step (b) + step (c) integration:
// a worktree created via CreateWorktree is immediately discoverable with its
// lease-lock, branch, and path all consistent.
func TestWM013c_DiscoverWorktreesBranchConvention(t *testing.T) {
	t.Parallel()

	repo, sha := tempRepo(t)
	runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0007"

	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
	leaseLockPath := LeaseLockPath(worktreePath)
	leaseFixtureWriteLockAtomic(t, leaseLockPath,
		leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))

	// Confirm the branch exists at the expected task-branch name.
	// #nosec G204 -- git inspection arguments are constructed by this test fixture.
	out, err := exec.CommandContext(t.Context(), "git", "-C", repo,
		"rev-parse", "--verify", TaskBranchName(runID)).Output()
	if err != nil || len(out) == 0 {
		t.Fatalf("WM-013c: task branch %q not found: %v", TaskBranchName(runID), err)
	}

	discovered, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
	if err != nil {
		t.Fatalf("WM-013c: DiscoverWorktrees: %v", err)
	}
	if len(discovered) != 1 || discovered[0].RunID != runID {
		t.Errorf("WM-013c: integration: discovered %+v, want run_id %q", discovered, runID)
	}
}

func TestConflictingWorktreeRegistration(t *testing.T) {
	runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0099"
	registrations := map[string]porcelainWorktreeRegistration{
		"/canonical/" + runID: {Branch: "run/" + runID},
		"/foreign/path":       {Branch: "run/" + runID},
	}
	if !conflictingWorktreeRegistration(registrations, "/canonical/"+runID, "/canonical/"+runID, runID) {
		t.Fatal("duplicate task branch was not a conflict")
	}
	delete(registrations, "/foreign/path")
	if conflictingWorktreeRegistration(registrations, "/canonical/"+runID, "/canonical/"+runID, runID) {
		t.Fatal("exact registration was a conflict")
	}
}

func TestDiscoverWorktreesQuarantinesUnsupportedAuthorityPaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(*testing.T, string)
		want func(DiscoveredWorktree) bool
	}{
		{
			name: "sessions regular file",
			make: func(t *testing.T, worktreePath string) {
				sessionsPath := SessionLogRootPath(worktreePath)
				if err := os.MkdirAll(filepath.Dir(sessionsPath), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(sessionsPath, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: func(got DiscoveredWorktree) bool { return got.SessionsPathConflict },
		},
		{
			name: "dangling lease symlink",
			make: func(t *testing.T, worktreePath string) {
				leasePath := LeaseLockPath(worktreePath)
				if err := os.MkdirAll(filepath.Dir(leasePath), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), leasePath); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
			want: func(got DiscoveredWorktree) bool { return got.LeaseLockUnreadable },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, sha := tempRepo(t)
			runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0098"
			if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
				t.Fatal(err)
			}
			tc.make(t, WorktreePath(repo, runID, NoWorktreeRootOverride()))
			got, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
			if err != nil || len(got) != 1 || !tc.want(got[0]) {
				t.Fatalf("DiscoverWorktrees() = (%+v, %v)", got, err)
			}
		})
	}
}

func TestDiscoverWorktreesReportsCanonicalAndForeignOnlyConflicts(t *testing.T) {
	t.Run("canonical path is not a directory", func(t *testing.T) {
		repo, _ := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0097"
		path := WorktreePath(repo, runID, NoWorktreeRootOverride())
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("not a worktree"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil || len(got) != 1 || !got[0].GitRegistrationConflict {
			t.Fatalf("DiscoverWorktrees() = (%+v, %v)", got, err)
		}
	})

	t.Run("task branch is registered only at foreign path", func(t *testing.T) {
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0096"
		foreign := filepath.Join(t.TempDir(), "foreign")
		// #nosec G204 -- all command arguments come from this test fixture.
		if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "worktree", "add", "-b", "run/"+runID, foreign, sha).CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v: %s", err, out)
		}
		got, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil || len(got) != 1 || got[0].RunID != runID || !got[0].GitRegistrationConflict {
			t.Fatalf("DiscoverWorktrees() = (%+v, %v)", got, err)
		}
	})

	t.Run("run basename is registered on another branch", func(t *testing.T) {
		repo, sha := tempRepo(t)
		runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0095"
		if err := os.MkdirAll(WorktreeRootPath(repo, NoWorktreeRootOverride()), 0o750); err != nil {
			t.Fatal(err)
		}
		foreign := filepath.Join(t.TempDir(), runID)
		// #nosec G204 -- all command arguments come from this test fixture.
		if out, err := exec.CommandContext(t.Context(), "git", "-C", repo, "worktree", "add", "-b", "other-branch", foreign, sha).CombinedOutput(); err != nil {
			t.Fatalf("git worktree add: %v: %s", err, out)
		}
		got, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
		if err != nil || len(got) != 1 || got[0].RunID != runID || !got[0].GitRegistrationConflict {
			t.Fatalf("DiscoverWorktrees() = (%+v, %v)", got, err)
		}
	})
}

func TestDiscoverWorktreesFindsExactRunSidecar(t *testing.T) {
	repo, sha := tempRepo(t)
	runID := "0196a1b2-c3d4-713c-8a1b-2c3d4e5f0094"
	if err := CreateWorktree(t.Context(), repo, runID, sha, NoWorktreeRootOverride()); err != nil {
		t.Fatal(err)
	}
	worktreePath := WorktreePath(repo, runID, NoWorktreeRootOverride())
	sessionID := "session-a"
	if err := CreateSessionLogDir(worktreePath, sessionID); err != nil {
		t.Fatal(err)
	}
	record := sidecarRecordFixtureValid(t)
	record.RunID = core.RunID(uuid.MustParse(runID))
	if err := WriteSessionMetadataSidecarAtomic(SessionMetadataSidecarPath(worktreePath, sessionID), &record); err != nil {
		t.Fatal(err)
	}
	leaseFixtureWriteLockAtomic(t, LeaseLockPath(worktreePath), leaseFixtureMakeLockJSON(runID, os.Getpid(), time.Now()))
	got, err := DiscoverWorktrees(t.Context(), repo, NoWorktreeRootOverride())
	if err != nil || len(got) != 1 || !got[0].HasExactSidecar || got[0].SessionsPathConflict {
		t.Fatalf("DiscoverWorktrees() = (%+v, %v)", got, err)
	}
}

func TestDiscoverExactRunSidecarRejectsInvalidAuthority(t *testing.T) {
	wantRunID := "0196e300-0000-7000-8000-000000000001"
	for _, tc := range []struct {
		name   string
		create func(*testing.T, string)
	}{
		{
			name: "wrong run",
			create: func(t *testing.T, path string) {
				record := sidecarRecordFixtureValid(t)
				record.RunID = core.RunID(uuid.MustParse("0196e300-0000-7000-8000-000000000009"))
				if err := WriteSessionMetadataSidecarAtomic(path, &record); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "unsupported schema",
			create: func(t *testing.T, path string) {
				record := sidecarRecordFixtureValid(t)
				record.SchemaVersion = SessionMetadataSidecarSchemaVersion + 1
				if err := WriteSessionMetadataSidecarAtomic(path, &record); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "invalid launch time",
			create: func(t *testing.T, path string) {
				record := sidecarRecordFixtureValid(t)
				record.LaunchedAt = "yesterday"
				if err := WriteSessionMetadataSidecarAtomic(path, &record); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "corrupt",
			create: func(t *testing.T, path string) {
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "nonregular",
			create: func(t *testing.T, path string) {
				if err := os.MkdirAll(path, 0o750); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worktreePath := t.TempDir()
			sessionsRoot := SessionLogRootPath(worktreePath)
			path := SessionMetadataSidecarPath(worktreePath, "session-a")
			tc.create(t, path)
			if found, err := discoverExactRunSidecar(sessionsRoot, wantRunID); err == nil || found {
				t.Fatalf("discoverExactRunSidecar() = (%v, %v)", found, err)
			}
		})
	}
}
