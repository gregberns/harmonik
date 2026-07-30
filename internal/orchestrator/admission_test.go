package orchestrator

// admission_test.go — the admission table's ORDER, STAGES and operator text.
//
// These tests are in-package on purpose. The table is the contract this fold
// creates, and the table is unexported, so the tests that pin it live beside it.
//
// Each test names the property it holds. None of them re-states a gate's boolean
// in a second place: they drive the entry points and assert the verdict an
// operator or the daemon would see.

import (
	"errors"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Stage placement
// ─────────────────────────────────────────────────────────────────────────────

// TestAdmissionStagePlacement pins which gate sits at which stage. A gate that
// changes stage changes when the daemon can evaluate it, and every ordering bug
// this fold exists to prevent starts with a stage change.
func TestAdmissionStagePlacement(t *testing.T) {
	t.Parallel()

	want := map[string]Stage{
		"split-capacity":    StageTick,
		"decision-required": StageBeforeLookup,
		"sentinel-queue":    StageBeforeLookup,
		"greenlight":        StageAfterLookup,
		"local-only-cap":    StageBeforeStamp,
	}
	if len(admissionGates) != len(want) {
		t.Fatalf("admissionGates has %d rows, want %d — a gate was added or removed without updating this test", len(admissionGates), len(want))
	}
	for _, g := range admissionGates {
		w, ok := want[g.name]
		if !ok {
			t.Errorf("unexpected gate %q in the table", g.name)
			continue
		}
		if g.stage != w {
			t.Errorf("gate %q is at stage %q, want %q", g.name, g.stage, w)
		}
	}
}

// TestGreenlightIsQueuePathOnly pins the path split. The br-ready path filters
// the needs-greenlight label at adapter read time, so running the gate there
// would double-gate one path and change nothing on the other.
func TestGreenlightIsQueuePathOnly(t *testing.T) {
	t.Parallel()

	held := AdmissionInput{
		Path:             PathBrReady,
		BeadID:           "hk-aaa",
		BeadRecordLoaded: true,
		BeadLabels:       []string{LabelNeedsGreenlight},
	}
	v, err := AdmitAfterLookup(held)
	if err != nil {
		t.Fatalf("AdmitAfterLookup on the br-ready path: unexpected error %v", err)
	}
	if !v.Admitted {
		t.Fatalf("br-ready path held on %s, want admit: the greenlight gate is queue-path only", v.Reason)
	}

	// Positive control on the SAME labels: the queue path must hold.
	held.Path = PathQueue
	v, err = AdmitAfterLookup(held)
	if err != nil {
		t.Fatalf("AdmitAfterLookup on the queue path: unexpected error %v", err)
	}
	if v.Admitted || v.Reason != ReasonNeedsGreenlight {
		t.Fatalf("queue path admitted a needs-greenlight bead: verdict %+v", v)
	}
}

// TestLocalOnlyCapIsQueuePathOnly is the same path split for the pre-stamp gate.
// The br-ready path has no queue item to strand and no per-queue local-only pin.
func TestLocalOnlyCapIsQueuePathOnly(t *testing.T) {
	t.Parallel()

	in := AdmissionInput{
		Path:           PathBrReady,
		GateMax:        1,
		LocalInFlight:  1,
		QueueLocalOnly: true,
	}
	v, err := AdmitBeforeStamp(in)
	if err != nil {
		t.Fatalf("AdmitBeforeStamp on the br-ready path: unexpected error %v", err)
	}
	if !v.Admitted {
		t.Fatalf("br-ready path held on %s, want admit: the local-only cap is queue-path only", v.Reason)
	}

	in.Path = PathQueue
	v, err = AdmitBeforeStamp(in)
	if err != nil {
		t.Fatalf("AdmitBeforeStamp on the queue path: unexpected error %v", err)
	}
	if v.Admitted || v.Reason != ReasonLocalOnlyCapacityFull {
		t.Fatalf("queue path admitted a local-only item at the cap: verdict %+v", v)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The staged-input guard — the reason this fold exists
// ─────────────────────────────────────────────────────────────────────────────

// TestAfterLookupRefusesAnUnloadedBeadRecord pins the loud failure. Before the
// fold, a greenlight gate hoisted above the bead-record read saw an empty label
// list, never held, and dispatched a staged bead with no message and no test
// failure. Now the entry point refuses.
func TestAfterLookupRefusesAnUnloadedBeadRecord(t *testing.T) {
	t.Parallel()

	// A staged bead, but the caller has not loaded the record yet.
	v, err := AdmitAfterLookup(AdmissionInput{
		Path:             PathQueue,
		BeadID:           "hk-aaa",
		BeadRecordLoaded: false,
		BeadLabels:       []string{LabelNeedsGreenlight},
	})
	if err == nil {
		t.Fatalf("AdmitAfterLookup returned no error with BeadRecordLoaded=false; verdict %+v — a hoisted gate must fail loudly, never pass quietly", v)
	}
	if v.Admitted {
		t.Errorf("the error verdict says Admitted=true; a caller that ignores err would dispatch")
	}
	if !errors.Is(err, ErrGateInputMissing) {
		t.Errorf("errors.Is(err, ErrGateInputMissing) = false for %v", err)
	}
	var gie *GateInputError
	if !errors.As(err, &gie) {
		t.Fatalf("errors.As(err, *GateInputError) = false for %v", err)
	}
	if gie.Gate != "greenlight" || gie.Stage != StageAfterLookup {
		t.Errorf("GateInputError = %+v, want gate greenlight at stage after_lookup", gie)
	}
}

// TestStagesWithoutBeadRecordGatesDoNotNeedIt is the paired control. Only the
// after-lookup stage requires the record, so the other three stages must decide
// with BeadRecordLoaded false. Without this, the test above would pass on a
// package that simply errors everywhere.
func TestStagesWithoutBeadRecordGatesDoNotNeedIt(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func(AdmissionInput) (Verdict, error)
		in   AdmissionInput
	}{
		{"tick", AdmitAtTick, AdmissionInput{Path: PathAny, GateMax: 4}},
		{"before_lookup", AdmitBeforeLookup, AdmissionInput{Path: PathQueue, BeadID: "hk-aaa"}},
		{"before_stamp", AdmitBeforeStamp, AdmissionInput{Path: PathQueue, GateMax: 4}},
	}
	for _, tc := range cases {
		v, err := tc.call(tc.in)
		if err != nil {
			t.Errorf("%s: unexpected error %v with BeadRecordLoaded=false", tc.name, err)
			continue
		}
		if !v.Admitted {
			t.Errorf("%s: held on %s, want admit for an unblocked input", tc.name, v.Reason)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Order within a stage
// ─────────────────────────────────────────────────────────────────────────────

// TestBeforeLookupReportsDecisionRequiredFirst pins the table order for the one
// stage that holds two gates.
//
// The two conditions are independent, so either order is correct for dispatch.
// The ORDER still decides what the operator reads, and an operator who greps for
// one line must see the same line for the same state on every tick. Permuting
// the two rows turns this red.
func TestBeforeLookupReportsDecisionRequiredFirst(t *testing.T) {
	t.Parallel()

	v, err := AdmitBeforeLookup(AdmissionInput{
		Path:            PathQueue,
		BeadID:          "hk-aaa",
		DecisionBlocked: true,
		SentinelBlocked: true,
	})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if v.Admitted {
		t.Fatal("both conditions set and the stage admitted")
	}
	if v.Reason != ReasonDecisionRequired {
		t.Errorf("first holding reason = %q, want %q: decision-required precedes sentinel-queue in the table", v.Reason, ReasonDecisionRequired)
	}
	if !strings.Contains(v.Message, "decision_required") {
		t.Errorf("message %q does not match the reported reason", v.Message)
	}
}

// TestBeforeLookupHoldsOnEitherConditionAlone is the control for the test above:
// it proves the fixture can reach BOTH gates, so the ordering assertion is about
// order and not about one gate being unreachable.
func TestBeforeLookupHoldsOnEitherConditionAlone(t *testing.T) {
	t.Parallel()

	decisionOnly, err := AdmitBeforeLookup(AdmissionInput{Path: PathQueue, BeadID: "hk-aaa", DecisionBlocked: true})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if decisionOnly.Admitted || decisionOnly.Reason != ReasonDecisionRequired {
		t.Errorf("decision-required alone: verdict %+v", decisionOnly)
	}

	sentinelOnly, err := AdmitBeforeLookup(AdmissionInput{Path: PathQueue, BeadID: "hk-aaa", SentinelBlocked: true})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if sentinelOnly.Admitted || sentinelOnly.Reason != ReasonSentinelTrip {
		t.Errorf("sentinel alone: verdict %+v", sentinelOnly)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Gate conditions
// ─────────────────────────────────────────────────────────────────────────────

// TestSplitCapacityGate pins the two-part condition: hold only when local is at
// the ceiling AND no remote worker can take the run.
func TestSplitCapacityGate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		localInFlight int
		gateMax       int
		freeSlot      bool
		wantAdmit     bool
	}{
		{"below the ceiling, no worker", 0, 1, false, true},
		{"below the ceiling, worker free", 0, 1, true, true},
		{"at the ceiling, no worker", 1, 1, false, false},
		{"at the ceiling, worker free (remote bypass)", 1, 1, true, true},
		{"over the ceiling after a lowered concurrency, no worker", 4, 1, false, false},
	}
	for _, tc := range cases {
		v, err := AdmitAtTick(AdmissionInput{
			Path:              PathAny,
			GateMax:           tc.gateMax,
			LocalInFlight:     tc.localInFlight,
			WorkerHasFreeSlot: tc.freeSlot,
		})
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if v.Admitted != tc.wantAdmit {
			t.Errorf("%s: Admitted = %v, want %v (verdict %+v)", tc.name, v.Admitted, tc.wantAdmit, v)
		}
		if !v.Admitted && v.Reason != ReasonLocalCapacityFull {
			t.Errorf("%s: reason = %q, want %q", tc.name, v.Reason, ReasonLocalCapacityFull)
		}
	}
}

// TestLocalOnlyCapGate pins that the pre-stamp gate holds ONLY for a local-only
// queue. A queue that can route remotely must pass, because the tick gate
// already let it through on the strength of a free worker slot.
func TestLocalOnlyCapGate(t *testing.T) {
	t.Parallel()

	atCap := AdmissionInput{Path: PathQueue, GateMax: 1, LocalInFlight: 1}

	notLocalOnly := atCap
	v, err := AdmitBeforeStamp(notLocalOnly)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !v.Admitted {
		t.Errorf("a remote-capable queue was held at the local cap: verdict %+v", v)
	}

	localOnly := atCap
	localOnly.QueueLocalOnly = true
	v, err = AdmitBeforeStamp(localOnly)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if v.Admitted || v.Reason != ReasonLocalOnlyCapacityFull {
		t.Errorf("a local-only queue was admitted at the local cap: verdict %+v", v)
	}

	// Below the ceiling the local-only queue passes.
	belowCap := localOnly
	belowCap.LocalInFlight = 0
	v, err = AdmitBeforeStamp(belowCap)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if !v.Admitted {
		t.Errorf("a local-only queue below the ceiling was held: verdict %+v", v)
	}
}

// TestSilentGatesCarryNoMessage pins which gates print. The two capacity gates
// fire at poll cadence on a busy fleet, so a message would bury the log.
func TestSilentGatesCarryNoMessage(t *testing.T) {
	t.Parallel()

	tick, err := AdmitAtTick(AdmissionInput{Path: PathAny, GateMax: 1, LocalInFlight: 1})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if tick.Admitted || tick.Message != "" {
		t.Errorf("split-capacity verdict = %+v, want a silent hold", tick)
	}

	stamp, err := AdmitBeforeStamp(AdmissionInput{Path: PathQueue, GateMax: 1, LocalInFlight: 1, QueueLocalOnly: true})
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if stamp.Admitted || stamp.Message != "" {
		t.Errorf("local-only-cap verdict = %+v, want a silent hold", stamp)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Operator text — byte-for-byte
// ─────────────────────────────────────────────────────────────────────────────

// TestGateMessagesAreByteForByte pins the exact stderr lines the daemon printed
// before the fold, including the br-ready suffix difference. Operators grep
// these, so the bytes are a contract and not a detail.
func TestGateMessagesAreByteForByte(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		call func(AdmissionInput) (Verdict, error)
		in   AdmissionInput
		want string
	}{
		{
			name: "decision-required, queue path",
			call: AdmitBeforeLookup,
			in:   AdmissionInput{Path: PathQueue, BeadID: "hk-aaa", DecisionBlocked: true},
			want: "daemon: workloop: bead hk-aaa blocked by unacknowledged decision_required (EV-043) — holding\n",
		},
		{
			name: "decision-required, br-ready path",
			call: AdmitBeforeLookup,
			in:   AdmissionInput{Path: PathBrReady, BeadID: "hk-aaa", DecisionBlocked: true},
			want: "daemon: workloop: bead hk-aaa blocked by unacknowledged decision_required (EV-043, br-ready path) — holding\n",
		},
		{
			name: "sentinel-queue, queue path",
			call: AdmitBeforeLookup,
			in:   AdmissionInput{Path: PathQueue, BeadID: "hk-aaa", SentinelBlocked: true},
			want: "daemon: workloop: bead hk-aaa blocked by sentinel governor trip (EV-043, FW3) — holding until real movement\n",
		},
		{
			name: "sentinel-queue, br-ready path",
			call: AdmitBeforeLookup,
			in:   AdmissionInput{Path: PathBrReady, BeadID: "hk-aaa", SentinelBlocked: true},
			want: "daemon: workloop: bead hk-aaa blocked by sentinel governor trip (EV-043, FW3, br-ready path) — holding until real movement\n",
		},
		{
			name: "greenlight, queue path",
			call: AdmitAfterLookup,
			in: AdmissionInput{
				Path:             PathQueue,
				BeadID:           "hk-aaa",
				BeadRecordLoaded: true,
				BeadLabels:       []string{"other", LabelNeedsGreenlight},
			},
			want: "daemon: workloop: bead hk-aaa has needs-greenlight label — holding until captain runs `harmonik greenlight hk-aaa`\n",
		},
	}
	for _, tc := range cases {
		v, err := tc.call(tc.in)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if v.Admitted {
			t.Errorf("%s: admitted, want a hold", tc.name)
			continue
		}
		if v.Message != tc.want {
			t.Errorf("%s: message mismatch\n got: %q\nwant: %q", tc.name, v.Message, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// The per-tick ceiling
// ─────────────────────────────────────────────────────────────────────────────

// TestLocalGateMax pins that the live controller wins when it is present and the
// boot value is the fallback. Both capacity gates read the ONE value this
// returns, so a wrong answer here mis-gates the whole tick.
func TestLocalGateMax(t *testing.T) {
	t.Parallel()

	if got := LocalGateMax(4, 0, false); got != 4 {
		t.Errorf("no controller: LocalGateMax = %d, want the boot value 4", got)
	}
	if got := LocalGateMax(4, 2, true); got != 2 {
		t.Errorf("controller present: LocalGateMax = %d, want the live value 2", got)
	}
	if got := LocalGateMax(4, 0, true); got != 0 {
		t.Errorf("controller present and drained to 0: LocalGateMax = %d, want 0", got)
	}
}
