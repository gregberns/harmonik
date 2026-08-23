package orchestrator

import (
	"errors"
	"fmt"
)

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

func pathNote(p Path) string {
	if p == PathBrReady {
		return ", br-ready path"
	}
	return ""
}

var admissionGates = []gateSpec{
	{
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
