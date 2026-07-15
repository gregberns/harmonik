package replay

// The StimulusSynthesizer maps a run's recorded OUTCOME summary (produced by
// scripts/extract-run-corpus.py) to a reactor INPUT schedule. The extracted
// per-run streams record daemon OUTPUTS; the reactor under test consumes
// INPUTS, so the inputs are synthesized from the recorded outcome — exactly the
// trace-driven-Twin reasoning of P1's measurement design (liveness-parity-design
// §3). RT11 drives these schedules through internal/runexec under virtual time.
//
// The schedule step Kind values are the string forms of internal/runexec's
// EventKind constants; they are kept as plain strings here so this package does
// not import internal/runexec (avoiding a substrate→reactor edge). RT11 maps
// each Kind back to a runexec.EventKind when feeding the machine.

// RunSummary is the golden summary the extractor writes per run
// (summary.json). The json tags MUST match the extractor's output so the same
// struct round-trips a corpus summary into SynthesizeSchedule.
type RunSummary struct {
	RunID        string   `json:"run_id"`
	Stratum      string   `json:"stratum"`
	TerminalType string   `json:"terminal_type"`
	Outcome      string   `json:"outcome"`
	Mode         string   `json:"mode"`
	Iterations   int      `json:"iterations"`
	Resumed      bool     `json:"resumed"`
	MergeOutcome string   `json:"merge_outcome"`
	EventCount   int      `json:"event_count"`
	Types        []string `json:"types"`
}

// StimulusStep is one synthesized reactor input. Kind is the runexec EventKind
// string; DelayMs is the virtual-time offset from the PRIOR step (0 for the
// first). RT11 arms a FakeClock and feeds steps at the cumulative offsets.
type StimulusStep struct {
	Kind    string `json:"kind"`
	DelayMs int64  `json:"delay_ms"`
}

// Schedule is the synthesized input program for one run.
type Schedule struct {
	RunID   string         `json:"run_id"`
	Stratum string         `json:"stratum"`
	Steps   []StimulusStep `json:"steps"`
}

// The Kind string constants mirror internal/runexec.EventKind. Kept local so
// the substrate does not import the reactor (see file header).
const (
	stimStartDispatch   = "start_dispatch"
	stimLaunched        = "launched"
	stimAgentReady      = "agent_ready"
	stimInputAck        = "input_ack"
	stimOutcomeReceived = "outcome_received"
	stimAgentCompleted  = "agent_completed"
	stimTimerFired      = "timer_fired"
	stimHeartbeatStale  = "heartbeat_stale"
)

// Nominal virtual-time gaps between synthesized steps (milliseconds). These are
// well inside the reactor's real timer bounds so a clean schedule never trips a
// timeout; RT11's fault matrix perturbs them to exercise the timeout edges.
const (
	launchGapMs = 500
	readyGapMs  = 1_000
	inputGapMs  = 250
	workGapMs   = 2_000
)

// SynthesizeSchedule maps a RunSummary to the reactor input schedule that
// reproduces its recorded terminal. The dispatch head (start_dispatch →
// launched → agent_ready, plus an input_ack when the run was resumed) is
// followed by a terminal tail selected from the recorded terminal type. A hung
// run (agent_ready_timeout terminal) synthesizes NO agent_ready — the reactor
// must reach the timeout via TimerFired, which is precisely the fault the
// resume-liveness fix asserts.
func SynthesizeSchedule(s RunSummary) Schedule {
	sched := Schedule{RunID: s.RunID, Stratum: s.Stratum}
	if s.TerminalType == "agent_ready_timeout" {
		// The hung-relaunch shape: launch, then the ready timer fires with no
		// agent_ready ever delivered → fail-closed reopen (RSM-025).
		sched.Steps = []StimulusStep{
			{Kind: stimStartDispatch, DelayMs: 0},
			{Kind: stimLaunched, DelayMs: launchGapMs},
			{Kind: stimTimerFired, DelayMs: readyGapMs},
		}
		return sched
	}
	sched.Steps = dispatchHead(s)
	sched.Steps = append(sched.Steps, terminalTail(s)...)
	return sched
}

// dispatchHead builds the common launch→ready(→input_ack) prefix.
func dispatchHead(s RunSummary) []StimulusStep {
	steps := []StimulusStep{
		{Kind: stimStartDispatch, DelayMs: 0},
		{Kind: stimLaunched, DelayMs: launchGapMs},
		{Kind: stimAgentReady, DelayMs: readyGapMs},
	}
	if s.Resumed {
		steps = append(steps, StimulusStep{Kind: stimInputAck, DelayMs: inputGapMs})
	}
	return steps
}

// terminalTail selects the terminating stimulus for the recorded terminal type.
func terminalTail(s RunSummary) []StimulusStep {
	switch s.TerminalType {
	case "run_stale":
		return []StimulusStep{{Kind: stimHeartbeatStale, DelayMs: workGapMs}}
	case "run_completed", "review_loop_cycle_complete":
		if s.Mode == "single" {
			return []StimulusStep{{Kind: stimAgentCompleted, DelayMs: workGapMs}}
		}
		return []StimulusStep{{Kind: stimOutcomeReceived, DelayMs: workGapMs}}
	default:
		// run_failed and any other recorded terminal: an outcome that the Run
		// machine classifies to a failed terminal.
		return []StimulusStep{{Kind: stimOutcomeReceived, DelayMs: workGapMs}}
	}
}
