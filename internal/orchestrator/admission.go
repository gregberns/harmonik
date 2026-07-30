package orchestrator

import (
	"errors"
	"fmt"
)

// admission.go — the pure dispatch ADMISSION gates, folded out of the inline
// gate run in internal/daemon runWorkLoop WITHOUT semantic change.
//
// # What this file is for
//
// The daemon decides whether to dispatch a bead through a run of gates. Before
// this file each gate was an inline statement block. No gate had a name, and the
// ORDER between the blocks was load-bearing. The failure mode was silent: move a
// block and the code still compiles, the gate stops firing, and no test goes
// red.
//
// This file makes the order DATA. Each gate is a table entry with a name, a
// STAGE, the dispatch paths it applies to, and a pure test. The daemon calls one
// entry point per stage and gets back the FIRST holding verdict. The daemon
// keeps every effect: it prints the message, it sleeps, it continues.
//
// # Stages
//
// A stage says WHEN in a tick a gate can be evaluated, which is what the source
// position used to say. The stage is not a hint. AdmitAfterLookup refuses to run
// a gate that reads the bead record before the loop loaded it, and it returns a
// hard error instead of a quiet pass. That refusal is the whole point of the
// fold: the greenlight gate reads labels off the bead record, so a hoist above
// the record read used to give an empty label list and a gate that never fired.
//
// Spec ref: specs/execution-model.md §4.11 EM-049 (capacity gate),
// specs/event-model.md §4.12 EV-043, EV-043a (decision-required, sentinel).
// Bead ref: hk-hs7ex, hk-pbmsq, hk-4toh, hk-lacr, hk-l5saf.

// Stage is the point in one dispatch tick at which a gate can be evaluated.
type Stage string

const (
	// StageTick is the top of a tick. The loop has not chosen a dispatch source
	// and has not chosen a bead.
	StageTick Stage = "tick"

	// StageBeforeLookup is after the loop chose a bead and before it reads the
	// bead record from the ledger. A gate here holds the bead WITHOUT paying for
	// the record read.
	StageBeforeLookup Stage = "before_lookup"

	// StageAfterLookup is after the bead record is in hand. Only a gate that
	// reads the record belongs here.
	StageAfterLookup Stage = "after_lookup"

	// StageBeforeStamp is the last point before the durable dispatch write. A
	// gate here holds the item WITHOUT stamping it, so the item stays selectable
	// on the next tick.
	StageBeforeStamp Stage = "before_stamp"
)

// Path is the dispatch source the loop pulled the bead from.
type Path string

const (
	// PathAny is the value at StageTick, where the loop has not yet branched.
	// A gate that declares no paths runs on every path, PathAny included.
	PathAny Path = ""

	// PathQueue is the queue-pull path.
	PathQueue Path = "queue"

	// PathBrReady is the br-ready poll fallback.
	PathBrReady Path = "br_ready"
)

// Reason is the stable machine-readable name of the gate that held a bead.
// Operators and tests match on these, so treat them as a contract.
type Reason string

const (
	// ReasonAdmitted is the reason on an admitting verdict: no gate held.
	ReasonAdmitted Reason = ""

	// ReasonLocalCapacityFull — local runs are at the ceiling and no remote
	// worker has a free slot (hk-hs7ex).
	ReasonLocalCapacityFull Reason = "local_capacity_full"

	// ReasonDecisionRequired — the bead has an unacknowledged decision_required
	// (hk-pbmsq, EV-043).
	ReasonDecisionRequired Reason = "decision_required"

	// ReasonSentinelTrip — the sentinel governor trip blocks ALL dispatch
	// (hk-4toh, EV-043a, FW3).
	ReasonSentinelTrip Reason = "sentinel_governor_trip"

	// ReasonNeedsGreenlight — the bead carries the needs-greenlight label
	// (hk-lacr).
	ReasonNeedsGreenlight Reason = "needs_greenlight"

	// ReasonLocalOnlyCapacityFull — the selected queue is local-only and local
	// runs are at the ceiling, so the remote bypass the tick gate allowed does
	// not apply to this item (hk-l5saf).
	ReasonLocalOnlyCapacityFull Reason = "local_only_capacity_full"
)

// Verdict is the result of one stage of admission.
//
// Message is the operator-facing line, newline included, ready to write to
// stderr with fmt.Fprint. It is EMPTY for the two silent gates, and the daemon
// prints nothing in that case. Never pass Message through fmt.Fprintf: it is
// already formatted and a bead ID could carry a percent sign.
type Verdict struct {
	// Admitted is true when no gate at this stage held the bead.
	Admitted bool
	// Reason names the holding gate. ReasonAdmitted on an admitting verdict.
	Reason Reason
	// Message is the stderr line, or "" when the gate is silent.
	Message string
}

