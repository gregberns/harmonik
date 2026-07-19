// Package handler — daemon opt-in for the WS3-Claude-A wire-tap seam.
//
// The Watcher exposes an optional WireTap io.Writer (SpawnWatcherConfig.WireTap,
// internal/handlercontract/watcher_hc011.go): when non-nil the NDJSON read-loop
// tees every consumed byte to it before decode; when nil the read-loop is
// byte-identical to the pre-tap watcher (production untouched).
//
// This file is the DAEMON opt-in the seam left as a follow-up: when the
// twin-parity capture harness sets HARMONIK_WIRE_CAPTURE_DIR, Handler.Launch
// opens <dir>/<scn>/wire.ndjson and points WireTap at it. When the env var is
// unset (the default) WireTap stays nil → no-op. The path layout mirrors what
// the capture harness reads back (internal/daemon/e2e_real_claude_capture_test.go)
// and the `capture-claude-fixtures` Makefile target: <scn> is HARMONIK_CAPTURE_SCN
// (default "happy-path"), matching filepath.Join(outRoot, scn) there.
package handler

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvWireCaptureDir names the directory the daemon writes a raw wire capture to.
// Set only by the twin-parity capture harness / `make capture-claude-fixtures`.
// Unset (the default) → WireTap nil → byte-identical no-op.
const EnvWireCaptureDir = "HARMONIK_WIRE_CAPTURE_DIR"

// EnvWireCaptureScn names the capture scenario subdirectory under
// EnvWireCaptureDir. Mirrors HARMONIK_CAPTURE_SCN in the capture harness.
const EnvWireCaptureScn = "HARMONIK_CAPTURE_SCN"

// DefaultWireCaptureScn is the scenario subdir used when EnvWireCaptureScn is
// unset — matches the harness default (internal/daemon/e2e_real_claude_capture_test.go).
const DefaultWireCaptureScn = "happy-path"

// openWireTap returns an open *os.File to use as SpawnWatcherConfig.WireTap when
// EnvWireCaptureDir is set, or (nil, nil) when it is unset (the production
// default). The caller MUST close the returned file once the watcher's Done
// channel closes, so the capture is flushed and no fd leaks.
//
// The file lands at <EnvWireCaptureDir>/<scn>/wire.ndjson where <scn> is
// EnvWireCaptureScn (default DefaultWireCaptureScn). This is exactly the path
// the capture harness stats and reads back.
func openWireTap() (*os.File, error) {
	dir := os.Getenv(EnvWireCaptureDir)
	if dir == "" {
		return nil, nil
	}
	scn := os.Getenv(EnvWireCaptureScn)
	if scn == "" {
		scn = DefaultWireCaptureScn
	}
	captureDir := filepath.Join(dir, scn)
	if err := os.MkdirAll(captureDir, 0o755); err != nil { //nolint:gosec // committed fixture dir must be world-readable
		return nil, fmt.Errorf("handler: wire capture: mkdir %s: %w", captureDir, err)
	}
	path := filepath.Join(captureDir, "wire.ndjson")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec // committed fixture file must be world-readable
	if err != nil {
		return nil, fmt.Errorf("handler: wire capture: open %s: %w", path, err)
	}
	return f, nil
}
