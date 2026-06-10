package core

import "testing"

// TestCheckVerdictStaleness_NotStale_WhenBothMatch verifies that the staleness
// check returns Stale=false when both the git head hash and Beads audit entry
// ID are unchanged since the snapshot.
//
// RC-024: "The verdict is stale if either [git advanced] OR [beads advanced]."
// Converse: if neither has advanced, the verdict is not stale.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024.
func TestCheckVerdictStaleness_NotStale_WhenBothMatch(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotToken{
		GitHeadHash:         "abc123",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-01T00:00:00Z",
	}

	result := CheckVerdictStaleness(snapshot, "abc123", "audit-001")

	if result.Stale {
		t.Error("RC-024: expected Stale=false when both hashes match; got Stale=true")
	}
	if result.Payload != nil {
		t.Error("RC-024: expected Payload=nil on non-stale result; got non-nil")
	}
}

// TestCheckVerdictStaleness_Stale_WhenGitAdvanced verifies that the staleness
// check returns Stale=true with DivergenceReason=git-branch-advanced when the
// target run's git head hash has changed since the snapshot.
//
// RC-024: "the target run's checkpoint trail has gained a new commit since the
// snapshot" triggers staleness.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024;
// specs/reconciliation/schemas.md §6.1 ENUM StaleDivergenceReason.
func TestCheckVerdictStaleness_Stale_WhenGitAdvanced(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotToken{
		GitHeadHash:         "abc123",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-01T00:00:00Z",
	}

	result := CheckVerdictStaleness(snapshot, "def456", "audit-001")

	if !result.Stale {
		t.Error("RC-024: expected Stale=true when git hash advanced; got Stale=false")
	}
	if result.Payload == nil {
		t.Fatal("RC-024: expected non-nil Payload when stale; got nil")
	}
	if !result.Payload.Valid() {
		t.Error("RC-024: Payload.Valid() = false; stale payload must be valid for event emission")
	}
	if result.Payload.DivergenceReason != StaleDivergenceReasonGitBranchAdvanced {
		t.Errorf("RC-024: DivergenceReason = %q, want %q",
			result.Payload.DivergenceReason, StaleDivergenceReasonGitBranchAdvanced)
	}
	if result.Payload.CurrentGitHeadHash != "def456" {
		t.Errorf("RC-024: CurrentGitHeadHash = %q, want %q",
			result.Payload.CurrentGitHeadHash, "def456")
	}
	if result.Payload.CurrentBeadsAuditID != "audit-001" {
		t.Errorf("RC-024: CurrentBeadsAuditID = %q, want %q",
			result.Payload.CurrentBeadsAuditID, "audit-001")
	}
}

// TestCheckVerdictStaleness_Stale_WhenBeadsAdvanced verifies that the staleness
// check returns Stale=true with DivergenceReason=beads-audit-advanced when the
// target bead's Beads audit entry ID has changed since the snapshot.
//
// RC-024: "the target run's bead has changed status in the Beads audit log
// since the snapshot" triggers staleness.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024;
// specs/reconciliation/schemas.md §6.1 ENUM StaleDivergenceReason.
func TestCheckVerdictStaleness_Stale_WhenBeadsAdvanced(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotToken{
		GitHeadHash:         "abc123",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-01T00:00:00Z",
	}

	result := CheckVerdictStaleness(snapshot, "abc123", "audit-099")

	if !result.Stale {
		t.Error("RC-024: expected Stale=true when Beads audit ID advanced; got Stale=false")
	}
	if result.Payload == nil {
		t.Fatal("RC-024: expected non-nil Payload when stale; got nil")
	}
	if !result.Payload.Valid() {
		t.Error("RC-024: Payload.Valid() = false; stale payload must be valid for event emission")
	}
	if result.Payload.DivergenceReason != StaleDivergenceReasonBeadsAuditAdvanced {
		t.Errorf("RC-024: DivergenceReason = %q, want %q",
			result.Payload.DivergenceReason, StaleDivergenceReasonBeadsAuditAdvanced)
	}
	if result.Payload.CurrentGitHeadHash != "abc123" {
		t.Errorf("RC-024: CurrentGitHeadHash = %q, want %q",
			result.Payload.CurrentGitHeadHash, "abc123")
	}
	if result.Payload.CurrentBeadsAuditID != "audit-099" {
		t.Errorf("RC-024: CurrentBeadsAuditID = %q, want %q",
			result.Payload.CurrentBeadsAuditID, "audit-099")
	}
}