// AdmissionInput projects everything the gates read. The daemon fills it from
// live state at the call site. Nothing here is a pointer to daemon state and
// nothing here is read twice.
type AdmissionInput struct {
	// Path is the dispatch source. Use PathAny at StageTick.
	Path Path

	// BeadID names the bead under test. It is empty at StageTick. Only the
	// operator-facing text reads it.
	BeadID string

	// GateMax is the local in-flight ceiling for THIS tick. Derive it once per
	// tick with LocalGateMax and pass the same value to every stage. Two gates
	// read it and they must agree.
	GateMax int

	// LocalInFlight is the number of local runs in flight. Remote runs are
	// excluded by construction (hk-4tjt6).
	LocalInFlight int

	// WorkerHasFreeSlot is true when a remote worker can take a run. The daemon
	// folds "no worker registry" into false.
	WorkerHasFreeSlot bool

	// DecisionBlocked is true when the bead has an unacknowledged
	// decision_required. The daemon folds "no decision blocker" into false.
	DecisionBlocked bool

	// SentinelBlocked is true when the sentinel governor trip is pending. The
	// daemon folds "no governor" into false, and that is load-bearing: a
	// subsystem that is off must not hold the dispatcher shut through a gate no
	// code path can open.
	SentinelBlocked bool

	// QueueLocalOnly is true when the selected queue pins its work to local
	// execution (hk-f10xl).
	QueueLocalOnly bool

	// BeadRecordLoaded is true once the loop has read the bead record. It gates
	// every StageAfterLookup entry. Do not set it to make a gate run: an empty
	// BeadLabels with BeadRecordLoaded true is a gate that silently never fires,
	// which is the exact bug this fold removes.
	BeadRecordLoaded bool

	// BeadLabels are the labels off the loaded bead record. Read only when
	// BeadRecordLoaded is true.
	BeadLabels []string
}

// LabelNeedsGreenlight is the label a captain clears with
// `harmonik greenlight <bead-id>`. It is declared here because the gate that
// reads it lives here.
const LabelNeedsGreenlight = "needs-greenlight"

// ErrGateInputMissing is the sentinel behind every GateInputError. Match it
// with errors.Is.
var ErrGateInputMissing = errors.New("admission gate ran before its input was loaded")

// GateInputError says a gate was asked to decide without the facts it reads.
// It is a programming error in the caller, not a runtime condition, and the
// daemon treats it as fatal. It exists so that hoisting a gate to an earlier
// stage FAILS instead of passing quietly.
type GateInputError struct {
	// Gate is the gate table name.
	Gate string
	// Stage is the stage the caller asked for.
	Stage Stage
	// Fact names the missing input in plain terms.
	Fact string
}

func (e *GateInputError) Error() string {
	return fmt.Sprintf("orchestrator: admission gate %q ran at stage %q without %s", e.Gate, e.Stage, e.Fact)
}

// Unwrap lets errors.Is(err, ErrGateInputMissing) match.
func (e *GateInputError) Unwrap() error { return ErrGateInputMissing }

// LocalGateMax returns the local in-flight ceiling for one dispatch tick.
//
// Derive it ONCE per tick and pass the result to every stage. Two gates read it
// — the tick capacity gate and the local-only cap before the stamp — and a
// second derivation could hand them different ceilings within one tick.
//
// The live controller wins when it is present, so a `queue set-concurrency`
// change takes effect without a daemon restart (hk-ohiaf). startupMax is the
// value the loop clamped at boot and is the fallback when no controller is
// wired.
func LocalGateMax(startupMax, controllerMax int, controllerPresent bool) int {
	if controllerPresent {
		return controllerMax
	}
	return startupMax
}

// gateSpec is one row of the ordered admission table.
type gateSpec struct {
	// name is the operator-facing gate name. Keep it stable.
	name string
	// stage is the earliest and only point the gate may be evaluated.
	stage Stage
	// paths lists the dispatch paths the gate applies to. Empty means every
	// path.
	paths []Path
	// reason is the Reason the verdict carries when this gate holds.
	reason Reason
	// needsBeadRecord marks a gate that reads the loaded bead record. The stage
	// entry point returns a GateInputError rather than running such a gate
	// against a zero record.
	needsBeadRecord bool
	// hold reports whether the gate holds the bead, plus the stderr line. An
	// empty line means the gate is silent.
	hold func(AdmissionInput) (bool, string)
}

