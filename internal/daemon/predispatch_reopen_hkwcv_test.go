package daemon_test

// predispatch_reopen_hkwcv_test.go — regression tests for hk-wcv.
//
// Bug: the pre-dispatch subsume check (beadRunOne, hk-ly0hg Fix-2) incorrectly
// closes a bead that was explicitly reopened for a corrective fix. The check
// calls beadAlreadySubsumedInMain which finds the prior (incomplete) commit on
// main and closes the bead — silently discarding the reopen intent.
//
// Fix: before applying the subsume close, query the bead's audit log for a
// "status_changed" event whose old_value is "closed". Such an event means the
// bead transitioned from closed→open, i.e. it was properly closed at some point
// and then intentionally reopened (reopen-for-fix). In that case the pre-dispatch
// close is skipped and the agent is dispatched normally.
//
// These tests exercise beadExplicitlyReopened — the predicate introduced by the
// fix — via ExportedBeadExplicitlyReopened:
//
//   (A) REOPEN-FOR-FIX: audit log contains a status_changed from closed→open →
//       beadExplicitlyReopened returns true (pre-dispatch close must be skipped).
//
//   (B) CRASH-RESTART: audit log shows only in_progress→open (no closed state)
//       → beadExplicitlyReopened returns false (pre-dispatch close fires).
//
//   (C) EMPTY LOG: audit log is empty (freshly created or no transitions) →
//       beadExplicitlyReopened returns false (conservative: no bypass).
//
//   (D) NIL LOGGER: beadAuditLogger is nil → beadExplicitlyReopened returns
//       false (conservative default for tests that don't supply an audit logger).
//
//   (E) LOGGER ERROR: audit logger returns an error → beadExplicitlyReopened
//       returns false (conservative: treat as crash-restart, do not bypass).
//
//   (F) CLOSED-TO-OPEN BURIED: the closed→open event exists among many other
//       events → still returns true (order-independent scan).
//
// Bead ref: hk-wcv.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// auditEvent builds a minimal brcli.AuditEvent for test fixtures.
func hkwcvAuditEvent(eventType, oldValue, newValue string) brcli.AuditEvent {
	return brcli.AuditEvent{
		ID:        1,
		EventType: eventType,
		Actor:     "test",
		Timestamp: time.Now(),
		OldValue:  oldValue,
		NewValue:  newValue,
	}
}

// stubAuditLogger returns a brcli.AuditEvent slice logger that always returns
// the given events without error.
func hkwcvStubLogger(events []brcli.AuditEvent) func(context.Context, core.BeadID) ([]brcli.AuditEvent, error) {
	return func(_ context.Context, _ core.BeadID) ([]brcli.AuditEvent, error) {
		return events, nil
	}
}

// errorAuditLogger returns a logger that always fails with the given error.
func hkwcvErrorLogger(err error) func(context.Context, core.BeadID) ([]brcli.AuditEvent, error) {
	return func(_ context.Context, _ core.BeadID) ([]brcli.AuditEvent, error) {
		return nil, err
	}
}

// TestBeadExplicitlyReopened_ReopenForFix_ReturnsTrue is case (A): the audit
// log contains a status_changed event from "closed" to "open" — the bead was
// properly closed and then intentionally reopened for a corrective fix. The
// pre-dispatch subsume close must be bypassed.
func TestBeadExplicitlyReopened_ReopenForFix_ReturnsTrue(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-jzpqo") // the repro bead from hk-wcv

	// Simulate the audit log after a proper close + human reopen:
	//   in_progress → closed  (daemon successfully closed the bead)
	//   closed → open         (human reopened for a corrective fix)
	events := []brcli.AuditEvent{
		hkwcvAuditEvent("status_changed", "in_progress", "closed"),
		hkwcvAuditEvent("status_changed", "closed", "open"),
		hkwcvAuditEvent("status_changed", "open", "in_progress"), // daemon re-claimed
	}

	got := daemon.ExportedBeadExplicitlyReopened(t.Context(), hkwcvStubLogger(events), beadID)
	if !got {
		t.Fatal("beadExplicitlyReopened = false; want true\n" +
			"A bead with a closed→open audit event was NOT detected as a reopen-for-fix. " +
			"This means the pre-dispatch subsume check would incorrectly close it, " +
			"defeating the operator's reopen intent (hk-wcv).")
	}
}

