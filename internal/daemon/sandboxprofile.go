package daemon

// sandboxprofile.go — per-run srt sandbox profile generator (codename:pi-sandbox, hk-p7smp).
//
// GenerateSandboxProfile converts per-run filesystem coordinates (worktree path,
// git dirs, cache config) into a @anthropic-ai/sandbox-runtime (srt) settings JSON
// blob. It produces LITERAL paths only — no globs — so the output is safe for
// Linux bwrap as well as macOS Seatbelt (bwrap requires literal bind-mount paths).
//
// The allowWrite set is EXACTLY:
//   - The run worktree checkout directory.
//   - The git worktree metadata entry (<gitDir>/worktrees/<runID>/).
//   - The shared git object store (<gitDir>/objects/).
//   - The run branch's ref path (<gitDir>/refs/heads/<branchName> when known;
//     <gitDir>/refs/heads/ subtree as fallback when branch name is unknown).
//   - <gitDir>/packed-refs (single file; git uses this when refs are packed).
//   - OS temp directories (TmpDirs).
//   - Per-run private cache areas (PrivateWriteCacheDirs — never shared).
//
// Warm shared toolchain caches go in allowRead (read-only) to avoid the
// concurrent-writer TOCTOU class (see cache-reaper TOCTOU incident).
//
// enableWeakerNetworkIsolation stays FALSE per the TLS decision in
// plans/2026-07-02-pi-sandbox/SPIKE-FINDINGS-hk-f39ny.md §TLS DECISION:
// Pi (node) honors the injected proxy CA; local Go CLIs reach the daemon over
// the unix socket; `gh` (Go, TLS-broken under srt) is not needed inside the
// sandbox in v1.
//
// Spec: plans/2026-07-02-pi-sandbox/HANDOFF.md §4 (git writable-set),
// §6 (cache read-only base + private write area), §8.2 (profile shape).
// Base recipe: plans/2026-07-02-pi-sandbox/srt-spike-settings.json.
// Bead: hk-p7smp.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
)

// SandboxProfileInput carries the per-run coordinates for GenerateSandboxProfile.
type SandboxProfileInput struct {
	// WorktreePath is the absolute path to the run's worktree checkout.
	// e.g. /repo/.harmonik/worktrees/<run-id>
	// REQUIRED.
	WorktreePath string

	// GitDir is the absolute path to the main repo's .git directory.
	// e.g. /repo/.git
	// REQUIRED.
	GitDir string

	// RunID is the run's string identifier (UUID form). Used to build the per-run
	// git worktree metadata path (<gitDir>/worktrees/<RunID>/).
	// REQUIRED.
	RunID string

	// BranchName is the run's git branch name. When non-empty the ref path is
	// scoped to <gitDir>/refs/heads/<BranchName> (single file — tightest scope).
	// When empty the fallback is the broader <gitDir>/refs/heads/ subtree.
	// OPTIONAL.
	BranchName string

	// DaemonSockPath is the absolute path to the project daemon's unix socket.
	// e.g. /repo/.harmonik/daemon.sock
	// REQUIRED — placed in network.allowUnixSockets so `br` and `harmonik comms`
	// can reach the daemon from inside the sandbox.
	DaemonSockPath string

	// AllowedDomains is the list of HTTPS domains the sandbox permits outbound
	// connections to (network.allowedDomains). Nil/empty → no outbound HTTPS.
	AllowedDomains []string

	// TmpDirs are OS temp directories included in allowWrite.
	// On macOS, both /tmp and /private/tmp are typically needed.
	// Nil/empty → no temp dir entries.
	TmpDirs []string

	// SharedReadCacheDirs are warm toolchain cache directories included in
	// allowRead (read-only). Shared across concurrent runs; the sandbox never
	// permits writes here to avoid the concurrent-writer TOCTOU class.
	SharedReadCacheDirs []string

	// PrivateWriteCacheDirs are per-run private cache directories included in
	// allowWrite. Never shared with concurrent runs.
	PrivateWriteCacheDirs []string
}

// srtNetworkConfig is the network section of the srt settings JSON.
// Schema: srt v1.0.0 (plans/2026-07-02-pi-sandbox/srt-spike-settings.json).
type srtNetworkConfig struct {
	AllowedDomains    []string `json:"allowedDomains"`
	DeniedDomains     []string `json:"deniedDomains"`
	AllowUnixSockets  []string `json:"allowUnixSockets"`
	AllowLocalBinding bool     `json:"allowLocalBinding"`
}

// srtFilesystemConfig is the filesystem section of the srt settings JSON.
type srtFilesystemConfig struct {
	DenyRead   []string `json:"denyRead"`
	AllowRead  []string `json:"allowRead"`
	AllowWrite []string `json:"allowWrite"`
	DenyWrite  []string `json:"denyWrite"`
}

