package queue

// failedarchivelayout.go — the ONE definition of where a failed-queue archive
// lives on disk, and the only supported way to find one.
//
// Tags: mechanism
// (ZFC per specs/architecture.md §4.2 AR-005/AR-006: this file does file I/O
// and string manipulation on a fixed layout. No judgment, no LLM.)
//
// # Why this file exists
//
// The archive layout used to be spelled out separately in each place that
// touched it. The writer built the name by string concatenation. Two daemon
// readers globbed `.harmonik/queues/*.json.failed-*`. A third reader — the
// boot-time archive sweep in internal/lifecycle — looked for files named
// `queue.json.failed-*` directly under `.harmonik/`. That third spelling has
// never matched a real archive. On a live store, 68 files matched the two
// agreeing readers and 0 matched the third. The sweep reported success and
// removed nothing for as long as it existed.
//
// One writer plus three hand-written readers is what made that possible. Every
// reader now derives its paths from [FailedArchiveInfix] and
// [ListFailedArchives], and [ArchiveFailedQueue] writes through the same
// constant, so a reader cannot drift from the writer without failing to
// compile or failing the layout-agreement test.
//
// # The layout
//
//	.harmonik/queues/<queue-name>.json.failed-<yyyymmddHHMMSS>
//
// The timestamp is UTC and is written by [ArchiveFailedQueue]. It sorts
// lexicographically in creation order, but callers that need age SHOULD stat
// the file rather than parse the name: the name records when the archive was
// asked for, the mtime records when it landed.
//
// # What an archive means — read this before you write another reader
//
// An archive is NOT a paused queue that someone can resume. Writing one is an
// EVACUATION: [ArchiveFailedQueue] renames the live queue file out of the way
// so the next `harmonik run` sees no active queue. After the rename the queue
// is gone from the daemon's view. Nothing in this repo reads an archive back
// into a live queue. The three readers only count archives as evidence that
// somebody's work stopped and was never picked back up.
//
// The in-place resume path is a different mechanism on a different precondition
// (the queue file still present, Status paused-by-failure). See
// docs/failed-queue-paths.md for the trace of both and for the one place where
// they collide.

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FailedArchiveInfix is the literal that [ArchiveFailedQueue] puts between a
// queue file name and the archive timestamp. It is the single source of truth
// for the archive layout: the writer builds names with it and every reader
// matches with it.
//
// Note the name says "failed" but the operator cancel path
// ([HandlerAdapter.HandleQueueCancel]) also archives under this infix, so a
// `.failed-` archive means "this queue stopped", not "this queue failed".
const FailedArchiveInfix = ".failed-"

const failedArchiveGlobPattern = "*.json" + FailedArchiveInfix + "*"

// FailedArchiveDir returns the directory that holds failed-queue archives for
// projectDir. Archives live beside the live queue files, one directory below
// .harmonik/ — NOT in .harmonik/ itself.
func FailedArchiveDir(projectDir string) string {
	return queuesDir(projectDir)
}

// FailedArchiveGlob returns the glob pattern that matches every failed-queue
// archive under projectDir. Use [ListFailedArchives] unless you need the
// pattern itself for an error message.
func FailedArchiveGlob(projectDir string) string {
	return filepath.Join(FailedArchiveDir(projectDir), failedArchiveGlobPattern)
}

// ListFailedArchives returns the paths of every failed-queue archive under
// projectDir, in the order filepath.Glob returns them (lexical). A missing
// .harmonik/queues/ directory yields an empty slice and a nil error: no
// archives is a valid state, not a fault.
//
// This is the ONLY supported way to enumerate archives. Do not re-glob by
// hand — see the file header for what that cost the last time.
func ListFailedArchives(projectDir string) ([]string, error) {
	pattern := FailedArchiveGlob(projectDir)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("queue: ListFailedArchives: glob %q: %w", pattern, err)
	}
	return matches, nil
}

// FailedArchiveQueueName returns the queue name an archive path came from, or
// "" when path is not a failed-queue archive name.
//
//	main.json.failed-20260519161437 → "main"
//	crew-paul.json.failed-2026…     → "crew-paul"
func FailedArchiveQueueName(path string) string {
	base := filepath.Base(path)
	idx := strings.Index(base, ".json"+FailedArchiveInfix)
	if idx <= 0 {
		return ""
	}
	return base[:idx]
}
