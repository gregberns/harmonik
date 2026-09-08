package keeper

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/keeper/panehost"
	"github.com/gregberns/harmonik/internal/substrate"
)

// CycleIDGenerator returns a fresh identifier for each run of the cycle.
type CycleIDGenerator interface{ Next() string }

// PaneWriter is the write side of the agent's tmux pane. It injects text,
// sends an Escape keystroke, and sets a tmux environment value.
type PaneWriter interface {
	Inject(context.Context, string, string) error
	SendEscape(context.Context, string) error
	SetEnv(context.Context, string, string, string) error
}

// ContextStore is the keeper's own file state for one agent. It reads the
// context gauge, records the managed session id, and clears the precompact
// trigger.
type ContextStore interface {
	ReadGauge() (*CtxFile, time.Time, error)
	SetManagedSession(string) error
	ClearPrecompactTrigger() error
}

// ActivityProbe reports the time of the last idle marker, the last user turn,
// and the last assistant turn for a session.
type ActivityProbe interface {
	IdleMarkerModTime() (time.Time, bool)
	LastUserTurn(string) (time.Time, bool)
	LastAssistantTurn(string) (time.Time, bool)
}

type (
	// ManagedProbe reports whether the agent is under keeper control.
	ManagedProbe interface{ IsManaged() bool }

	// IdleProbe reports whether the session waits at a clean input prompt.
	IdleProbe interface{ CrispIdle() bool }

	// DispatchProbe reports whether the agent has queue work in flight.
	DispatchProbe interface{ HoldingDispatch() bool }

	// SleepProbe reports whether the daemon parked the named session.
	SleepProbe interface{ Sleeping(string) bool }

	// HoldProbe reports whether an operator hold is active.
	HoldProbe interface{ Held() bool }

	// OperatorPresenceProbe reports whether an operator is attached to the
	// named tmux target and was recently active there.
	OperatorPresenceProbe interface{ Attached(string) bool }
)

// HandoffDocument is the handoff file that the agent writes. It gives the
// path, the content, and the modification time, and it scrubs the keeper nonce
// marker from the file.
type HandoffDocument interface {
	Path() string
	Read() (string, error)
	ModTime() (time.Time, bool)
	ScrubNonce() error
}

// CycleJournalStore reads and writes the one record that says which phase the
// cycle is in.
type CycleJournalStore interface {
	Write(*CycleJournal) error
	Read() (*CycleJournal, error)
}

// CycleDeps is the validated dependency bundle for the automatic cycle.
// Emitter and Respawn are optional. Every other dependency is required.
type CycleDeps struct {
	Clock    substrate.ClockPort
	CycleIDs CycleIDGenerator
	Pane     PaneWriter
	Context  ContextStore
	Activity ActivityProbe
	Managed  ManagedProbe
	Idle     IdleProbe
	Dispatch DispatchProbe
	Sleep    SleepProbe
	Hold     HoldProbe
	Operator OperatorPresenceProbe
	Handoff  HandoffDocument
	Journal  CycleJournalStore
	Emitter  Emitter
	Respawn  RespawnPort
}