// srtSettings is the top-level srt settings JSON object.
// Field names and shape proven by the working recipe in
// plans/2026-07-02-pi-sandbox/srt-spike-settings.json.
type srtSettings struct {
	Network                      srtNetworkConfig    `json:"network"`
	Filesystem                   srtFilesystemConfig `json:"filesystem"`
	EnableWeakerNestedSandbox    bool                `json:"enableWeakerNestedSandbox"`
	EnableWeakerNetworkIsolation bool                `json:"enableWeakerNetworkIsolation"`
	AllowAppleEvents             bool                `json:"allowAppleEvents"`
}

// GenerateSandboxProfile produces the srt settings JSON for a sandboxed Pi run.
//
// All paths in the output are LITERAL — no globs or shell patterns. This is
// required for Linux bwrap compatibility: bwrap accepts only literal bind-mount
// paths.
//
// The allowWrite set is exactly the set mandated by hk-p7smp:
//   - WorktreePath (run checkout)
//   - <GitDir>/worktrees/<RunID>/ (git worktree metadata)
//   - <GitDir>/objects/ (shared git object store)
//   - <GitDir>/refs/heads/<BranchName> when BranchName is set (tightest scope),
//     or <GitDir>/refs/heads/ subtree as fallback
//   - <GitDir>/packed-refs (packed refs support)
//   - TmpDirs (OS temp directories)
//   - PrivateWriteCacheDirs (per-run private cache areas)
//
// Shared toolchain caches go in allowRead only. enableWeakerNetworkIsolation is
// always false. Returns an error when any required field is absent or not absolute.
func GenerateSandboxProfile(in SandboxProfileInput) ([]byte, error) {
	if in.WorktreePath == "" {
		return nil, fmt.Errorf("sandboxprofile: WorktreePath must be non-empty")
	}
	if !filepath.IsAbs(in.WorktreePath) {
		return nil, fmt.Errorf("sandboxprofile: WorktreePath must be an absolute path, got %q", in.WorktreePath)
	}
	if in.GitDir == "" {
		return nil, fmt.Errorf("sandboxprofile: GitDir must be non-empty")
	}
	if !filepath.IsAbs(in.GitDir) {
		return nil, fmt.Errorf("sandboxprofile: GitDir must be an absolute path, got %q", in.GitDir)
	}
	if in.RunID == "" {
		return nil, fmt.Errorf("sandboxprofile: RunID must be non-empty")
	}
	if in.DaemonSockPath == "" {
		return nil, fmt.Errorf("sandboxprofile: DaemonSockPath must be non-empty")
	}
	if !filepath.IsAbs(in.DaemonSockPath) {
		return nil, fmt.Errorf("sandboxprofile: DaemonSockPath must be an absolute path, got %q", in.DaemonSockPath)
	}

	// Build allowWrite: exact per-spec set, all literal paths, no globs.
	allowWrite := make([]string, 0, 5+len(in.TmpDirs)+len(in.PrivateWriteCacheDirs))

	// 1. Run worktree checkout.
	allowWrite = append(allowWrite, in.WorktreePath)

	// 2. Git worktree metadata for this run (HEAD, gitdir pointer, etc.).
	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "worktrees", in.RunID))

	// 3. Shared git object store (blobs, trees, commits — content-addressed).
	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "objects"))

	// 4. Branch ref path — scoped as tightly as possible.
	//    With a known branch name: single literal file path.
	//    Without: the heads/ subtree (coarser but still excludes tags/, remotes/).
	if in.BranchName != "" {
		allowWrite = append(allowWrite, filepath.Join(in.GitDir, "refs", "heads", in.BranchName))
	} else {
		allowWrite = append(allowWrite, filepath.Join(in.GitDir, "refs", "heads"))
	}

	// 5. Packed-refs file — a single file git updates when refs are packed.
	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "packed-refs"))

	// 6. OS temp directories.
	allowWrite = append(allowWrite, in.TmpDirs...)

	// 7. Per-run private cache areas (never shared with concurrent runs).
	allowWrite = append(allowWrite, in.PrivateWriteCacheDirs...)

	// Build allowRead: warm shared caches (read-only base, never writable).
	allowRead := make([]string, len(in.SharedReadCacheDirs))
	copy(allowRead, in.SharedReadCacheDirs)

	// Normalise nil AllowedDomains to an empty slice for clean JSON output.
	allowedDomains := in.AllowedDomains
	if allowedDomains == nil {
		allowedDomains = []string{}
	}

	settings := srtSettings{
		Network: srtNetworkConfig{
			AllowedDomains:    allowedDomains,
			DeniedDomains:     []string{},
			AllowUnixSockets:  []string{in.DaemonSockPath},
			AllowLocalBinding: false,
		},
		Filesystem: srtFilesystemConfig{
			DenyRead:   []string{"~/.ssh"},
			AllowRead:  allowRead,
			AllowWrite: allowWrite,
			DenyWrite:  []string{},
		},
		EnableWeakerNestedSandbox:    false,
		EnableWeakerNetworkIsolation: false,
		AllowAppleEvents:             false,
	}

	return json.MarshalIndent(settings, "", "  ")
}
