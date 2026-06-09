package queue_test

// defaultharness_hk4x3rg_test.go — tests for the per-queue DefaultHarness field
// (codex-harness C4/T6, hk-4x3rg). The field is tier 2 of the four-tier
// harness-selection precedence walk in internal/daemon/harnessresolve.go
// (bead-label > per-queue > node > global). This file proves the field exists,
// persists round-trip, is carried from QueueSubmitRequest onto the persisted
// Queue, validates via core.AgentType.Valid(), and is backward-compatible with
// queue.json files that predate it.
//
// Dispatch-time wiring into the resolveHarness call is C5/T12 (hk-xhawy) — out
// of scope here; these tests only exercise existence/persistence/validation.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// TestDefaultHarness_PersistRoundTrip verifies a valid DefaultHarness value set
// on a Queue survives a Persist → Load round-trip unchanged.
func TestDefaultHarness_PersistRoundTrip(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	original := typesFixtureQueue()
	original.DefaultHarness = core.AgentTypeCodex
	ctx := context.Background()

	if err := queue.Persist(ctx, projectDir, &original); err != nil {
		t.Fatalf("Persist: %v", err)
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got == nil {
		t.Fatal("Load: got nil, want non-nil Queue")
	}
	if got.DefaultHarness != core.AgentTypeCodex {
		t.Errorf("DefaultHarness after round-trip: got %q, want %q", got.DefaultHarness, core.AgentTypeCodex)
	}
}

// TestDefaultHarness_BackwardCompatLoad verifies that a queue.json file written
// before the DefaultHarness field existed (no default_harness key) loads with an
// empty DefaultHarness — the "no per-queue default" backward-compatible value.
func TestDefaultHarness_BackwardCompatLoad(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	// Hand-write a legacy envelope with NO default_harness key. schema_version
	// must be 1 so UnmarshalQueue accepts it.
	legacy := `{
		"schema_version": 1,
		"queue_id": "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0099",
		"name": "main",
		"submitted_at": "2026-06-01T00:00:00Z",
		"status": "active",
		"groups": [
			{
				"group_index": 0,
				"kind": "wave",
				"status": "pending",
				"items": [{"bead_id": "hk-legacy1", "status": "pending", "run_id": null, "appended_at": null}],
				"created_at": "2026-06-01T00:00:00Z",
				"started_at": null,
				"completed_at": null
			}
		]
	}`
	path := filepath.Join(projectDir, ".harmonik", "queues", "main.json")
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy queue file: %v", err)
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load legacy: %v", err)
	}
	if got == nil {
		t.Fatal("Load legacy: got nil, want non-nil Queue")
	}
	if got.DefaultHarness != core.AgentType("") {
		t.Errorf("legacy DefaultHarness: got %q, want empty", got.DefaultHarness)
	}
	// The legacy field-absent value must NOT be reported as Valid() so the
	// harness resolver falls through to the node/global tiers.
	if got.DefaultHarness.Valid() {
		t.Errorf("empty DefaultHarness reported Valid(); resolver tier-2 would wrongly fire")
	}
}

// TestDefaultHarness_OmitemptyMarshal verifies the default_harness key is
// omitted from the marshalled JSON when DefaultHarness is empty (omitempty),
// preserving byte-compatibility with pre-field consumers.
func TestDefaultHarness_OmitemptyMarshal(t *testing.T) {
	t.Parallel()

	q := typesFixtureQueue() // DefaultHarness left at its zero value ("")
	data, err := json.Marshal(&q)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := string(data); strings.Contains(got, "default_harness") {
		t.Errorf("empty DefaultHarness should be omitted; got JSON containing default_harness: %s", got)
	}

	q.DefaultHarness = core.AgentTypeCodex
	data2, err := json.Marshal(&q)
	if err != nil {
		t.Fatalf("Marshal (set): %v", err)
	}
	if got := string(data2); !strings.Contains(got, `"default_harness":"codex"`) {
		t.Errorf("set DefaultHarness should be present; got JSON: %s", got)
	}
}

