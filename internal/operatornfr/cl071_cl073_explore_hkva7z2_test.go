package operatornfr_test

// cl071_cl073_explore_hkva7z2_test.go — exploratory probe for
// specs/cognition-loop.md CL-071 (eager pure-code refill) and CL-073
// (wake-LLM only at empty-queue boundary).
//
// Observable behaviors this fixture documents:
//
//  1. Spec text: CL-071, CL-073, CL-072, and acceptance scenario 7 are present
//     in specs/cognition-loop.md with the key normative phrases.
//
//  2. Stream-only dispatch surface: AppendItems accepts a stream group and
//     returns a queue_appended event (the concrete dispatch step CL-071
//     requires). A wave group rejects the append with a validation error
//     (QM-040 / CL-071: "wave groups reject the append this requirement issues").
//
//  3. CL-072 pre-screen guard table: the four named guards are present in the
//     spec and their ordering is documented by the fixture.
//
//  4. Three-tier wake filter (CL-061): run_completed is classified as
//     "deterministic" (no model wake); run_failed is "wake-LLM". The fixture
//     documents the CL-071 / CL-073 division between the two tiers.
//
// This is a spec-artifact existence and structural-constraint probe. Runtime
// cognition-loop enforcement lives in the Pi harness (CL-100); this file is
// the sensor verifying the spec text encodes the eager-refill contract
// correctly and that the queue-append surface it relies on behaves as CL-071
// specifies.
//
// Spec refs:
//   - specs/cognition-loop.md §4.9 CL-071, CL-072, CL-073
//   - specs/cognition-loop.md §4.8 CL-061 (three-tier wake filter)
//   - specs/cognition-loop.md §7 acceptance scenario 7
//   - specs/queue-model.md §7 QM-040 (stream-only append target)
//
// Bead: hk-va7z2

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ---------------------------------------------------------------------------
// eagerRefillFixture — structural types
// ---------------------------------------------------------------------------

// eagerRefillFixturePreScreenGuard models one entry in the CL-072 pre-screen
// guard table. Applied in order; the first matching guard causes the candidate
// bead to be skipped.
//
// Spec ref: cognition-loop.md §4.9 CL-072 — "Apply in order; skip on hit."
type eagerRefillFixturePreScreenGuard struct {
	Rank       int    // 1-based guard order (CL-072 application order)
	Name       string // canonical guard name from CL-072
	SkipReason string // dispatch-log skip_reason value for this guard
	SpecPhrase string // key phrase that MUST appear in cognition-loop.md CL-072
}

// eagerRefillFixtureGuardTable is the authoritative fixture encoding of the
// CL-072 pre-screen guards.
//
// The guard order is normative per CL-072: guards are applied in rank order
// and the first matching guard causes the candidate to be skipped. The
// harness MUST NOT skip guard 4 because guard 3 was not evaluated; every
// guard runs until one fires.
//
// Spec ref: cognition-loop.md §4.9 CL-072.
var eagerRefillFixtureGuardTable = []eagerRefillFixturePreScreenGuard{
	{
		Rank:       1,
		Name:       "already-in-queue",
		SkipReason: "already_in_queue",
		SpecPhrase: "Already in queue",
	},
	{
		Rank:       2,
		Name:       "already-landed",
		SkipReason: "already_landed",
		SpecPhrase: "Already landed",
	},
	{
		Rank:       3,
		Name:       "failed-twice-this-session",
		SkipReason: "failed_twice_this_session",
		SpecPhrase: "Failed twice this session",
	},
	{
		Rank:       4,
		Name:       "conflicts-with-in-flight",
		SkipReason: "conflicts_with_in_flight",
		SpecPhrase: "Conflicts with in-flight",
	},
}

