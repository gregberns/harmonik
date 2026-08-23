package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

const defaultRestartBackoffBase = 30 * time.Second

const defaultRestartBackoffCap = 10 * time.Minute

const defaultRestartBackoffWindow = 1 * time.Hour

type resolvedRestartBackoffConfig struct {
	Base   time.Duration
	Cap    time.Duration
	Window time.Duration
}

func resolveRestartBackoffConfig(raw projectconfig.DaemonRestartBackoffConfig) resolvedRestartBackoffConfig {
	cfg := resolvedRestartBackoffConfig{
		Base:   defaultRestartBackoffBase,
		Cap:    defaultRestartBackoffCap,
		Window: defaultRestartBackoffWindow,
	}
	if raw.Base > 0 {
		cfg.Base = raw.Base
	}
	if raw.Cap > 0 {
		cfg.Cap = raw.Cap
	}
	if raw.Window > 0 {
		cfg.Window = raw.Window
	}
	return cfg
}

func restartRecordPath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "cognition", "restart-record.json")
}

type restartRecord struct {
	SchemaVersion int     `json:"schema_version"`
	BootTimesUnix []int64 `json:"boot_times_unix_sec"`
}

func applyBootBackoff(ctx context.Context, projectDir string, rawCfg projectconfig.DaemonRestartBackoffConfig) time.Duration {
	if projectDir == "" {
		return 0
	}
	cfg := resolveRestartBackoffConfig(rawCfg)

	path := restartRecordPath(projectDir)
	now := time.Now()

	rec, readErr := readRestartRecord(path)
	if readErr != nil && !os.IsNotExist(readErr) {
		fmt.Fprintf(os.Stderr, "daemon: restart-backoff: read %q: %v (skipping backoff)\n", path, readErr)
		if writeErr := writeRestartRecord(ctx, path, restartRecord{
			SchemaVersion: 1,
			BootTimesUnix: []int64{now.Unix()},
		}); writeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: restart-backoff: write %q: %v\n", path, writeErr)
		}
		return 0
	}

	windowStart := now.Add(-cfg.Window)
	recent := make([]int64, 0, len(rec.BootTimesUnix)+1)
	for _, t := range rec.BootTimesUnix {
		if time.Unix(t, 0).After(windowStart) {
			recent = append(recent, t)
		}
	}

	n := len(recent) // boots before this one
	delay := computeRestartBackoffDelay(n, cfg.Base, cfg.Cap)

	rec.SchemaVersion = 1
	rec.BootTimesUnix = append(recent, now.Unix())
	if writeErr := writeRestartRecord(ctx, path, rec); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: restart-backoff: write %q: %v\n", path, writeErr)
	}

	if delay > 0 {
		fmt.Fprintf(os.Stderr,
			"daemon: restart-backoff: %d rapid boot(s) in the last %s — delaying dispatch by %s (after socket bind)\n",
			n, cfg.Window, delay)
	}

	return delay
}

func sleepBootBackoff(ctx context.Context, delay time.Duration) {
	if delay <= 0 {
		return
	}
	select {
	case <-time.After(delay):
	case <-ctx.Done():
	}
}

func computeRestartBackoffDelay(n int, base, cap time.Duration) time.Duration {
	if n <= 0 {
		return 0
	}
	if n > 30 {
		n = 30
	}
	delay := time.Duration(float64(base) * math.Pow(2, float64(n-1)))
	if delay > cap || delay < 0 {
		return cap
	}
	return delay
}

func readRestartRecord(path string) (restartRecord, error) {
	//nolint:gosec // G304: path derived from projectDir (operator-controlled daemon arg)
	data, err := os.ReadFile(path)
	if err != nil {
		return restartRecord{}, err
	}
	var rec restartRecord
	if unmarshalErr := json.Unmarshal(data, &rec); unmarshalErr != nil {
		return restartRecord{}, fmt.Errorf("restartRecord unmarshal: %w", unmarshalErr)
	}
	return rec, nil
}

func writeRestartRecord(ctx context.Context, path string, rec restartRecord) error {
	dir := filepath.Dir(path)
	if mkErr := os.MkdirAll(dir, core.HarmonikDirMode); mkErr != nil {
		return fmt.Errorf("mkdir %s: %w", dir, mkErr)
	}
	data, marshalErr := json.MarshalIndent(rec, "", "  ")
	if marshalErr != nil {
		return fmt.Errorf("marshal: %w", marshalErr)
	}
	data = append(data, '\n')

	tmp, tmpErr := os.CreateTemp(dir, "restart-record-*.json.tmp")
	if tmpErr != nil {
		return fmt.Errorf("create temp: %w", tmpErr)
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			if closeErr := tmp.Close(); closeErr != nil {
				slog.WarnContext(ctx, "restartbackoff: close temp during cleanup", "err", closeErr, "path", tmpPath)
			}
			_ = os.Remove(tmpPath) //nolint:errcheck // cleanup; unactionable
		}
	}()
	if _, writeErr := tmp.Write(data); writeErr != nil {
		return writeErr
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		return syncErr
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return closeErr
	}
	if renameErr := os.Rename(tmpPath, path); renameErr != nil {
		return renameErr
	}
	ok = true
	return nil
}
