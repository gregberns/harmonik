package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

const pathKindScript = `
if [ -L "$1" ]; then printf 'symlink\n'
elif [ -d "$1" ]; then printf 'directory\n'
elif [ -f "$1" ]; then printf 'regular\n'
elif [ -e "$1" ]; then printf 'other\n'
else
  probe=${1%/*}
  while [ -n "$probe" ] && [ "$probe" != "/" ]; do
    if [ -L "$probe" ] || { [ -e "$probe" ] && [ ! -d "$probe" ]; }; then
      printf 'indeterminate\n'; exit 0
    fi
    if [ -d "$probe" ]; then
      if ls -A "$probe" >/dev/null 2>&1; then printf 'absent\n'; else printf 'indeterminate\n'; fi
      exit 0
    fi
    probe=${probe%/*}
  done
  if ls -A / >/dev/null 2>&1; then printf 'absent\n'; else printf 'indeterminate\n'; fi
fi
`

// ObserveDispatchWorktree reads the exact authority for one dispatch run.
// A runner-backed configuration performs every filesystem and Git read through
// that runner. It never inspects the same path on the coordinator.
func ObserveDispatchWorktree(
	ctx context.Context,
	repoRoot string,
	runID string,
	cfg WorktreeRootConfig,
) ([]DiscoveredWorktree, error) {
	if err := validateDispatchWorktreeCreate(repoRoot, runID, strings.Repeat("0", 40)); err != nil {
		return nil, fmt.Errorf("workspace: ObserveDispatchWorktree: %w", err)
	}
	if cfg.runner == nil {
		return observeLocalDispatchWorktree(ctx, repoRoot, runID, cfg)
	}
	return observeRemoteDispatchWorktree(ctx, cfg.commandRunner(), repoRoot, runID, cfg)
}

func observeLocalDispatchWorktree(
	ctx context.Context,
	repoRoot string,
	runID string,
	cfg WorktreeRootConfig,
) ([]DiscoveredWorktree, error) {
	all, err := DiscoverWorktrees(ctx, repoRoot, cfg)
	if err != nil {
		return nil, err
	}
	result := make([]DiscoveredWorktree, 0, 1)
	wantPath := WorktreePath(repoRoot, runID, cfg)
	wantBranch := TaskBranchName(runID)
	for _, value := range all {
		if value.RunID == runID || value.WorktreePath == wantPath || value.GitBranch == wantBranch {
			result = append(result, value)
		}
	}
	return result, nil
}

func observeRemoteDispatchWorktree(
	ctx context.Context,
	runner tmux.CommandRunner,
	repoRoot string,
	runID string,
	cfg WorktreeRootConfig,
) ([]DiscoveredWorktree, error) {
	registrations, err := porcelainWorktreeRegistrationsVia(ctx, runner, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("workspace: observe remote dispatch worktree registrations: %w", err)
	}
	worktreePath := WorktreePath(repoRoot, runID, cfg)
	kind, err := runnerPathKind(ctx, runner, worktreePath)
	if err != nil {
		return nil, err
	}
	registration, registered := registrations[worktreePath]
	conflict := conflictingWorktreeRegistration(registrations, worktreePath, worktreePath, runID)
	branchExists, err := runnerGitBranchExists(ctx, runner, repoRoot, TaskBranchName(runID))
	if err != nil {
		return nil, err
	}
	if kind == "absent" && !registered && !conflict && !branchExists {
		return nil, nil
	}
	observation := DiscoveredWorktree{
		RunID: runID, WorktreePath: worktreePath, RegisteredInGit: registered,
		GitBranch: registration.Branch, HeadCommit: registration.Head,
		GitRegistrationConflict: conflict || kind != "directory" || branchExists && !registered,
	}
	if kind != "directory" {
		return []DiscoveredWorktree{observation}, nil
	}
	if err := observeRemoteLease(ctx, runner, worktreePath, &observation); err != nil {
		return nil, err
	}
	if err := observeRemoteSessions(ctx, runner, worktreePath, runID, &observation); err != nil {
		return nil, err
	}
	return []DiscoveredWorktree{observation}, nil
}

func runnerPathKind(ctx context.Context, runner tmux.CommandRunner, path string) (string, error) {
	out, err := runner.Command(ctx, "sh", "-c", pathKindScript, "sh", path).Output()
	if err != nil {
		return "", fmt.Errorf("workspace: inspect remote path %q: %w", path, err)
	}
	kind := strings.TrimSpace(string(out))
	switch kind {
	case "absent", "directory", "regular", "symlink", "other":
		return kind, nil
	case "indeterminate":
		return "", fmt.Errorf("workspace: remote path %q is inaccessible or indeterminate", path)
	default:
		return "", fmt.Errorf("workspace: inspect remote path %q returned %q", path, kind)
	}
}

