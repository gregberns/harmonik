package daemon

// codexwalguard.go — per-launch stale-WAL guard for the codex harness (hk-2pb79).
//
// # Why
//
// codex persists session/thread state in $CODEX_HOME/state_*.sqlite (with the
// usual SQLite -wal / -shm sidecars). When a codex run is killed mid-flight
// (e.g. the daemon SIGKILLs a hung implementer), the state_*.sqlite-wal it left
// behind can be stale and corrupt codex's session state on the NEXT launch. The
// symptom is a fast-fail: codex exits in <10s with "exited without advancing
// HEAD" because it never gets a usable session.
//
// This guard runs once per codex launch (CodexHarness.LaunchSpec) and removes a
// stale, UNHELD WAL before codex starts. It is deliberately conservative:
//
//   - It NEVER touches a WAL that a live process holds open (confirmed via
//     lsof). If lsof is unavailable or errors, the WAL is LEFT IN PLACE — we
//     would rather skip cleanup than yank a file out from under a running codex.
//   - It backs up the WAL (and matching -shm) before removing them, so a
//     mis-classified file can be recovered from $CODEX_HOME/.wal-backup-<ns>/.
//   - It leaves the base state_*.sqlite untouched; only the sidecars are removed.
//
// # Threshold (config, fail-loud, zero-defaults)
//
// The size threshold is REQUIRED and has NO compiled default — the same
// fail-loud-on-missing-key / zero-hardcoded-thresholds mandate the governor and
// keeper follow. The operator must set `codex.stale_wal_max_bytes` under a
// `codex:` block in .harmonik/config.yaml. A WAL with size <= the threshold is
// left in place; one larger is a removal candidate. `0` is an explicit valid
// value meaning "any non-empty WAL is stale".
//
// # No-config no-op
//
// If projectRoot is empty, or .harmonik/config.yaml does not exist, the guard is
// a silent no-op (returns nil). This mirrors the governor's "no config.yaml =>
// disabled" rule (hk-drygf): a deployment with no project config never blocks a
// codex launch on this guard, and never fails loud for a missing key in a file
// that does not exist.
//
// Bead ref: hk-2pb79.

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// codexWALGuardConfig is a minimal local view of .harmonik/config.yaml's codex
// block. It is deliberately NOT ProjectConfig: keeping a local struct here lets
// this guard read its single required key without coupling to (or having to
// edit) projectconfig.go.
type codexWALGuardConfig struct {
	Codex struct {
		// StaleWALMaxBytes is the size threshold. A pointer so an absent key
		// (nil) is distinguishable from an explicit 0 (clean any non-empty WAL).
		StaleWALMaxBytes *int64 `yaml:"stale_wal_max_bytes"`
	} `yaml:"codex"`
}

// ErrMissingCodexStaleWALMaxBytes is returned by cleanCodexStaleWAL when a
// .harmonik/config.yaml exists but the required key codex.stale_wal_max_bytes is
// absent. The threshold has NO compiled default (the "no hardcoded thresholds"
// mandate): an absent key must fail the codex launch loud, not silently run with
// the guard disabled.
type ErrMissingCodexStaleWALMaxBytes struct{}

func (e *ErrMissingCodexStaleWALMaxBytes) Error() string {
	return "daemon: codex stale-WAL guard: required key `codex.stale_wal_max_bytes` is not set; " +
		"set it under a `codex:` block in .harmonik/config.yaml " +
		"(e.g. `stale_wal_max_bytes: 1048576`, or `stale_wal_max_bytes: 0` to clean any " +
		"non-empty WAL) — there is no compiled default"
}

