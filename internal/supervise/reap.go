package supervise

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// flywheelSessionRe matches a flywheel orphan session name:
// harmonik-<12hex>-flywheel. The hash is the existing 12-hex project hash
// (sha256[:6] rendered as 12 lowercase hex chars). This is the ONLY family the
// reaper may ever kill — it can never match a -default, -captain, -crew-*,
// -supervise, or any non-flywheel session (CONTRACT.md invariant I3).
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
	// pane_dead state and creation time. When no tmux server is running (or no
	// sessions exist) it returns (nil, nil) — a no-op, NOT an error.
	ListFlywheelSessions(ctx context.Context) ([]FlywheelSession, error)
	// KillSession destroys the named session (tmux kill-session). Idempotent:
	// killing an absent session is not an error.
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
func ReapOrphanFlywheelSessions(ctx context.Context, adapter ReapAdapter, opts ReapOptions) (ReapResult, error) {
	var result ReapResult
	if adapter == nil {
		return result, nil
	}

	sessions, err := adapter.ListFlywheelSessions(ctx)
	if err != nil {
		return result, fmt.Errorf("supervise: reap: list flywheel sessions: %w", err)
	}

	for _, s := range sessions {
		result.Scanned++

		// Defense-in-depth: re-assert the flywheel-name discipline on every
		// candidate. This is the I3 guard mirrored from
		// reapDeadCoordinatorSession's belt-and-suspenders suffix check.
		if !IsFlywheelOrphanName(s.Name) {
			result.Skipped++
			continue
		}
		if opts.ProtectSession != "" && s.Name == opts.ProtectSession {
			result.Skipped++
			continue
		}
		if !s.PaneDead {
			// Live pane → an active flywheel (or a wedged-but-running shim).
			// Never reap; restart/stop owns that path.
			result.Skipped++
			continue
		}
		if !opts.DaemonStartTime.IsZero() && !s.Created.Before(opts.DaemonStartTime) {
			// Created at/after the live daemon start: may belong to the live
			// supervisor. Preserve.
			result.Skipped++
			continue
		}

		// Eligible: dead pane, predates the daemon, not protected. Reap it.
		now := time.Now().UTC()
		if killErr := adapter.KillSession(ctx, s.Name); killErr != nil {
			// TOCTOU (session vanished) or kill error: still count it as reaped —
			// we positively identified it as a dead-supervisor orphan. Mirrors the
			// daemon primitive's "proceed and count" behavior.
			_ = killErr
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

	return result, nil
}

// parseSessionCreated parses a tmux #{session_created} epoch field (unix
// seconds, possibly with trailing whitespace) into a time.Time. A blank or
// unparseable value yields the zero time, which the reaper treats as "predates
// every daemon start" only when no predate gate is set; with a gate, a zero
// created-time is Before any non-zero start and therefore eligible — acceptable
// because such a session is malformed and pane-dead to even be considered.
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
