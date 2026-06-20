package supervisecmd

// assetskew.go — supervisor wiring for asset version-skew detection (hk-yqx9,
// plans/2026-06-20-doc-instruction-audit/10-asset-sync.md §"Daemon-safety").
//
// The DETECTION logic lives in package main (asset_skew.go) because it needs the
// embedded asset manifest (//go:embed assets) and the reconcile planner, both of
// which live there. supervisecmd is imported BY main, so it cannot import main back.
// We bridge with a registration hook: main installs SkewCheckHook at init time, and
// the shim calls RunAssetSkewCheck once at supervisor boot.
//
// SAFETY: the boot check is detection + NOTIFY only. It runs ONCE at supervisor
// startup (not every loop — the supervisee runs as a long-lived exec, there is no
// fast tick here to spam from), logs the verdict, and on skew posts a single comms
// notice to the captain telling someone to run `harmonik sync-assets`. Auto-apply is
// config-gated (AssetSyncConfig.AutoApply, OFF by default) and DEFERRED — see the
// TODO in maybeAutoApply.

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
)

// AssetSkewVerdict is the supervisor-facing projection of the main-package skew
// check. It deliberately mirrors the fields the notice needs without importing the
// main package's SkewResult type (which the hook can't expose across the import
// boundary). The hook populates it.
type AssetSkewVerdict struct {
	Skewed              bool
	ChangedCount        int
	ConflictCount       int
	AutoApplyCandidates int
	NeverSynced         bool
	BinaryDigest        string
	LockDigest          string
}

// SkewCheckHook is installed by package main (init-time) to run the actual skew
// computation for a project dir. nil when not installed (e.g. in a supervise-package
// unit test) — RunAssetSkewCheck then no-ops safely.
var SkewCheckHook func(projectDir string) (AssetSkewVerdict, error)

// RunAssetSkewCheck runs the boot-time asset version-skew check for projectDir and,
// on skew, notifies the captain. It is best-effort: any error is logged and
// swallowed (a skew-check failure must NEVER take down the supervisor). cfg gates the
// optional auto-apply path.
//
// Cadence: called ONCE at supervisor boot from runWithSupervisor. We do not poll —
// the supervisee is a long-lived exec and a binary swap requires a supervisor
// restart, which re-runs this check. So "at boot" already covers "on version bump".
func RunAssetSkewCheck(projectDir string, cfg Config, log *slog.Logger, stderr io.Writer) {
	if SkewCheckHook == nil {
		// No detection hook wired (shouldn't happen in the real binary); nothing to do.
		return
	}
	v, err := SkewCheckHook(projectDir)
	if err != nil {
		if log != nil {
			log.Warn("asset-skew check failed", "err", err)
		}
		return
	}
	if !v.Skewed {
		if log != nil {
			log.Info("asset-skew: project assets up to date", "digest", v.BinaryDigest)
		}
		return
	}

	if log != nil {
		log.Info("asset-skew: project assets behind running binary",
			"changed", v.ChangedCount,
			"conflicts", v.ConflictCount,
			"auto_apply_candidates", v.AutoApplyCandidates,
			"never_synced", v.NeverSynced)
	}

	notifyCaptainSkew(projectDir, v, log, stderr)

	// Optional, config-gated, OFF by default.
	maybeAutoApply(projectDir, cfg, v, log, stderr)
}

