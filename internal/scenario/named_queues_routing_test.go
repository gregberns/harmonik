package scenario

// named_queues_routing_test.go — SC1+SC5: two named queues both active; bare
// submit lands in main.
//
// Bead: hk-tigaf.9 (NQ-D2 scenario test)
// Spec refs:
//   - specs/queue-model.md §2.1 QM-002 (queue naming rule; empty→"main")
//   - specs/queue-model.md §2.9 (per-name queue files)
//   - specs/queue-model.md §3.1 QM-001 (atomic persist)
//   - specs/queue-model.md §8.1 QM-050 (submit sequence)
//   - specs/queue-model.md §8.9 QM-058 (queue-list: enumerate all active queues)
//
// SC1 scenario:
//  1. Submit --queue investigate: build an "investigate" Queue{Status:active},
//     persist it to .harmonik/queues/investigate.json.
//  2. Submit --queue main: build a "main" Queue{Status:active}, persist it to
//     .harmonik/queues/main.json.
//  3. queue list: HandleQueueList enumerates both; response contains exactly
//     two summaries — one for "investigate", one for "main", both active.
//
// SC5 scenario:
//  1. Bare submit (no --queue flag): Name = "" normalises to "main" per
//     NormaliseQueueName. Persist writes to .harmonik/queues/main.json.
//  2. HandleQueueList returns exactly one queue named "main".
//  3. Load(ctx, projectDir, "main") round-trips correctly — confirming that
//     the bare-submit queue is reachable by name and not silently lost.
//  4. "Existing single-queue scenario tests pass unchanged" is structural: the
//     above sub-cases confirm no regression in the per-name persistence path.
//
// These tests exercise the persistence layer and HandleQueueList directly —
// no live daemon, no event bus. Layer is identical to queue_paused_test.go
// and named_queues_pause_test.go.
//
// Helper prefix: namedQueuesRouting (implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

var namedQueuesRoutingNow = time.Date(2026, 5, 31, 12, 0, 0, 0, time.UTC)

const (
	namedQueuesRoutingInvestigateQueueID = "01906000-0020-7000-8000-000000000020"
	namedQueuesRoutingMainQueueID        = "01906000-0020-7000-8000-000000000021"
	namedQueuesRoutingBareQueueID        = "01906000-0020-7000-8000-000000000022"
)

// namedQueuesRoutingProjectDir creates a temporary project root pre-populated
// with .harmonik/ for queue.Persist / queue.Load / queue.HandleQueueList.
func namedQueuesRoutingProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("namedQueuesRoutingProjectDir: MkdirAll .harmonik: %v", err)
	}
	return dir
}

// namedQueuesRoutingInvestigateQueue returns the "investigate" fixture queue:
//   - group0 active, wave, 1 pending item (hk-sc1-inv-a)
func namedQueuesRoutingInvestigateQueue() queue.Queue {
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesRoutingInvestigateQueueID,
		Name:          "investigate",
		SubmittedAt:   namedQueuesRoutingNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc1-inv-a"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesRoutingNow,
			},
		},
	}
}