func (d CycleDeps) validate() error {
	required := []struct {
		name string
		dep  any
	}{
		{"Clock", d.Clock},
		{"CycleIDs", d.CycleIDs},
		{"Pane", d.Pane},
		{"Context", d.Context},
		{"Activity", d.Activity},
		{"Managed", d.Managed},
		{"Idle", d.Idle},
		{"Dispatch", d.Dispatch},
		{"Sleep", d.Sleep},
		{"Hold", d.Hold},
		{"Operator", d.Operator},
		{"Handoff", d.Handoff},
		{"Journal", d.Journal},
	}
	missing := make([]string, 0)
	for _, item := range required {
		if nilDependency(item.dep) {
			missing = append(missing, item.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("keeper cycle: missing dependencies: %s", strings.Join(missing, ", "))
	}
	return nil
}

func nilDependency(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// NewCyclerWithDeps constructs a cycle from separate policy, identity, and
// dependencies. It validates the complete dependency graph before any port call.
func NewCyclerWithDeps(policy CyclePolicy, env CycleEnv, deps CycleDeps) (*Cycler, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}
	if deps.Emitter == nil {
		deps.Emitter = NoopEmitter{}
	}
	cfg := configFromPolicyAndEnv(policy, env)
	cfg.Clock = deps.Clock
	cfg.hasRespawn = deps.Respawn != nil
	sampler := narrowGaugeAdapter{deps: deps, policy: policy, target: env.TmuxTarget}
	c := &Cycler{
		cfg: cfg, emitter: deps.Emitter, cycleIDs: deps.CycleIDs,
		pane: deps.Pane, context: deps.Context, activity: deps.Activity,
		handoff: deps.Handoff, journal: deps.Journal, snapshot: sampler.Snapshot,
		respawn: deps.Respawn,
	}
	c.machine = NewCycle(&c.cfg)
	return c, nil
}

// CycleDepsFromConfig resolves the command's configuration functions into the
// narrow dependency bundle. Broad legacy ports are not part of this path.
func CycleDepsFromConfig(cfg CyclerConfig, emitter Emitter) CycleDeps {
	cfg.applyDefaults()
	deps := CycleDeps{
		Clock: cfg.Clock, CycleIDs: cycleIDFunc(newCycleIDGen(cfg.Clock)),
		Pane: configPaneWriter{cfg: &cfg}, Context: configContextStore{cfg: &cfg},
		Activity: configActivityProbe{cfg: &cfg},
		Managed:  boolProbe(func() bool { return IsManaged(cfg.ProjectDir, cfg.AgentName) }),
		Idle:     boolProbe(func() bool { return CrispIdle(cfg.ProjectDir, cfg.AgentName) }),
		Dispatch: boolProbe(func() bool { return HoldingDispatch(cfg.ProjectDir, cfg.AgentName) }),
		Sleep:    sleepProbeFunc(func(sid string) bool { return IsSleeping(cfg.ProjectDir, sid) }),
		Hold:     boolProbe(func() bool { return isHeldAt(cfg.ProjectDir, cfg.AgentName, cfg.HoldTTL, cfg.Clock) }),
		Operator: operatorProbeFunc(func(target string) bool { return cfg.PaneHost.OperatorAttached(panehost.Target(target)) }),
		Handoff:  configHandoffDocument{cfg: &cfg}, Journal: configJournalStore{cfg: &cfg},
		Emitter: emitter,
	}
	return deps
}

type cycleIDFunc func() string

func (f cycleIDFunc) Next() string { return f() }

type boolProbe func() bool

func (f boolProbe) IsManaged() bool       { return f() }
func (f boolProbe) CrispIdle() bool       { return f() }
func (f boolProbe) HoldingDispatch() bool { return f() }
func (f boolProbe) Held() bool            { return f() }

type sleepProbeFunc func(string) bool

func (f sleepProbeFunc) Sleeping(sid string) bool { return f(sid) }

type operatorProbeFunc func(string) bool

func (f operatorProbeFunc) Attached(target string) bool { return f(target) }

func configFromPolicyAndEnv(p CyclePolicy, env CycleEnv) CyclerConfig {
	return CyclerConfig{
		AgentName: env.AgentName, ProjectDir: env.ProjectDir, TmuxTarget: env.TmuxTarget,
		TranscriptDir: env.TranscriptDir, ActAbsTokens: p.ActAbsTokens,
		ActPctCeil: p.ActPctCeil, WarnAbsTokens: p.WarnAbsTokens, WarnPctCeil: p.WarnPctCeil,
		ForceActAbsTokens: p.ForceActAbsTokens, ForceActPctCeil: p.ForceActPctCeil,
		ActPct: p.ActPct, WarnPct: p.WarnPct, ForceActPct: p.ForceActPct,
		HardBandCycleOnly: p.HardBandCycleOnly,
		HandoffTimeout:    p.HandoffTimeout, ClearSettle: p.ClearSettle,
		PollInterval: p.PollInterval, ClearConfirmBackstop: p.ClearConfirmBackstop,
		ClearConfirmRetries: p.ClearConfirmRetries, ModelDoneTimeout: p.ModelDoneTimeout,
		ForceRetryInterval: p.ForceRetryInterval, IdleRestartAbsTokens: p.IdleRestartAbsTokens,
		IdleRestartCooldown: p.IdleRestartCooldown, BootGracePeriod: p.BootGracePeriod,
		MaxBootGraceTotal: p.MaxBootGraceTotal, MaxHandoffTimeouts: p.MaxHandoffTimeouts,
		HoldTTL: p.HoldTTL, OperatorTurnLookback: p.OperatorTurnLookback,
		PostAnswerGrace: p.PostAnswerGrace,
	}
}

type narrowGaugeAdapter struct {
	deps   CycleDeps
	policy CyclePolicy
	target string
}

func (a narrowGaugeAdapter) Snapshot(sid string) GateSnapshot {
	s := GateSnapshot{
		Managed: a.deps.Managed.IsManaged(), CrispIdle: a.deps.Idle.CrispIdle(),
		HoldingDispatch: a.deps.Dispatch.HoldingDispatch(), Held: a.deps.Hold.Held(),
	}
	if sid != "" {
		s.Sleeping = a.deps.Sleep.Sleeping(sid)
		if a.policy.OperatorTurnLookback > 0 {
			if at, ok := a.deps.Activity.LastUserTurn(sid); ok {
				s.LastUserTurnAt = at
			}
		}
		if a.policy.PostAnswerGrace > 0 {
			if at, ok := a.deps.Activity.LastAssistantTurn(sid); ok {
				s.LastAssistantTurnAt = at
			}
		}
	}
	if a.target != "" {
		s.OperatorAttached = a.deps.Operator.Attached(a.target)
	}
	return s
}
