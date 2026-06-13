// Package release — last-good binary state tracking.
//
// The last-good binary protocol is part of the ROLLBACK stage (stage 4) of
// the release pipeline. The supervisor pins the path to the last known-good
// harmonik binary in a state file; on yanked-binary detection or crash-loop
// the supervisor restores from that pinned path.
//
// State file path: <projectDir>/.harmonik/state/last-good-binary
// Binary snapshot path: <original-binary>.last-good
//
// Spec ref: specs/release-pipeline.md §7.2, specs/operator-nfr.md §4(d).1.
package release

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LastGoodStatePath returns the per-project state file path for the last-good
// binary record: <projectDir>/.harmonik/state/last-good-binary.
//
// Spec ref: specs/release-pipeline.md §7.2; specs/operator-nfr.md §4(d).1
// (ON-058c) — per-project path replaces the pre-1.0 machine-global
// /tmp/hk-last-good-binary. Absent file on first read is a fresh start with
// no migration from the old /tmp path.
func LastGoodStatePath(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "state", "last-good-binary")
}

// ErrNoLastGood is returned when the last-good state file does not exist or
// contains no path.
var ErrNoLastGood = errors.New("release: no last-good binary recorded")

// ReadLastGoodBinary reads the last-good binary path from statePath.
// Returns ErrNoLastGood when the state file does not exist or is empty.
func ReadLastGoodBinary(statePath string) (string, error) {
	data, err := os.ReadFile(statePath) //nolint:gosec // operator-supplied path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNoLastGood
		}
		return "", fmt.Errorf("release: read last-good state %s: %w", statePath, err)
	}
	path := strings.TrimSpace(string(data))
	if path == "" {
		return "", ErrNoLastGood
	}
	return path, nil
}

// WriteLastGoodBinary writes binPath to statePath atomically (write + rename).
func WriteLastGoodBinary(statePath, binPath string) error {
	dir := filepath.Dir(statePath)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: matches .harmonik dir conventions
		return fmt.Errorf("release: mkdir last-good dir %s: %w", dir, err)
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(binPath+"\n"), 0o644); err != nil { //nolint:gosec // G306: state file is not a secret
		return fmt.Errorf("release: write last-good state: %w", err)
	}
	if err := os.Rename(tmp, statePath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("release: rename last-good state: %w", err)
	}
	return nil
}

// PinLastGoodBinary copies srcBin to srcBin+".last-good" and records the
// snapshot path in statePath. Called by the supervisor after the daemon has
// been confirmed healthy for the required window.
//
// Spec ref: specs/release-pipeline.md §7.2 — "snapshot it to $BIN.last-good".
func PinLastGoodBinary(statePath, srcBin string) error {
	dst := srcBin + ".last-good"
	if err := copyBinary(srcBin, dst); err != nil {
		return fmt.Errorf("release: pin last-good binary: %w", err)
	}
	if err := WriteLastGoodBinary(statePath, dst); err != nil {
		return fmt.Errorf("release: update last-good state after pin: %w", err)
	}
	return nil
}

// RestoreLastGoodBinary reads the last-good binary path from statePath and
// copies it to dstBin, making it the active binary. Returns ErrNoLastGood
// when no last-good binary has been pinned yet.
//
// Spec ref: specs/release-pipeline.md §7.2 — "on rollback restore it".
func RestoreLastGoodBinary(statePath, dstBin string) error {
	src, err := ReadLastGoodBinary(statePath)
	if err != nil {
		return err
	}
	if err := copyBinary(src, dstBin); err != nil {
		return fmt.Errorf("release: restore last-good to %s: %w", dstBin, err)
	}
	return nil
}

// copyBinary copies src to dst atomically (tmp + rename), preserving the
// source file's permission bits.
func copyBinary(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // G304: operator-supplied path
	if err != nil {
		return fmt.Errorf("open src %s: %w", src, err)
	}
	defer in.Close()

	st, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat src %s: %w", src, err)
	}

	tmp := dst + ".tmp"
	//nolint:gosec // G306: mode comes from source file stat
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode())
	if err != nil {
		return fmt.Errorf("create dst tmp %s: %w", tmp, err)
	}
	success := false
	defer func() {
		_ = out.Close()
		if !success {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy body: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync dst: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close dst tmp: %w", err)
	}
	success = true

	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename %s → %s: %w", tmp, dst, err)
	}
	return nil
}
