// Package core — requirement-traceable sensors for the EM-020a audit-tool
// detection rule for transition-record integrity.
//
// EM-020a requires a post-hoc audit tool to detect five integrity-violation
// conditions for every commit reachable from every active or archived task
// branch. This file tests the types that encode those conditions:
//
//   - AuditViolationKind: the five-value closed enum ((a)–(e) in EM-020a)
//   - AuditViolation: the record type that the audit tool produces per flagged commit
//
// Spec ref: specs/execution-model.md §4.4.EM-020a.
// Bead: hk-b3f.26.
package core

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// auditDetectFixtureViolation builds a valid AuditViolation with the given kind
// and a pre-populated CommitSHA, RunID, TransitionID, and Description.
func auditDetectFixtureViolation(t *testing.T, kind AuditViolationKind) AuditViolation {
	t.Helper()
	return AuditViolation{
		Kind:         kind,
		CommitSHA:    "aabbccddeeff0011223344556677889900aabbcc",
		RunID:        RunID(uuid.Must(uuid.NewV7())),
		TransitionID: TransitionID(uuid.Must(uuid.NewV7())),
		Description:  "test violation: " + string(kind),
	}
}

// auditDetectFixtureAllKinds returns all five declared AuditViolationKind values.
func auditDetectFixtureAllKinds() []AuditViolationKind {
	return []AuditViolationKind{
		AuditViolationKindNoSiblingFile,
		AuditViolationKindOrphanSiblingFile,
		AuditViolationKindDuplicateTransitionID,
		AuditViolationKindSchemaVersionMismatch,
		AuditViolationKindRunIDPathMismatch,
	}
}

// --- AuditViolationKind.Valid ---

// TestAuditViolationKind_ValidAcceptsDeclared verifies that every declared
// AuditViolationKind constant is accepted by Valid(). Each constant maps to
// one of the five EM-020a violation conditions (a)–(e).
func TestAuditViolationKind_ValidAcceptsDeclared(t *testing.T) {
	t.Parallel()

	for _, k := range auditDetectFixtureAllKinds() {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			if !k.Valid() {
				t.Errorf("AuditViolationKind.Valid() = false for declared constant %q", k)
			}
		})
	}
}

// TestAuditViolationKind_ValidRejectsUnknown verifies that undeclared strings
// are rejected by Valid().
func TestAuditViolationKind_ValidRejectsUnknown(t *testing.T) {
	t.Parallel()

	invalid := []AuditViolationKind{
		"",
		"No-Sibling-File",
		"NO-SIBLING-FILE",
		"no_sibling_file",
		"orphanSiblingFile",
		"duplicatetransitionid",
		"schema_version_mismatch",
		"run-id-path-Mismatch",
		"condition-a",
		"unknown",
		"violation",
	}
	for _, k := range invalid {
		k := k
		t.Run(string(k)+"_rejected", func(t *testing.T) {
			t.Parallel()
			if k.Valid() {
				t.Errorf("AuditViolationKind.Valid() = true for invalid value %q; want false", k)
			}
		})
	}
}

// TestAuditViolationKind_FiveConditions verifies that exactly five constants
// are declared, matching the five violation conditions in EM-020a.
func TestAuditViolationKind_FiveConditions(t *testing.T) {
	t.Parallel()

	kinds := auditDetectFixtureAllKinds()
	if len(kinds) != 5 {
		t.Fatalf("expected exactly 5 AuditViolationKind constants (EM-020a conditions a-e), got %d", len(kinds))
	}
}

// --- AuditViolationKind.MarshalText / UnmarshalText ---

// TestAuditViolationKind_MarshalTextAcceptsDeclared verifies that every
// declared kind marshals to its string value without error.
func TestAuditViolationKind_MarshalTextAcceptsDeclared(t *testing.T) {
	t.Parallel()

	want := map[AuditViolationKind]string{
		AuditViolationKindNoSiblingFile:         "no-sibling-file",
		AuditViolationKindOrphanSiblingFile:     "orphan-sibling-file",
		AuditViolationKindDuplicateTransitionID: "duplicate-transition-id",
		AuditViolationKindSchemaVersionMismatch: "schema-version-mismatch",
		AuditViolationKindRunIDPathMismatch:     "run-id-path-mismatch",
	}
	for k, wantStr := range want {
		k, wantStr := k, wantStr
		t.Run(wantStr, func(t *testing.T) {
			t.Parallel()
			got, err := k.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText(%q): unexpected error: %v", k, err)
			}
			if string(got) != wantStr {
				t.Errorf("MarshalText(%q) = %q, want %q", k, string(got), wantStr)
			}
		})
	}
}

// TestAuditViolationKind_MarshalTextRejectsInvalid verifies that MarshalText
// returns an error for any undeclared value.
func TestAuditViolationKind_MarshalTextRejectsInvalid(t *testing.T) {
	t.Parallel()

	if _, err := AuditViolationKind("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted undeclared value; want error")
	}
	if _, err := AuditViolationKind("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string; want error")
	}
}

// TestAuditViolationKind_UnmarshalTextRoundTrip verifies that marshaling then
// unmarshaling each declared constant produces the original value.
func TestAuditViolationKind_UnmarshalTextRoundTrip(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Kind AuditViolationKind `json:"kind"`
	}

	for _, k := range auditDetectFixtureAllKinds() {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			data, err := json.Marshal(wrapper{Kind: k})
			if err != nil {
				t.Fatalf("json.Marshal(%q): %v", k, err)
			}
			var out wrapper
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("json.Unmarshal(%q): %v", string(data), err)
			}
			if out.Kind != k {
				t.Errorf("round-trip: got %q, want %q", out.Kind, k)
			}
		})
	}
}

