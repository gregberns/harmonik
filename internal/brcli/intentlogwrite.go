package brcli

// intentlogwrite.go — BI-030 step 1: write IntentLogEntry to a temp file.
//
// This file implements only Step 1 of the BI-030 multi-step atomic write
// protocol: encode the IntentLogEntry as JSON and write it to a temp file
// named "<encoded_key>.json.tmp-<rand>" in the intent-log directory.
//
// Steps 2–6 (fsync(temp_fd), rename, fsync(parent_dir), br invocation,
// unlink + fsync(parent_dir)) are addressed by follow-up beads
// hk-872.37.2 through hk-872.37.6.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 step 1; §6.1 RECORD
// IntentLogEntry; §6.2 on-disk layout.

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// intentLogEntryWire is the on-disk JSON shape for an IntentLogEntry.
// It mirrors core.IntentLogEntry using the snake_case field names from
// the spec's RECORD definition (specs/beads-integration.md §6.1).
//
// The core.IntentLogEntry struct carries no JSON tags; this separate wire
// type ensures the on-disk representation is spec-compliant regardless of
// any future changes to the Go struct's exported field names.
type intentLogEntryWire struct {
	IdempotencyKey    string    `json:"idempotency_key"`
	RunID             string    `json:"run_id"`
	TransitionID      string    `json:"transition_id"`
	Op                string    `json:"op"`
	BeadID            string    `json:"bead_id"`
	IntendedPostState string    `json:"intended_post_state"`
	RequestedAt       time.Time `json:"requested_at"`
	SchemaVersion     int       `json:"schema_version"`
}

// WriteIntentLogTmp encodes entry as JSON and writes it to a temp file in dir
// following BI-030 step 1.
//
// The temp file is named:
//
//	<encoded_key>.json.tmp-<rand>
//
// where <encoded_key> is entry.IdempotencyKey with colons replaced by
// underscores (filesystem-portability per specs/beads-integration.md §6.2
// OQ-BI-003: colons are permitted on Linux but forbidden on macOS HFS+), and
// <rand> is 8 cryptographically random lowercase hex characters (crypto/rand).
//
// The temp file is created with mode 0600 via O_CREATE|O_EXCL; the exclusive
// flag guards against accidental collision on the random suffix.
//
// On success, WriteIntentLogTmp returns the absolute path of the written temp
// file. The file is NOT fsynced and NOT renamed here — those are BI-030
// steps 2 and 3, addressed by hk-872.37.2 and hk-872.37.3 respectively.
//
// Returns an error (non-nil tmpPath = "") on any of: invalid entry, random
// suffix generation failure, JSON encoding failure, O_EXCL open failure, or
// write failure.
//
// Spec ref: specs/beads-integration.md §4.10 BI-030 step 1; §6.2.
func WriteIntentLogTmp(dir string, entry core.IntentLogEntry) (tmpPath string, err error) {
	if !entry.Valid() {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: entry is invalid: %+v", entry)
	}

	wire := intentLogEntryWire{
		IdempotencyKey:    entry.IdempotencyKey,
		RunID:             entry.RunID.String(),
		TransitionID:      entry.TransitionID.String(),
		Op:                string(entry.Op),
		BeadID:            string(entry.BeadID),
		IntendedPostState: string(entry.IntendedPostState),
		RequestedAt:       entry.RequestedAt,
		SchemaVersion:     entry.SchemaVersion,
	}

	data, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: json.Marshal: %w", err)
	}

	// Encode colons in the idempotency key to underscores for filesystem
	// portability (OQ-BI-003 — colons are forbidden on macOS HFS+).
	encodedKey := strings.ReplaceAll(entry.IdempotencyKey, ":", "_")

	randSuffix, err := intentLogRandHex(8)
	if err != nil {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: random suffix: %w", err)
	}

	name := encodedKey + ".json.tmp-" + randSuffix
	path := filepath.Join(dir, name)

	//nolint:gosec // G304: dir is the adapter-owned intent-log directory (.harmonik/beads-intents/), not user input
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: create temp file %q: %w", path, err)
	}

	if _, err := f.Write(data); err != nil {
		defer func() { _ = f.Close() }()
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: write temp file %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("brcli.WriteIntentLogTmp: close temp file %q: %w", path, err)
	}

	return path, nil
}

// intentLogRandHex returns n cryptographically random lowercase hex characters
// using crypto/rand. Used to generate the random suffix of intent-log temp
// files (BI-030: "random suffix prevents collision under concurrent recovery").
func intentLogRandHex(n int) (string, error) {
	const hexChars = "0123456789abcdef"
	out := make([]byte, n)
	for i := range out {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(hexChars))))
		if err != nil {
			return "", err
		}
		out[i] = hexChars[idx.Int64()]
	}
	return string(out), nil
}
