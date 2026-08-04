package readiness

// validate.go — the readiness validator. Pure, total, and unable to reach the
// fleet.
//
// [Validate] takes plain values and returns a record. It has no port parameter,
// no context, and no filesystem handle.
//
// Say what that buys, exactly. A signature constrains what a CALLER can hand
// in, not what the body can reach: a package-level port variable would bypass
// it, and depguard allows $gostd here, which includes os/exec. So the true
// statement is narrower than "it cannot call the fleet" — no caller can hand
// this function the fleet, and what stops it reaching the fleet on its own is
// this file's import set (encoding/json, fmt, io, time) and nothing else.
// TestValidateTakesNothingItCouldCallTheFleetWith is the ratchet on the first
// half.
//
// Spec ref: specs/beads-integration.md §4.5b BI-013e; specs/operator-nfr.md
// ON-032a. Task: T9 of the queue-dogfood-readiness work.

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// ValidationSchemaVersion is the schema version stamped into every validation
// record. Same additive contract as [SchemaVersion].
const ValidationSchemaVersion = 1

// QueueKind is how the queue feeds work to the daemon. Pass one runs one stream
// item; a wave queue is a different dispatch shape and is out of scope.
type QueueKind string

// The two queue shapes. Pass one runs a stream queue; a wave queue dispatches a
// whole batch at once and is a different thing to prove.
const (
	QueueKindStream QueueKind = "stream"
	QueueKindWave   QueueKind = "wave"
)

// RunPlan is the run the evidence is being validated for.
//
// It is the plan, not the outcome. The validator's whole job is to refuse a
// plan that does not match the posture the first assessor pass is allowed to
// run under, before anyone starts a daemon.
type RunPlan struct {
	// Harness is the agent harness the canary item would run on. Pass one is
	// local, and the Pi harness reaches a remote endpoint by construction.
	Harness string `json:"harness"`

	// RemoteWorker is the named remote worker, empty for a local run.
	RemoteWorker string `json:"remote_worker,omitempty"`

	// RepoTarget is the repository the run would land in. ScratchRepo is the
	// throwaway clone the pass is allowed to touch. They must be the same.
	RepoTarget  string `json:"repo_target"`
	ScratchRepo string `json:"scratch_repo"`

	QueueKind       QueueKind `json:"queue_kind"`
	FeedbackEnabled bool      `json:"feedback_enabled"`
	Concurrency     int       `json:"concurrency"`

	// ItemCount is how many items the queue would carry. It exists so that all
	// three fields of the snapshot's Posture have a counterpart here and can be
	// cross-checked; a posture field with nothing to compare against drifts in
	// silence.
	ItemCount int `json:"item_count"`
}

// isLocal reports whether the plan runs entirely on this machine.
//
// A named remote worker is the obvious case. The Pi harness is the other one:
// it reaches a remote endpoint by construction, which is why it is refused
// above. Both must be here or this file would carry two definitions of "local"
// and the posture check would read the weaker one.
func (p RunPlan) isLocal() bool {
	return p.RemoteWorker == "" && p.Harness != "pi"
}

// HostFacts is what was measured about the machine at validation time. The
// caller measures; this package only judges.
type HostFacts struct {
	LoadAverage  float64 `json:"load_average"`
	CPUCount     int     `json:"cpu_count"`
	FreeDiskGB   float64 `json:"free_disk_gb"`
	DaemonsAlive int     `json:"daemons_alive"`
}

// HostLimits is the bar the host is held to.
//
// These are inputs and not constants on purpose. Picking the numbers is an
// operator decision (task T12a), and a validator that baked one in would have
// to be edited to record that decision — which is how a number nobody chose
// becomes a number nobody can find.
type HostLimits struct {
	MaxLoadPerCPU float64 `json:"max_load_per_cpu"`
	MinFreeDiskGB float64 `json:"min_free_disk_gb"`
	MaxDaemons    int     `json:"max_daemons"`
}