// namedQueuesRoutingMainQueue returns the "main" fixture queue:
//   - group0 active, wave, 2 pending items (hk-sc1-main-a, hk-sc1-main-b)
func namedQueuesRoutingMainQueue() queue.Queue {
	return queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesRoutingMainQueueID,
		Name:          "main",
		SubmittedAt:   namedQueuesRoutingNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc1-main-a"), Status: queue.ItemStatusPending},
					{BeadID: core.BeadID("hk-sc1-main-b"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesRoutingNow,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// SC1 — two named queues both active: queue list shows both
// ---------------------------------------------------------------------------

// TestNamedQueuesRouting_SC1_BothQueuesAppearInList verifies that after
// persisting "investigate" and "main" queues, HandleQueueList returns exactly
// two summaries — one per named queue — both with status=active.
//
// This is the canonical SC1 assertion: two concurrent named queues are visible
// in the list surface simultaneously.
//
// Spec ref: specs/queue-model.md §8.9 QM-058; §2.9 (per-name queue files).
func TestNamedQueuesRouting_SC1_BothQueuesAppearInList(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesRoutingProjectDir(t)
	ctx := context.Background()

	investigateQ := namedQueuesRoutingInvestigateQueue()
	mainQ := namedQueuesRoutingMainQueue()

	// Persist both queues to simulate --queue investigate + --queue main submits.
	if err := queue.Persist(ctx, projectDir, &investigateQ); err != nil {
		t.Fatalf("Persist(investigate): %v", err)
	}
	if err := queue.Persist(ctx, projectDir, &mainQ); err != nil {
		t.Fatalf("Persist(main): %v", err)
	}

	// queue list must enumerate both named queues.
	resp, rpcErr := queue.HandleQueueList(ctx, projectDir)
	if rpcErr != nil {
		t.Fatalf("HandleQueueList: unexpected RPCError: code=%d msg=%s", rpcErr.Code, rpcErr.Message)
	}

	if len(resp.Queues) != 2 {
		t.Fatalf("HandleQueueList: Queues len = %d, want 2 (investigate + main)", len(resp.Queues))
	}

	// Collect names for assertion — order is filesystem-dependent.
	byName := make(map[string]queue.QueueSummary, 2)
	for _, s := range resp.Queues {
		byName[s.Name] = s
	}

	inv, ok := byName["investigate"]
	if !ok {
		t.Errorf("HandleQueueList: no summary for queue %q; got names: %v", "investigate", queueSummaryNames(resp.Queues))
	} else {
		if inv.Status != queue.QueueStatusActive {
			t.Errorf("investigate.Status = %q, want active", inv.Status)
		}
		if inv.QueueID != namedQueuesRoutingInvestigateQueueID {
			t.Errorf("investigate.QueueID = %q, want %q", inv.QueueID, namedQueuesRoutingInvestigateQueueID)
		}
		if inv.PendingItems != 1 {
			t.Errorf("investigate.PendingItems = %d, want 1", inv.PendingItems)
		}
	}

	main, ok := byName["main"]
	if !ok {
		t.Errorf("HandleQueueList: no summary for queue %q; got names: %v", "main", queueSummaryNames(resp.Queues))
	} else {
		if main.Status != queue.QueueStatusActive {
			t.Errorf("main.Status = %q, want active", main.Status)
		}
		if main.QueueID != namedQueuesRoutingMainQueueID {
			t.Errorf("main.QueueID = %q, want %q", main.QueueID, namedQueuesRoutingMainQueueID)
		}
		if main.PendingItems != 2 {
			t.Errorf("main.PendingItems = %d, want 2", main.PendingItems)
		}
	}
}

// TestNamedQueuesRouting_SC1_EachQueueLoadableByName verifies that after
// persisting both queues, each is independently loadable by name. This
// confirms the per-name file isolation (§2.9): "investigate" and "main" do
// not overwrite each other's on-disk state.
//
// Spec ref: specs/queue-model.md §2.9 (per-name queue files); §3.2 QM-002.
func TestNamedQueuesRouting_SC1_EachQueueLoadableByName(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesRoutingProjectDir(t)
	ctx := context.Background()

	investigateQ := namedQueuesRoutingInvestigateQueue()
	mainQ := namedQueuesRoutingMainQueue()

	if err := queue.Persist(ctx, projectDir, &investigateQ); err != nil {
		t.Fatalf("Persist(investigate): %v", err)
	}
	if err := queue.Persist(ctx, projectDir, &mainQ); err != nil {
		t.Fatalf("Persist(main): %v", err)
	}

	// Load "investigate" — must return the correct queue.
	loadedInv, err := queue.Load(ctx, projectDir, "investigate")
	if err != nil {
		t.Fatalf("Load(investigate): %v", err)
	}
	if loadedInv == nil {
		t.Fatal("Load(investigate): returned nil; want active investigate queue")
	}
	if loadedInv.QueueID != namedQueuesRoutingInvestigateQueueID {
		t.Errorf("loaded investigate.QueueID = %q, want %q",
			loadedInv.QueueID, namedQueuesRoutingInvestigateQueueID)
	}
	if loadedInv.Name != "investigate" {
		t.Errorf("loaded investigate.Name = %q, want %q", loadedInv.Name, "investigate")
	}

	// Load "main" — must return the correct queue without contamination from investigate.
	loadedMain, err := queue.Load(ctx, projectDir, "main")
	if err != nil {
		t.Fatalf("Load(main): %v", err)
	}
	if loadedMain == nil {
		t.Fatal("Load(main): returned nil; want active main queue")
	}
	if loadedMain.QueueID != namedQueuesRoutingMainQueueID {
		t.Errorf("loaded main.QueueID = %q, want %q",
			loadedMain.QueueID, namedQueuesRoutingMainQueueID)
	}
	if loadedMain.Name != "main" {
		t.Errorf("loaded main.Name = %q, want %q", loadedMain.Name, "main")
	}

	// Item counts must be per-queue, not mixed.
	if len(loadedInv.Groups[0].Items) != 1 {
		t.Errorf("loaded investigate group0 items = %d, want 1", len(loadedInv.Groups[0].Items))
	}
	if len(loadedMain.Groups[0].Items) != 2 {
		t.Errorf("loaded main group0 items = %d, want 2", len(loadedMain.Groups[0].Items))
	}
}

// TestNamedQueuesRouting_SC1_PersistInvestigateDoesNotOverwriteMain verifies
// that persisting "investigate" leaves "main" unaffected and vice versa.
// Overwrites the investigate queue after main is already on disk and confirms
// main is still intact.
//
// Spec ref: specs/queue-model.md §2.9 (per-name queue files).
func TestNamedQueuesRouting_SC1_PersistInvestigateDoesNotOverwriteMain(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesRoutingProjectDir(t)
	ctx := context.Background()

	mainQ := namedQueuesRoutingMainQueue()
	if err := queue.Persist(ctx, projectDir, &mainQ); err != nil {
		t.Fatalf("Persist(main): %v", err)
	}

	// Now persist "investigate" — must not touch main.json.
	investigateQ := namedQueuesRoutingInvestigateQueue()
	if err := queue.Persist(ctx, projectDir, &investigateQ); err != nil {
		t.Fatalf("Persist(investigate): %v", err)
	}

	// Reload main and verify its state is unchanged.
	loadedMain, err := queue.Load(ctx, projectDir, "main")
	if err != nil {
		t.Fatalf("Load(main) after investigate persist: %v", err)
	}
	if loadedMain == nil {
		t.Fatal("Load(main): returned nil after investigate persist — main was unexpectedly removed")
	}
	if loadedMain.QueueID != namedQueuesRoutingMainQueueID {
		t.Errorf("main.QueueID = %q after investigate persist, want %q (queue was overwritten)",
			loadedMain.QueueID, namedQueuesRoutingMainQueueID)
	}
	if len(loadedMain.Groups[0].Items) != 2 {
		t.Errorf("main group0 items = %d after investigate persist, want 2",
			len(loadedMain.Groups[0].Items))
	}
}

// ---------------------------------------------------------------------------
// SC5 — bare submit (no --queue flag) lands in main
// ---------------------------------------------------------------------------

// TestNamedQueuesRouting_SC5_BareSubmitNormalisesToMain verifies that
// NormaliseQueueName("") returns "main" — the semantic contract for bare
// submits (no --queue flag). This is the routing rule that maps an empty
// queue name to the default "main" queue.
//
// Spec ref: specs/queue-model.md §2.1 QM-002 (empty name → "main").
func TestNamedQueuesRouting_SC5_BareSubmitNormalisesToMain(t *testing.T) {
	t.Parallel()

	got := queue.NormaliseQueueName("")
	if got != queue.QueueNameMain {
		t.Errorf("NormaliseQueueName(\"\") = %q, want %q (SC5: bare submit must route to main)",
			got, queue.QueueNameMain)
	}
}

// TestNamedQueuesRouting_SC5_BareSubmitPersistsAsMain verifies that a queue
// persisted with Name="" (the bare-submit path) lands in the "main" slot and
// is returned by HandleQueueList as "main".
//
// Spec ref: specs/queue-model.md §3.1 QM-001 (atomic persist); §2.1 QM-002.
func TestNamedQueuesRouting_SC5_BareSubmitPersistsAsMain(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesRoutingProjectDir(t)
	ctx := context.Background()

	// Bare-submit queue: Name is empty — Persist normalises it to "main".
	bareQ := queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesRoutingBareQueueID,
		Name:          "", // intentionally empty — the bare-submit case
		SubmittedAt:   namedQueuesRoutingNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc5-bare-a"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesRoutingNow,
			},
		},
	}

	if err := queue.Persist(ctx, projectDir, &bareQ); err != nil {
		t.Fatalf("Persist(bare, Name=\"\"): %v", err)
	}

	// HandleQueueList must return exactly one queue named "main".
	resp, rpcErr := queue.HandleQueueList(ctx, projectDir)
	if rpcErr != nil {
		t.Fatalf("HandleQueueList: unexpected RPCError: code=%d msg=%s", rpcErr.Code, rpcErr.Message)
	}

	if len(resp.Queues) != 1 {
		t.Fatalf("HandleQueueList: Queues len = %d, want 1 (bare submit routes to main only)", len(resp.Queues))
	}
	if resp.Queues[0].Name != "main" {
		t.Errorf("bare submit: queue name = %q, want %q (SC5: empty name must route to main)",
			resp.Queues[0].Name, "main")
	}
	if resp.Queues[0].QueueID != namedQueuesRoutingBareQueueID {
		t.Errorf("bare submit: queue_id = %q, want %q", resp.Queues[0].QueueID, namedQueuesRoutingBareQueueID)
	}
}