// TestCheckVerdictStaleness_GitPriorityWhenBothAdvanced verifies that when both
// the git hash and the Beads audit ID have changed, git staleness is reported
// (listed first in RC-024 and corresponds to the more fundamental state authority).
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024.
func TestCheckVerdictStaleness_GitPriorityWhenBothAdvanced(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotToken{
		GitHeadHash:         "abc123",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-01T00:00:00Z",
	}

	result := CheckVerdictStaleness(snapshot, "def456", "audit-099")

	if !result.Stale {
		t.Error("RC-024: expected Stale=true when both advanced; got Stale=false")
	}
	if result.Payload == nil {
		t.Fatal("RC-024: expected non-nil Payload when stale; got nil")
	}
	if result.Payload.DivergenceReason != StaleDivergenceReasonGitBranchAdvanced {
		t.Errorf("RC-024: DivergenceReason = %q, want %q (git has priority over beads)",
			result.Payload.DivergenceReason, StaleDivergenceReasonGitBranchAdvanced)
	}
}

// TestCheckVerdictStaleness_SnapshotPreservedInPayload verifies that the
// original snapshot token is preserved verbatim in the StaleVerdictPayload
// so that the re-dispatch path has access to both the original snapshot and
// the current values.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024;
// specs/reconciliation/schemas.md §6.1 RECORD StaleVerdictPayload.
func TestCheckVerdictStaleness_SnapshotPreservedInPayload(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotToken{
		GitHeadHash:         "original-hash",
		BeadsAuditEntryID:   "original-audit",
		CapturedAtTimestamp: "2026-05-01T12:34:56Z",
	}

	result := CheckVerdictStaleness(snapshot, "new-hash", "original-audit")

	if result.Payload == nil {
		t.Fatal("RC-024: expected non-nil Payload; got nil")
	}
	if result.Payload.Snapshot != snapshot {
		t.Errorf("RC-024: Snapshot in payload = %+v, want %+v; original snapshot must be preserved",
			result.Payload.Snapshot, snapshot)
	}
}

// TestCheckVerdictStaleness_SiblingBeadsAndJSONLDoNotTriggerStaleness documents
// the RC-024 boundary: changes to sibling beads or the daemon's JSONL event log
// MUST NOT trigger staleness. Only the target run's git branch and the target
// bead's Beads audit entries count.
//
// This test anchors the boundary at the pure-comparison layer: CheckVerdictStaleness
// receives only the two relevant values (git hash, beads audit ID), so callers
// are responsible for not passing sibling or JSONL values.
//
// RC-024: "Changes to sibling beads or to the daemon's JSONL event log MUST NOT
// trigger staleness."
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-024.
func TestCheckVerdictStaleness_SiblingBeadsAndJSONLDoNotTriggerStaleness(t *testing.T) {
	t.Parallel()

	snapshot := SnapshotToken{
		GitHeadHash:         "abc123",
		BeadsAuditEntryID:   "audit-001",
		CapturedAtTimestamp: "2026-05-01T00:00:00Z",
	}

	// Even if a sibling bead changed (the caller passes only the target bead's
	// audit ID), staleness is not triggered when both target-scoped values match.
	// The caller owns the re-capture scope; this test confirms the function
	// treats its two arguments as the complete staleness signal.
	result := CheckVerdictStaleness(snapshot, "abc123", "audit-001")

	if result.Stale {
		t.Errorf("RC-024: staleness check returned Stale=true when both target-scoped values match; " +
			"sibling changes must not propagate to CheckVerdictStaleness arguments")
	}
}