// DefaultHostLimits is the bar this package uses when the operator has not set
// one: load at or below the CPU count, at least 10 GB free, at most one daemon.
//
// It is a starting point, not the decision. T12a records the operator's numbers,
// and a validation record always names the limits it applied, so a result read
// six weeks later says which bar it cleared.
var DefaultHostLimits = HostLimits{MaxLoadPerCPU: 1.0, MinFreeDiskGB: 10, MaxDaemons: 1}

// RejectionReason names why a plan or a host was refused. The set is closed:
// a new reason is a new constant here and a new case in [Validate].
type RejectionReason string

// Every reason a readiness validation can refuse. Six come from the run shape
// pass one excludes, three from a host that cannot produce evidence, and one
// from a plan that disagrees with the snapshot it is being judged against.
const (
	RejectPiHarness         RejectionReason = "pi_harness"
	RejectRemoteWorker      RejectionReason = "remote_worker"
	RejectCrossRepository   RejectionReason = "cross_repository"
	RejectQueueKindUnset    RejectionReason = "queue_kind_unset"
	RejectWaveQueue         RejectionReason = "wave_queue"
	RejectFeedbackEnabled   RejectionReason = "feedback_enabled"
	RejectConcurrencyNotOne RejectionReason = "concurrency_not_one"
	RejectHostLoad          RejectionReason = "host_load_above_limit"
	RejectHostDisk          RejectionReason = "host_free_disk_below_limit"
	RejectDaemonCount       RejectionReason = "daemon_count_above_limit"
	RejectPostureMismatch   RejectionReason = "plan_contradicts_snapshot_posture"
	RejectNoHostLimits      RejectionReason = "host_limits_not_set"
)

// Rejection is one refusal, with the measurement that produced it. The detail
// is required: "rejected: host load" sends the reader back to the machine to
// find out what the load was.
type Rejection struct {
	Reason RejectionReason `json:"reason"`
	Detail string          `json:"detail"`
}

// Validation is the record written to validation.json.
//
// Accepted is a plain bool derived from Rejections being empty, and it is
// stored rather than computed so a reader of the file does not have to know
// the rule.
type Validation struct {
	SchemaVersion  int         `json:"schema_version"`
	ValidatedAt    time.Time   `json:"validated_at"`
	SnapshotTaken  time.Time   `json:"snapshot_taken"`
	SelectedBead   string      `json:"selected_bead"`
	Accepted       bool        `json:"accepted"`
	Rejections     []Rejection `json:"rejections"`
	Plan           RunPlan     `json:"plan"`
	Host           HostFacts   `json:"host"`
	AppliedLimits  HostLimits  `json:"applied_limits"`
	PendingIntents int         `json:"pending_terminal_intents"`
}

