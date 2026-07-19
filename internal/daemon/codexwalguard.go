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
// This guard runs once per codex launch (CodexHarness.LaunchSpec) and cleans ANY
// present, unheld stale WAL regardless of size. Staleness is a function of being
// LEFT BEHIND BY A KILLED/SLEPT RUN, not of size — a SMALL stale WAL fast-fails
// codex just as hard as a large one (field incident hk-xisvb: a 234 KB stale
// state WAL, well under the 1 MiB threshold, made the old size-gated guard a
// no-op and codex fast-failed fleet-wide). The ONLY safety gate is "unheld"
// (lsof) plus a TOCTOU re-check. It is deliberately conservative:
//
//   - It NEVER touches a WAL that a live process holds open (confirmed via
//     lsof). If lsof is unavailable or errors, the WAL is LEFT IN PLACE — we
//     would rather skip cleanup than yank a file out from under a running codex.
//   - It backs up the WAL (and matching -shm) before removing them, so a
//     mis-classified file can be recovered from $CODEX_HOME/.wal-backup-<ns>/.
//   - It leaves the base state_*.sqlite untouched; only the sidecars are removed.
//
// # Threshold (config, fail-loud, zero-defaults — secondary signal only)
//
// `codex.stale_wal_max_bytes` is REQUIRED and has NO compiled default — the same
// fail-loud-on-missing-key / zero-hardcoded-thresholds mandate the governor and
// keeper follow. The operator must set it under a `codex:` block in
// .harmonik/config.yaml. Its ROLE, however, is NOT a cleanup gate: every present,
// unheld stale WAL is cleaned regardless of size. The threshold is a SECONDARY
// SIGNAL used only to classify the cleanup log line — a cleaned WAL larger than
// the threshold is flagged as a notably-large stale WAL
// (codex_wal_guard_removed_large_stale), otherwise it logs as a normal cleanup
// (codex_wal_guard_removed_stale). Cleanup happens either way.
//
// # No-config no-op
//
// If projectRoot is empty, or .harmonik/config.yaml does not exist, the guard is
// a silent no-op (returns nil). This mirrors the governor's "no config.yaml =>
// disabled" rule (hk-drygf): a deployment with no project config never blocks a
// codex launch on this guard, and never fails loud for a missing key in a file
// that does not exist.
//
// # AIS-017 adapt-not-delete disposition (agent-input-substrate M2, T10)
//
// The structured Codex app-server driver (internal/codexdriver +
// internal/codexinput) REDUCES the need for this guard exactly as AIS-017
// mandates, and does so BY CONSTRUCTION — it carries no per-launch WAL-guard call
// of its own because it never SIGKILLs a healthy child:
//
//   - graceful turn-termination: a mid-turn CloseInput emits `turn/interrupt`
//     and drains, never a SIGKILL (codexinput.stepClose InTurn branch;
//     codexdriver.writeInterrupt). An ungraceful kill is what leaves the stale
//     state_*.sqlite-wal behind — the interrupt path avoids creating one.
//   - positive fast-fail: no handshake within the bound emits a structured
//     agent_launch_failure event, not the <10s silent exit-0 that this guard's
//     "exited without advancing HEAD" symptom describes (codexinput TimerHandshake
//     edge; surfaced by codexdriver via setFailure/failCh — never swallowed).
//
// This guard is therefore NOT deleted (AIS-017 "adapt, don't delete"): it still
// compensates for RESIDUAL ungraceful SIGKILL/crash paths, which remain live via
// the legacy `codex exec` one-shot harness (codexharness.go), whose Teardown
// still Kills and which is the codex path in service until the structured driver
// is wired in (T12). Its single call site (codexharness.LaunchSpec) is thus still
// LOAD-BEARING and is retained. The guard's post-M2 home is codex process
// lifecycle proper; relocating the residual recovery to a dedicated boot-time
// step there (rather than per-legacy-launch) is a codex-process-lifecycle
// follow-up, deliberately out of this driver task's surface.
//
// Bead ref: hk-2pb79.

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	yaml "gopkg.in/yaml.v3"
)

// walBackupKeepLast bounds how many $CODEX_HOME/.wal-backup-<unixnano>/
// directories the guard leaves on disk. Each launch that cleans a stale WAL
// creates one more backup dir, so on a host that repeatedly hits stale WALs
// these accumulate forever without a reap step. This is a fixed retention
// count, not a tunable business threshold (unlike stale_wal_max_bytes above),
// so it is a compiled constant rather than a required config key.
const walBackupKeepLast = 5

