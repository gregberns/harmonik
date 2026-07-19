package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// runIDRegexProduction is the canonical filesystem-safety regex for run_id values
// per workspace-model.md §4.1 WM-002: "run_id MUST match the filesystem-safe
// regex [A-Za-z0-9-]+ (UUIDv7 satisfies this by construction)".
var runIDRegexProduction = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// RunIDValid reports whether s matches the canonical filesystem-safety regex for
// run_id values per workspace-model.md §4.1 WM-002.
//
// The regex [A-Za-z0-9-]+ is the normative constraint; UUIDv7 satisfies it by
// construction. Post-MVH ID-scheme extensions must preserve this invariant or
// declare an escape rule before adoption (WM-002).
func RunIDValid(s string) bool {
	if len(s) == 0 {
		return false
	}
	return runIDRegexProduction.MatchString(s)
}

// DiscoveredWorktree holds the result of the startup discovery pass for one
// worktree directory under `<repo>/.harmonik/worktrees/<run_id>/`, per
// workspace-model.md §4.3 WM-013c.
//
// Fields map directly to the four discovery steps:
//
//	(a) RunID — the subdirectory name, validated against the WM-002 regex.
//	(b) RegisteredInGit — confirmed via `git worktree list --porcelain`.
//	(c) LeaseLock — the parsed lease-lock (nil when the file is absent).
//	(d) HasSessionsDir — true iff ${path}/.harmonik/sessions/ exists on disk.
type DiscoveredWorktree struct {
	// RunID is the run_id derived from the directory name (step a).
	RunID string

	// WorktreePath is the absolute path to the worktree directory.
	WorktreePath string

	// RegisteredInGit reports whether the directory is a registered worktree
	// per `git worktree list --porcelain` (step b).
	// A directory with RegisteredInGit == false is an orphan worktree subject
	// to [process-lifecycle.md §4.2 PL-006].
	RegisteredInGit bool

	// LeaseLock is the parsed lease-lock file (step c). Nil when the
	// lease-lock file is absent — caller interprets absence as "not leased"
	// per WM-013a.
	LeaseLock *discoveredLeaseLock

	// LeaseLockUnreadable reports that a lease-lock file was PRESENT on disk but
	// could not be read or parsed (corrupt/truncated/permission). When true,
	// LeaseLock is nil, but the worktree MUST NOT be treated as unleased: its
	// lease state is UNKNOWN, so it is quarantined from both the stale-lock
	// removal and the age-based no-lock removal. This is the fail-safe distinction
	// between "lock absent" and "lock present, state unknown" — mistaking the
	// latter for the former would force-remove a possibly-live worktree.
	LeaseLockUnreadable bool

	// HasSessionsDir reports whether ${path}/.harmonik/sessions/ exists on disk
	// (step d). False means no session was ever started against this worktree,
	// which contributes to the "bare-worktree-no-lease" evidence type of WM-003a.
	HasSessionsDir bool
}

// discoveredLeaseLock is the subset of lease-lock fields recovered by startup
// discovery per WM-013c step (c): run_id, pid, and created_at.
//
// The full LeaseLockFile type (core.LeaseLockFile) is used by write/read paths.
// Discovery only needs the three cited fields for classification purposes.
type discoveredLeaseLock struct {
	RunID     string
	PID       int
	CreatedAt string // RFC 3339 string; not parsed to time.Time to keep discovery lean
	TTLSec    int
}