func runnerGitBranchExists(
	ctx context.Context,
	runner tmux.CommandRunner,
	repoRoot string,
	branch string,
) (bool, error) {
	const script = `
if git -C "$1" show-ref --verify --quiet "refs/heads/$2"; then printf 'present\n'
else code=$?; if [ "$code" -eq 1 ]; then printf 'absent\n'; else exit "$code"; fi
fi
`
	out, err := runner.Command(ctx, "sh", "-c", script, "sh", repoRoot, branch).Output()
	if err != nil {
		return false, fmt.Errorf("workspace: inspect remote branch %q: %w", branch, err)
	}
	switch strings.TrimSpace(string(out)) {
	case "present":
		return true, nil
	case "absent":
		return false, nil
	default:
		return false, fmt.Errorf("workspace: inspect remote branch %q returned %q", branch, out)
	}
}

func observeRemoteLease(
	ctx context.Context,
	runner tmux.CommandRunner,
	worktreePath string,
	observation *DiscoveredWorktree,
) error {
	path := LeaseLockPath(worktreePath)
	kind, err := runnerPathKind(ctx, runner, path)
	if err != nil {
		return err
	}
	if kind == "absent" {
		return nil
	}
	if kind != "regular" {
		observation.LeaseLockUnreadable = true
		return nil
	}
	data, err := runner.Command(ctx, "cat", path).Output()
	if err != nil {
		return markRemoteLeaseUnreadable(observation)
	}
	lock, err := decodeRemoteLease(data)
	if err != nil {
		return markRemoteLeaseUnreadable(observation)
	}
	observation.LeaseLock = lock
	return nil
}

func markRemoteLeaseUnreadable(observation *DiscoveredWorktree) error {
	observation.LeaseLockUnreadable = true
	return nil
}

func decodeRemoteLease(data []byte) (*discoveredLeaseLock, error) {
	var wire struct {
		RunID     string `json:"run_id"`
		PID       int    `json:"pid"`
		CreatedAt string `json:"created_at"`
		TTLSec    int    `json:"ttl_sec"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return nil, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("lease must contain one JSON value")
	}
	var runID core.RunID
	if err := runID.UnmarshalText([]byte(wire.RunID)); err != nil || !runID.IsUUIDv7() {
		return nil, errors.New("invalid lease run ID")
	}
	createdAt, err := time.Parse(time.RFC3339, wire.CreatedAt)
	if err != nil {
		return nil, errors.New("invalid lease creation time")
	}
	lock := core.LeaseLockFile{RunID: runID, PID: wire.PID, CreatedAt: createdAt, TTLSec: wire.TTLSec}
	if !lock.Valid() {
		return nil, errors.New("invalid lease")
	}
	return &discoveredLeaseLock{
		RunID: lock.RunID.String(), PID: lock.PID,
		CreatedAt: lock.CreatedAt.UTC().Format(time.RFC3339), TTLSec: lock.TTLSec,
	}, nil
}

func observeRemoteSessions(
	ctx context.Context,
	runner tmux.CommandRunner,
	worktreePath string,
	runID string,
	observation *DiscoveredWorktree,
) error {
	root := SessionLogRootPath(worktreePath)
	kind, err := runnerPathKind(ctx, runner, root)
	if err != nil {
		return err
	}
	if kind == "absent" {
		return nil
	}
	if kind != "directory" {
		observation.SessionsPathConflict = true
		return nil
	}
	observation.HasSessionsDir = true
	out, err := runner.Command(ctx, "find", root, "-mindepth", "1", "-maxdepth", "1", "-print").Output()
	if err != nil {
		return markRemoteSessionsConflict(observation)
	}
	for _, entry := range nonemptyLines(string(out)) {
		if filepath.Dir(entry) != root {
			observation.SessionsPathConflict = true
			return nil
		}
		entryKind, kindErr := runnerPathKind(ctx, runner, entry)
		if kindErr != nil || entryKind != "directory" {
			return markRemoteSessionsConflict(observation)
		}
		sidecarPath := filepath.Join(entry, "harmonik.meta.json")
		sidecarKind, sidecarKindErr := runnerPathKind(ctx, runner, sidecarPath)
		if sidecarKindErr != nil || sidecarKind != "regular" {
			return markRemoteSessionsConflict(observation)
		}
		data, readErr := runner.Command(ctx, "cat", sidecarPath).Output()
		if readErr != nil || !validRemoteSidecar(data, runID) {
			return markRemoteSessionsConflict(observation)
		}
		observation.HasExactSidecar = true
	}
	return nil
}

func markRemoteSessionsConflict(observation *DiscoveredWorktree) error {
	observation.SessionsPathConflict = true
	return nil
}

func validRemoteSidecar(data []byte, runID string) bool {
	var sidecar SessionMetadataSidecar
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&sidecar); err != nil {
		return false
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return false
	}
	return sidecar.Valid() == nil && validReplayAuthoritySidecar(sidecar, runID)
}

func nonemptyLines(value string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(value, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