// eagerRefillFixtureWakeTier models one entry in the CL-061 three-tier wake
// filter as it applies to the CL-071 eager-refill path.
//
// Spec ref: cognition-loop.md §4.8 CL-061.
type eagerRefillFixtureWakeTier struct {
	EventType    string // event.type value from harmonik subscribe
	Tier         string // "ignore" | "deterministic" | "wake-llm"
	TriggersCL71 bool   // whether this event type triggers the CL-071 eager-refill path
	SpecPhrase   string // key phrase from CL-061 describing this event's tier
}

// eagerRefillFixtureWakeFilter is the partial wake-filter table relevant to
// CL-071/CL-073. Only the three event types that affect queue pressure are
// listed; the full wake-filter table lives in CL-061.
//
// run_completed{success} → deterministic tier → harness runs eager-refill in
// pure code without waking the model (CL-071, CL-013).
//
// run_failed / run_canceled → wake-LLM tier (CL-061 §4.8: "run_failed" is
// listed under Wake-LLM). The CL-071 eager-refill sub-path executes as part
// of the Wake-LLM handler — the refill itself is mechanism-tagged (CL-013),
// but the enclosing tier stays Wake-LLM because the harness also presents
// failure context to the model for investigation. TriggersCL71 = true
// captures that the slot is freed and the refill sub-path fires; Tier =
// "wake-llm" captures the CL-061 classification of the enclosing event.
//
// Spec ref: cognition-loop.md §4.8 CL-061, §4.9 CL-071.
var eagerRefillFixtureWakeFilter = []eagerRefillFixtureWakeTier{
	{
		EventType:    "run_completed",
		Tier:         "deterministic",
		TriggersCL71: true,
		SpecPhrase:   "run_completed{success}",
	},
	{
		EventType:    "run_failed",
		Tier:         "wake-llm",
		TriggersCL71: true,
		SpecPhrase:   "run_failed",
	},
	{
		EventType:    "run_canceled",
		Tier:         "wake-llm",
		TriggersCL71: true,
		SpecPhrase:   "run_completed",
	},
}

// ---------------------------------------------------------------------------
// eagerRefillFixtureStubLedger — minimal BeadLedger fake for append tests
// ---------------------------------------------------------------------------

// eagerRefillFixtureStubLedger is a minimal BeadLedger fake for CL-071
// dispatch-surface tests. All listed IDs return BeadStatusOpen; unknown IDs
// return BeadStatusNotFound. No edges are defined (no ledger-dep deferrals).
type eagerRefillFixtureStubLedger struct {
	openIDs map[core.BeadID]struct{}
}

func (l *eagerRefillFixtureStubLedger) LookupStatus(_ context.Context, id core.BeadID) (queue.BeadStatus, error) {
	if _, ok := l.openIDs[id]; ok {
		return queue.BeadStatusOpen, nil
	}
	return queue.BeadStatusNotFound, nil
}

func (l *eagerRefillFixtureStubLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// eagerRefillFixtureOpenLedger returns a fake ledger with all given IDs open.
func eagerRefillFixtureOpenLedger(ids ...string) *eagerRefillFixtureStubLedger {
	m := make(map[core.BeadID]struct{}, len(ids))
	for _, id := range ids {
		m[core.BeadID(id)] = struct{}{}
	}
	return &eagerRefillFixtureStubLedger{openIDs: m}
}

// eagerRefillFixtureStreamQueue builds a minimal active stream-group Queue for
// append tests. The group has one pre-existing dispatched item so that
// subsequent CL-071 eager-appends extend a live stream mid-flight (QM-043).
func eagerRefillFixtureStreamQueue(t *testing.T) *queue.Queue {
	t.Helper()

	runID := "019e8008-0308-7d94-87ff-000000000001"
	now := time.Now().UTC()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019e8008-0308-7d94-87ff-cl071-stream",
		Status:        queue.QueueStatusActive,
		SubmittedAt:   now,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: "hk-prefill-01",
						Status: queue.ItemStatusDispatched,
						RunID:  &runID,
					},
					{
						BeadID:     "hk-prefill-02",
						Status:     queue.ItemStatusPending,
						AppendedAt: &now,
					},
				},
			},
		},
	}
	return q
}

