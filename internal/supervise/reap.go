package supervise

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var flywheelSessionRe = regexp.MustCompile(`^harmonik-[0-9a-f]{12}-flywheel$`)

// IsFlywheelOrphanName reports whether name is a flywheel-suffixed session in
// the harmonik-<12hex>-flywheel family. Exported so call sites (boot auto-reap)
// can assert the session-name discipline before reaping.
func IsFlywheelOrphanName(name string) bool {
	return flywheelSessionRe.MatchString(strings.TrimSpace(name))
}

// FlywheelSession is one enumerated tmux session candidate for the reaper.
type FlywheelSession struct {
	// Name is the tmux session name (must match harmonik-<12hex>-flywheel to be
	// eligible).
	Name string
	// PaneDead is true when the session's first pane has pane_dead=1 (the shim
	// exited but remain-on-exit kept the empty pane visible).
	PaneDead bool
	// Created is the session's creation time (tmux #{session_created}, a unix
	// epoch). A session is only reaped when it predates the live daemon start.
	Created time.Time
}

// ReapAdapter is the minimal tmux surface the flywheel orphan reaper needs.
// It is intentionally NOT the full lifecycle/tmux.Adapter — the reaper lives in
// internal/supervise (which must not pull the heavy lifecycle dependency for a
// host-wide enumeration) and only needs list + kill. The OS-backed
// implementation is osReapAdapter (reap_osadapter.go); tests supply a fake.
type ReapAdapter interface {
	// ListFlywheelSessions enumerates all live tmux sessions and returns the
	// subset whose name matches the flywheel family, each annotated with its
	// pane_dead state and creation time. Absence is not an error: no tmux
	// server, no sessions, or no tmux binary at all all yield (nil, nil). Any
	// OTHER failure (e.g. a permission error talking to the server) is returned.
	ListFlywheelSessions(ctx context.Context) ([]FlywheelSession, error)
	// KillSession destroys the named session (tmux kill-session). Idempotent:
	// an already-gone session — whether the session alone vanished or the whole
	// tmux server exited between the list and the kill — is not an error. Any
	// other kill failure IS returned, and the reaper then neither counts nor
	// emits an event for that session.
	KillSession(ctx context.Context, name string) error
}

// ReapEvent is one tmux_orphan_reaped record emitted per killed session.
type ReapEvent struct {
	Event    string    `json:"event"`     // always "tmux_orphan_reaped"
	Session  string    `json:"session"`   // the killed flywheel session name
	Reason   string    `json:"reason"`    // "dead_pane_predates_daemon"
	Created  time.Time `json:"created"`   // the reaped session's creation time
	ReapedAt time.Time `json:"reaped_at"` // when the kill was issued
}

// ReapResult summarizes a reap pass.
type ReapResult struct {
	// Scanned is the number of flywheel-family sessions enumerated.
	Scanned int
	// Reaped is the names of the sessions actually killed.
	Reaped []string
	// Skipped is the number of flywheel sessions left alive (live pane, or
	// not predating the daemon, or the protected live session).
	Skipped int
	// Events is one ReapEvent per kill, in reap order.
	Events []ReapEvent
}

// ReapOptions configure a reap pass.
type ReapOptions struct {
	// DaemonStartTime is the live daemon/supervisor start time. Only flywheel
	// sessions created strictly before this are eligible — a session created at
	// or after daemon start may belong to the live supervisor and is preserved.
	// Zero value means "no predate gate" (every dead-pane flywheel is eligible);
	// callers SHOULD supply a real time when one is resolvable.
	DaemonStartTime time.Time
	// ProtectSession, when non-empty, is a flywheel session name the reaper must
	// NEVER kill regardless of pane/created state. The boot auto-reap path passes
	// the session it just created so a fresh `supervise start` can clean stale
	// orphans without ever touching its own live one.
	ProtectSession string
}

// ReapOrphanFlywheelSessions enumerates flywheel-family tmux sessions and kills
// those whose pane is dead AND that predate the live daemon start, emitting a
// tmux_orphan_reaped event per kill. It is the host-wide generalization of the
// daemon's single-project reapDeadCoordinatorSession primitive: same safety
// envelope (ONLY -flywheel sessions, never a live agent — CONTRACT.md I3), but
// it scans every flywheel orphan a crashed/killed daemon may have leaked rather
// than one deterministic per-project name.
//
// Safety:
//   - adapter==nil or no tmux server → clean no-op (empty result, no error).
//   - a non-flywheel name surfacing in the adapter's list is refused (defense in
//     depth: the adapter already filters, but the kill loop re-checks every name).
//   - a session with a live pane (pane_dead=0) is preserved.
//   - a session created at/after DaemonStartTime is preserved.
//   - opts.ProtectSession is never killed.
//
// Errors: a list failure aborts the pass (nothing was killed). A kill failure
// does NOT abort the pass — that session is left out of Reaped/Events (a failed
// kill is never reported as a successful reap), the remaining candidates are
// still processed, and every kill error is joined into the returned error. So
// the result is always the truth about what WAS killed even when err != nil;
// callers that surface events must emit result.Events before acting on err.
func ReapOrphanFlywheelSessions(ctx context.Context, adapter ReapAdapter, opts ReapOptions) (ReapResult, error) {
	var result ReapResult
	var killErrs []error
	if adapter == nil {
		return result, nil
	}

	sessions, err := adapter.ListFlywheelSessions(ctx)
	if err != nil {
		return result, fmt.Errorf("supervise: reap: list flywheel sessions: %w", err)
	}

	for _, s := range sessions {
		result.Scanned++

		if !IsFlywheelOrphanName(s.Name) {
			result.Skipped++
			continue
		}
		if opts.ProtectSession != "" && s.Name == opts.ProtectSession {
			result.Skipped++
			continue
		}
		if !s.PaneDead {
			result.Skipped++
			continue
		}
		if !opts.DaemonStartTime.IsZero() && !s.Created.Before(opts.DaemonStartTime) {
			result.Skipped++
			continue
		}

		now := time.Now().UTC()
		if killErr := adapter.KillSession(ctx, s.Name); killErr != nil {
			killErrs = append(killErrs, fmt.Errorf("supervise: reap: kill session %q: %w", s.Name, killErr))
			continue
		}
		result.Reaped = append(result.Reaped, s.Name)
		result.Events = append(result.Events, ReapEvent{
			Event:    "tmux_orphan_reaped",
			Session:  s.Name,
			Reason:   "dead_pane_predates_daemon",
			Created:  s.Created.UTC(),
			ReapedAt: now,
		})
	}

	return result, errors.Join(killErrs...)
}

func parseSessionCreated(field string) time.Time {
	field = strings.TrimSpace(field)
	if field == "" {
		return time.Time{}
	}
	secs, err := strconv.ParseInt(field, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(secs, 0).UTC()
}
