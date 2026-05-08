package scenario

import "path/filepath"

// EventLogRelPath is the path of the per-scenario event-log JSONL file
// relative to the synthetic project root per specs/scenario-harness.md §SH-014.
//
// The daemon's startup sequence writes its event log at this path relative to
// its working directory (the per-scenario synthetic project root per SH-016a),
// so the file lands inside the synthetic root by construction — no
// path-override surface is mutated in production code.
const EventLogRelPath = ".harmonik/events/events.jsonl"

// EventLogPath returns the absolute path to the per-scenario event-log JSONL
// file given the absolute path to the scenario's synthetic project root.
//
// The result is always projectRoot joined with EventLogRelPath; callers MUST
// NOT read from or write to the operator's own .harmonik/events/events.jsonl
// (the .harmonik/ directory under git rev-parse --show-toplevel) — doing so
// violates SH-014.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-014.
func EventLogPath(projectRoot string) string {
	return filepath.Join(projectRoot, EventLogRelPath)
}

// EventLogDir returns the absolute path to the directory that contains the
// per-scenario event-log JSONL file given the absolute path to the scenario's
// synthetic project root.
//
// Harness fixture setup MUST create this directory (with os.MkdirAll) before
// starting the per-scenario daemon so the daemon can write its event log on
// first open.
//
// Spec ref: specs/scenario-harness.md §4.4 SH-014 and §4.4 SH-016a.
func EventLogDir(projectRoot string) string {
	return filepath.Dir(EventLogPath(projectRoot))
}