// eagerRefillFixtureWaveQueue builds a minimal active wave-group Queue. CL-071
// specifies that the curated-dispatch path targets a stream group and that wave
// groups reject the append; this fixture is the negative-case target.
func eagerRefillFixtureWaveQueue(t *testing.T) *queue.Queue {
	t.Helper()

	now := time.Now().UTC()
	q := &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "019e8008-0308-7d94-87ff-cl071-wave",
		Status:        queue.QueueStatusActive,
		SubmittedAt:   now,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindWave,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{
						BeadID: "hk-wave-01",
						Status: queue.ItemStatusPending,
					},
				},
			},
		},
	}
	return q
}

// ---------------------------------------------------------------------------
// readCognitionLoopSpec — helper
// ---------------------------------------------------------------------------

func eagerRefillFixtureReadCognitionLoopSpec(t *testing.T) []byte {
	t.Helper()
	root := obligationsFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "cognition-loop.md")
	//nolint:gosec // G304: path from runtime.Caller source location, not user input
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("eagerRefillFixtureReadCognitionLoopSpec: cannot read %s: %v", specPath, err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Tests — spec text existence
// ---------------------------------------------------------------------------

// TestCL071_EagerRefillSpecSectionExists verifies CL-071 is present in
// specs/cognition-loop.md with its key normative phrases.
//
// Spec ref: cognition-loop.md §4.9 CL-071.
func TestCL071_EagerRefillSpecSectionExists(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "CL-071") {
		t.Error("CL-071: cognition-loop.md does not contain 'CL-071'")
	}
	if !strings.Contains(content, "Eager pure-code refill on slot release") {
		t.Error("CL-071: cognition-loop.md missing 'Eager pure-code refill on slot release' heading")
	}
	if !strings.Contains(content, "kerf next --format=json --only=bead") {
		t.Error("CL-071: cognition-loop.md missing 'kerf next --format=json --only=bead' concrete shell invocation")
	}
	if !strings.Contains(content, "harmonik queue append") {
		t.Error("CL-071: cognition-loop.md missing 'harmonik queue append' dispatch verb")
	}
}

// TestCL071_MechanismTaggedInSpec verifies CL-071 is declared mechanism-tagged
// (CL-013 / CL-INV-001): kerf next, CL-072 guards, and queue append MUST NOT
// round-trip the model.
//
// Spec ref: cognition-loop.md §4.9 CL-071 — "Mechanism-tagged (CL-013, CL-INV-001)."
func TestCL071_MechanismTaggedInSpec(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "Mechanism-tagged") {
		t.Error("CL-071: cognition-loop.md missing 'Mechanism-tagged' tag in CL-071 section")
	}
	if !strings.Contains(content, "MUST NOT round-trip the model") {
		t.Error("CL-071: cognition-loop.md missing 'MUST NOT round-trip the model' constraint")
	}
}

// TestCL071_StreamGroupTargetStatedInSpec verifies CL-071 specifies the stream
// group as the dispatch target and references the queue-model's QM-040 surface.
//
// Spec ref: cognition-loop.md §4.9 CL-071 — "wave groups reject the append
// this requirement issues."
func TestCL071_StreamGroupTargetStatedInSpec(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "stream") {
		t.Error("CL-071: cognition-loop.md missing 'stream' group type reference")
	}
	if !strings.Contains(content, "wave groups reject") {
		t.Error("CL-071: cognition-loop.md missing 'wave groups reject' statement — stream-only dispatch surface not stated")
	}
}

// TestCL072_PreScreenGuardsSpecSectionExists verifies CL-072 is present in
// cognition-loop.md with all four pre-screen guards.
//
// Spec ref: cognition-loop.md §4.9 CL-072.
func TestCL072_PreScreenGuardsSpecSectionExists(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "CL-072") {
		t.Fatal("CL-072: cognition-loop.md does not contain 'CL-072'")
	}

	for _, g := range eagerRefillFixtureGuardTable {
		if !strings.Contains(content, g.SpecPhrase) {
			t.Errorf("CL-072: guard %d (%s): spec phrase %q missing from cognition-loop.md",
				g.Rank, g.Name, g.SpecPhrase)
		}
	}
}