// TestNamedQueuesRouting_SC5_BareSubmitLoadableByMainName verifies that a
// queue persisted with Name="" is loadable by the name "main". This confirms
// the round-trip: bare submit → Persist(Name="") → Load("main") → same queue.
//
// Spec ref: specs/queue-model.md §3.2 QM-002 (Load reads by normalised name).
func TestNamedQueuesRouting_SC5_BareSubmitLoadableByMainName(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesRoutingProjectDir(t)
	ctx := context.Background()

	bareQ := queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesRoutingBareQueueID,
		Name:          "", // bare submit
		SubmittedAt:   namedQueuesRoutingNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc5-bare-b"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesRoutingNow,
			},
		},
	}

	if err := queue.Persist(ctx, projectDir, &bareQ); err != nil {
		t.Fatalf("Persist(bare, Name=\"\"): %v", err)
	}

	// Load must succeed using "main" as the name.
	loaded, err := queue.Load(ctx, projectDir, "main")
	if err != nil {
		t.Fatalf("Load(\"main\") after bare Persist: %v", err)
	}
	if loaded == nil {
		t.Fatal("Load(\"main\"): returned nil after bare persist; want active queue")
	}
	if loaded.QueueID != namedQueuesRoutingBareQueueID {
		t.Errorf("loaded.QueueID = %q, want %q (bare submit must be reachable by name main)",
			loaded.QueueID, namedQueuesRoutingBareQueueID)
	}
}