// appliesTo reports whether the gate runs on the given path. A gate that
// declares no paths runs on every path.
func (g gateSpec) appliesTo(p Path) bool {
	if len(g.paths) == 0 {
		return true
	}
	for _, gp := range g.paths {
		if gp == p {
			return true
		}
	}
	return false
}

// pathNote is the extra text the br-ready path adds inside the spec-reference
// parenthesis of a gate message. The queue path adds nothing. Operators grep
// these lines, so the bytes match the pre-fold daemon exactly.
func pathNote(p Path) string {
	if p == PathBrReady {
		return ", br-ready path"
	}
	return ""
}

// admissionGates is the ORDERED admission table. Order within a stage is the
// order the daemon used to evaluate the inline blocks, and the first holding
// gate wins. This value IS the ordering contract — changing a row's stage or
// moving a row changes daemon behavior.
var admissionGates = []gateSpec{
	{
		// Split capacity gate (hk-hs7ex): a local hard sub-cap kept separate from
		// remote-worker capacity. Hold only when local is full AND no remote
		// worker has a free slot. When a worker has a free slot the loop
		// proceeds and the run routes remotely, which does not consume a local
		// slot. Raising the ceiling lets the local side admit more. Remote runs
		// stay bounded by the worker's own MaxSlots.
		//
		// Silent by design. It fires at poll cadence whenever the fleet is busy,
		// so a message here would bury every other line in the log.
		//
		// Spec ref: specs/execution-model.md §4.11 EM-049.
		name:   "split-capacity",
		stage:  StageTick,
		reason: ReasonLocalCapacityFull,
		hold: func(in AdmissionInput) (bool, string) {
			if in.LocalInFlight >= in.GateMax && !in.WorkerHasFreeSlot {
				return true, ""
			}
			return false, ""
		},
	},
	{
		// Decision-required gate (hk-pbmsq, EV-043): the bead has an
		// unacknowledged decision_required pending. Hold it WITHOUT claiming and
		// retry on the next poll tick.
		//
		// It runs before the bead-record read so a blocked bead never pays for
		// that subprocess at poll cadence.
		//
		// Spec ref: specs/event-model.md §4.12 EV-043.
		name:   "decision-required",
		stage:  StageBeforeLookup,
		reason: ReasonDecisionRequired,
		hold: func(in AdmissionInput) (bool, string) {
			if !in.DecisionBlocked {
				return false, ""
			}
			return true, fmt.Sprintf(
				"daemon: workloop: bead %s blocked by unacknowledged decision_required (EV-043%s) — holding\n",
				in.BeadID, pathNote(in.Path))
		},
	},
	{
		// Sentinel queue-level gate (hk-4toh, FW3, EV-043a): the sentinel
		// governor trip is pending, which blocks ALL beads — not one specific
		// bead — until real movement clears the trip.
		//
		// The test reads no bead ID. The bead ID appears only in the message, to
		// tell the operator which bead the loop was about to dispatch.
		//
		// The daemon asks the maintenance handle that owns the governor, and
		// folds an ABSENT governor into false. That is deliberate. The "sentinel"
		// queue block has exactly one writer, the governor's ACT mode. Keeping
		// the read while removing the subsystem would leave a gate no code path
		// can open, and one stale pending trip file would wedge every dispatch on
		// every later boot.
		//
		// Order against decision-required is free: the two are independent and
		// only the message differs. The table still fixes one order so the
		// operator sees the same line for the same state on every tick.
		//
		// Spec ref: specs/event-model.md §4.12 EV-043a.
		name:   "sentinel-queue",
		stage:  StageBeforeLookup,
		reason: ReasonSentinelTrip,
		hold: func(in AdmissionInput) (bool, string) {
			if !in.SentinelBlocked {
				return false, ""
			}
			return true, fmt.Sprintf(
				"daemon: workloop: bead %s blocked by sentinel governor trip (EV-043, FW3%s) — holding until real movement\n",
				in.BeadID, pathNote(in.Path))
		},
	},
	{
		// Greenlight gate (AC2 — hk-lacr, queue path only): a staged
		// deploy-and-verify bead carries the needs-greenlight label and MUST NOT
		// dispatch until a captain clears the label with
		// `harmonik greenlight <bead-id>`.
		//
		// The test reads the LIVE bead label, not a daemon mode flag, so
		// --no-auto-pull does not change it.
		//
		// Stage is StageAfterLookup and needsBeadRecord is true. Both are
		// load-bearing. The gate reads labels off the bead record, so at an
		// earlier stage it sees an empty list, never holds, and lets a staged
		// bead through. That failure compiles clean and prints nothing, which is
		// why this gate carries the hard input check.
		//
		// The br-ready path has NO greenlight gate. It excludes the label at
		// adapter read time instead (internal/brcli ready path).
		name:            "greenlight",
		stage:           StageAfterLookup,
		paths:           []Path{PathQueue},
		reason:          ReasonNeedsGreenlight,
		needsBeadRecord: true,
		hold: func(in AdmissionInput) (bool, string) {
			for _, lbl := range in.BeadLabels {
				if lbl == LabelNeedsGreenlight {
					return true, fmt.Sprintf(
						"daemon: workloop: bead %s has needs-greenlight label — holding until captain runs `harmonik greenlight %s`\n",
						in.BeadID, in.BeadID)
				}
			}
			return false, ""
		},
	},
	{
		// Local-only cap gate (hk-l5saf, queue path only): the second local-cap
		// read, taken immediately before the dispatch stamp.
		//
		// The tick capacity gate may have admitted in "remote bypass" mode: local
		// was full but a worker had a free slot, so the loop expected the bead to
		// route remotely. If the selected item turned out to belong to a
		// local-only queue, dispatching it locally would overrun the hard local
		// cap. So hold it WITHOUT stamping, exactly like the sibling hold gates.
		//
		// StageBeforeStamp is the bug fix, not a preference. The guard used to
		// sit AFTER the stamp. By then the item was already marked dispatched
		// with a placeholder run ID and queue.json was persisted, so the hold
		// left the item stranded: a dispatched item with no run is never
		// re-selected, nothing reverts it, and the group wedged until a daemon
		// restart.
		//
		// The read is safe before the stamp because only this dispatch goroutine
		// increments the local in-flight count, and it does that AFTER the claim.
		// A count below the ceiling here stays below it through dispatch. Moving
		// that increment earlier breaks this gate.
		//
		// Silent by design, matching the tick capacity gate it backstops.
		name:   "local-only-cap",
		stage:  StageBeforeStamp,
		paths:  []Path{PathQueue},
		reason: ReasonLocalOnlyCapacityFull,
		hold: func(in AdmissionInput) (bool, string) {
			if in.QueueLocalOnly && in.LocalInFlight >= in.GateMax {
				return true, ""
			}
			return false, ""
		},
	},
}

