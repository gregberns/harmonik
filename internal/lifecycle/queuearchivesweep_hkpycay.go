package lifecycle

// queuearchivesweep_hkpycay.go — startup sweep of accumulated queue.json
// archive files under .harmonik/.
//
// Per Gap-4 of the recovery audit: archive files
// (queue.json.failed-*, queue.json.cancelled-*, queue.json.crashed-*,
// queue.json.no-work-*, queue.json.silent-heartbeat-*, queue.json.pane-misdirect-*,
// queue.json.panicked-*, queue.json.v49-stuck-*, queue.json.parallel-fail-*, etc.)
// accumulate indefinitely.  This sweep keeps the newest N archives per category
// (default 5) and removes older ones.  N is configurable via the
// HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT environment variable or the KeepCount field
// on SweepQueueArchivesConfig.
//
// Bead ref: hk-pycay.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// defaultQueueArchiveKeepCount is the number of archive files to retain per
// category when no explicit override is provided.
const defaultQueueArchiveKeepCount = 5

// queueArchiveEnvVar is the environment variable that overrides KeepCount.
const queueArchiveEnvVar = "HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT"

// queueFileName is duplicated from internal/queue to avoid a circular import.
// The queue package is leaf-level; this package is also leaf-level. We avoid
// importing queue here to keep the cycle-free graph.
const queueArchivePrefix = "queue.json."

// SweepQueueArchivesConfig carries configuration for SweepQueueArchives.
type SweepQueueArchivesConfig struct {
	// KeepCount is the number of archive files to retain per category.
	// 0 → defaultQueueArchiveKeepCount (5).
	// The env var HARMONIK_QUEUE_ARCHIVE_KEEP_COUNT overrides this field when
	// non-zero; the field takes precedence when both are set.
	KeepCount int

	// Logger receives diagnostic messages. Nil → silent.
	Logger *log.Logger
}

// SweepQueueArchivesResult reports the outcome of a SweepQueueArchives call.
type SweepQueueArchivesResult struct {
	// Deleted is the count of archive files removed.
	Deleted int

	// Retained is the count of archive files kept (newest N per category).
	Retained int
}