// cleanCodexStaleWAL removes a stale, unheld codex state WAL from CODEX_HOME
// before a codex launch.
//
// projectRoot is the harmonik project root (the daemon CWD == ProjectDir).
// codexHome is the configured CODEX_HOME ("" => $HOME/.codex via
// resolveCodexHome).
//
// Behavior:
//   - projectRoot == "" or .harmonik/config.yaml absent => no-op, returns nil.
//   - config.yaml present but codex.stale_wal_max_bytes absent => returns
//     *ErrMissingCodexStaleWALMaxBytes (fail loud). A YAML parse error is also
//     returned.
//   - Otherwise: for each $CODEX_HOME/state_*.sqlite-wal larger than the
//     threshold AND not held open by any process, back up the wal + matching
//     -shm into $CODEX_HOME/.wal-backup-<unixnano>/ and remove them.
//
// The ONLY errors returned are the missing-required-key error and a YAML parse
// error. All cleanup IO (stat/lsof/backup/remove) is best-effort and logged via
// slog; a cleanup hiccup never blocks a codex launch.
func cleanCodexStaleWAL(projectRoot, codexHome string) error {
	if projectRoot == "" {
		return nil
	}
	configPath := filepath.Join(projectRoot, ".harmonik", "config.yaml")
	//nolint:gosec // G304: configPath is built from operator-supplied projectRoot; not user input.
	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			// No project config => guard disabled (governor "no config.yaml" rule).
			return nil
		}
		// A config.yaml exists but is unreadable — surface it as a parse-class
		// error so the launch fails loud rather than silently skipping the guard.
		return fmt.Errorf("daemon: codex stale-WAL guard: read %s: %w", configPath, readErr)
	}

	var cfg codexWALGuardConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("daemon: codex stale-WAL guard: parse %s: %w", configPath, err)
	}
	if cfg.Codex.StaleWALMaxBytes == nil {
		return &ErrMissingCodexStaleWALMaxBytes{}
	}
	maxBytes := *cfg.Codex.StaleWALMaxBytes

	home := resolveCodexHome(codexHome)
	pattern := filepath.Join(home, "state_*.sqlite-wal")
	matches, globErr := filepath.Glob(pattern)
	if globErr != nil {
		// Bad glob pattern is the only error filepath.Glob returns; best-effort.
		slog.Warn("codex_wal_guard_glob_error", "pattern", pattern, "error", globErr.Error())
		return nil
	}

	for _, wal := range matches {
		info, statErr := os.Stat(wal)
		if statErr != nil {
			slog.Warn("codex_wal_guard_stat_error", "wal", wal, "error", statErr.Error())
			continue
		}
		if info.Size() <= maxBytes {
			// Within threshold — leave it.
			continue
		}

		base := strings.TrimSuffix(wal, "-wal") // state_*.sqlite
		shm := base + "-shm"

		// SAFETY: never yank a WAL a live process holds open. If we cannot
		// confirm it is unheld (lsof missing / errored), skip removal.
		held, handleErr := fileHasOpenHandle(wal)
		if handleErr != nil {
			slog.Warn("codex_wal_guard_lsof_unavailable", "wal", wal, "error", handleErr.Error())
			continue
		}
		if held {
			slog.Info("codex_wal_guard_skip_held", "wal", wal)
			continue
		}
		// Also check the base db file is not held (a live codex would hold both).
		if baseHeld, baseErr := fileHasOpenHandle(base); baseErr != nil {
			slog.Warn("codex_wal_guard_lsof_unavailable", "file", base, "error", baseErr.Error())
			continue
		} else if baseHeld {
			slog.Info("codex_wal_guard_skip_held", "file", base)
			continue
		}

		// Stale + unheld: back up the sidecars, then remove them.
		backupDir := filepath.Join(home, fmt.Sprintf(".wal-backup-%d", time.Now().UnixNano()))
		if mkErr := os.MkdirAll(backupDir, 0o700); mkErr != nil {
			slog.Warn("codex_wal_guard_backup_mkdir_failed", "dir", backupDir, "error", mkErr.Error())
			continue
		}
		if cpErr := copyFileForBackup(wal, filepath.Join(backupDir, filepath.Base(wal))); cpErr != nil {
			slog.Warn("codex_wal_guard_backup_failed", "wal", wal, "error", cpErr.Error())
			continue
		}
		// Best-effort -shm backup; absence is fine (not all WALs have a -shm).
		if _, shmStatErr := os.Stat(shm); shmStatErr == nil {
			if cpErr := copyFileForBackup(shm, filepath.Join(backupDir, filepath.Base(shm))); cpErr != nil {
				slog.Warn("codex_wal_guard_backup_failed", "shm", shm, "error", cpErr.Error())
				// Continue to removal anyway: the -wal backup (the corrupting
				// file) succeeded; -shm is reconstructable by SQLite.
			}
		}

		if rmErr := os.Remove(wal); rmErr != nil {
			slog.Warn("codex_wal_guard_remove_failed", "wal", wal, "error", rmErr.Error())
			continue
		}
		// Remove the -shm too; absence is not an error.
		if rmErr := os.Remove(shm); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.Warn("codex_wal_guard_remove_failed", "shm", shm, "error", rmErr.Error())
		}
		slog.Info("codex_wal_guard_cleaned",
			"wal", wal,
			"size_bytes", info.Size(),
			"max_bytes", maxBytes,
			"backup_dir", backupDir,
		)
	}

	return nil
}

// fileHasOpenHandle reports whether any process holds path open, via `lsof`.
//
// It is deliberately conservative: if lsof is not on PATH, or the invocation
// errors in a way that is not lsof's normal "no open handle" exit (exit 1, empty
// output), it returns a non-nil error so the caller SKIPS removal. The caller
// must treat "cannot confirm" as "do not delete".
//
// A non-existent path is reported as not-held (false, nil): there is nothing to
// hold open, and the caller has already stat'd the WAL it cares about.
func fileHasOpenHandle(path string) (bool, error) {
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, statErr
	}
	lsofPath, lookErr := exec.LookPath("lsof")
	if lookErr != nil {
		return false, fmt.Errorf("lsof not on PATH: %w", lookErr)
	}
	// `lsof -- <path>` lists processes holding path open. Exit 0 + output =>
	// held. Exit 1 + empty output => not held (lsof's normal "nothing found").
	//nolint:gosec // G204: lsofPath resolved via LookPath; path is a CODEX_HOME sidecar, not user input.
	out, runErr := exec.Command(lsofPath, "--", path).Output()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			// lsof exits 1 with no matches; that is the unheld case.
			if exitErr.ExitCode() == 1 && len(strings.TrimSpace(string(out))) == 0 {
				return false, nil
			}
		}
		return false, fmt.Errorf("lsof %s: %w", path, runErr)
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// copyFileForBackup copies src to dst, creating dst (0600). It is used to stage
// a WAL/-shm into the backup dir before removal. Best-effort caller: any error
// is surfaced so the caller can skip removal and keep the original in place.
func copyFileForBackup(src, dst string) error {
	//nolint:gosec // G304: src is a CODEX_HOME sidecar path, not user input.
	data, readErr := os.ReadFile(src)
	if readErr != nil {
		return readErr
	}
	//nolint:gosec // G306: backup file mirrors the sidecar; 0600 is restrictive.
	return os.WriteFile(dst, data, 0o600)
}