// TestDefaultHarness_SubmitCarriesValidValue verifies a queue-submit request
// carrying a valid DefaultHarness lands that value on the persisted Queue and
// makes it readable as resolveHarness's tier-2 queueDefault.
func TestDefaultHarness_SubmitCarriesValidValue(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-dh001"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA)

	req := queue.QueueSubmitRequest{
		SchemaVersion:  1,
		DefaultHarness: core.AgentTypeCodex,
		Groups:         []queue.Group{rpcFixtureWaveGroup(0, beadA)},
	}

	_, q, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: unexpected RPCError: %v", rpcErr)
	}
	if q == nil {
		t.Fatal("returned *Queue is nil")
	}
	if q.DefaultHarness != core.AgentTypeCodex {
		t.Errorf("submitted DefaultHarness not carried: got %q, want %q", q.DefaultHarness, core.AgentTypeCodex)
	}
	// Must also be valid so tier 2 of resolveHarness would honour it.
	if !q.DefaultHarness.Valid() {
		t.Errorf("carried DefaultHarness %q not Valid()", q.DefaultHarness)
	}

	// And it must survive persistence to disk (the daemon reads the loaded queue).
	loaded, err := queue.Load(t.Context(), projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after submit: %v", err)
	}
	if loaded == nil || loaded.DefaultHarness != core.AgentTypeCodex {
		t.Errorf("persisted DefaultHarness: got %v, want %q", loaded, core.AgentTypeCodex)
	}
}

// TestDefaultHarness_SubmitNormalisesInvalid verifies that an invalid requested
// DefaultHarness is normalised to empty on the persisted Queue (treated as
// absent), so the harness resolver falls through to the node/global tiers rather
// than rejecting the whole submit.
func TestDefaultHarness_SubmitNormalisesInvalid(t *testing.T) {
	t.Parallel()

	const beadA core.BeadID = "hk-dh002"
	projectDir := rpcFixtureTempProjectDir(t)
	ledger := rpcFixtureOpenLedger(beadA)

	// "Codex" has an uppercase letter — fails the AR-025 regex, so it is invalid.
	req := queue.QueueSubmitRequest{
		SchemaVersion:  1,
		DefaultHarness: core.AgentType("Codex"),
		Groups:         []queue.Group{rpcFixtureWaveGroup(0, beadA)},
	}

	_, q, _, rpcErr := queue.HandleQueueSubmit(t.Context(), req, ledger, projectDir, 1)
	if rpcErr != nil {
		t.Fatalf("HandleQueueSubmit: invalid harness should be ignored, not rejected; got RPCError: %v", rpcErr)
	}
	if q == nil {
		t.Fatal("returned *Queue is nil")
	}
	if q.DefaultHarness != core.AgentType("") {
		t.Errorf("invalid DefaultHarness should normalise to empty; got %q", q.DefaultHarness)
	}
}

// TestNormaliseDefaultHarness covers the helper directly: valid values pass
// through verbatim; invalid/empty values normalise to empty.
func TestNormaliseDefaultHarness(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   core.AgentType
		want core.AgentType
	}{
		{"valid-codex", core.AgentTypeCodex, core.AgentTypeCodex},
		{"valid-claude-code", core.AgentTypeClaudeCode, core.AgentTypeClaudeCode},
		{"empty", core.AgentType(""), core.AgentType("")},
		{"uppercase-invalid", core.AgentType("Codex"), core.AgentType("")},
		{"single-char-invalid", core.AgentType("c"), core.AgentType("")},
		{"leading-digit-invalid", core.AgentType("1codex"), core.AgentType("")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := queue.NormaliseDefaultHarness(tc.in); got != tc.want {
				t.Errorf("NormaliseDefaultHarness(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
