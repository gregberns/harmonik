package daemon

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

type srtNetworkConfig struct {
	AllowedDomains    []string `json:"allowedDomains"`
	DeniedDomains     []string `json:"deniedDomains"`
	AllowUnixSockets  []string `json:"allowUnixSockets"`
	AllowLocalBinding bool     `json:"allowLocalBinding"`
}

type srtFilesystemConfig struct {
	DenyRead   []string `json:"denyRead"`
	AllowRead  []string `json:"allowRead"`
	AllowWrite []string `json:"allowWrite"`
	DenyWrite  []string `json:"denyWrite"`
}

type srtSettings struct {
	Network                      srtNetworkConfig    `json:"network"`
	Filesystem                   srtFilesystemConfig `json:"filesystem"`
	EnableWeakerNestedSandbox    bool                `json:"enableWeakerNestedSandbox"`
	EnableWeakerNetworkIsolation bool                `json:"enableWeakerNetworkIsolation"`
	AllowAppleEvents             bool                `json:"allowAppleEvents"`
}

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
	for _, dir := range in.TmpDirs {
		if worldSharedTempRoot(dir) {
			return nil, fmt.Errorf("sandboxprofile: TmpDirs entry %q is a world-shared temp root "+
				"(hk-guapd) — srt expands it into a recursive write rule covering every other "+
				"process's scratch state. Pass a per-run subdirectory instead", dir)
		}
	}

	allowWrite := make([]string, 0, 5+len(in.TmpDirs)+len(in.PrivateWriteCacheDirs))

	allowWrite = append(allowWrite, in.WorktreePath)

	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "worktrees", in.RunID))

	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "objects"))

	if in.BranchName != "" {
		allowWrite = append(allowWrite, filepath.Dir(filepath.Join(in.GitDir, "refs", "heads", in.BranchName)))
	} else {
		allowWrite = append(allowWrite, filepath.Join(in.GitDir, "refs", "heads"))
	}

	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "packed-refs"))
	allowWrite = append(allowWrite, filepath.Join(in.GitDir, "packed-refs.lock"))

	if in.BranchName != "" {
		allowWrite = append(allowWrite, filepath.Dir(filepath.Join(in.GitDir, "logs", "refs", "heads", in.BranchName)))
	} else {
		allowWrite = append(allowWrite, filepath.Join(in.GitDir, "logs", "refs", "heads"))
	}

	allowWrite = append(allowWrite, in.TmpDirs...)

	allowWrite = append(allowWrite,
		SandboxScratchDir(in.WorktreePath),
		"/tmp/claude", "/private/tmp/claude")

	allowWrite = append(allowWrite, in.PrivateWriteCacheDirs...)

	allowRead := make([]string, len(in.SharedReadCacheDirs))
	copy(allowRead, in.SharedReadCacheDirs)

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
