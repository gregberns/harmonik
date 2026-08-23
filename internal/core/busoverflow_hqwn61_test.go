// Package core — spec sensors for EV-011a, the bus's non-blocking back-pressure
// contract.
//
// Both tests here read specs/event-model.md and fail when its EV-011a text is
// removed or softened. They say nothing about the bus itself, and that is the
// honest reading today: EV-011a's bounded per-consumer queue, shed rule, spill
// files, reservation slot and bus_overflow emission are all unbuilt, so there is
// no behaviour here to assert against. See hk-5pj8r.
//
// This file also held six tests that logged what a future implementer should
// write and then called t.SkipNow(). They asserted nothing and no product change
// could turn any of them red, so they were deleted on 2026-08-05 (hk-702l3). The
// plan they carried lives in hk-5pj8r, where it can be scheduled.
//
// Spec ref: event-model.md §4.7 EV-011a, §8.8.4 bus_overflow.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBusOverflow_SpecContainsEV011a verifies that event-model.md §4.7 EV-011a
// is present and carries the three required policy terms.
//
// This sensor guards against silent removal or softening of the non-blocking
// back-pressure spec text. Any edit that removes EV-011a or its shed policies
// will fail this test, forcing a deliberate spec-amendment review.
//
// Spec ref: event-model.md §4.7 EV-011a (hk-hqwn.61).
func TestBusOverflow_SpecContainsEV011a(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate event-model.md")
	}
	specPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "specs", "event-model.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: specPath constructed from runtime.Caller + known relative segments; not user input
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v\nSpec must be present at repo root specs/event-model.md", specPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "EV-011a") {
		t.Error("event-model.md does not contain \"EV-011a\"; " +
			"the non-blocking back-pressure section is missing from the spec")
	}

	if !strings.Contains(content, "Non-blocking producer back-pressure") {
		t.Error("event-model.md does not contain the EV-011a heading " +
			"\"Non-blocking producer back-pressure\"; the section may have been renamed or removed")
	}

	for _, policy := range []string{"fsync-spilled", "ordinary-dropped", "lossy-dropped"} {
		if !strings.Contains(content, policy) {
			t.Errorf("event-model.md does not contain shed_policy value %q; "+
				"spec §8.8.4 or EV-011a shed-policy enum may have been edited", policy)
		}
	}

	if !strings.Contains(content, "spill-") {
		t.Error("event-model.md does not contain spill-file naming pattern " +
			"\"spill-<consumer>.jsonl\"; the EV-011a spill-file requirement may have been removed")
	}

	if !strings.Contains(content, "capacity-1 reservation") {
		t.Error("event-model.md does not contain \"capacity-1 reservation\"; " +
			"the EV-011a observer-queue reservation requirement may have been removed")
	}

	if !strings.Contains(content, "direct JSONL append") {
		t.Error("event-model.md does not contain \"direct JSONL append\"; " +
			"the EV-011a reservation-exhausted fallback requirement may have been removed")
	}
}

// TestBusOverflow_SpecContainsBusOverflowPayload verifies that event-model.md
// §8.8.4 declares the bus_overflow payload with all six required fields.
//
// Spec ref: event-model.md §8.8.4 bus_overflow (hk-hqwn.61).
func TestBusOverflow_SpecContainsBusOverflowPayload(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate event-model.md")
	}
	specPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "specs", "event-model.md")

	raw, err := os.ReadFile(specPath) //nolint:gosec // G304: specPath constructed from runtime.Caller + known relative segments; not user input
	if err != nil {
		t.Fatalf("os.ReadFile(%q): %v", specPath, err)
	}
	content := string(raw)

	if !strings.Contains(content, "bus_overflow") {
		t.Error("event-model.md does not contain event type \"bus_overflow\"; §8.8.4 is missing")
	}

	requiredFields := []string{
		"consumer_name",
		"event_type",
		"event_id",
		"queue_depth",
		"shed_at",
		"shed_policy",
	}
	for _, field := range requiredFields {
		if !strings.Contains(content, field) {
			t.Errorf("event-model.md §8.8.4 bus_overflow payload missing field %q", field)
		}
	}
}
