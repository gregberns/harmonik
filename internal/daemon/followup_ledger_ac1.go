package daemon

// followup_ledger_ac1.go — durable at-most-once ledger for the staged-bead
// generator (flywheel-motion.md §5.4 B guardrail 4).
//
// The in-memory followUpLedger (workLoopDeps.followUpLedger) prevents
// duplicate follow-up beads within a single daemon session, but is re-made on
// restart (workloop.go). This file provides load/append helpers so the ledger
// is persisted to .harmonik/follow-up-ledger.jsonl and re-seeded at boot,
// making the at-most-once guarantee durable across daemon restarts.
//
// File format: one JSON object per line — {"k":"<beadID>:<class>"}.
// Malformed or empty-key lines are skipped silently on load.
//
// Bead ref: hk-3ndb (AC1 — durable staged-bead ledger).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

const followUpLedgerFileName = "follow-up-ledger.jsonl"

// followUpLedgerEntry is the on-disk JSON format for one ledger record.
type followUpLedgerEntry struct {
	K string `json:"k"`
}

// loadFollowUpLedger reads the JSONL ledger at path and returns the set of
// already-seen keys (format: "<beadID>:<class>"). A missing file is not an
// error — it is treated as an empty ledger.
func loadFollowUpLedger(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]struct{}), nil
		}
		return nil, fmt.Errorf("loadFollowUpLedger: open %s: %w", path, err)
	}
	defer f.Close()

	result := make(map[string]struct{})
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		b := scanner.Bytes()
		if len(b) == 0 {
			continue
		}
		var entry followUpLedgerEntry
		if jsonErr := json.Unmarshal(b, &entry); jsonErr != nil || entry.K == "" {
			continue // skip malformed or empty-key lines
		}
		result[entry.K] = struct{}{}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return result, fmt.Errorf("loadFollowUpLedger: scan %s: %w", path, scanErr)
	}
	return result, nil
}

// appendFollowUpLedger appends a single key entry to the JSONL ledger at
// path, creating the file if necessary. Failures are non-fatal to the caller
// but should be logged.
func appendFollowUpLedger(path, key string) error {
	data, err := json.Marshal(followUpLedgerEntry{K: key})
	if err != nil {
		return fmt.Errorf("appendFollowUpLedger: marshal: %w", err)
	}
	data = append(data, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("appendFollowUpLedger: open %s: %w", path, err)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("appendFollowUpLedger: write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("appendFollowUpLedger: close %s: %w", path, closeErr)
	}
	return nil
}
