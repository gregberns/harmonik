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
//   - The directory containing the run branch's ref (<gitDir>/refs/heads/<dir>/
//     for namespaced branches; <gitDir>/refs/heads/ as both the path and the
//     fallback for flat branch names).  The directory (not the ref file) is
//     required because git creates <ref>.lock as a sibling during commit.
//   - <gitDir>/packed-refs and <gitDir>/packed-refs.lock (git pack-refs atomic pair).
//   - OS temp directories (TmpDirs).
//   - srt's own hardcoded scratch TMPDIR, /tmp/claude (and /private/tmp/claude);
//     see hk-cdpxu below.
//   - Per-run private cache areas (PrivateWriteCacheDirs — never shared).
//
// Warm shared toolchain caches go in allowRead (read-only) to avoid the
// concurrent-writer TOCTOU class (see cache-reaper TOCTOU incident).
//
// enableWeakerNetworkIsolation defaults FALSE per the TLS decision in
// plans/2026-07-02-pi-sandbox/SPIKE-FINDINGS-hk-f39ny.md §TLS DECISION:
// Pi (node) honors the injected proxy CA; local Go CLIs reach the daemon over
// the unix socket; `gh` (Go, TLS-broken under srt) is not needed inside the
// sandbox in v1. It is now driven by SandboxProfileInput.WeakerNetworkIsolation
// (config: sandbox.network.weaker_network_isolation) rather than hardcoded, so
// the parsed field is honored instead of silently ignored.
//
// allowLocalBinding is driven by SandboxProfileInput.AllowLocalBinding
// (config: sandbox.network.allow_local_binding). It is REQUIRED to reach a
// locally-hosted model endpoint (a LAN vLLM / loopback stub): those addresses
// fall in srt's no_proxy set, so they are connected to directly and Seatbelt
// denies the socket ("Operation not permitted") unless local binding is
// permitted — the allowedDomains proxy path does not cover them. Bead hk-ybuts /
// hk-u69my (Pi srt egress: sandboxed Pi could not reach the DGX vLLM).
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
	//
	// NOTE: allowedDomains only covers PROXIED HTTPS to public domains (srt's
	// MITM proxy path — the openrouter.ai spike). It does NOT cover a direct
	// connection to a private-LAN / loopback address: srt's default no_proxy
	// (localhost, 127.0.0.1, 10/8, 172.16/12, 192.168/16, 169.254/16) makes
	// those bypass the proxy and connect directly, which macOS Seatbelt denies
	// unless AllowLocalBinding is set. A locally-hosted model server (vLLM at
	// e.g. http://192.168.1.86:8551) therefore needs AllowLocalBinding, NOT an
	// allowedDomains entry. See hk-ybuts / hk-u69my (Pi srt egress).
	AllowedDomains []string

	// AllowLocalBinding, when true, permits the sandboxed process to open direct
	// sockets to local / private-LAN / loopback network addresses
	// (network.allowLocalBinding). REQUIRED to reach a locally-hosted, OpenAI-
	// compatible model endpoint (e.g. a DGX vLLM on the LAN, or an httptest stub
	// on 127.0.0.1) because those addresses fall in srt's no_proxy set and are
	// connected to directly rather than through the MITM proxy. Default false
	// keeps the tightest posture; the operator opts in via
	// sandbox.network.allow_local_binding. Bead: hk-ybuts / hk-u69my.
	AllowLocalBinding bool

	// WeakerNetworkIsolation, when true, sets srt's enableWeakerNetworkIsolation
	// (network.weaker_network_isolation config field). v1 default false per the
	// TLS decision (SPIKE-FINDINGS-hk-f39ny §TLS DECISION); wired here so the
	// already-parsed config field is honored rather than silently ignored.
	WeakerNetworkIsolation bool

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
//   - directory containing the run branch ref: filepath.Dir(<GitDir>/refs/heads/<BranchName>)
//     when BranchName is set, or <GitDir>/refs/heads/ as fallback
//   - <GitDir>/packed-refs and <GitDir>/packed-refs.lock (atomic update pair)
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

	// 4. Branch ref directory — git creates <ref>.lock as a sibling of the ref
	//    file during commit (not inside it), so we need the DIRECTORY containing
	//    the ref, not the ref file itself.  For a branch like "run/abc" the
	//    directory is refs/heads/run/; for a flat branch like "main" it equals
	//    refs/heads/ (same as the no-name fallback).
	if in.BranchName != "" {
		allowWrite = append(allowWrite, filepath.Dir(filepath.Join(in.GitDir, "refs", "heads", in.BranchName)))
	} else {
		allowWrite = append(allowWrite, filepath.Join(in.GitDir, "refs", "heads"))
	}

	// 5. Packed-refs file and its lock sibling (created atomically by git pack-refs).
	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "packed-refs"))
	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "packed-refs.lock"))

	// 5a. Reflog directory for the branch.  Git appends a log entry to
	//     logs/refs/heads/<branch> on every commit; we need write access to
	//     the directory containing that file (not just the file itself, so new
	//     entries for sub-branches can be created).
	if in.BranchName != "" {
		allowWrite = append(allowWrite, filepath.Dir(filepath.Join(in.GitDir, "logs", "refs", "heads", in.BranchName)))
	} else {
		allowWrite = append(allowWrite, filepath.Join(in.GitDir, "logs", "refs", "heads"))
	}

	// 6. OS temp directories.
	allowWrite = append(allowWrite, in.TmpDirs...)

	// 6a. srt's own scratch TMPDIR (hk-cdpxu). Empirically (srt 1.0.0), the
	// sandboxed child ALWAYS gets TMPDIR=/tmp/claude injected by srt itself,
	// regardless of the parent process's TMPDIR and regardless of what this
	// profile's allowWrite otherwise contains — it is not one of in.TmpDirs.
	// Any tool that honors TMPDIR for scratch/work-dir creation (e.g. `go
	// build`'s "creating work dir" step) fails with ENOENT inside the sandbox
	// unless /tmp/claude is both present on disk AND in allowWrite. Both the
	// /tmp and /private/tmp forms are listed (macOS symlinks /tmp ->
	// /private/tmp; bwrap/Seatbelt need the literal path used at open time),
	// mirroring the existing TmpDirs /private/tmp fallback above. Directory
	// creation is the caller's responsibility (sandboxWrapExecArgv), since this
	// function is a pure profile generator.
	allowWrite = append(allowWrite, "/tmp/claude", "/private/tmp/claude")

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
			AllowLocalBinding: in.AllowLocalBinding,
		},
		Filesystem: srtFilesystemConfig{
			DenyRead:   []string{"~/.ssh"},
			AllowRead:  allowRead,
			AllowWrite: allowWrite,
			DenyWrite:  []string{},
		},
		EnableWeakerNestedSandbox:    false,
		EnableWeakerNetworkIsolation: in.WeakerNetworkIsolation,
		AllowAppleEvents:             false,
	}

	return json.MarshalIndent(settings, "", "  ")
}