// notifyCaptainSkew posts a single status notice to the captain over the comms bus
// telling someone to run sync-assets. It shells out to `harmonik comms send` (the
// same self-invocation pattern the shim already uses for the daemon revival argv):
// the comms surface is a daemon-socket RPC, and the daemon is up under the
// supervisor, so the directed message lands in the captain's inbox / the comms log.
//
// Best-effort: a send failure (no daemon socket, no captain) is logged, not fatal.
func notifyCaptainSkew(projectDir string, v AssetSkewVerdict, log *slog.Logger, stderr io.Writer) {
	exe, err := os.Executable()
	if err != nil {
		if log != nil {
			log.Warn("asset-skew: cannot resolve executable for comms notify", "err", err)
		}
		return
	}

	var body string
	if v.NeverSynced {
		body = fmt.Sprintf(
			"assets never synced on this project: %d files have updates — run 'harmonik sync-assets' (dry-run) to review",
			v.ChangedCount)
	} else {
		body = fmt.Sprintf(
			"assets behind the running binary: %d files have updates (%d conflicts) — run 'harmonik sync-assets' (dry-run) to review",
			v.ChangedCount, v.ConflictCount)
	}

	//nolint:gosec // G204: exe is os.Executable(), projectDir operator-controlled.
	cmd := exec.Command(exe,
		"comms", "send",
		"--project", projectDir,
		"--from", "supervisor",
		"--to", "captain",
		"--topic", "status",
		"--", body)
	cmd.Stdout = stderr // comms send writes nothing useful to stdout; merge to stderr log
	cmd.Stderr = stderr
	if runErr := cmd.Run(); runErr != nil {
		if log != nil {
			log.Warn("asset-skew: comms notify failed (daemon down or no captain?)", "err", runErr)
		}
		return
	}
	if log != nil {
		log.Info("asset-skew: notified captain to run sync-assets", "changed", v.ChangedCount)
	}
}

// maybeAutoApply is the config-gated, OFF-by-default auto-apply gate (doc 10
// §Daemon-safety: "may auto-apply only the FAST-FORWARD, MANAGED (skill) files
// during a quiescent window, surfacing every CONFLICT and every content-owned change
// for human review").
//
// DEFERRED: per hk-yqx9, the shippable core is detection + notify. Wiring the actual
// apply into the running supervisor loop is intentionally NOT done in this pass — it
// would have the supervisor write the project's main working tree, which is exactly
// the hazard the daemon-lull gate guards against, and doing it safely mid-supervision
// needs a confirmed-lull handshake with the daemon that is larger than this bead.
//
// What this DOES today: when AutoApply is explicitly enabled AND there are safe
// (Managed + FastForward) candidates, it NOTIFIES what WOULD be auto-applied so the
// behaviour is visible, and leaves a clear marker that execution is deferred. When
// AutoApply is OFF (the default) it is a no-op beyond a debug log.
//
// TODO(hk-yqx9 follow-up): execute the auto-apply by invoking the sync-assets apply
// path filtered to (class==Managed && action==FastForward) ONLY, gated on the SAME
// daemonDispatchGate lull check sync-assets uses, after which re-stamp the lock. Every
// Conflict and every non-managed change must still be surfaced (notified), never
// applied. Until then, the operator runs `harmonik sync-assets --apply` manually.
func maybeAutoApply(projectDir string, cfg Config, v AssetSkewVerdict, log *slog.Logger, stderr io.Writer) {
	if !cfg.AssetSync.AutoApply {
		if log != nil {
			log.Debug("asset-skew: auto-apply disabled (default); notify-only")
		}
		return
	}
	// Enabled, but execution is deferred this pass: report what WOULD be applied.
	if v.AutoApplyCandidates == 0 {
		if log != nil {
			log.Info("asset-skew: auto-apply enabled but no safe (Managed+FastForward) candidates")
		}
		return
	}
	if log != nil {
		log.Warn("asset-skew: auto-apply ENABLED but execution is DEFERRED (hk-yqx9 follow-up); "+
			"run 'harmonik sync-assets --apply' manually",
			"would_apply", v.AutoApplyCandidates,
			"conflicts_held_for_review", v.ConflictCount)
	}
	// Surface the deferred-auto-apply state to the captain too, so it is not silent.
	exe, err := os.Executable()
	if err != nil {
		return
	}
	body := fmt.Sprintf(
		"asset auto-apply is ENABLED but deferred: %d safe skill fast-forwards would apply, %d conflicts held for review — run 'harmonik sync-assets --apply' (in a lull)",
		v.AutoApplyCandidates, v.ConflictCount)
	//nolint:gosec // G204: exe is os.Executable(), projectDir operator-controlled.
	cmd := exec.Command(exe,
		"comms", "send",
		"--project", projectDir,
		"--from", "supervisor",
		"--to", "captain",
		"--topic", "status",
		"--", body)
	cmd.Stdout = stderr
	cmd.Stderr = stderr
	_ = cmd.Run() // best-effort
}
