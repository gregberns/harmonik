package codex

import (
	"context"
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

const walBackupKeepLast = 5

type codexWALGuardConfig struct {
	Codex struct {
		// StaleWALMaxBytes is the secondary log-classification threshold (hk-xisvb:
		// NOT a cleanup gate — every unheld stale WAL is cleaned regardless of
		// size). A pointer so an absent key (nil → fail loud) is distinguishable
		// from an explicit 0 (a valid, required choice).
		StaleWALMaxBytes *int64 `yaml:"stale_wal_max_bytes"`
	} `yaml:"codex"`
}

// ErrMissingStaleWALMaxBytes is returned by cleanCodexStaleWAL when a
// .harmonik/config.yaml exists but the required key codex.stale_wal_max_bytes is
// absent. The key has NO compiled default (the "no hardcoded thresholds"
// mandate): an absent key must fail the codex launch loud, not silently run with
// the guard disabled. The key is a SECONDARY SIGNAL (it classifies the cleanup
// log as large-vs-normal), NOT a cleanup gate — cleanup is unconditional on size.
type ErrMissingStaleWALMaxBytes struct{}

func (e *ErrMissingStaleWALMaxBytes) Error() string {
	return "daemon: codex stale-WAL guard: required key `codex.stale_wal_max_bytes` is not set; " +
		"set it under a `codex:` block in .harmonik/config.yaml " +
		"(e.g. `stale_wal_max_bytes: 1048576`) — there is no compiled default. " +
		"Note: this key is a secondary signal that flags notably-large stale WALs in " +
		"logs; it is NOT a cleanup gate (every unheld stale WAL is cleaned regardless of size)"
}

func cleanCodexStaleWAL(ctx context.Context, projectRoot, codexHome string) error {
	if projectRoot == "" {
		return nil
	}
	configPath := filepath.Join(projectRoot, ".harmonik", "config.yaml")
	//nolint:gosec // G304: configPath is built from operator-supplied projectRoot; not user input.
	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		if errors.Is(readErr, fs.ErrNotExist) {
			return nil
		}
		slog.WarnContext(ctx, "codex_wal_guard_config_read_error", "path", configPath, "error", readErr.Error())
		return nil
	}

	var cfg codexWALGuardConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("daemon: codex stale-WAL guard: parse %s: %w", configPath, err)
	}
	if cfg.Codex.StaleWALMaxBytes == nil {
		return &ErrMissingStaleWALMaxBytes{}
	}
	maxBytes := *cfg.Codex.StaleWALMaxBytes

	home := resolveCodexHome(codexHome)
	pattern := filepath.Join(home, "state_*.sqlite-wal")
	matches, globErr := filepath.Glob(pattern)
	if globErr != nil {
		slog.WarnContext(ctx, "codex_wal_guard_glob_error", "pattern", pattern, "error", globErr.Error())
		return nil
	}

	for _, wal := range matches {
		info, statErr := os.Stat(wal)
		if statErr != nil {
			slog.WarnContext(ctx, "codex_wal_guard_stat_error", "wal", wal, "error", statErr.Error())
			continue
		}

		base := strings.TrimSuffix(wal, "-wal") // state_*.sqlite
		shm := base + "-shm"

		held, handleErr := fileHasOpenHandle(ctx, wal)
		if handleErr != nil {
			slog.WarnContext(ctx, "codex_wal_guard_lsof_unavailable", "wal", wal, "error", handleErr.Error())
			continue
		}
		if held {
			slog.InfoContext(ctx, "codex_wal_guard_skip_held", "wal", wal)
			continue
		}
		if baseHeld, baseErr := fileHasOpenHandle(ctx, base); baseErr != nil {
			slog.WarnContext(ctx, "codex_wal_guard_lsof_unavailable", "file", base, "error", baseErr.Error())
			continue
		} else if baseHeld {
			slog.InfoContext(ctx, "codex_wal_guard_skip_held", "file", base)
			continue
		}

		backupDir := filepath.Join(home, fmt.Sprintf(".wal-backup-%d", time.Now().UnixNano()))
		if mkErr := os.MkdirAll(backupDir, 0o700); mkErr != nil {
			slog.WarnContext(ctx, "codex_wal_guard_backup_mkdir_failed", "dir", backupDir, "error", mkErr.Error())
			continue
		}
		if cpErr := copyFileForBackup(wal, filepath.Join(backupDir, filepath.Base(wal))); cpErr != nil {
			slog.WarnContext(ctx, "codex_wal_guard_backup_failed", "wal", wal, "error", cpErr.Error())
			if rmErr := os.Remove(backupDir); rmErr != nil {
				slog.WarnContext(ctx, "codex_wal_guard_empty_backup_dir_cleanup_failed", "dir", backupDir, "error", rmErr.Error())
			}
			continue
		}
		if _, shmStatErr := os.Stat(shm); shmStatErr == nil {
			if cpErr := copyFileForBackup(shm, filepath.Join(backupDir, filepath.Base(shm))); cpErr != nil {
				slog.WarnContext(ctx, "codex_wal_guard_backup_failed", "shm", shm, "error", cpErr.Error())
			}
		}

		reWalHeld, reWalErr := fileHasOpenHandle(ctx, wal)
		if reWalErr != nil || reWalHeld {
			slog.WarnContext(ctx, "codex_wal_guard_skip_held_after_backup", "wal", wal, "held", reWalHeld, "uncertain", reWalErr != nil)
			continue
		}
		reBaseHeld, reBaseErr := fileHasOpenHandle(ctx, base)
		if reBaseErr != nil || reBaseHeld {
			slog.WarnContext(ctx, "codex_wal_guard_skip_held_after_backup", "file", base, "held", reBaseHeld, "uncertain", reBaseErr != nil)
			continue
		}
		if !walUnchanged(info, wal) {
			slog.WarnContext(ctx, "codex_wal_guard_skip_changed_after_backup", "wal", wal)
			continue
		}

		if rmErr := os.Remove(wal); rmErr != nil {
			slog.WarnContext(ctx, "codex_wal_guard_remove_failed", "wal", wal, "error", rmErr.Error())
			continue
		}
		if rmErr := os.Remove(shm); rmErr != nil && !os.IsNotExist(rmErr) {
			slog.WarnContext(ctx, "codex_wal_guard_remove_failed", "shm", shm, "error", rmErr.Error())
		}
		if info.Size() > maxBytes {
			slog.WarnContext(ctx, "codex_wal_guard_removed_large_stale",
				"wal", wal,
				"size_bytes", info.Size(),
				"max_bytes", maxBytes,
				"backup_dir", backupDir,
			)
		} else {
			slog.InfoContext(ctx, "codex_wal_guard_removed_stale",
				"wal", wal,
				"size_bytes", info.Size(),
				"max_bytes", maxBytes,
				"backup_dir", backupDir,
			)
		}
	}

	reapCodexWALBackupDirs(ctx, home)

	return nil
}