// codexWALGuardConfig is a minimal local view of .harmonik/config.yaml's codex
// block. It is deliberately NOT ProjectConfig: keeping a local struct here lets
// this guard read its single required key without coupling to (or having to
// edit) projectconfig.go.
type codexWALGuardConfig struct {
	Codex struct {
		// StaleWALMaxBytes is the secondary log-classification threshold (hk-xisvb:
		// NOT a cleanup gate — every unheld stale WAL is cleaned regardless of
		// size). A pointer so an absent key (nil → fail loud) is distinguishable
		// from an explicit 0 (a valid, required choice).
		StaleWALMaxBytes *int64 `yaml:"stale_wal_max_bytes"`
	} `yaml:"codex"`
}

// ErrMissingCodexStaleWALMaxBytes is returned by cleanCodexStaleWAL when a
// .harmonik/config.yaml exists but the required key codex.stale_wal_max_bytes is
// absent. The key has NO compiled default (the "no hardcoded thresholds"
// mandate): an absent key must fail the codex launch loud, not silently run with
// the guard disabled. The key is a SECONDARY SIGNAL (it classifies the cleanup
// log as large-vs-normal), NOT a cleanup gate — cleanup is unconditional on size.
type ErrMissingCodexStaleWALMaxBytes struct{}

func (e *ErrMissingCodexStaleWALMaxBytes) Error() string {
	return "daemon: codex stale-WAL guard: required key `codex.stale_wal_max_bytes` is not set; " +
		"set it under a `codex:` block in .harmonik/config.yaml " +
		"(e.g. `stale_wal_max_bytes: 1048576`) — there is no compiled default. " +
		"Note: this key is a secondary signal that flags notably-large stale WALs in " +
		"logs; it is NOT a cleanup gate (every unheld stale WAL is cleaned regardless of size)"
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
//   - config.yaml present but UNREADABLE (permission/IO, not fs.ErrNotExist) =>
//     best-effort no-op (slog.Warn + return nil); a read error does not block a
//     launch.
//   - config.yaml present but codex.stale_wal_max_bytes absent => returns
//     *ErrMissingCodexStaleWALMaxBytes (fail loud). A YAML parse error is also
//     returned.
//   - Otherwise: for each $CODEX_HOME/state_*.sqlite-wal that is present AND not
//     held open by any process (regardless of size), back up the wal + matching
//     -shm into $CODEX_HOME/.wal-backup-<unixnano>/ and remove them — with a
//     pre-backup AND post-backup (TOCTOU) unheld + unchanged re-check. The byte
//     threshold is NOT a cleanup gate; it only classifies the cleanup log line.
//
// The ONLY errors returned are the missing-required-key error and a YAML parse
// error. All other paths (config read/IO, stat/lsof/backup/remove) are
// best-effort and logged via slog; a cleanup hiccup never blocks a codex launch.
func cleanCodexStaleWAL(projectRoot, codexHome string) error {
	if projectRoot == "" {
		return nil
	}
	configPath := filepath.Join(projectRoot, ".harmonik", "config.yaml")
	//nolint:gosec // G304: configPath is built from operator-supplied projectRoot; not user input.
	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) {
			// No project config => guard disabled (governor "no config.yaml" rule).
			return nil
		}
		// config.yaml is present but unreadable (permission/IO). Only a
		// missing-key or YAML-parse failure is allowed to block a codex launch;
		// a read/IO error is best-effort — log and no-op rather than fail loud.
		slog.Warn("codex_wal_guard_config_read_error", "path", configPath, "error", readErr.Error())
		return nil
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
		// NO size gate (hk-xisvb): staleness is a function of being left behind by
		// a killed/slept run, not of size. Every present, unheld WAL is a cleanup
		// candidate; the byte threshold is consulted later only to classify the log.

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
			// backupDir was just created and holds nothing (the wal copy that
			// would populate it failed) — drop the empty dir rather than
			// leaving a useless stub behind on every failed backup attempt.
			if rmErr := os.Remove(backupDir); rmErr != nil {
				slog.Warn("codex_wal_guard_empty_backup_dir_cleanup_failed", "dir", backupDir, "error", rmErr.Error())
			}
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

		// TOCTOU re-check (data-corruption guard). $HOME/.codex is shared across
		// all concurrent codex runs, and the backup copy above is slow. During it
		// a freshly-dispatched codex can open the same state_*.sqlite and start
		// writing a LIVE -wal/-shm at these exact paths. Removing them now would
		// delete a live writer's sidecars — the exact corruption this guard
		// exists to prevent. So immediately before os.Remove, re-validate both
		// "unheld" (lsof) and "unchanged-stale" (re-stat) within a syscall pair;
		// only then remove. flock would not help — codex takes no such lock, so a
		// lock only serializes guard runs against each other, not guard-vs-codex.
		reWalHeld, reWalErr := fileHasOpenHandle(wal)
		if reWalErr != nil || reWalHeld {
			slog.Warn("codex_wal_guard_skip_held_after_backup", "wal", wal, "held", reWalHeld, "uncertain", reWalErr != nil)
			continue
		}
		reBaseHeld, reBaseErr := fileHasOpenHandle(base)
		if reBaseErr != nil || reBaseHeld {
			slog.Warn("codex_wal_guard_skip_held_after_backup", "file", base, "held", reBaseHeld, "uncertain", reBaseErr != nil)
			continue
		}
		if !walUnchanged(info, wal) {
			// A live writer touched the WAL after our pre-backup stat (gone,
			// changed size, or mtime changed). Leave it.
			slog.Warn("codex_wal_guard_skip_changed_after_backup", "wal", wal)
			continue
		}

		if rmErr := os.Remove(wal); rmErr != nil {
			slog.Warn("codex_wal_guard_remove_failed", "wal", wal, "error", rmErr.Error())
			continue
		}
		// Remove the -shm too; absence is not an error.
		if rmErr := os.Remove(shm); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.Warn("codex_wal_guard_remove_failed", "shm", shm, "error", rmErr.Error())
		}
		// Secondary signal: the byte threshold no longer gates cleanup; it only
		// classifies this log line. A cleaned WAL larger than the threshold is
		// flagged as notably-large; everything else is a normal stale cleanup.
		if info.Size() > maxBytes {
			slog.Warn("codex_wal_guard_removed_large_stale",
				"wal", wal,
				"size_bytes", info.Size(),
				"max_bytes", maxBytes,
				"backup_dir", backupDir,
			)
		} else {
			slog.Info("codex_wal_guard_removed_stale",
				"wal", wal,
				"size_bytes", info.Size(),
				"max_bytes", maxBytes,
				"backup_dir", backupDir,
			)
		}
	}

	reapCodexWALBackupDirs(home)

	return nil
}