// TestCL072_GuardTableIsOrdered verifies the eagerRefillFixtureGuardTable
// fixture encodes the four guards in strictly ascending rank order (1..4).
//
// Spec ref: cognition-loop.md §4.9 CL-072 — "Apply in order; skip on hit."
func TestCL072_GuardTableIsOrdered(t *testing.T) {
	t.Parallel()

	if len(eagerRefillFixtureGuardTable) != 4 {
		t.Fatalf("CL-072: guard table has %d entries, want 4 (one per CL-072 guard)",
			len(eagerRefillFixtureGuardTable))
	}

	for i, g := range eagerRefillFixtureGuardTable {
		wantRank := i + 1
		if g.Rank != wantRank {
			t.Errorf("CL-072: guard at index %d has Rank=%d, want %d (guards must be listed in application order)",
				i, g.Rank, wantRank)
		}
	}
}

// TestCL073_EmptyQueueBoundarySpecSectionExists verifies CL-073 is present in
// cognition-loop.md with the key "empty-queue" wake-LLM boundary phrase.
//
// Spec ref: cognition-loop.md §4.9 CL-073.
func TestCL073_EmptyQueueBoundarySpecSectionExists(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "CL-073") {
		t.Fatal("CL-073: cognition-loop.md does not contain 'CL-073'")
	}
	if !strings.Contains(content, "empty-queue boundary") {
		t.Error("CL-073: cognition-loop.md missing 'empty-queue boundary' phrase")
	}
	if !strings.Contains(content, "kerf next") {
		t.Error("CL-073: cognition-loop.md missing 'kerf next' reference in CL-073 section")
	}
}

// TestCL073_SpeculativeBeadGenerationForbiddenInSpec verifies CL-073 explicitly
// forbids speculative bead generation in the eager-refill path.
//
// Spec ref: cognition-loop.md §4.9 CL-073 — "Speculative bead generation in
// eager-refill is FORBIDDEN."
func TestCL073_SpeculativeBeadGenerationForbiddenInSpec(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "Speculative bead generation") {
		t.Error("CL-073: cognition-loop.md missing 'Speculative bead generation' constraint")
	}
	if !strings.Contains(content, "FORBIDDEN") {
		t.Error("CL-073: cognition-loop.md missing 'FORBIDDEN' keyword for speculative-generation prohibition")
	}
}

// TestCL071_AcceptanceScenario7InSpec verifies acceptance scenario 7 is
// present in cognition-loop.md §7 — the harness appends the next ranked bead
// via harmonik queue append WITHOUT waking the model.
//
// Spec ref: cognition-loop.md §7 — scenario 7 (CL-071, CL-073).
func TestCL071_AcceptanceScenario7InSpec(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "harmonik queue append") {
		t.Error("scenario 7: cognition-loop.md missing 'harmonik queue append' in acceptance scenario 7")
	}
	if !strings.Contains(content, "WITHOUT waking the model") {
		t.Error("scenario 7: cognition-loop.md missing 'WITHOUT waking the model' phrase in scenario 7")
	}
}

// ---------------------------------------------------------------------------
// Tests — CL-061 wake-filter fixture
// ---------------------------------------------------------------------------

// TestCL061_WakeFilterClassifiesRunCompletedDeterministic verifies the fixture
// table classifies run_completed as "deterministic" (not wake-LLM), consistent
// with CL-061 and CL-071's mechanism-tag obligation.
//
// Spec ref: cognition-loop.md §4.8 CL-061 — "run_completed{success} for
// expected completion (triggers deterministic-refill)."
func TestCL061_WakeFilterClassifiesRunCompletedDeterministic(t *testing.T) {
	t.Parallel()

	for _, row := range eagerRefillFixtureWakeFilter {
		if row.EventType == "run_completed" {
			if row.Tier != "deterministic" {
				t.Errorf("CL-061: run_completed tier = %q, want 'deterministic' (CL-071 mechanism-tagged)", row.Tier)
			}
			if !row.TriggersCL71 {
				t.Error("CL-061: run_completed.TriggersCL71 = false, want true (slot-release triggers eager-refill)")
			}
			return
		}
	}
	t.Fatal("CL-061: run_completed not found in eagerRefillFixtureWakeFilter table")
}