// Validate judges a snapshot, a plan, and a host against the limits.
//
// It is total: every input has an answer, and a refusal is data in the returned
// record rather than an error. There is nothing to fail at — the function
// touches nothing that can fail.
//
// validatedAt is passed in for the same reason the capture time is: a validator
// that read the clock could not be replayed.
func Validate(snap Snapshot, plan RunPlan, host HostFacts, limits HostLimits, validatedAt time.Time) Validation {
	// Empty and not nil: this list is written to validation.json, and a reader
	// running `jq '.rejections | length'` over a null gets an error instead of
	// the zero it expects.
	rejections := []Rejection{}

	if plan.Harness == "pi" {
		rejections = append(rejections, Rejection{
			Reason: RejectPiHarness,
			Detail: "the Pi harness reaches a remote endpoint; pass one is local only",
		})
	}
	if plan.RemoteWorker != "" {
		rejections = append(rejections, Rejection{
			Reason: RejectRemoteWorker,
			Detail: fmt.Sprintf("plan names remote worker %q; pass one runs no remote worker", plan.RemoteWorker),
		})
	}
	if plan.RepoTarget != plan.ScratchRepo {
		rejections = append(rejections, Rejection{
			Reason: RejectCrossRepository,
			Detail: fmt.Sprintf("run would land in %q but the scratch clone is %q", plan.RepoTarget, plan.ScratchRepo),
		})
	}
	switch plan.QueueKind {
	case QueueKindStream:
		// The one shape pass one runs.
	case "":
		// An unset field is not a wave queue, and reporting it as one would send
		// the reader looking for a wave nobody configured.
		rejections = append(rejections, Rejection{
			Reason: RejectQueueKindUnset,
			Detail: "plan does not say what kind of queue this is; pass one is a stream queue",
		})
	default:
		rejections = append(rejections, Rejection{
			Reason: RejectWaveQueue,
			Detail: fmt.Sprintf("queue kind is %q; pass one is one stream item", plan.QueueKind),
		})
	}
	if plan.FeedbackEnabled {
		rejections = append(rejections, Rejection{
			Reason: RejectFeedbackEnabled,
			Detail: "feedback is on; pass one runs the item once with no feedback loop",
		})
	}
	if plan.Concurrency != 1 {
		rejections = append(rejections, Rejection{
			Reason: RejectConcurrencyNotOne,
			Detail: fmt.Sprintf("plan concurrency is %d; pass one pins it at 1", plan.Concurrency),
		})
	}

	// The snapshot already recorded the posture the selected item was judged
	// against. A plan that disagrees with it means one of the two is stale, and
	// guessing which would be worse than refusing.
	posture := snap.Selection.Posture
	if plan.Concurrency != posture.Concurrency ||
		plan.ItemCount != posture.ItemCount ||
		plan.isLocal() != posture.Local {
		rejections = append(rejections, Rejection{
			Reason: RejectPostureMismatch,
			Detail: fmt.Sprintf("snapshot judged the item at local=%t items=%d concurrency=%d; plan is local=%t items=%d concurrency=%d",
				posture.Local, posture.ItemCount, posture.Concurrency,
				plan.isLocal(), plan.ItemCount, plan.Concurrency),
		})
	}

	rejections = append(rejections, checkHost(host, limits)...)

	return Validation{
		SchemaVersion:  ValidationSchemaVersion,
		ValidatedAt:    validatedAt.UTC(),
		SnapshotTaken:  snap.CapturedAt,
		SelectedBead:   string(snap.Selection.Candidate.BeadID),
		Accepted:       len(rejections) == 0,
		Rejections:     rejections,
		Plan:           plan,
		Host:           host,
		AppliedLimits:  limits,
		PendingIntents: len(snap.TerminalIntent.Pending),
	}
}