// reapCodexWALBackupDirs keeps at most walBackupKeepLast of the
// $CODEX_HOME/.wal-backup-<unixnano>/ directories the guard creates,
// removing the oldest excess ones. It runs once per guard invocation (after
// any cleanup above) so backup dirs from repeated stale-WAL launches don't
// accumulate on disk forever. Best-effort: a glob/stat/remove failure is
// logged and otherwise ignored — reaping stale backups never blocks a codex
// launch.
func reapCodexWALBackupDirs(codexHome string) {
	pattern := filepath.Join(codexHome, ".wal-backup-*")
	dirs, globErr := filepath.Glob(pattern)
	if globErr != nil {
		slog.Warn("codex_wal_guard_backup_reap_glob_error", "pattern", pattern, "error", globErr.Error())
		return
	}
	if len(dirs) <= walBackupKeepLast {
		return
	}

	// The unixnano suffix means lexical sort == chronological sort (fixed
	// digit count for a given epoch), oldest first.
	sort.Strings(dirs)

	excess := len(dirs) - walBackupKeepLast
	for _, dir := range dirs[:excess] {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.Warn("codex_wal_guard_backup_reap_failed", "dir", dir, "error", rmErr.Error())
		} else {
			slog.Info("codex_wal_guard_backup_reaped", "dir", dir)
		}
	}
}

// walUnchanged reports whether wal still exists and is byte-for-byte the same
// file pre described (same size AND same mtime). It is the re-stat half of the
// TOCTOU re-check: its sole purpose is "did a live writer touch this file between
// the lsof check and the remove?". A true result means no live writer touched the
// WAL between the pre-backup stat and now, so removal is safe.
//
// Returns false if the file is gone, its size differs from pre, or its ModTime
// differs from pre — any of which indicates a concurrent writer. It is
// deliberately size-threshold-FREE (hk-xisvb): staleness no longer depends on
// size, so the re-check must not reintroduce a byte gate.
func walUnchanged(pre os.FileInfo, wal string) bool {
	cur, err := os.Stat(wal)
	if err != nil {
		return false
	}
	return cur.Size() == pre.Size() && cur.ModTime().Equal(pre.ModTime())
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