// TestCL061_WakeFilterRunFailedIsWakeLLM verifies the fixture classifies
// run_failed as wake-LLM tier (CL-061) and as a slot-release trigger for
// CL-071 eager-refill.
//
// The tier is wake-LLM because failures require model investigation; the
// refill sub-path within the handler is mechanism-tagged (CL-013/CL-071).
//
// Spec ref: cognition-loop.md §4.8 CL-061 — run_failed listed under Wake-LLM;
// §4.9 CL-071 — "On run_completed/run_failed/run_canceled, harness MUST refill
// eagerly."
func TestCL061_WakeFilterRunFailedIsWakeLLM(t *testing.T) {
	t.Parallel()

	for _, row := range eagerRefillFixtureWakeFilter {
		if row.EventType == "run_failed" {
			if row.Tier != "wake-llm" {
				t.Errorf("CL-061: run_failed Tier = %q, want 'wake-llm' (CL-061 §4.8)", row.Tier)
			}
			if !row.TriggersCL71 {
				t.Error("CL-061: run_failed.TriggersCL71 = false, want true (slot-release triggers eager-refill sub-path)")
			}
			return
		}
	}
	t.Fatal("CL-061: run_failed not found in eagerRefillFixtureWakeFilter table")
}

// TestCL061_WakeFilterRunCanceledIsWakeLLM verifies the fixture classifies
// run_canceled as wake-LLM tier (CL-061) and as a slot-release trigger for
// CL-071 eager-refill.
//
// Spec ref: cognition-loop.md §4.8 CL-061 — new types default to Wake-LLM;
// §4.9 CL-071 — "On run_completed/run_failed/run_canceled, harness MUST refill
// eagerly."
func TestCL061_WakeFilterRunCanceledIsWakeLLM(t *testing.T) {
	t.Parallel()

	for _, row := range eagerRefillFixtureWakeFilter {
		if row.EventType == "run_canceled" {
			if row.Tier != "wake-llm" {
				t.Errorf("CL-061: run_canceled Tier = %q, want 'wake-llm' (CL-061 §4.8 new-types-default-wake-llm)", row.Tier)
			}
			if !row.TriggersCL71 {
				t.Error("CL-061: run_canceled.TriggersCL71 = false, want true (slot-release triggers eager-refill sub-path)")
			}
			return
		}
	}
	t.Fatal("CL-061: run_canceled not found in eagerRefillFixtureWakeFilter table")
}

// ---------------------------------------------------------------------------
// Tests — concrete dispatch surface probe (AppendItems / QM-040 / CL-071)
// ---------------------------------------------------------------------------