// DiscoverWorktrees performs the WM-013c startup discovery pass against
// repoRoot, returning one [DiscoveredWorktree] per candidate directory under
// `<repoRoot>/.harmonik/worktrees/` that matches the run_id regex.
//
// Discovery executes the four steps mandated by WM-013c:
//
//	(a) Enumerate subdirectories of <repo>/.harmonik/worktrees/ matching [A-Za-z0-9-]+.
//	(b) For each, call `git worktree list --porcelain` against <repo> and record
//	    whether the directory is a registered worktree.
//	(c) Read the lease-lock file per WM-013a (if present) and recover run_id, pid,
//	    created_at.
//	(d) Stat ${path}/.harmonik/sessions/ to detect whether any session was started.
//
// A directory failing (b) is flagged with RegisteredInGit == false (orphan,
// subject to PL-006). A directory passing (b) with a live lease-lock file whose
// recorded pid is NOT the current daemon is subject to WM-033 orphan-sweep; the
// caller performs that classification using [DiscoveredWorktree.LeaseLock.PID].
//
// cfg carries the operator-configurable worktree root per
// [control-points.md §4.7 CP-037]; use [NoWorktreeRootOverride] for the
// default `<repo>/.harmonik/worktrees/` per WM-002.
//
// DiscoverWorktrees returns a nil-error empty slice when the worktree root does
// not exist (no workspaces have ever been created). Returns an error only for
// unexpected I/O failures on the directory enumeration itself; per-entry errors
// from git or the lease-lock read are folded into the DiscoveredWorktree fields
// rather than terminating the walk.
//
// ctx is passed to exec.CommandContext for the git invocation.
//
// Spec refs:
//   - workspace-model.md §4.3 WM-013c — startup lease discovery mechanism.
//   - workspace-model.md §4.1 WM-002 — run_id regex + canonical path.
//   - workspace-model.md §4.3 WM-013a — lease-lock file location and format.
func DiscoverWorktrees(ctx context.Context, repoRoot string, cfg WorktreeRootConfig) ([]DiscoveredWorktree, error) {
	worktreeRoot := WorktreeRootPath(repoRoot, cfg)

	// Step (a): enumerate subdirectories of the worktree root.
	entries, err := os.ReadDir(worktreeRoot)
	if err != nil {
		if os.IsNotExist(err) {
			// No worktrees have ever been created; not an error.
			return nil, nil
		}
		return nil, fmt.Errorf("workspace: DiscoverWorktrees: ReadDir %q: %w", worktreeRoot, err)
	}

	// Step (b): call `git worktree list --porcelain` once for the whole repo.
	// Parse the output into a set of registered worktree paths.
	registeredPaths, err := porcelainWorktreePaths(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace: DiscoverWorktrees: git worktree list: %w", err)
	}

	var results []DiscoveredWorktree

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !RunIDValid(name) {
			// Does not match the WM-002 run_id regex; skip.
			continue
		}

		worktreePath := WorktreePath(repoRoot, name, cfg)

		// Resolve symlinks before checking the git-registered set: on macOS,
		// t.TempDir() paths under /var/folders are symlinks to /private/var/folders,
		// and git --version 2.34+ resolves to the realpath in its output.
		resolvedPath := worktreePath
		if rp, err := filepath.EvalSymlinks(worktreePath); err == nil {
			resolvedPath = rp
		}

		dw := DiscoveredWorktree{
			RunID:           name,
			WorktreePath:    worktreePath,
			RegisteredInGit: registeredPaths[worktreePath] || registeredPaths[resolvedPath],
		}

		// Step (c): read the lease-lock file if present.
		leaseLockPath := LeaseLockPath(worktreePath)
		ll, llErr := readDiscoveredLeaseLock(leaseLockPath)
		switch {
		case llErr != nil:
			// A read/parse error means the lock file EXISTS but its content
			// cannot be recovered (corrupt/truncated). Fail safe: mark the lock
			// present-but-unknown so downstream reapers treat the worktree as
			// possibly-live and NEVER as absent (which would route it to
			// force-removal). A cleanly-absent lock returns (nil, nil), NOT an
			// error, so this branch is reached only for genuine corruption.
			dw.LeaseLockUnreadable = true
		case ll != nil:
			dw.LeaseLock = ll
		}
		// A missing lease-lock leaves both LeaseLock nil and LeaseLockUnreadable
		// false — the caller classifies it as NoLock.

		// Step (d): stat the sessions root directory.
		sessionsRoot := SessionLogRootPath(worktreePath)
		if info, err := os.Stat(sessionsRoot); err == nil && info.IsDir() {
			dw.HasSessionsDir = true
		}

		results = append(results, dw)
	}

	return results, nil
}

// porcelainWorktreePaths returns a set of absolute worktree paths reported by
// `git worktree list --porcelain` as registered for the given repo.
//
// Each "worktree <path>" line in the porcelain output contributes one entry.
// The set is keyed by the cleaned absolute path string.
func porcelainWorktreePaths(ctx context.Context, repoRoot string) (map[string]bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("workspace: porcelainWorktreePaths: git worktree list: %w", err)
	}

	registered := make(map[string]bool)
	for _, line := range strings.Split(string(out), "\n") {
		const prefix = "worktree "
		if strings.HasPrefix(line, prefix) {
			registered[strings.TrimSpace(line[len(prefix):])] = true
		}
	}
	return registered, nil
}

// readDiscoveredLeaseLock reads the lease-lock fields required by WM-013c step
// (c) from the given path. Returns (nil, nil) when the file is absent.
func readDiscoveredLeaseLock(leaseLockPath string) (*discoveredLeaseLock, error) {
	lock, err := ReadLeaseLock(leaseLockPath)
	if err != nil {
		return nil, err
	}
	if lock == nil {
		// Absent — caller interprets as "not leased" per WM-013a.
		return nil, nil //nolint:nilnil // nil result with nil error is the documented "absent" signal
	}
	return &discoveredLeaseLock{
		RunID:     lock.RunID.String(),
		PID:       lock.PID,
		CreatedAt: lock.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		TTLSec:    lock.TTLSec,
	}, nil
}
