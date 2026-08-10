package keeper

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

type CycleIDGenerator interface{ Next() string }

type PaneWriter interface {
	Inject(context.Context, string, string) error
	SendEscape(context.Context, string) error
	SetEnv(context.Context, string, string, string) error
}

type ContextStore interface {
	ReadGauge() (*CtxFile, time.Time, error)
	SetManagedSession(string) error
	ClearPrecompactTrigger() error
}

type ActivityProbe interface {
	IdleMarkerModTime() (time.Time, bool)
	LastUserTurn(string) (time.Time, bool)
	LastAssistantTurn(string) (time.Time, bool)
}

type (
	ManagedProbe          interface{ IsManaged() bool }
	IdleProbe             interface{ CrispIdle() bool }
	DispatchProbe         interface{ HoldingDispatch() bool }
	SleepProbe            interface{ Sleeping(string) bool }
	HoldProbe             interface{ Held() bool }
	OperatorPresenceProbe interface{ Attached(string) bool }
)

type HandoffDocument interface {
	Path() string
	Read() (string, error)
	ModTime() (time.Time, bool)
	ScrubNonce() error
}

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
	cfg.CycleIDGen = deps.CycleIDs.Next
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

// CycleDepsFromConfig adapts the legacy function seams into the validated
// dependency bundle. It is the migration bridge for production and old tests.
func CycleDepsFromConfig(cfg CyclerConfig, emitter Emitter) CycleDeps {
	cfg.applyDefaults()
	gauge := fnGauge{cfg: &cfg}
	handoff := fnHandoff{cfg: &cfg}
	deps := CycleDeps{
		Clock: cfg.Clock, CycleIDs: cycleIDFunc(cfg.CycleIDGen),
		Pane: fnPane{cfg: &cfg}, Context: gauge, Activity: gauge,
		Managed:  boolProbe(func() bool { return cfg.IsManagedFn(cfg.ProjectDir, cfg.AgentName) }),
		Idle:     boolProbe(func() bool { return cfg.CrispIdleFn(cfg.ProjectDir, cfg.AgentName) }),
		Dispatch: boolProbe(func() bool { return cfg.HoldingDispatchFn(cfg.ProjectDir, cfg.AgentName) }),
		Sleep:    sleepProbeFunc(func(sid string) bool { return cfg.SleepingCheckFn(cfg.ProjectDir, sid) }),
		Hold:     boolProbe(func() bool { return cfg.HeldCheckFn(cfg.ProjectDir, cfg.AgentName) }),
		Operator: operatorProbeFunc(cfg.OperatorAttachedFn),
		Handoff:  legacyHandoffDocument{port: handoff}, Journal: legacyJournalStore{port: handoff},
		Emitter: emitter, Respawn: cfg.Respawn,
	}
	if deps.Respawn == nil && cfg.ForceRestartFn != nil {
		deps.Respawn = fnRespawn{fn: cfg.ForceRestartFn}
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

type legacyHandoffDocument struct{ port HandoffPort }

func (a legacyHandoffDocument) Path() string               { return a.port.HandoffPath() }
func (a legacyHandoffDocument) Read() (string, error)      { return a.port.ReadHandoff() }
func (a legacyHandoffDocument) ModTime() (time.Time, bool) { return a.port.HandoffModTime() }
func (a legacyHandoffDocument) ScrubNonce() error          { return a.port.TruncateHandoff() }

type legacyJournalStore struct{ port HandoffPort }

func (a legacyJournalStore) Write(j *CycleJournal) error  { return a.port.WriteJournal(j) }
func (a legacyJournalStore) Read() (*CycleJournal, error) { return a.port.ReadJournal() }

type legacyActivityProbe struct{ port GaugePort }

func (a legacyActivityProbe) IdleMarkerModTime() (time.Time, bool) {
	return a.port.IdleMarkerModTime()
}

func (a legacyActivityProbe) LastUserTurn(string) (time.Time, bool) {
	return time.Time{}, false
}

func (a legacyActivityProbe) LastAssistantTurn(sid string) (time.Time, bool) {
	return a.port.LastAssistantTurn(sid)
}

func configFromPolicyAndEnv(p CyclePolicy, env CycleEnv) CyclerConfig {
	return CyclerConfig{
		AgentName: env.AgentName, ProjectDir: env.ProjectDir, TmuxTarget: env.TmuxTarget,
		TranscriptDir: env.TranscriptDir, ActAbsTokens: p.ActAbsTokens,
		ActPctCeil: p.ActPctCeil, WarnAbsTokens: p.WarnAbsTokens, WarnPctCeil: p.WarnPctCeil,
		ForceActAbsTokens: p.ForceActAbsTokens, ForceActPctCeil: p.ForceActPctCeil,
		ActPct: p.ActPct, WarnPct: p.WarnPct, ForceActPct: p.ForceActPct,
		HandoffTimeout: p.HandoffTimeout, ClearSettle: p.ClearSettle,
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

func (a narrowGaugeAdapter) ReadGauge() (*CtxFile, time.Time, error) {
	return a.deps.Context.ReadGauge()
}

func (a narrowGaugeAdapter) SetManagedSession(sid string) error {
	return a.deps.Context.SetManagedSession(sid)
}

func (a narrowGaugeAdapter) ClearPrecompactTrigger() error {
	return a.deps.Context.ClearPrecompactTrigger()
}

func (a narrowGaugeAdapter) IdleMarkerModTime() (time.Time, bool) {
	return a.deps.Activity.IdleMarkerModTime()
}

func (a narrowGaugeAdapter) LastAssistantTurn(sid string) (time.Time, bool) {
	if sid == "" {
		return time.Time{}, false
	}
	return a.deps.Activity.LastAssistantTurn(sid)
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
