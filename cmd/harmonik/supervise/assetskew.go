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
// config-gated (AssetSyncConfig.AutoApply, OFF by default); AutoApplyHook executes
// the apply when AutoApplyGateHook confirms the daemon is quiescent.

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

// AutoApplyGateHook is installed by package main to check the daemon-lull gate
// before invoking AutoApplyHook. Returns (true, reason, nil) when the daemon is
// actively dispatching — auto-apply is skipped (notify-only). When nil, the gate
// is skipped (treated as not dispatching).
var AutoApplyGateHook func(projectDir string) (dispatching bool, reason string, err error)

// AutoApplyHook is installed by package main to execute the actual auto-apply
// (Managed+FastForward only) for a project dir. Returns the count of files applied.
// Called only when AutoApply is enabled, there are safe candidates, and the lull
// gate confirms the daemon is quiescent.
var AutoApplyHook func(projectDir string) (applied int, err error)

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

// maybeAutoApply is the config-gated, OFF-by-default auto-apply path (doc 10
// §Daemon-safety: "may auto-apply only the FAST-FORWARD, MANAGED (skill) files
// during a quiescent window, surfacing every CONFLICT and every content-owned change
// for human review").
//
// When AutoApply is OFF (the default) it is a no-op. When ON, it checks the
// daemon-lull gate (AutoApplyGateHook) before delegating the actual apply to
// AutoApplyHook (both installed by package main at init time).
func maybeAutoApply(projectDir string, cfg Config, v AssetSkewVerdict, log *slog.Logger, _ io.Writer) {
	if !cfg.AssetSync.AutoApply {
		if log != nil {
			log.Debug("asset-skew: auto-apply disabled (default); notify-only")
		}
		return
	}
	if v.AutoApplyCandidates == 0 {
		if log != nil {
			log.Info("asset-skew: auto-apply enabled but no safe (Managed+FastForward) candidates")
		}
		return
	}

	// Lull-gate: refuse to apply while the daemon is actively dispatching.
	if AutoApplyGateHook != nil {
		dispatching, reason, err := AutoApplyGateHook(projectDir)
		if err != nil {
			if log != nil {
				log.Warn("asset-skew: auto-apply lull-gate check failed; skipping apply", "err", err)
			}
			return
		}
		if dispatching {
			if log != nil {
				log.Warn("asset-skew: auto-apply skipped — daemon is dispatching; notify-only",
					"reason", reason, "would_apply", v.AutoApplyCandidates)
			}
			return
		}
	}

	if AutoApplyHook == nil {
		if log != nil {
			log.Warn("asset-skew: auto-apply enabled but AutoApplyHook not installed")
		}
		return
	}

	applied, err := AutoApplyHook(projectDir)
	if err != nil {
		if log != nil {
			log.Warn("asset-skew: auto-apply failed", "err", err,
				"conflicts_held_for_review", v.ConflictCount)
		}
		return
	}
	if log != nil {
		log.Info("asset-skew: auto-apply complete",
			"applied", applied,
			"conflicts_held_for_review", v.ConflictCount)
	}
}
