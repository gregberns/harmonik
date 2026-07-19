package twinparity

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

// CanonEvent is a canonicalized event record: a stable Kind plus a whitelisted
// set of stable payload fields. Volatile fields (ids, timestamps, absolute
// paths, pids) are dropped during canonicalization so two equivalent streams
// compare equal regardless of run-specific noise.
type CanonEvent struct {
	// Kind is the event type, extracted via the dual-field rule (see
	// kindOf): the envelope "event_type" if present, else the embedded raw
	// payload "type".
	Kind string
	// Fields is the whitelisted, scrubbed set of stable payload fields for
	// this Kind. Never nil (empty for kind-only records).
	Fields map[string]string
	// RawSeq is the 0-based position of this record in its source stream.
	RawSeq int
	// elapsed is the envelope-timestamp offset relative to the stream's first
	// record. Retained for timing assertions ONLY — equivalence never compares
	// it. Unexported so it cannot leak into equality comparisons.
	elapsed time.Duration
}

// Stream is an ordered sequence of canonicalized events.
type Stream struct {
	Events []CanonEvent
}

// ─────────────────────────────────────────────────────────────────────────────
// Dual-field kind extraction (load-bearing)
// ─────────────────────────────────────────────────────────────────────────────

// kindOf extracts the event kind using the dual-field rule that mirrors
// test/scenario/harness_test.go:302-311: prefer the envelope "event_type"
// field; fall back to the embedded raw-payload "type" field when "event_type"
// is absent or empty. The daemon publishes with an "event_type" envelope while
// the raw wire line carries only "type"; missing this fallback silently reads
// empty kinds off raw wire lines.
func kindOf(obj map[string]json.RawMessage) string {
	var et string
	if raw, ok := obj["event_type"]; ok {
		if err := json.Unmarshal(raw, &et); err != nil {
			et = ""
		}
	}
	if et == "" {
		if raw, ok := obj["type"]; ok {
			if err := json.Unmarshal(raw, &et); err != nil {
				et = ""
			}
		}
	}
	return et
}

// ─────────────────────────────────────────────────────────────────────────────
// Volatile-field policy
// ─────────────────────────────────────────────────────────────────────────────

// volatileFields are dropped wholesale during canonicalization: identity/run
// correlators, timestamps, and absolute-path carriers that vary per run.
var volatileFields = map[string]struct{}{
	"event_id":          {},
	"timestamp":         {},
	"emitted_at":        {},
	"transitioned_at":   {},
	"source_subsystem":  {},
	"run_id":            {},
	"session_id":        {},
	"claude_session_id": {},
	"pid":               {},
	"bead_id":           {},
	"worktree_path":     {},
	"codex_home":        {},
	// The kind carriers themselves are consumed into Kind, not retained.
	"event_type": {},
	"type":       {},
}

// isVolatileKey reports whether a field key is volatile and must be dropped:
// the explicit set above, any *_at timestamp field, or any session_log_* path
// carrier.
func isVolatileKey(k string) bool {
	if _, ok := volatileFields[k]; ok {
		return true
	}
	if strings.HasSuffix(k, "_at") {
		return true
	}
	if strings.HasPrefix(k, "session_log_") {
		return true
	}
	return false
}

// Scrub regexes for free-text values retained in the whitelist. UUIDs, PIDs,
// and absolute paths are replaced with stable placeholders so run-specific
// noise embedded in an otherwise-stable field does not defeat equality.
var (
	uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	pathRe = regexp.MustCompile(`/(?:[^\s/]+/)+[^\s/]+`)
	pidRe  = regexp.MustCompile(`\bpid[=:]\s*\d+\b`)
)

// scrubValue replaces UUID / PID / absolute-path substrings in a retained
// free-text value with stable placeholders.
func scrubValue(v string) string {
	v = uuidRe.ReplaceAllString(v, "<uuid>")
	v = pidRe.ReplaceAllString(v, "pid=<pid>")
	v = pathRe.ReplaceAllString(v, "<path>")
	return v
}

// ─────────────────────────────────────────────────────────────────────────────
// Stable payload whitelist (start minimal per WS3-F1)
// ─────────────────────────────────────────────────────────────────────────────

// stablePayloadFields is the per-kind whitelist of payload fields retained
// after volatile-field dropping. Kinds absent from this map are kind-only
// (no retained payload). Intentionally minimal — extend deliberately.
var stablePayloadFields = map[string][]string{
	"outcome_emitted":  {"outcome_status", "node_id"},
	"agent_completed":  {"exit_code"},
	"reviewer_verdict": {"verdict"},
	"hook_fired":       {"hook_type"}, // retained only when present
}

// rawValueToString renders a raw JSON value as a stable string. String values
// are returned unquoted; everything else falls back to its compact JSON form.
func rawValueToString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

// canonRecord canonicalizes a single decoded JSONL object into a CanonEvent.
// seq is the 0-based stream position; firstTS is the envelope timestamp of the
// stream's first record (zero if unknown) used to derive elapsed.
func canonRecord(obj map[string]json.RawMessage, seq int, firstTS time.Time) CanonEvent {
	ev := CanonEvent{
		Kind:   kindOf(obj),
		Fields: map[string]string{},
		RawSeq: seq,
	}

	// Retain the envelope timestamp into elapsed (timing-only).
	if ts, ok := recordTimestamp(obj); ok && !firstTS.IsZero() {
		ev.elapsed = ts.Sub(firstTS)
	}

	whitelist, hasWhitelist := stablePayloadFields[ev.Kind]
	if !hasWhitelist {
		return ev // kind-only
	}
	for _, field := range whitelist {
		raw, ok := obj[field]
		if !ok {
			continue // e.g. hook_type "if present"
		}
		if isVolatileKey(field) {
			continue
		}
		ev.Fields[field] = scrubValue(rawValueToString(raw))
	}
	return ev
}

// recordTimestamp extracts the envelope timestamp of a record, trying the
// common timestamp-bearing fields in priority order. Returns ok=false when no
// parseable timestamp is present.
func recordTimestamp(obj map[string]json.RawMessage) (time.Time, bool) {
	for _, field := range []string{"timestamp", "emitted_at", "transitioned_at"} {
		raw, ok := obj[field]
		if !ok {
			continue
		}
		s := rawValueToString(raw)
		if s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, true
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