// TestBeadExplicitlyReopened_CrashRestart_ReturnsFalse is case (B): the audit
// log shows in_progress→open (reconciler reopen) but no closed state. The bead
// was crash-restarted — its commit is on main but CloseBead was never called.
// The pre-dispatch subsume close must fire.
func TestBeadExplicitlyReopened_CrashRestart_ReturnsFalse(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-crashrestart-001")

	// Simulate crash-restart: daemon claimed the bead, agent committed, daemon
	// crashed before CloseBead. The reconciler reopened via in_progress→open.
	events := []brcli.AuditEvent{
		hkwcvAuditEvent("status_changed", "open", "in_progress"), // original claim
		hkwcvAuditEvent("status_changed", "in_progress", "open"), // reconciler reopen
		hkwcvAuditEvent("status_changed", "open", "in_progress"), // re-claim
	}

	got := daemon.ExportedBeadExplicitlyReopened(t.Context(), hkwcvStubLogger(events), beadID)
	if got {
		t.Fatal("beadExplicitlyReopened = true; want false\n" +
			"A crash-restart bead (in_progress→open by reconciler, no prior closed state) " +
			"was incorrectly detected as a reopen-for-fix. " +
			"The pre-dispatch subsume check would be skipped, causing a wasted agent dispatch.")
	}
}

// TestBeadExplicitlyReopened_EmptyLog_ReturnsFalse is case (C): an empty audit
// log (no transitions). Conservative: return false, do not bypass.
func TestBeadExplicitlyReopened_EmptyLog_ReturnsFalse(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-emptylog-001")

	got := daemon.ExportedBeadExplicitlyReopened(t.Context(), hkwcvStubLogger(nil), beadID)
	if got {
		t.Fatal("beadExplicitlyReopened = true for empty audit log; want false (conservative)")
	}
}

// TestBeadExplicitlyReopened_NilLogger_ReturnsFalse is case (D): when
// beadAuditLogger is nil the check is skipped and returns false (conservative,
// preserving prior crash-restart behaviour for tests that don't wire a logger).
func TestBeadExplicitlyReopened_NilLogger_ReturnsFalse(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-nil-logger-001")

	got := daemon.ExportedBeadExplicitlyReopened(t.Context(), nil, beadID)
	if got {
		t.Fatal("beadExplicitlyReopened = true with nil logger; want false (conservative)")
	}
}

// TestBeadExplicitlyReopened_LoggerError_ReturnsFalse is case (E): the audit
// logger returns an error. Conservative: return false, do not bypass.
func TestBeadExplicitlyReopened_LoggerError_ReturnsFalse(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-logger-error-001")

	got := daemon.ExportedBeadExplicitlyReopened(
		t.Context(),
		hkwcvErrorLogger(errors.New("br audit log: brcli: br audit log failed")),
		beadID,
	)
	if got {
		t.Fatal("beadExplicitlyReopened = true when logger errors; want false (conservative)")
	}
}

// TestBeadExplicitlyReopened_ClosedOpenBuriedAmongEvents_ReturnsTrue is case
// (F): the closed→open event exists among many other events. The scan is
// order-independent: it must return true regardless of position.
func TestBeadExplicitlyReopened_ClosedOpenBuriedAmongEvents_ReturnsTrue(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-buried-close-001")

	events := []brcli.AuditEvent{
		hkwcvAuditEvent("created", "", ""),
		hkwcvAuditEvent("label_added", "", "codename:some-work"),
		hkwcvAuditEvent("status_changed", "open", "in_progress"),
		hkwcvAuditEvent("status_changed", "in_progress", "closed"), // first close
		hkwcvAuditEvent("commented", "", ""),
		hkwcvAuditEvent("status_changed", "closed", "open"), // reopen-for-fix
		hkwcvAuditEvent("status_changed", "open", "in_progress"),
	}

	got := daemon.ExportedBeadExplicitlyReopened(t.Context(), hkwcvStubLogger(events), beadID)
	if !got {
		t.Fatal("beadExplicitlyReopened = false; want true\n" +
			"A closed→open event buried among other events was not detected. " +
			"The scan must be order-independent.")
	}
}

// TestBeadExplicitlyReopened_OnlyNonClosedTransitions_ReturnsFalse verifies
// that status_changed events with old_value != "closed" do not trigger the
// bypass (e.g. open→in_progress, in_progress→open, in_progress→closed is a
// close not a reopen).
func TestBeadExplicitlyReopened_OnlyNonClosedTransitions_ReturnsFalse(t *testing.T) {
	t.Parallel()

	const beadID = core.BeadID("hk-nonclose-001")

	// Only non-"closed" old_values. The closed→open pattern is absent.
	events := []brcli.AuditEvent{
		hkwcvAuditEvent("status_changed", "open", "in_progress"),
		hkwcvAuditEvent("status_changed", "in_progress", "open"), // crash-restart
		hkwcvAuditEvent("label_added", "", "found-in:session-x"),
		hkwcvAuditEvent("commented", "", ""),
	}

	got := daemon.ExportedBeadExplicitlyReopened(t.Context(), hkwcvStubLogger(events), beadID)
	if got {
		t.Fatal("beadExplicitlyReopened = true; want false\n" +
			"Events with old_value != 'closed' must not trigger the reopen-for-fix bypass.")
	}
}
