package keeper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CtxFile is the JSON content written by scripts/keeper-statusline.sh to
// .harmonik/keeper/<agent>.ctx on every statusLine update.
type CtxFile struct {
	Pct       float64 `json:"pct"`
	SessionID string  `json:"session_id"`
	Ts        string  `json:"ts"` // RFC 3339
}

// ctxFilePath returns the absolute path to <projectDir>/.harmonik/keeper/<agent>.ctx.
func ctxFilePath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".ctx")
}

// ReadCtxFile reads and parses the gauge file for the given agent. It returns
// (file, modTime, nil) on success. Returns (nil, zero, os.ErrNotExist) when
// the file is absent, or (nil, zero, err) for other I/O errors.
func ReadCtxFile(projectDir, agent string) (*CtxFile, time.Time, error) {
	path := ctxFilePath(projectDir, agent)
	//nolint:gosec // G304: path derived from operator-controlled projectDir and agent validated by AcquireLock
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("keeper: stat ctx file %q: %w", path, err)
	}
	var cf CtxFile
	if err := json.Unmarshal(raw, &cf); err != nil {
		return nil, time.Time{}, fmt.Errorf("keeper: parse ctx file %q: %w", path, err)
	}
	return &cf, stat.ModTime(), nil
}
