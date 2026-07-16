package twinparity

import (
	"sort"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// TerminalKinds is the default equivalence spine: the ordered set of
// terminal-landmark event kinds that a complete run journals to the DURABLE
// event log (.harmonik/events/events.jsonl), in the exact order the daemon
// emits them:
//
//	outcome_emitted → bead_closed → run_completed
//
// Evidence (daemon source): internal/runexec/run.go finalizeClose emits
// outcome_emitted then ActCloseBead; internal/daemon/runbridge.go emits
// bead_closed only after CloseBead succeeds; run.go emits run_completed last.
//
// This is the DEFAULT equivalence spine because AssertStreamEquivalent gates a
// durable JSONL stream. A twin stream and a real capture are equivalent when
// these three kinds appear, in this order, as an ordered subsequence in both.
//
// NOTE — observability vs durable: agent_completed and hook_fired are
// durability-class O (observability) events. They are NOT journaled to
// events.jsonl (a real durable log contains zero of them), so they MUST NOT
// appear in the durable-log spine — including them makes locateSpine fail
// vacuously against any real capture. Their constants remain defined in the
// vocabulary (KnownKinds / coreEventTypes) for callers that assert against the
// PROGRESS stream instead. If/when a progress-stream spine is asserted, the
// correct progress order is: agent_completed comes FIRST (it TRIGGERS the close
// spine per workloop.go), and hook_fired precedes outcome_emitted — the reverse
// of the two inversions this default deliberately avoids.
var TerminalKinds = []string{
	string(core.EventTypeOutcomeEmitted),
	string(core.EventTypeBeadClosed),
	string(core.EventTypeRunCompleted),
}

// DefaultTimingEdges are the causal edges whose latency parity is checked by
// default, over durable (journaled) endpoints only: ready→outcome,
// outcome→bead-close, bead-close→run-complete. agent_ready is durability-class
// F (journaled); the other three are the durable terminal triad. No edge
// references an observability-class kind (agent_completed / hook_fired), which
// would be absent from a real durable capture.
var DefaultTimingEdges = []TimingEdge{
	{From: string(core.EventTypeAgentReady), To: string(core.EventTypeOutcomeEmitted)},
	{From: string(core.EventTypeOutcomeEmitted), To: string(core.EventTypeBeadClosed)},
	{From: string(core.EventTypeBeadClosed), To: string(core.EventTypeRunCompleted)},
}

// AnomalyKinds are the diagnostic hang/timeout kinds whose presence signals a
// stalled run — not expected in an equivalent happy-path stream.
var AnomalyKinds = []string{
	string(core.EventTypeAgentReadyTimeout),
	string(core.EventTypePostAgentReadyHang),
	string(core.EventTypeAgentWarningSilentHang),
	string(core.EventTypeAgentResumedAfterWarning),
}

// KnownKinds returns the full legal-kind vocabulary assembled from the
// core.EventType taxonomy plus the handlercontract progress-stream message
// types (via handlercontract.KnownProgressMsgTypes — not re-listed by hand).
// The result is sorted and de-duplicated.
func KnownKinds() []string {
	set := map[string]struct{}{}
	for _, k := range coreEventTypes() {
		set[k] = struct{}{}
	}
	for _, k := range handlercontract.KnownProgressMsgTypes() {
		set[k] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// IsKnownKind reports whether kind is in the assembled legal-kind vocabulary.
func IsKnownKind(kind string) bool {
	for _, k := range coreEventTypes() {
		if k == kind {
			return true
		}
	}
	for _, k := range handlercontract.KnownProgressMsgTypes() {
		if k == kind {
			return true
		}
	}
	return false
}

// coreEventTypes lists the core.EventType constants that participate in the
// equivalence vocabulary. Kept explicit (rather than reflected) so the set is
// auditable; the terminal/anomaly named sets are drawn from these same consts.
func coreEventTypes() []string {
	return []string{
		string(core.EventTypeAgentReady),
		string(core.EventTypeAgentStarted),
		string(core.EventTypeAgentCompleted),
		string(core.EventTypeAgentFailed),
		string(core.EventTypeOutcomeEmitted),
		string(core.EventTypeHookFired),
		string(core.EventTypeBeadClosed),
		string(core.EventTypeRunStarted),
		string(core.EventTypeRunCompleted),
		string(core.EventTypeRunFailed),
		string(core.EventTypeReviewerLaunched),
		string(core.EventTypeReviewerVerdict),
		string(core.EventTypeImplementerResumed),
		string(core.EventTypeReviewLoopCycleComplete),
		string(core.EventTypeAgentReadyTimeout),
		string(core.EventTypePostAgentReadyHang),
		string(core.EventTypeAgentWarningSilentHang),
		string(core.EventTypeAgentResumedAfterWarning),
	}
}