// SweepQueueArchives scans .harmonik/ for queue.json archive files, groups
// them by category (the portion of the filename between "queue.json." and the
// first non-letter-or-dash character following the category word), sorts each
// group by filename (which is lexicographically equivalent to creation-time
// order for the timestamp and counter suffixes used by all known archive
// writers), retains the newest KeepCount, and deletes the rest.
//
// Archive file naming convention observed in production:
//
//	queue.json.cancelled-<yyyymmddHHMMSS>
//	queue.json.failed-<yyyymmddHHMMSS>
//	queue.json.claude-crashed-<date>
//	queue.json.no-work-<counter>
//	queue.json.pane-misdirect-<date>
//	queue.json.panicked-<yyyymmddHHMMSS>
//	queue.json.parallel-fail-<date>
//	queue.json.silent-after-runstarted-<date>
//	queue.json.silent-heartbeat-<yyyymmddHHMMSS>
//	queue.json.v49-stuck-<date>
//
// The category is the word(s) before the last hyphen-separated suffix, e.g.
// "cancelled", "failed", "no-work", "silent-heartbeat", etc.  Two files share
// a category when their names differ only in the trailing timestamp/counter.
//
// Returns (zero, nil) if the .harmonik/ directory does not exist or contains
// no archive files.  Non-fatal per-file removal errors are logged and counted
// against Deleted (they reduce it), but do not cause the function to return a
// non-nil error unless ALL removals fail — callers SHOULD log the error but
// not abort startup.
//
// Bead ref: hk-pycay.
func SweepQueueArchives(projectDir string, cfg SweepQueueArchivesConfig) (SweepQueueArchivesResult, error) {
	keep := resolveKeepCount(cfg)

	hDir := filepath.Join(projectDir, ".harmonik")
	entries, err := os.ReadDir(hDir)
	if err != nil {
		if os.IsNotExist(err) {
			return SweepQueueArchivesResult{}, nil
		}
		return SweepQueueArchivesResult{}, fmt.Errorf("lifecycle: SweepQueueArchives: ReadDir %q: %w", hDir, err)
	}

	// Collect archive file names grouped by category.
	// Map: category → sorted list of file names (ascending = oldest first).
	byCategory := make(map[string][]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, queueArchivePrefix) {
			continue
		}
		// The category is the tail after "queue.json." with the trailing
		// timestamp/counter stripped.  We detect the category by walking
		// backwards through the hyphen-delimited fields and dropping the last
		// segment if it looks like a pure-numeric or date-like token.
		cat := archiveCategory(name)
		if cat == "" {
			continue
		}
		byCategory[cat] = append(byCategory[cat], name)
	}

	var result SweepQueueArchivesResult
	var lastErr error

	for cat, names := range byCategory {
		// Sort ascending so names[0] is the oldest.
		sort.Strings(names)

		if len(names) <= keep {
			result.Retained += len(names)
			continue
		}

		// Delete the oldest (len - keep) files; retain the newest keep files.
		toDelete := names[:len(names)-keep]
		toKeep := names[len(names)-keep:]
		result.Retained += len(toKeep)

		for _, name := range toDelete {
			path := filepath.Join(hDir, name)
			//nolint:gosec // G304: path constructed from projectDir + .harmonik/ + archive filename, not user input
			if removeErr := os.Remove(path); removeErr != nil {
				orphanLog(cfg.Logger, "SweepQueueArchives: remove %q (category %q): %v", name, cat, removeErr)
				lastErr = removeErr
				continue
			}
			orphanLog(cfg.Logger, "SweepQueueArchives: deleted old archive %q (category %q)", name, cat)
			result.Deleted++
		}
	}

	if lastErr != nil {
		return result, fmt.Errorf("lifecycle: SweepQueueArchives: some removals failed (last: %w)", lastErr)
	}
	return result, nil
}

// resolveKeepCount returns the effective keep-count for the sweep.
// Priority: cfg.KeepCount > env var > default.
func resolveKeepCount(cfg SweepQueueArchivesConfig) int {
	if cfg.KeepCount > 0 {
		return cfg.KeepCount
	}
	if envVal := os.Getenv(queueArchiveEnvVar); envVal != "" {
		if n, err := strconv.Atoi(envVal); err == nil && n > 0 {
			return n
		}
	}
	return defaultQueueArchiveKeepCount
}

// archiveCategory extracts the category prefix from an archive filename.
//
// E.g.:
//
//	"queue.json.cancelled-20260518223853" → "cancelled"
//	"queue.json.failed-20260519161437"    → "failed"
//	"queue.json.no-work-164935"           → "no-work"
//	"queue.json.silent-heartbeat-20260519155547" → "silent-heartbeat"
//	"queue.json.claude-crashed-2026-05-20"       → "claude-crashed"
//	"queue.json.v49-stuck-20260519001647"         → "v49-stuck"
//
// The rule: strip the "queue.json." prefix, then drop the last hyphen-delimited
// field.  A file with no hyphen after the prefix (e.g. a hypothetical bare
// "queue.json.backup") is treated as its own singleton category using the full
// remainder as the category name.
//
// Returns "" for names that don't match the expected shape (no prefix, or the
// remainder is empty).
func archiveCategory(name string) string {
	remainder := strings.TrimPrefix(name, queueArchivePrefix)
	if remainder == "" || remainder == name {
		return ""
	}
	// Drop the last hyphen-delimited token (the timestamp/counter suffix).
	idx := strings.LastIndex(remainder, "-")
	if idx <= 0 {
		// No hyphen or hyphen at position 0 — treat entire remainder as the category.
		return remainder
	}
	return remainder[:idx]
}