// TestAuditViolationKind_UnmarshalTextRejectsUnknown verifies that
// UnmarshalText rejects undeclared strings with an informative error.
func TestAuditViolationKind_UnmarshalTextRejectsUnknown(t *testing.T) {
	t.Parallel()

	type wrapper struct {
		Kind AuditViolationKind `json:"kind"`
	}

	var w wrapper
	err := json.Unmarshal([]byte(`{"kind":"condition-x"}`), &w)
	if err == nil {
		t.Fatal("UnmarshalText accepted unknown value; want error")
	}
}

// TestAuditViolationKind_UnmarshalTextErrorMentionsAllValues verifies that the
// error message from UnmarshalText names all five valid constants.
func TestAuditViolationKind_UnmarshalTextErrorMentionsAllValues(t *testing.T) {
	t.Parallel()

	var k AuditViolationKind
	err := k.UnmarshalText([]byte("not-a-real-kind"))
	if err == nil {
		t.Fatal("UnmarshalText: expected error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"no-sibling-file",
		"orphan-sibling-file",
		"duplicate-transition-id",
		"schema-version-mismatch",
		"run-id-path-mismatch",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}

// --- AuditViolation.Valid ---

// TestAuditViolation_ValidHappyPath verifies that a fully-populated
// AuditViolation with a declared kind, non-empty CommitSHA, and non-empty
// Description is accepted by Valid().
func TestAuditViolation_ValidHappyPath(t *testing.T) {
	t.Parallel()

	for _, k := range auditDetectFixtureAllKinds() {
		k := k
		t.Run(string(k), func(t *testing.T) {
			t.Parallel()
			av := auditDetectFixtureViolation(t, k)
			if !av.Valid() {
				t.Errorf("AuditViolation.Valid() = false for valid AuditViolation with kind %q", k)
			}
		})
	}
}

// TestAuditViolation_ValidRejectsUnknownKind verifies that Valid() returns
// false when Kind is not a declared AuditViolationKind constant.
func TestAuditViolation_ValidRejectsUnknownKind(t *testing.T) {
	t.Parallel()

	av := auditDetectFixtureViolation(t, AuditViolationKindNoSiblingFile)
	av.Kind = AuditViolationKind("unknown-kind")
	if av.Valid() {
		t.Error("AuditViolation.Valid() = true for unknown Kind; want false")
	}
}

// TestAuditViolation_ValidRejectsEmptyCommitSHA verifies that Valid() returns
// false when CommitSHA is empty.
func TestAuditViolation_ValidRejectsEmptyCommitSHA(t *testing.T) {
	t.Parallel()

	av := auditDetectFixtureViolation(t, AuditViolationKindNoSiblingFile)
	av.CommitSHA = ""
	if av.Valid() {
		t.Error("AuditViolation.Valid() = true for empty CommitSHA; want false")
	}
}

// TestAuditViolation_ValidRejectsEmptyDescription verifies that Valid() returns
// false when Description is empty.
func TestAuditViolation_ValidRejectsEmptyDescription(t *testing.T) {
	t.Parallel()

	av := auditDetectFixtureViolation(t, AuditViolationKindNoSiblingFile)
	av.Description = ""
	if av.Valid() {
		t.Error("AuditViolation.Valid() = true for empty Description; want false")
	}
}

// TestAuditViolation_ValidAllowsZeroRunID verifies that Valid() returns true
// even when RunID is the zero value. Per EM-020a condition (a), the RunID may
// be absent when a trailer pair is entirely missing from the commit.
func TestAuditViolation_ValidAllowsZeroRunID(t *testing.T) {
	t.Parallel()

	av := auditDetectFixtureViolation(t, AuditViolationKindNoSiblingFile)
	av.RunID = RunID(uuid.Nil)
	if !av.Valid() {
		t.Error("AuditViolation.Valid() = false for zero RunID; want true (trailer may be absent per EM-020a(a))")
	}
}

// TestAuditViolation_ValidAllowsZeroTransitionID verifies that Valid() returns
// true even when TransitionID is the zero value, for the same reason as
// zero RunID.
func TestAuditViolation_ValidAllowsZeroTransitionID(t *testing.T) {
	t.Parallel()

	av := auditDetectFixtureViolation(t, AuditViolationKindNoSiblingFile)
	av.TransitionID = TransitionID(uuid.Nil)
	if !av.Valid() {
		t.Error("AuditViolation.Valid() = false for zero TransitionID; want true (trailer may be absent per EM-020a(a))")
	}
}

// TestAuditViolation_KindsMapToConditions verifies the string values of the
// five AuditViolationKind constants match the spec's condition labels. These
// are the wire values used when routing violations to reconciliation (RC-010).
func TestAuditViolation_KindsMapToConditions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind AuditViolationKind
		want string
	}{
		// condition (a): trailer pair with no matching sibling file
		{AuditViolationKindNoSiblingFile, "no-sibling-file"},
		// condition (b): orphaned sibling file not matching any trailer pair
		{AuditViolationKindOrphanSiblingFile, "orphan-sibling-file"},
		// condition (c): transition_id on more than one commit within a run
		{AuditViolationKindDuplicateTransitionID, "duplicate-transition-id"},
		// condition (d): schema_version mismatch between sibling file and trailer
		{AuditViolationKindSchemaVersionMismatch, "schema-version-mismatch"},
		// condition (e): path run_id component disagrees with Harmonik-Run-ID trailer
		{AuditViolationKindRunIDPathMismatch, "run-id-path-mismatch"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if string(tc.kind) != tc.want {
				t.Errorf("AuditViolationKind = %q, want %q", tc.kind, tc.want)
			}
		})
	}
}
