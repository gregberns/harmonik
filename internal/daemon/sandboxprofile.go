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
//   - Per-run temp directories (TmpDirs) — never a world-shared root (hk-guapd).
//   - The run's own scratch directory, SandboxScratchDir(WorktreePath), which is
//     what the sandboxed child gets as TMPDIR.
//   - srt's own default scratch TMPDIR, /tmp/claude (and /private/tmp/claude);
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
// (config: sandbox.network.allow_local_binding). It is REQUIRED to reach an
// endpoint on THIS host — loopback or one of this machine's own interfaces.
// Those addresses fall in srt's no_proxy set, so they are connected to
// directly and Seatbelt denies the socket ("Operation not permitted") unless
// local binding is permitted; the allowedDomains proxy path does not cover
// them. It does NOT open a remote host, so it does not on its own reach a
// model server on another machine — srt_pi_egress_e2e_test.go in this package
// measures the local half and explains why the remote half cannot be
// reproduced in-process. The discriminator is remote-host vs local, not
// loopback vs non-loopback. The model box is reached by tunnelling it to
// loopback instead. Bead hk-ybuts / hk-u69my (Pi srt egress: sandboxed Pi
// could not reach the DGX vLLM).
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
	// unless AllowLocalBinding is set. AllowLocalBinding is the answer for an
	// endpoint on THIS host; it does not open a remote one, so a model server
	// on another machine stays blocked either way and is reached through a
	// loopback tunnel (config base_url http://127.0.0.1:8551/v1). See
	// srt_pi_egress_e2e_test.go and hk-ybuts / hk-u69my (Pi srt egress).
	AllowedDomains []string

	// AllowLocalBinding, when true, permits the sandboxed process to open direct
	// sockets to addresses on THIS host — loopback and this machine's own
	// interfaces (network.allowLocalBinding). REQUIRED to reach an OpenAI-
	// compatible model endpoint there (the loopback tunnel entrance for the DGX
	// vLLM, or an httptest stub on 127.0.0.1) because those addresses fall in
	// srt's no_proxy set and are connected to directly rather than through the
	// MITM proxy. A host on the LAN is a different case: the socket stays denied
	// whatever this is set to. Default false
	// keeps the tightest posture; the operator opts in via
	// sandbox.network.allow_local_binding. Bead: hk-ybuts / hk-u69my.
	AllowLocalBinding bool

	// WeakerNetworkIsolation, when true, sets srt's enableWeakerNetworkIsolation
	// (network.weaker_network_isolation config field). v1 default false per the
	// TLS decision (SPIKE-FINDINGS-hk-f39ny §TLS DECISION); wired here so the
	// already-parsed config field is honored rather than silently ignored.
	WeakerNetworkIsolation bool

	// TmpDirs are PER-RUN temp directories included in allowWrite.
	// Nil/empty → no temp dir entries, which is what both production call
	// sites supply (hk-guapd). A world-shared root — "/tmp", "/private/tmp",
	// "/var/tmp", "/" — is rejected by GenerateSandboxProfile; srt expands
	// each entry recursively, so a shared root grants the run write access to
	// every other process's scratch state. Pass /tmp/harmonik-run-<id> or
	// similar if a run genuinely needs one.
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
// Schema: the srt settings file (plans/2026-07-02-pi-sandbox/srt-spike-settings.json),
// pinned against srt 0.0.63. Read the version from package.json or `npm ls -g`, never
// from `srt --version`: dist/cli.js falls back to a hardcoded "1.0.0" whenever
// npm_package_version is unset, which is every invocation outside an npm script. The
// "srt v1.0.0" this line used to claim came from that fallback.
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
//   - TmpDirs (per-run temp directories; a world-shared root is rejected)
//   - SandboxScratchDir(WorktreePath) — the child's TMPDIR
//   - PrivateWriteCacheDirs (per-run private cache areas)
//
// Shared toolchain caches go in allowRead only. enableWeakerNetworkIsolation is
// always false. Returns an error when any required field is absent or not
// absolute, or when a TmpDirs entry is a world-shared temp root (hk-guapd).