func reapCodexWALBackupDirs(ctx context.Context, codexHome string) {
	pattern := filepath.Join(codexHome, ".wal-backup-*")
	dirs, globErr := filepath.Glob(pattern)
	if globErr != nil {
		slog.WarnContext(ctx, "codex_wal_guard_backup_reap_glob_error", "pattern", pattern, "error", globErr.Error())
		return
	}
	if len(dirs) <= walBackupKeepLast {
		return
	}

	sort.Strings(dirs)

	excess := len(dirs) - walBackupKeepLast
	for _, dir := range dirs[:excess] {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.WarnContext(ctx, "codex_wal_guard_backup_reap_failed", "dir", dir, "error", rmErr.Error())
		} else {
			slog.InfoContext(ctx, "codex_wal_guard_backup_reaped", "dir", dir)
		}
	}
}

func walUnchanged(pre os.FileInfo, wal string) bool {
	cur, err := os.Stat(wal)
	if err != nil {
		return false
	}
	return cur.Size() == pre.Size() && cur.ModTime().Equal(pre.ModTime())
}

func fileHasOpenHandle(ctx context.Context, path string) (bool, error) {
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
	out, runErr := exec.CommandContext(ctx, lsofPath, "--", path).Output()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			if exitErr.ExitCode() == 1 && strings.TrimSpace(string(out)) == "" {
				return false, nil
			}
		}
		return false, fmt.Errorf("lsof %s: %w", path, runErr)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func copyFileForBackup(src, dst string) error {
	//nolint:gosec // G304: src is a CODEX_HOME sidecar path, not user input.
	data, readErr := os.ReadFile(src)
	if readErr != nil {
		return readErr
	}
	return os.WriteFile(dst, data, 0o600)
}
