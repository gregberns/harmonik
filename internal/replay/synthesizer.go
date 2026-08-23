package replay

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
		return []StimulusStep{{Kind: stimOutcomeReceived, DelayMs: workGapMs}}
	}
}
