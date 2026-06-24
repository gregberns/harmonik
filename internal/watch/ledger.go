// Package watch implements the WE2 event-ledger machinery for the watch tier.
//
// The watch tier is an always-on Sonnet session that sits between the event bus
// and the captain (design: plans/2026-06-23-captain-wake-economy/design.md §3).
//
// Ledger responsibilities (WE2 scope):
//
//   - Maintain a cursor file at .harmonik/watch/cursor (last processed event_id).
//   - Read events via eventbus.ScanAfter — a pure read-side function that NEVER
//     advances any comms-recv cursor (critic-2 / EV-020).  The recv cursor lives
//     in the daemon's CursorStore (.harmonik/comms/cursors/) and is entirely
//     separate from the watch's own watermark.
//   - Deduplicate on event_id using an in-memory seen set (N3 / EV-018).
//   - Consume subscription_gap by re-scanning events.jsonl from the cursor.
//   - Maintain a minimal .harmonik/watch/latest.json digest for the captain to
//     pull on its own idle.
//
// Richer per-lane query surfaces are follow-on WE10 (out of scope here).
// No SQLite, no new socket op.
package watch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// WatchDigest is the minimal pull-digest written to .harmonik/watch/latest.json.
// The captain reads this file on its own idle — it is never pushed via comms.
type WatchDigest struct {
	UpdatedAt                          string            `json:"updated_at"`
	Cursor                             string            `json:"cursor"`
	CrewLastSeen                       map[string]string `json:"crew_last_seen"`
	PendingFlags                       []string          `json:"pending_flags"`
	ImmediateCountSinceLastCaptainWake int               `json:"immediate_count_since_last_captain_wake"`
}

// Ledger tracks the watch cursor and deduplicates events.
//
// The cursor file records the last processed event_id.  On every Scan or
// ScanOnSubscriptionGap call the Ledger reads events.jsonl strictly after the
// cursor, deduplicates via an in-memory seen set, advances the cursor to the
// last new event, and returns the new events.
//
// MarkSeen registers an event_id delivered by the live subscribe stream into
// the in-memory seen set without advancing the cursor.  This prevents
// double-counting when ScanOnSubscriptionGap re-reads the same events from
// events.jsonl.
//
// The zero value is not usable; construct with NewLedger.
type Ledger struct {
	cursorPath string
	digestPath string
	cursor     core.EventID
	seen       map[core.EventID]struct{}
}

// NewLedger constructs a Ledger rooted at harmonikDir (.harmonik/).
//
// It reads the cursor from .harmonik/watch/cursor if the file exists.
// A missing cursor file is not an error — it means "scan from the beginning"
// (zero EventID).  The .harmonik/watch/ directory is created if absent.
func NewLedger(harmonikDir string) (*Ledger, error) {
	watchDir := filepath.Join(harmonikDir, "watch")
	if err := os.MkdirAll(watchDir, 0o755); err != nil {
		return nil, err
	}

	cursorPath := filepath.Join(watchDir, "cursor")
	digestPath := filepath.Join(watchDir, "latest.json")

	var cursor core.EventID
	raw, err := os.ReadFile(cursorPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if len(raw) > 0 {
		s := strings.TrimSpace(string(raw))
		if s != "" {
			if parseErr := cursor.UnmarshalText([]byte(s)); parseErr != nil {
				return nil, parseErr
			}
		}
	}

	return &Ledger{
		cursorPath: cursorPath,
		digestPath: digestPath,
		cursor:     cursor,
		seen:       make(map[core.EventID]struct{}),
	}, nil
}

// Cursor returns the current cursor (last processed event_id).
func (l *Ledger) Cursor() core.EventID {
	return l.cursor
}

// MarkSeen registers an event_id in the in-memory seen set without advancing
// the cursor.  Call this for events delivered by the live subscribe stream so
// that a subsequent ScanOnSubscriptionGap re-scan skips them.
func (l *Ledger) MarkSeen(evID core.EventID) {
	l.seen[evID] = struct{}{}
}

// Scan reads events from eventsPath strictly after the current cursor,
// deduplicates via the in-memory seen set, advances the cursor, and returns
// the new (not-previously-seen) events.
//
// Scan uses eventbus.ScanAfter — a pure read-only function that DOES NOT
// advance any comms-recv cursor.  Only .harmonik/watch/cursor is written.
func (l *Ledger) Scan(eventsPath string) ([]core.Event, error) {
	return l.scan(eventsPath)
}

// ScanOnSubscriptionGap re-scans events.jsonl from the current cursor to
// catch events dropped by the 256-slot live-stream buffer.  Identical to
// Scan in implementation; the separate name makes the call site's intent
// explicit.
func (l *Ledger) ScanOnSubscriptionGap(eventsPath string) ([]core.Event, error) {
	return l.scan(eventsPath)
}

// scan is the shared implementation used by Scan and ScanOnSubscriptionGap.
func (l *Ledger) scan(eventsPath string) ([]core.Event, error) {
	var fresh []core.Event
	var lastSeen core.EventID
	hasNew := false

	for ev := range eventbus.ScanAfter(eventsPath, l.cursor) {
		if _, alreadySeen := l.seen[ev.EventID]; alreadySeen {
			continue
		}
		l.seen[ev.EventID] = struct{}{}
		fresh = append(fresh, ev)
		lastSeen = ev.EventID
		hasNew = true
	}

	if hasNew {
		l.cursor = lastSeen
		if err := l.writeCursor(); err != nil {
			return fresh, err
		}
	}

	return fresh, nil
}

// writeCursor writes the current cursor to the cursor file.
func (l *Ledger) writeCursor() error {
	//nolint:gosec // G306: cursor file contains only a UUIDv7 string; world-readable is fine.
	return os.WriteFile(l.cursorPath, []byte(l.cursor.String()+"\n"), 0o644)
}

// WriteDigest writes d to .harmonik/watch/latest.json.
func (l *Ledger) WriteDigest(d WatchDigest) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	//nolint:gosec // G306: digest file is operator-readable status; world-readable is fine.
	return os.WriteFile(l.digestPath, b, 0o644)
}