// TestNamedQueuesRouting_SC5_OnlyMainPresentAfterBareSubmit verifies that a
// bare submit does NOT create a spurious second queue. After a single bare
// submit, HandleQueueList returns exactly one queue.
//
// This is the "existing single-queue scenario tests pass unchanged" sub-case:
// bare-submit behaviour must not introduce an unexpected named-queue side-effect.
//
// Spec ref: specs/queue-model.md §8.9 QM-058; §2.1 QM-002.
func TestNamedQueuesRouting_SC5_OnlyMainPresentAfterBareSubmit(t *testing.T) {
	t.Parallel()

	projectDir := namedQueuesRoutingProjectDir(t)
	ctx := context.Background()

	bareQ := queue.Queue{
		SchemaVersion: 1,
		QueueID:       namedQueuesRoutingBareQueueID,
		Name:          "",
		SubmittedAt:   namedQueuesRoutingNow,
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: core.BeadID("hk-sc5-bare-c"), Status: queue.ItemStatusPending},
				},
				CreatedAt: namedQueuesRoutingNow,
			},
		},
	}

	if err := queue.Persist(ctx, projectDir, &bareQ); err != nil {
		t.Fatalf("Persist(bare): %v", err)
	}

	resp, rpcErr := queue.HandleQueueList(ctx, projectDir)
	if rpcErr != nil {
		t.Fatalf("HandleQueueList: unexpected RPCError: code=%d msg=%s", rpcErr.Code, rpcErr.Message)
	}

	// Exactly one queue — bare submit must not split into two queue files.
	if len(resp.Queues) != 1 {
		t.Errorf("HandleQueueList after bare submit: %d queues, want 1; names: %v",
			len(resp.Queues), queueSummaryNames(resp.Queues))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// queueSummaryNames extracts just the Name field from each QueueSummary for
// use in failure messages.
func queueSummaryNames(summaries []queue.QueueSummary) []string {
	names := make([]string, len(summaries))
	for i, s := range summaries {
		names[i] = s.Name
	}
	return names
}
