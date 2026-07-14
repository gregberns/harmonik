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
//
// Tokens and WindowSize are populated by keeper-statusline.sh from
// .context_window.total_input_tokens and .context_window_size respectively.
// They default to 0 on older Claude Code versions that do not emit those fields;
// keeper logic falls back to Pct-based gating when either is zero.
type CtxFile struct {
	Pct        float64 `json:"pct"`
	Tokens     int64   `json:"tokens,omitempty"`
	WindowSize int64   `json:"window_size,omitempty"`
	SessionID  string  `json:"session_id"`
	Ts         string  `json:"ts"` // RFC 3339
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
	// PRIMARY identity source (hk-8prq): the SessionStart hook writes the live
	// session_id to the single-writer <agent>.sid channel the daemon never
	// touches. When present and well-formed it OVERRIDES the gauge's session_id
	// — the gauge remains the source for pct/tokens, but .sid is authoritative
	// for identity, removing the multi-writer ambiguity that the watcher's
	// latch/UUIDv7/uppercase heuristics work around. When .sid is absent or
	// malformed, cf.SessionID keeps the gauge value (the FALLBACK path; the
	// heuristics still apply). Centralising the override here means BOTH the
	// watcher loop AND the cycler's clear-settle detection poll (which read through this
	// function / the default ReadGaugeFn) pick up the authoritative id.
	if sid, _, sidErr := ReadSessionIDFile(projectDir, agent); sidErr == nil && isPrimarySID(sid) {
		cf.SessionID = sid
	}
	return &cf, stat.ModTime(), nil
}