// SandboxScratchDir returns the per-run scratch directory for a run whose
// worktree is at worktreePath. It is the directory the sandboxed child gets as
// TMPDIR, and it is the answer to "where may a normal POSIX process write a
// scratch file" — a commit message, a compiler intermediate, anything a tool
// spools.
//
// It is DERIVED from the worktree rather than carried as a field so the profile
// generator and the argv wrapper cannot name two different directories: one
// function computes it and both call it.
//
// The worktree is the location for three reasons that no path under /tmp has:
//
//   - It is already inside the profile's write grant (srt expands allowWrite
//     entries recursively), so granting it widens the sandbox by nothing.
//
//   - It needs no second spelling. On macOS /tmp is a symlink to /private/tmp,
//     so a grant under /tmp has to be written both ways (see the /tmp/claude
//     pair below); a path under the repo resolves to itself.
//
//   - It lives and dies with the worktree, so it adds no new class of
//     accumulation. It does not follow that it is short-lived. A run that ends
//     any ordinary way gives the worktree back and the scratch directory goes
//     with it. A run kept FOR its evidence does not: RetainEvidence.Releases
//     gives back every resource EXCEPT the worktree.
//
//     Read runlease.Decide for what picks that. It reads ONE fact,
//     Exit.EvidenceWorthKeeping, and Survive pre-empts it when the session runs
//     independently and the daemon is stopping. The workloop is what fills the
//     fact in, and it wants three things at once: a run handle exists, the
//     handle reports captured agent output, and the bridge did not report
//     success. runAgentLaunch marks the capture on a pi run, but BELOW the
//     sandbox-engagement gate's early return, so a pi run that fails that gate
//     is never marked and is reclaimed like any other.
//
//     A retained worktree is not given back promptly, but it IS time-bounded.
//     Every daemon boot runs the orphan sweep over .harmonik/worktrees/, which
//     reaches a left-behind worktree by one of two routes:
//     workspace.RemoveStaleWorktrees force-removes one whose lease lock names a
//     dead PID, and workspace.RemoveAgedNoLockWorktrees prunes one that holds no
//     lock and is older than DefaultHarmonikWorktreeMaxAgeDays — seven days, or
//     what HARMONIK_WORKTREE_MAX_AGE_DAYS says. Both routes hold back a worktree
//     whose run is still working. Two more removers exist: the low-disk path
//     (reclaimStaleWorktrees in diskcheck_hksxlb.go), which runs only below the
//     free-space watermark with no run in flight, and the queue-archive reaper
//     in internal/lifecycle, which removes the worktrees of a cancelled or
//     failed queue item. So a failed run's scratch directory can outlive its run
//     by about a week, not for ever. <worktree>/.harmonik/go-cache, set by the
//     same sandbox path, already lives and dies this way, and it is the larger
//     of the two.
//
// <worktree>/.harmonik/ is gitignored, so scratch files here never appear in
// `git status` and never become part of the agent's change.
func SandboxScratchDir(worktreePath string) string {
	return filepath.Join(worktreePath, ".harmonik", "tmp")
}

// worldSharedTempRoot reports whether dir is a temp root shared by every user
// and process on the host. "/var/tmp" is included as the other POSIX shared-temp
// location and an equally plausible $TMPDIR value; "/" is included because it is
// the degenerate case of the same mistake.
func worldSharedTempRoot(dir string) bool {
	switch filepath.Clean(dir) {
	case "/tmp", "/private/tmp", "/var/tmp", "/private/var/tmp", "/":
		return true
	}
	return false
}

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
	// hk-guapd: reject a world-shared temp root in TmpDirs. srt expands every
	// entry into a RECURSIVE write rule, so granting one of these hands the run
	// write access to every other process's scratch state on the box. The
	// ambient feed that caused the original defect is gone from both call
	// sites; this makes its return a launch-time error instead of a silent
	// over-grant. Cleaning first is load-bearing — exact comparison alone lets
	// "/tmp/" and "/tmp/../tmp" through. A per-run subdirectory such as
	// /tmp/harmonik-run-<id> still passes, which is the supported escape hatch.
	for _, dir := range in.TmpDirs {
		if worldSharedTempRoot(dir) {
			return nil, fmt.Errorf("sandboxprofile: TmpDirs entry %q is a world-shared temp root "+
				"(hk-guapd) — srt expands it into a recursive write rule covering every other "+
				"process's scratch state. Pass a per-run subdirectory instead", dir)
		}
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

	// 6a. The run's own scratch directory — the child's TMPDIR
	// (hk-sandbox-no-writable-tmpdir-7484h). Before this entry a run could still
	// write a temp file: srt pointed the child's TMPDIR at its own default,
	// /tmp/claude, which 6b below grants. What the run did not have was a temp
	// directory of its OWN, so every concurrent run on the box spooled into the
	// same one. The run that threw away finished work was refused for a different
	// reason — it was told to write the literal path /tmp/commit-msg.txt, which
	// no entry here covers. That instruction is fixed in internal/workspace
	// buildAgentTaskContent.
	//
	// It sits UNDER the worktree, which srt already grants recursively, so this
	// line widens nothing — it states where TMPDIR points, in the one document
	// that says what this run may write. See SandboxScratchDir for why the
	// worktree and not /tmp.
	//
	// 6b. srt's own DEFAULT scratch TMPDIR (hk-cdpxu). Absent
	// CLAUDE_CODE_TMPDIR in srt's own environment, srt injects
	// TMPDIR=/tmp/claude into the sandboxed child regardless of the parent's
	// TMPDIR and of what this profile's allowWrite contains. Any tool that
	// honors TMPDIR for scratch/work-dir creation (e.g. `go build`'s "creating
	// work dir" step) then fails with ENOENT unless /tmp/claude is both present
	// on disk AND writable. srtWrapArgv now sets CLAUDE_CODE_TMPDIR, so the
	// fallback is no longer the path a run takes; these two entries remain as
	// the belt for a host whose srt is older than that knob.
	//
	// MEASURED, srt 0.0.63, sandbox-utils.js getDefaultWritePaths(): srt merges
	// its OWN default write set — which already contains both /tmp/claude
	// spellings — into allowOnly ahead of everything this profile says. So on
	// that version these two entries grant nothing srt was not granting anyway,
	// and they are a candidate for deletion once the srt floor is pinned. Left
	// in place deliberately rather than removed on that reading alone.
	//
	// Both the /tmp and /private/tmp forms are listed (macOS symlinks /tmp ->
	// /private/tmp; bwrap/Seatbelt need the literal path used at open time).
	// Directory creation is the caller's responsibility (srtWrapArgv), since
	// this function is a pure profile generator.
	allowWrite = append(allowWrite,
		SandboxScratchDir(in.WorktreePath),
		"/tmp/claude", "/private/tmp/claude")

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
