package main

// beadsdedup.go — `harmonik beads-dedup` subcommand implementation.
//
// Deduplicates .beads/issues.jsonl in-place, keeping the record with the
// newest updated_at timestamp for each bead ID.  This is the one-time fix for
// ghost "open" beads left by older-open + newer-closed duplicate records that
// caused br show / br list to over-report open work.
//
// Bead ref: hk-0f35x.

import (
	"fmt"
	"os"
	"sort"
)

// beadsDedupUsage prints the help text for `harmonik beads-dedup`.
func beadsDedupUsage() {
	fmt.Print(`harmonik beads-dedup — deduplicate .beads/issues.jsonl in-place

USAGE
  harmonik beads-dedup [--path FILE] [--dry-run]

FLAGS
  --path FILE  Path to issues.jsonl (default: .beads/issues.jsonl)
  --dry-run    Report duplicate count without modifying the file

NOTES
  Keeps the record with the newest updated_at timestamp for each bead ID.
  When two records share the same updated_at, the one appearing later in the
  file is kept (same tiebreaker as the git merge-driver).
  Output is sorted by bead ID (same ordering as beads-merge).

EXIT CODES
  0  Success (or dry-run regardless of dup count)
  1  Argument or file error

EXAMPLES
  harmonik beads-dedup
  harmonik beads-dedup --path /path/to/.beads/issues.jsonl
  harmonik beads-dedup --dry-run
`)
}

// runBeadsDedupSubcommand implements `harmonik beads-dedup`.
// subArgs is os.Args[2:] (everything after "beads-dedup").
func runBeadsDedupSubcommand(subArgs []string) int {
	path := ".beads/issues.jsonl"
	dryRun := false

	for i := 0; i < len(subArgs); i++ {
		switch subArgs[i] {
		case "--help", "-h":
			beadsDedupUsage()
			return 0
		case "--dry-run":
			dryRun = true
		case "--path":
			if i+1 >= len(subArgs) {
				fmt.Fprintln(os.Stderr, "harmonik beads-dedup: --path requires an argument")
				return 1
			}
			i++
			path = subArgs[i]
		default:
			fmt.Fprintf(os.Stderr, "harmonik beads-dedup: unknown flag %q\n", subArgs[i])
			return 1
		}
	}

	//nolint:gosec // G304: path is operator-supplied or default project-relative path
	rows, err := parseBeadsJSONL(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-dedup: read %s: %v\n", path, err)
		return 1
	}
	if len(rows) == 0 {
		return 0
	}

	deduplicated := deduplicateBeadRows(rows)
	dupCount := len(rows) - len(deduplicated)

	if dryRun {
		fmt.Printf("harmonik beads-dedup: %d duplicate record(s) found in %s\n", dupCount, path)
		return 0
	}

	if writeErr := writeBeadsJSONL(path, deduplicated); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik beads-dedup: write %s: %v\n", path, writeErr)
		return 1
	}

	if dupCount > 0 {
		fmt.Printf("harmonik beads-dedup: removed %d duplicate record(s) from %s\n", dupCount, path)
	}
	return 0
}

// deduplicateBeadRows returns a deduplicated slice keeping the record with the
// newest updated_at for each bead ID.  When two records share the same
// updated_at, the later one in the input slice wins (file-order tiebreaker,
// consistent with rowsToMap).  Output is sorted by bead ID.
func deduplicateBeadRows(rows []beadRow) []beadRow {
	best := make(map[string]beadRow, len(rows))
	for _, r := range rows {
		existing, seen := best[r.id]
		if !seen || r.updatedAt.After(existing.updatedAt) {
			best[r.id] = r
		}
	}

	ids := make([]string, 0, len(best))
	for id := range best {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	result := make([]beadRow, 0, len(ids))
	for _, id := range ids {
		result = append(result, best[id])
	}
	return result
}