// AdmitAtTick runs the StageTick gates: the split capacity gate.
func AdmitAtTick(in AdmissionInput) (Verdict, error) {
	return admit(StageTick, in)
}

// AdmitBeforeLookup runs the StageBeforeLookup gates: decision-required, then
// sentinel-queue. Call it after the loop picks a bead and BEFORE it reads the
// bead record, so a held bead does not pay for that read.
func AdmitBeforeLookup(in AdmissionInput) (Verdict, error) {
	return admit(StageBeforeLookup, in)
}

// AdmitAfterLookup runs the StageAfterLookup gates: greenlight.
//
// It returns a GateInputError when a gate at this stage reads the bead record
// and AdmissionInput.BeadRecordLoaded is false. Do not treat that error as an
// admit and do not swallow it. It means a gate moved above the code that loads
// its input, which is a bug that would otherwise never show.
func AdmitAfterLookup(in AdmissionInput) (Verdict, error) {
	return admit(StageAfterLookup, in)
}

// AdmitBeforeStamp runs the StageBeforeStamp gates: the local-only cap. Call it
// immediately before the durable dispatch write. Every gate here holds WITHOUT
// stamping, which is what keeps a held item selectable on the next tick.
func AdmitBeforeStamp(in AdmissionInput) (Verdict, error) {
	return admit(StageBeforeStamp, in)
}

// admit walks the ordered table, keeps the rows for this stage and path, and
// returns the FIRST holding verdict. It returns an admitting verdict when no
// gate holds.
func admit(stage Stage, in AdmissionInput) (Verdict, error) {
	for _, g := range admissionGates {
		if g.stage != stage || !g.appliesTo(in.Path) {
			continue
		}
		if g.needsBeadRecord && !in.BeadRecordLoaded {
			return Verdict{}, &GateInputError{Gate: g.name, Stage: stage, Fact: "a loaded bead record"}
		}
		if held, msg := g.hold(in); held {
			return Verdict{Admitted: false, Reason: g.reason, Message: msg}, nil
		}
	}
	return Verdict{Admitted: true, Reason: ReasonAdmitted}, nil
}
