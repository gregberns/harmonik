package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
)

const followUpLedgerFileName = "follow-up-ledger.jsonl"

type followUpLedgerEntry struct {
	K string `json:"k"`
}

func loadFollowUpLedger(path string) (map[string]struct{}, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]struct{}), nil
		}
		return nil, fmt.Errorf("loadFollowUpLedger: open %s: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "loadFollowUpLedger: close ledger", "err", closeErr, "path", path)
		}
	}()

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