// TestCL071_StreamGroupAcceptsAppend probes that AppendItems succeeds on an
// active stream group — the concrete dispatch step CL-071 issues via
// `harmonik queue append`.
//
// Finding: AppendItems on a stream group returns a queue_appended event and
// the appended bead appears at the tail of the group's items list.
//
// Spec ref: cognition-loop.md §4.9 CL-071; queue-model.md §7 QM-040.
func TestCL071_StreamGroupAcceptsAppend(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	q := eagerRefillFixtureStreamQueue(t)
	ledger := eagerRefillFixtureOpenLedger("hk-refill-01")

	preLen := len(q.Groups[0].Items)

	_, evts, err := queue.AppendItems(ctx, q, 0, []string{"hk-refill-01"}, ledger)
	if err != nil {
		t.Fatalf("CL-071: AppendItems on stream group: unexpected error: %v", err)
	}

	// CL-071 / QM-042: queue_appended event MUST be emitted.
	if len(evts) == 0 {
		t.Fatal("CL-071: AppendItems returned no events; queue_appended event expected")
	}
	if evts[0].Type != "queue_appended" {
		t.Errorf("CL-071: first event type = %q, want 'queue_appended'", evts[0].Type)
	}

	// CL-071 / QM-041: appended bead lands at the tail.
	postLen := len(q.Groups[0].Items)
	if postLen != preLen+1 {
		t.Errorf("CL-071: after append, group items len = %d, want %d (pre+1)", postLen, preLen+1)
	}
	tail := q.Groups[0].Items[postLen-1]
	if string(tail.BeadID) != "hk-refill-01" {
		t.Errorf("CL-071: tail bead_id = %q, want 'hk-refill-01'", tail.BeadID)
	}
	if tail.Status != queue.ItemStatusPending {
		t.Errorf("CL-071: tail item status = %q, want 'pending' (QM-041)", tail.Status)
	}
	if tail.AppendedAt == nil {
		t.Error("CL-071: tail item AppendedAt is nil, want non-nil (QM-041 appended_at stamp)")
	}
}

// TestCL071_WaveGroupRejectsAppend probes that AppendItems rejects an append
// to a wave group — matching CL-071's explicit statement that "wave groups
// reject the append this requirement issues" (QM-040).
//
// Finding: AppendItems on a wave group returns a ValidationError.
//
// Spec ref: cognition-loop.md §4.9 CL-071; queue-model.md §7 QM-040.
func TestCL071_WaveGroupRejectsAppend(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	q := eagerRefillFixtureWaveQueue(t)
	ledger := eagerRefillFixtureOpenLedger("hk-wave-refill-01")

	_, _, err := queue.AppendItems(ctx, q, 0, []string{"hk-wave-refill-01"}, ledger)
	if err == nil {
		t.Fatal("CL-071: AppendItems on wave group returned nil error; expected ValidationError (QM-040 stream-only)")
	}
	if !queue.IsValidationError(err) {
		t.Errorf("CL-071: AppendItems on wave group: error type is not ValidationError: %v", err)
	}
}

// TestCL071_DispatchSurfaceIsStreamNotWave verifies the fixture queues encode
// the correct kind values for the stream (CL-071 target) and wave (CL-071
// rejected) cases.
//
// This is a fixture-consistency check, not a runtime assertion.
func TestCL071_DispatchSurfaceIsStreamNotWave(t *testing.T) {
	t.Parallel()

	streamQ := eagerRefillFixtureStreamQueue(t)
	waveQ := eagerRefillFixtureWaveQueue(t)

	if streamQ.Groups[0].Kind != queue.GroupKindStream {
		t.Errorf("CL-071: stream fixture group Kind = %q, want %q", streamQ.Groups[0].Kind, queue.GroupKindStream)
	}
	if waveQ.Groups[0].Kind != queue.GroupKindWave {
		t.Errorf("CL-071: wave fixture group Kind = %q, want %q", waveQ.Groups[0].Kind, queue.GroupKindWave)
	}
}

// TestCL071_DispatchLogEntryPhraseInSpec verifies CL-072 specifies the
// dispatch-log file at .harmonik/cognition/dispatch-log.jsonl and defines the
// per-skip entry schema.
//
// Spec ref: cognition-loop.md §4.9 CL-072 — "Every skip appends to
// .harmonik/cognition/dispatch-log.jsonl."
func TestCL071_DispatchLogEntryPhraseInSpec(t *testing.T) {
	t.Parallel()

	content := string(eagerRefillFixtureReadCognitionLoopSpec(t))

	if !strings.Contains(content, "dispatch-log.jsonl") {
		t.Error("CL-072: cognition-loop.md missing 'dispatch-log.jsonl' dispatch-log file path")
	}
	if !strings.Contains(content, "dispatch_intent") {
		t.Error("CL-071: cognition-loop.md missing 'dispatch_intent' idempotency key for dispatch log")
	}
}