// checkHost judges the machine. A gate result from a loaded box is not
// evidence, green or red, which is why load is a refusal and not a warning.
func checkHost(host HostFacts, limits HostLimits) []Rejection {
	var rejections []Rejection

	// A zero-value HostLimits disables every check below: a zero load ceiling is
	// cleared by a zero load, a zero disk floor is cleared by an empty disk, and
	// a zero daemon ceiling is cleared by no daemon. That fails OPEN, which is
	// the one direction a gate must never fail, so an unset bar is a refusal.
	if limits.MaxLoadPerCPU <= 0 || limits.MinFreeDiskGB <= 0 || limits.MaxDaemons <= 0 {
		rejections = append(rejections, Rejection{
			Reason: RejectNoHostLimits,
			Detail: fmt.Sprintf("host limits are not set (load/cpu=%.2f min_disk_gb=%.1f max_daemons=%d); "+
				"an unset bar would accept any host", limits.MaxLoadPerCPU, limits.MinFreeDiskGB, limits.MaxDaemons),
		})
	}

	// A zero CPU count means nobody measured it. Treating that as "any load is
	// fine" would silently disable the check that exists because this machine
	// saturates. The disk and daemon checks do not depend on it, so they still
	// run — a record that stopped here would understate the problem.
	if host.CPUCount <= 0 {
		rejections = append(rejections, Rejection{
			Reason: RejectHostLoad,
			Detail: "host CPU count was not measured, so load cannot be judged",
		})
	} else if ceiling := limits.MaxLoadPerCPU * float64(host.CPUCount); host.LoadAverage > ceiling {
		rejections = append(rejections, Rejection{
			Reason: RejectHostLoad,
			Detail: fmt.Sprintf("load average %.2f is above the ceiling %.2f (%d CPUs x %.2f)",
				host.LoadAverage, ceiling, host.CPUCount, limits.MaxLoadPerCPU),
		})
	}
	if host.FreeDiskGB < limits.MinFreeDiskGB {
		rejections = append(rejections, Rejection{
			Reason: RejectHostDisk,
			Detail: fmt.Sprintf("free disk %.1f GB is below the floor %.1f GB", host.FreeDiskGB, limits.MinFreeDiskGB),
		})
	}
	if host.DaemonsAlive > limits.MaxDaemons {
		rejections = append(rejections, Rejection{
			Reason: RejectDaemonCount,
			Detail: fmt.Sprintf("%d daemons are alive; the ceiling is %d", host.DaemonsAlive, limits.MaxDaemons),
		})
	}
	return rejections
}

// DecodeSnapshot reads a snapshot from r and re-checks it.
//
// The re-check is the point. [Capture] guarantees a snapshot it returns, but
// encoding/json does not go through any constructor — it writes straight into
// the fields. A snapshot that arrives from a file has therefore never been
// checked by anything, and trusting it because its Go type says Snapshot is
// exactly the "a check happened once, everyone downstream trusts it" failure.
// This is the same boundary as [Capture], one file later.
func DecodeSnapshot(r io.Reader) (Snapshot, error) {
	var decoded Snapshot
	dec := json.NewDecoder(r)
	if err := dec.Decode(&decoded); err != nil {
		return Snapshot{}, fmt.Errorf("readiness: decode snapshot: %w", err)
	}

	// Refuse, do not repair. newSnapshot re-stamps the schema version and the
	// observational note from constants, which is right when CONSTRUCTING a
	// record and wrong when reading one back: it would silently downgrade a
	// record written by a later version, and silently correct a doctored note.
	// This file is evidence an assessor reads. A validator that quietly rewrites
	// the thing it is validating is worse than one that refuses.
	if decoded.SchemaVersion <= 0 {
		return Snapshot{}, fmt.Errorf("%w: file states schema version %d", ErrSnapshotSchemaUnreadable, decoded.SchemaVersion)
	}
	if decoded.SchemaVersion > SchemaVersion {
		return Snapshot{}, fmt.Errorf("%w: file states schema version %d, this build reads %d",
			ErrSnapshotSchemaUnreadable, decoded.SchemaVersion, SchemaVersion)
	}
	if decoded.Events.Note != EventEvidenceNote {
		return Snapshot{}, fmt.Errorf("%w: file states %q", ErrSnapshotNoteAltered, decoded.Events.Note)
	}

	rechecked, err := newSnapshot(request{
		CapturedAt:      decoded.CapturedAt,
		Selection:       decoded.Selection,
		Excluded:        decoded.Excluded,
		Commands:        decoded.Commands,
		EventLogPaths:   decoded.Events.LogPaths,
		TerminalIntent:  decoded.TerminalIntent,
		StaleFindings:   decoded.StaleFindings,
		CurrentFindings: decoded.CurrentFindings,
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("readiness: snapshot on disk does not satisfy BI-013e: %w", err)
	}
	return rechecked, nil
}

// WriteValidation writes v to path as indented JSON.
func WriteValidation(path string, v Validation) error {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("readiness: marshal validation: %w", err)
	}
	return writeJSONFile(path, append(body, '\n'))
}
