package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

// briefRestartCmd is injected after /clear to re-orient the agent via the
// agent-manifest boot command. Identity re-pins from soul.md (I1, SPEC §4).
const briefRestartCmd = "harmonik agent brief --wake keeper-restart"

// CycleJournal is the on-disk state for an in-progress keeper reset cycle.
// Written atomically to .harmonik/keeper/<agent>.cycle before any injection.
//
// Phase transitions: "opened" → "handoff_injected" → "confirmed" → "cleared"
// → "resumed" → "complete" (happy path) or "aborted" (timeout path).
type CycleJournal struct {
	CycleID   string    `json:"cycle_id"`
	Phase     string    `json:"phase"`
	OpenedAt  time.Time `json:"opened_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Reason    string    `json:"reason,omitempty"`
}

// CyclerConfig configures the Phase-2 cycle core.
// Zero values for numeric/duration fields are replaced with the defaults below.
// Spec ref: codename:session-keeper §4.3 Phase-2 cycle core (hk-22i70, hk-kct9t).
type CyclerConfig struct {
	AgentName  string
	ProjectDir string
	TmuxTarget string // empty = skip real injection (test / warn-only mode)

	// Absolute-token thresholds (preferred when CtxFile.Tokens + WindowSize are
	// available). The effective act threshold is min(ActAbsTokens, ActPctCeil *
	// WindowSize); similarly for warn. This handles both 200k and 1M windows:
	// on a 200k window the pct-ceil wins (~170k); on a 1M window the abs cap
	// wins (215k) — preventing the 90%-pct gate from firing at ~900k tokens.
	// Refs: hk-cl74g, hk-8hr1.
	ActAbsTokens  int64   // absolute cycle threshold; default 215000
	ActPctCeil    float64 // pct-of-window cap for cycle gate; default 0.85
	WarnAbsTokens int64   // absolute warn/re-arm threshold; default 200000
	WarnPctCeil   float64 // pct-of-window cap for warn gate; default 0.70

	// ForceActAbsTokens / ForceActPctCeil define the hard upper threshold above
	// which the cycle fires unconditionally, bypassing the CrispIdle gate. This
	// ensures a perpetually-busy crew (one that never satisfies CrispIdle) still
	// gets cleared before context exhaustion. Effective threshold is
	// min(ForceActAbsTokens, ForceActPctCeil * WindowSize).
	// Refs: hk-0uu, hk-lhu2.
	ForceActAbsTokens int64   // default ActAbsTokens+25000 (i.e. 240000 with defaults)
	ForceActPctCeil   float64 // default 0.95

	// Pct-based fallbacks used when CtxFile.Tokens == 0 or WindowSize == 0
	// (older Claude Code versions that do not emit absolute token counts).
	ActPct      float64 // threshold to fire; default 90
	WarnPct     float64 // re-arm threshold; default 80
	ForceActPct float64 // forced-clear fallback pct; default 95

	HandoffTimeout time.Duration // wait for handoff nonce; default 300s (hk-4xni9 K2)
	ClearSettle    time.Duration // per-attempt wait for new session_id; default 10s (hk-4xni9 K3)
	PollInterval   time.Duration // polling cadence for nonce + settle; default 200ms

	// ClearConfirmBackstop / ClearConfirmRetries turn the clear->brief hand-off
	// into a HARD GATE (hk-vdqe2): the Clearing phase retries the ClearSettle poll
	// (defensively re-injecting /clear each retry, in case a busy pane never
	// consumed the first one) until a new session_id is confirmed OR this
	// backstop is exhausted, instead of firing the brief on the very first
	// missed poll. Only once the backstop is exhausted does the brief fire as a
	// last resort (still logged via session_keeper_clear_unconfirmed). Zero →
	// DefaultClearConfirmBackstop / DefaultClearConfirmRetries.
	ClearConfirmBackstop time.Duration
	ClearConfirmRetries  int

	// ModelDoneTimeout is the SR4 fail-open liveness bound (SK-014 / D12): the
	// maximum wait in AwaitModelDone for the model-done signal (.idle-marker
	// mtime ≥ t_nonce primary; assistant-transcript-turn backstop) after the
	// handoff is confirmed, before the reactor proceeds to Clearing anyway with
	// model_done{source:"timeout", degraded:true}. Must stay strictly less than
	// ClearConfirmBackstop (150s) so the SR4 wait cannot dominate the SR9
	// liveness budget. Zero → DefaultModelDoneTimeout (60s).
	ModelDoneTimeout time.Duration

	// Clock is the ClockPort — the determinism port for all cycle-timing reads
	// (SK-008/SK-R3, substrate D4; keeper requires the type by reference from
	// internal/substrate). Nil → substrate.SystemClock{} (production wall
	// clock). The cycle core reads time exclusively through this port so a
	// substrate.FakeClock can drive timeouts and poll cadences in virtual time.
	Clock substrate.ClockPort

	// Named ports (T6, session-keeper-design §1 / D10). When non-nil these
	// OVERRIDE the corresponding function-field adapters below: the cycle core
	// depends exclusively on the port interfaces (ports.go), and a nil port is
	// filled by NewCycler with the fn* adapter over the (defaulted) function
	// fields — so existing fn-field wiring and test fakes keep working while
	// T7's Step reactor drives every side effect through a port.
	Pane    PanePort
	Gauge   GaugePort
	Handoff HandoffPort
	Respawn RespawnPort // nil AND ForceRestartFn nil → escalation dormant

	// Injectable dependencies. Nil → production default. These are the WIRING
	// INPUTS for the fn* port adapters (ports.go); the cycle core never calls
	// them directly. CycleIDGen stays a config seam (not a port): the shell
	// mints cycle ids (design §2a).
	CycleIDGen      func() string
	IsManagedFn     func(projectDir, agentName string) bool
	HandoffFilePath func(projectDir, agentName string) string
	ReadHandoff     func(path string) (string, error)
	// HandoffModTimeFn returns the handoff file's modification time and whether it
	// exists. Nil → defaultHandoffModTime (os.Stat). Used by the ack-timeout
	// recovery path (hk-fi78d) to decide whether the agent actually WROTE a fresh
	// handoff despite the nonce echo never landing — in which case the brief
	// injection must still survive rather than blindly aborting before /clear.
	HandoffModTimeFn  func(path string) (time.Time, bool)
	TruncateHandoffFn func(path string) error
	// IdleMarkerModTimeFn reports the Stop-hook .idle marker's mtime and whether
	// it exists — the PRIMARY model-done source (SK-014): in AwaitModelDone the
	// shell reads it each detection tick and the first mtime ≥ t_nonce (the
	// nonce-confirmation instant, strict compare, NO crispIdleTolerance) yields
	// ModelDone{source:"idle_marker"}. Nil → defaultIdleMarkerModTime (os.Stat
	// on .harmonik/keeper/<agent>.idle).
	IdleMarkerModTimeFn      func(projectDir, agentName string) (time.Time, bool)
	InjectFn                 func(ctx context.Context, target, text string) error
	ReadGaugeFn              func(projectDir, agentName string) (*CtxFile, time.Time, error)
	CrispIdleFn              func(projectDir, agentName string) bool
	HoldingDispatchFn        func(projectDir, agentName string) bool
	WriteJournalFn           func(path string, j *CycleJournal) error
	ReadJournalFn            func(path string) (*CycleJournal, error)
	ClearPrecompactTriggerFn func(projectDir, agentName string) error

	// SetManagedSessionFn writes the new session_id into .managed after a cycle
	// completes post-/clear. This unblocks the watcher's session_id binding so it
	// resumes monitoring the resumed session. Called unconditionally: an empty
	// sessionID clears the binding so the .sid channel can rebind the next
	// session (IsManaged stays true; only the binding is cleared). Nil →
	// WriteManagedSessionID. (Refs: hk-igt, hk-uxu)
	SetManagedSessionFn func(projectDir, agent, sessionID string) error

	// SetTmuxEnvFn sets a key=value in the tmux session that owns TmuxTarget.
	// Called after nonce confirmation so HARMONIK_AGENT is inherited by the
	// new Claude process started after /clear. Nil → default tmux setenv call.
	// No-op when TmuxTarget is empty.
	SetTmuxEnvFn func(ctx context.Context, target, key, value string) error

	// ForceRetryInterval is the minimum duration after a forced-clear attempt
	// (above ForceActPct) before the keeper retries on the same session_id.
	// After an abort (handoff_timeout) or a completed forced cycle, the
	// same-session Gate 6 suppression is lifted only after this interval.
	// Prevents tight retry loops while still guaranteeing eventual clearance.
	// Default: 120s. Refs: hk-qoz (forced-clear catch-22 fix).
	ForceRetryInterval time.Duration

	// IdleRestartAbsTokens is the absolute-token floor above which an idle crew
	// session (CrispIdle + not HoldingDispatch) is restarted to compact its
	// context to a small baseline. Below this threshold the keeper emits
	// session_keeper_idle_crew (notifying the captain) without restarting.
	// Default: 150_000. Refs: hk-ee81.
	IdleRestartAbsTokens int64

	// IdleRestartCooldown is the minimum duration between consecutive
	// idle-restart attempts. Prevents repeated restarts if a crew re-enters
	// the idle+large-ctx state quickly after resuming.
	// Default: 30 minutes. Refs: hk-ee81.
	IdleRestartCooldown time.Duration

	// BootGracePeriod is the minimum duration after a session_id CHANGE before
	// the keeper starts a cycle on the new session. During the grace window all
	// cycle-gate checks return immediately, preventing forced-clear cycles from
	// firing while the agent is still booting after an agent-brief restart (the
	// agent cannot respond to /session-handoff during boot).
	// Zero (the package default) disables boot-grace; set to a positive value
	// in production (e.g. 5 * time.Minute). The grace applies only when the
	// session_id CHANGES — not on first Cycler startup — so an already-running
	// agent is monitored without delay on keeper boot.
	// EXCEPTION: an agent above ForceActPct bypasses the grace (hk-ibb fix 1).
	// Refs: hk-4f8 (bad-trigger-timing + no-re-arm fix), hk-ibb (follow-up).
	BootGracePeriod time.Duration

	// MaxBootGraceTotal is an upper bound on how long boot-grace can suppress
	// cycles across all SID transitions since the first grace was armed. After
	// this total duration the grace gate is skipped even if per-SID grace has
	// not yet expired. This caps the total latency added by grace in adversarial
	// SID-flapping scenarios. Default: 2 * BootGracePeriod when BootGracePeriod
	// > 0, otherwise zero (disabled). Refs: hk-ibb (fix 2 — flap cap).
	MaxBootGraceTotal time.Duration

	// MaxHandoffTimeouts is the number of consecutive handoff timeouts above
	// the force threshold before escalating to ForceRestartFn. Zero disables
	// escalation. Default: 3. Refs: hk-qoz.
	MaxHandoffTimeouts int

	// ForceRestartFn, when non-nil, is called after MaxHandoffTimeouts
	// consecutive handoff timeouts while above the force threshold. Expected
	// to kill and restart the agent (e.g. via the respawn path). Non-fatal:
	// a failure is logged but does not stop the keeper loop. Refs: hk-qoz.
	ForceRestartFn func(ctx context.Context, agentName string) error

	// SendEscapeFn, when non-nil, is called before injecting /session-handoff
	// to preempt any in-progress input on a busy pane. Nil → no Escape sent.
	// Set to keeper.SendEscapeKey in production; leave nil in tests.
	// Refs: hk-qoz (forced-clear busy-pane fix).
	SendEscapeFn func(ctx context.Context, target string) error

	// OperatorAttachedFn reports whether a human operator is currently attached
	// to the target tmux session. When it returns true the act-path goes
	// warn-only: the destructive reset-cycle injection (/session-handoff,
	// /clear, agent brief) is suppressed so the keeper never races the
	// operator's own keystrokes and clobbers an in-flight turn. The watcher's
	// warn/gauge emissions continue, and the cycle resumes on the next tick
	// once the operator detaches. Nil → OperatorAttached (real tmux
	// list-clients). The check is skipped entirely when TmuxTarget is empty
	// (no pane to inject into). Refs: hk-6qf.
	OperatorAttachedFn func(target string) bool

	// OperatorAttachedSampleInterval bounds how often the poll-tick
	// session_keeper_operator_attached event is persisted. Gate 7 re-checks live
	// tmux on every watcher tick (~5s); without this throttle every tick wrote a
	// durable event (logmine F55: 51% of one events.jsonl window). One sample per
	// interval keeps the digest resolver's attached-source fresh (its
	// AttachedInactiveTimeout default is 5m) while cutting event volume ~12x.
	// Zero → defaultOperatorAttachedSampleInterval. Refs: hk-2yvx.
	OperatorAttachedSampleInterval time.Duration

	// SleepingCheckFn reports whether the session identified by sessionID is
	// currently parked by the QuiesceArbiter (.harmonik/.sleeping.<sessionID>,
	// M1 / hk-jeby). MaybeRun returns nil (cycle deferred) when this returns
	// true, so the keeper does not inject /session-handoff into a sleeping
	// session. M1's max-sleep failsafe wakes the session first; the keeper acts
	// on the next tick after the marker is cleared. When nil, IsSleeping is used.
	// Refs: hk-l3gs, hk-jeby.
	SleepingCheckFn func(projectDir, sessionID string) bool

	// HoldTTL is the keeper HOLD timer backstop; zero → DefaultHoldTTL.
	HoldTTL time.Duration

	// HeldCheckFn reports whether a fresh, session-scoped operator HOLD is active
	// (D5). MaybeRun returns nil (cycle deferred) when true — the destructive
	// clear/restart is suspended while WARN still fires. Auto-reverts structurally
	// (keyed by the re-minted session-id) plus a timer backstop. When nil, a
	// closure over IsHeld(.,.,HoldTTL) is used. Refs: hk-9waz.
	HeldCheckFn func(projectDir, agent string) bool

	// TranscriptDir is the Claude Code transcript projects directory (~/.claude/projects/<munged>).
	// When empty the cycler derives it from ProjectDir via transcriptDirFor.
	// Set explicitly in tests to avoid touching the real transcript directory.
	// Refs: hk-74iyd.
	TranscriptDir string

	// OperatorTurnLookback is the maximum age of a real inbound operator user
	// turn in the session transcript that triggers Gate 5d auto-hold: when a
	// user turn (not exclusively tool_result content) landed within this window,
	// SetHold is called and ACT is deferred for this tick. The hold auto-reverts
	// via the existing session-id keying and TTL backstop (Gates.go). Zero
	// disables Gate 5d. Refs: hk-74iyd.
	OperatorTurnLookback time.Duration

	// PostAnswerGrace is the minimum duration after the agent's most recent real
	// assistant text turn (content includes a "text" item) before ACT may fire.
	// Gate 5e: the operator may still be reading the response. Unlike Gate 5d
	// this does NOT write a hold marker; it is a transient tick-level deferral
	// that lifts automatically when the grace window expires. Zero disables
	// Gate 5e. Refs: hk-74iyd.
	PostAnswerGrace time.Duration

	// RecentTranscriptTurnFn returns the timestamp of the most recent "real"
	// transcript entry with the given role ("user" or "assistant") under
	// transcriptDir/sessionID.jsonl. Nil → recentTranscriptTurn (production).
	// Injectable for tests that write controlled transcript files. Refs: hk-74iyd.
	RecentTranscriptTurnFn func(transcriptDir, sessionID, role string) (time.Time, bool)

	// hasRespawn is set by NewCycler once the RespawnPort is bound; the pure
	// reactor reads it (a policy scalar, not IO) to reproduce the pre-rebuild
	// "count marches but never fires when no respawn is wired" escalation
	// semantics exactly (cycle.go history, hk-qoz).
	hasRespawn bool
}

// defaultOperatorAttachedSampleInterval is the default Gate-7 emission sample
// window: at most one operator_attached event per minute while an operator stays
// attached. Well inside the digest resolver's 5m AttachedInactiveTimeout so
// suppression stays pinned, but ~12x below the ~5s poll cadence. Refs: hk-2yvx.
const defaultOperatorAttachedSampleInterval = time.Minute

func (c *CyclerConfig) applyDefaults() {
	// Threshold defaults are sourced from thresholds.go (the single source of
	// truth shared with WatcherConfig.applyDefaults). Refs: hk-bpkv.
	if c.ActAbsTokens <= 0 {
		c.ActAbsTokens = defaultActAbsTokens
	}
	if c.ActPctCeil <= 0 {
		c.ActPctCeil = defaultActPctCeil
	}
	if c.WarnAbsTokens <= 0 {
		c.WarnAbsTokens = defaultWarnAbsTokens
	}
	if c.WarnPctCeil <= 0 {
		c.WarnPctCeil = defaultWarnPctCeil
	}
	if c.ActPct <= 0 {
		c.ActPct = defaultActPct
	}
	if c.WarnPct <= 0 {
		c.WarnPct = defaultWarnPct
	}
	// ForceAct thresholds are derived from their corresponding act thresholds so
	// that a custom --act-pct/--act-abs-tokens never creates a dead zone where
	// context is above the act gate but below the force-clear gate (hk-6el).
	// Offset is +25k per the TA1 band-retune (hk-8hr1): the resulting default
	// force_act=240k (act 215k + 25k) is the final operator-decided value.
	if c.ForceActAbsTokens <= 0 {
		c.ForceActAbsTokens = c.ActAbsTokens + defaultForceActAbsOffset
	}
	if c.ForceActPctCeil <= 0 {
		c.ForceActPctCeil = c.ActPctCeil + defaultForceActPctCeilOffset
	}
	if c.ForceActPct <= 0 {
		c.ForceActPct = c.ActPct + defaultForceActPctOffset
	}
	if c.ForceRetryInterval <= 0 {
		c.ForceRetryInterval = DefaultForceRetryInterval
	}
	if c.OperatorAttachedSampleInterval <= 0 {
		c.OperatorAttachedSampleInterval = defaultOperatorAttachedSampleInterval
	}
	if c.BootGracePeriod > 0 && c.MaxBootGraceTotal <= 0 {
		c.MaxBootGraceTotal = 2 * c.BootGracePeriod
	}
	if c.MaxHandoffTimeouts <= 0 {
		c.MaxHandoffTimeouts = DefaultMaxHandoffTimeouts
	}
	if c.HandoffTimeout <= 0 {
		c.HandoffTimeout = DefaultHandoffTimeout
	}
	if c.ClearSettle <= 0 {
		c.ClearSettle = DefaultClearSettle
	}
	if c.PollInterval <= 0 {
		c.PollInterval = DefaultCyclerPollInterval
	}
	if c.ClearConfirmBackstop <= 0 {
		c.ClearConfirmBackstop = DefaultClearConfirmBackstop
	}
	if c.ClearConfirmRetries <= 0 {
		c.ClearConfirmRetries = DefaultClearConfirmRetries
	}
	if c.ModelDoneTimeout <= 0 {
		c.ModelDoneTimeout = DefaultModelDoneTimeout
	}
	if c.Clock == nil {
		c.Clock = substrate.SystemClock{}
	}
	if c.CycleIDGen == nil {
		c.CycleIDGen = newCycleIDGen(c.Clock)
	}
	if c.IsManagedFn == nil {
		c.IsManagedFn = IsManaged
	}
	if c.HandoffFilePath == nil {
		c.HandoffFilePath = defaultHandoffFilePath
	}
	if c.ReadHandoff == nil {
		c.ReadHandoff = defaultReadHandoff
	}
	if c.HandoffModTimeFn == nil {
		c.HandoffModTimeFn = defaultHandoffModTime
	}
	if c.TruncateHandoffFn == nil {
		c.TruncateHandoffFn = defaultTruncateHandoff
	}
	if c.IdleMarkerModTimeFn == nil {
		c.IdleMarkerModTimeFn = defaultIdleMarkerModTime
	}
	if c.InjectFn == nil {
		// Bind the production injector to the cycle Clock so the settle/retry
		// sleeps honor the determinism port (the T5 injectorClock fold).
		clock := c.Clock
		c.InjectFn = func(ctx context.Context, target, text string) error {
			return injectTextClocked(ctx, clock, target, text)
		}
	}
	if c.ReadGaugeFn == nil {
		c.ReadGaugeFn = ReadCtxFile
	}
	if c.CrispIdleFn == nil {
		c.CrispIdleFn = CrispIdle
	}
	if c.HoldingDispatchFn == nil {
		c.HoldingDispatchFn = HoldingDispatch
	}
	if c.WriteJournalFn == nil {
		c.WriteJournalFn = writeJournalFile
	}
	if c.ReadJournalFn == nil {
		c.ReadJournalFn = defaultReadJournal
	}
	if c.ClearPrecompactTriggerFn == nil {
		c.ClearPrecompactTriggerFn = ClearPrecompactTrigger
	}
	if c.SetManagedSessionFn == nil {
		c.SetManagedSessionFn = WriteManagedSessionID
	}
	if c.SetTmuxEnvFn == nil {
		c.SetTmuxEnvFn = SetTmuxEnv
	}
	if c.OperatorAttachedFn == nil {
		c.OperatorAttachedFn = OperatorAttached
	}
	if c.SleepingCheckFn == nil {
		c.SleepingCheckFn = IsSleeping
	}
	if c.HoldTTL <= 0 {
		c.HoldTTL = DefaultHoldTTL
	}
	if c.HeldCheckFn == nil {
		ttl := c.HoldTTL
		clock := c.Clock
		c.HeldCheckFn = func(projectDir, agent string) bool { return isHeldAt(projectDir, agent, ttl, clock) }
	}
	if c.IdleRestartAbsTokens <= 0 {
		c.IdleRestartAbsTokens = DefaultIdleRestartAbsTokens
	}
	if c.IdleRestartCooldown <= 0 {
		c.IdleRestartCooldown = DefaultIdleRestartCooldown
	}
}

// actThreshold returns the effective absolute-token cycle threshold for the
// given windowSize. It returns min(ActAbsTokens, int64(ActPctCeil * windowSize))
// when windowSize > 0, ensuring the gate fires early enough on both 200k and 1M
// windows. When windowSize == 0 (old .ctx without window data) returns ActAbsTokens
// so callers can still apply it as a hard cap if they have a token count.
func (c *CyclerConfig) actThreshold(windowSize int64) int64 {
	return minAbsOrPctCeil(c.ActAbsTokens, c.ActPctCeil, windowSize)
}

// warnThreshold returns the effective absolute-token warn/re-arm threshold for
// the given windowSize, using the same min(abs, pct*window) formula as actThreshold.
func (c *CyclerConfig) warnThreshold(windowSize int64) int64 {
	return minAbsOrPctCeil(c.WarnAbsTokens, c.WarnPctCeil, windowSize)
}

// belowActThreshold reports whether cf is below the cycle-trigger threshold.
// Uses absolute tokens when both Tokens and WindowSize are available; otherwise
// falls back to Pct vs ActPct (backwards compat for old .ctx files).
func (c *CyclerConfig) belowActThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		return cf.Tokens < c.actThreshold(cf.WindowSize)
	}
	return cf.Pct < c.ActPct
}

// belowWarnThreshold reports whether cf is below the warn/re-arm threshold.
// Uses absolute tokens when available, otherwise falls back to Pct vs WarnPct.
// pct<WarnPct is a NECESSARY condition — see WatcherConfig.belowWarnThreshold
// (watcher.go) for the rationale. Byte-identical logic. Refs: hk-lbo9w.
func (c *CyclerConfig) belowWarnThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		return cf.Pct < c.WarnPct || cf.Tokens < c.warnThreshold(cf.WindowSize)
	}
	return cf.Pct < c.WarnPct
}

// forceActThreshold returns the effective absolute-token forced-clear threshold
// using the same min(abs, pct*window) formula as actThreshold.
func (c *CyclerConfig) forceActThreshold(windowSize int64) int64 {
	return minAbsOrPctCeil(c.ForceActAbsTokens, c.ForceActPctCeil, windowSize)
}

// aboveForceThreshold reports whether cf is at or above the hard forced-clear
// threshold. Uses absolute tokens when available; falls back to ForceActPct.
// Refs: hk-0uu.
func (c *CyclerConfig) aboveForceThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		return cf.Tokens >= c.forceActThreshold(cf.WindowSize)
	}
	return cf.Pct >= c.ForceActPct
}

// newCycleIDGen returns a closure that generates collision-resistant cycle IDs.
// The ID includes a startup-time timestamp prefix so IDs issued by different
// process instances never collide, addressing DEFECT-2 (stale on-disk nonce).
func newCycleIDGen(clock substrate.ClockPort) func() string {
	prefix := clock.Now().UTC().Format("20060102T150405")
	var seq uint64
	return func() string {
		n := atomic.AddUint64(&seq, 1)
		return fmt.Sprintf("cyc-%s-%06d", prefix, n)
	}
}

func defaultHandoffFilePath(projectDir, agentName string) string {
	return filepath.Join(projectDir, fmt.Sprintf("HANDOFF-%s.md", agentName))
}

func defaultReadHandoff(path string) (string, error) {
	//nolint:gosec // G304: path derived from operator-controlled projectDir + agentName
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// defaultHandoffModTime reports the handoff file's mtime and existence via
// os.Stat. A missing/unreadable file returns (zero, false). Refs: hk-fi78d.
func defaultHandoffModTime(path string) (time.Time, bool) {
	fi, err := os.Stat(path) //nolint:gosec // G304: path is operator-controlled projectDir + agentName
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

// defaultIdleMarkerModTime reports the Stop-hook .idle marker's mtime and
// existence via os.Stat — the production primary model-done source (SK-014).
// A missing/unreadable marker returns (zero, false): the shell then falls to
// the transcript backstop and, ultimately, the model_done_timeout fail-open.
func defaultIdleMarkerModTime(projectDir, agent string) (time.Time, bool) {
	fi, err := os.Stat(idleMarkerPath(projectDir, agent))
	if err != nil {
		return time.Time{}, false
	}
	return fi.ModTime(), true
}

func defaultTruncateHandoff(path string) error {
	//nolint:gosec // G304,G306: path is operator-controlled; 0600 — keeper-owned
	return os.WriteFile(path, []byte{}, 0o600)
}

func writeJournalFile(path string, j *CycleJournal) error {
	data, err := json.Marshal(j)
	if err != nil {
		return fmt.Errorf("keeper: marshal journal: %w", err)
	}
	// Ensure the parent directory exists (keeper dir may not exist yet in tests).
	if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil { //nolint:gosec // G301: 0755 matches .harmonik conventions
		return fmt.Errorf("keeper: create journal dir: %w", mkErr)
	}
	//nolint:gosec // G306: 0600 — keeper-owned file; no world-read needed
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("keeper: write journal %q: %w", path, err)
	}
	return nil
}

func defaultReadJournal(path string) (*CycleJournal, error) {
	//nolint:gosec // G304: path derived from operator-controlled projectDir + agentName
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var j CycleJournal
	if err := json.Unmarshal(bytes.TrimSpace(data), &j); err != nil {
		return nil, fmt.Errorf("keeper: parse journal %q: %w", path, err)
	}
	return &j, nil
}

// nonceMarker returns the HTML-comment nonce embedded in the handoff file
// to confirm that the agent wrote the handoff for this specific cycle.
func nonceMarker(cycleID string) string {
	return fmt.Sprintf("<!-- KEEPER:%s -->", cycleID)
}

// nonceMarkerPrefix is the stable prefix shared by every keeper nonce marker.
// Its presence (with a value other than the current nonce) signals a leftover
// nonce from a prior cycle that must be cleared before polling. Refs: hk-vpnp.
const nonceMarkerPrefix = "<!-- KEEPER:"

// NOTE (T7): the stale-nonce PREDICATE is now the pure
// handoffContentHasStaleNonce (step.go), evaluated by the reactor over the
// handoff content the shell samples onto the firing entry event. The reading
// stayed shell-side and fire-aligned so ReadHandoff call counts match the
// pre-rebuild code exactly (hk-vpnp / Bug 3b semantics unchanged).

// isOnlyNonce reports whether every keeper nonce marker in content equals
// currentNonce (i.e. there is no foreign/stale nonce present).
func isOnlyNonce(content, currentNonce string) bool {
	rest := content
	for {
		i := strings.Index(rest, nonceMarkerPrefix)
		if i < 0 {
			return true
		}
		// Extract the full marker up to the closing "-->".
		tail := rest[i:]
		end := strings.Index(tail, "-->")
		if end < 0 {
			// Malformed marker; treat as stale to be safe.
			return false
		}
		marker := tail[:end+len("-->")]
		if marker != currentNonce {
			return false
		}
		rest = tail[end+len("-->"):]
	}
}

// Cycler is the IMPERATIVE SHELL around the pure Cycle reactor (T7). It owns
// the ports, the ClockPort timer deadlines, and the detection poll; ALL cycle
// decisions — the 11-gate ladder, the phase machine, and every
// anti-loop/hysteresis rule (hk-vpnp / hk-qoz / hk-4f8 / hk-ibb / hk-hz9 /
// hk-4i0s scars included) — live in the pure Step reactor (step.go), whose
// CycleState carries what used to be this struct's mutable fields.
//
// It is safe to call MaybeRun on every watcher tick; while a cycle is in
// flight the call blocks until the terminal — reproducing the pre-rebuild
// synchronous freeze (SK-017 / D11) — so the Cycler stays single-goroutine.
//
// Spec ref: specs/session-keeper.md SK-009..011, SK-016/017.
type Cycler struct {
	cfg     CyclerConfig
	emitter Emitter

	// The named ports (T6). The shell routes EVERY side effect through these;
	// they are filled by NewCycler from cfg.Pane/Gauge/Handoff/Respawn or,
	// when nil, from the fn* adapters over the defaulted function fields.
	// respawn stays nil when neither cfg.Respawn nor cfg.ForceRestartFn is set
	// (escalation dormant).
	pane    PanePort
	gauge   GaugePort
	handoff HandoffPort
	respawn RespawnPort

	// machine is the pure Step reactor holding ALL cycle state (design §3c).
	machine *Cycle

	// Shell-owned timer deadlines for the reactor's ArmTimer/CancelTimer
	// actions (SK-010); timersArmed marks that the last action batch re-armed
	// a timer, telling the drive loop to start a fresh detection ticker
	// (first-tick-after-interval per wait segment — parity risk #4).
	timers      map[TimerKind]time.Time
	timersArmed bool

	// handoffInjectedAt is the freshness anchor captured immediately before
	// the /session-handoff injection (hk-fi78d); consumed by the shell's
	// handoff-timeout freshness sample.
	handoffInjectedAt time.Time
}

// NewCycler constructs a Cycler. Defaults are applied to zero-valued config
// fields, then the named ports are bound: an explicitly injected port wins;
// otherwise the fn* adapter over the (defaulted) function fields is used, so
// runtime behavior is unchanged (the production adapters wire the real fns).
func NewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	cfg.applyDefaults()
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	c := &Cycler{cfg: cfg, emitter: emitter}
	c.pane = c.cfg.Pane
	if c.pane == nil {
		c.pane = fnPane{cfg: &c.cfg}
	}
	c.gauge = c.cfg.Gauge
	if c.gauge == nil {
		c.gauge = fnGauge{cfg: &c.cfg}
	}
	c.handoff = c.cfg.Handoff
	if c.handoff == nil {
		c.handoff = fnHandoff{cfg: &c.cfg}
	}
	c.respawn = c.cfg.Respawn
	if c.respawn == nil && c.cfg.ForceRestartFn != nil {
		c.respawn = fnRespawn{fn: c.cfg.ForceRestartFn}
	}
	// The pure reactor reads policy scalars (and whether escalation is wired)
	// from the defaulted config; it never calls a fn-field or port.
	c.cfg.hasRespawn = c.respawn != nil
	c.machine = NewCycle(&c.cfg)
	return c
}

// InCycle reports whether a restart cycle is currently in flight (the reactor
// is off-Idle). The watcher's tick loop consults this to park all non-cycle
// processing while a cycle runs (the InCycle suppression, SK-017 / D11).
func (c *Cycler) InCycle() bool { return c.machine.InCycle() }

// journalFilePath returns the path to the cycle journal file for the agent:
// <projectDir>/.harmonik/keeper/<agent>.cycle.
func journalFilePath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".cycle")
}

// MaybeRun checks all gate conditions and runs the cycle if they are all met.
//
// Gate order:
//  1. .managed opt-in guard (DEFECT-3: enforced here, not only in the caller).
//  2. Empty session_id → refuse; cannot establish anti-loop identity (DEFECT-1).
//  3. Observe pct for re-arm tracking (low pct on new session_id).
//  4. pct >= act_pct threshold.
//  5. CrispIdle.
//  6. NOT HoldingDispatch (fail-closed).
//  7. Full anti-loop suppression: stay suppressed after a cycle (complete or
//     aborted) until BOTH (a) new session_id AND (b) pct<WarnPct observed on
//     that new session_id.
//
// It is safe to call on every watcher tick; gating is done internally by the
// pure reactor (stepIdleGaugeTick, step.go — the ladder above, verbatim, as a
// pure function of the event-carried GateSnapshot per SK-011). This method is
// the shell entry: it samples the per-tick GateSnapshot read-burst, stamps
// the event with the Clock, and — when the ladder passes — drives the cycle
// SYNCHRONOUSLY to its terminal (the InCycle freeze, SK-017).
func (c *Cycler) MaybeRun(ctx context.Context, cf *CtxFile) error {
	// A CF-less tick carries no gauge/session identity: the reactor's gate ladder
	// (stepIdleGaugeTick) dereferences ev.CF unconditionally, so a nil cf would
	// crash the keeper. Skip gracefully — mirrors RunForIdle's nil guard.
	if cf == nil {
		return nil
	}
	snap := c.gauge.Snapshot(cf.SessionID)
	return c.runEntry(ctx, Event{
		Kind:  EvGaugeTick,
		At:    c.cfg.Clock.Now(),
		CF:    cf,
		Gates: snap,
	})
}

// resolvedTranscriptDir returns the effective transcript directory: the
// configured TranscriptDir when set, otherwise derived from ProjectDir.
// Refs: hk-74iyd.
func (c *CyclerConfig) resolvedTranscriptDir() string {
	if c.TranscriptDir != "" {
		return c.TranscriptDir
	}
	return transcriptDirFor(c.ProjectDir)
}

// recentTurnFn returns the effective RecentTranscriptTurnFn: the configured
// one when set, otherwise the production recentTranscriptTurn. Refs: hk-74iyd.
func (c *CyclerConfig) recentTurnFn() func(transcriptDir, sessionID, role string) (time.Time, bool) {
	if c.RecentTranscriptTurnFn != nil {
		return c.RecentTranscriptTurnFn
	}
	return recentTranscriptTurn
}

// NOTE (T7): the hk-fi78d freshness recovery is now split between the
// shell's handoff-timeout sample (Cycler.sampleHandoffFreshness, shell.go —
// the reads, verbatim semantics incl. the injection-time anchor and the
// load-bearing non-empty-content check) and the pure TimerFired(handoff_
// timeout) recovered edge (step.go).

// NOTE (T7): runCycle and completeCycleTail are DISSOLVED into the pure Step
// reactor (step.go: stepStartCycle → stepAbort / stepEnterClearing /
// stepClearUnconfirmed / stepBriefing) plus the shell drive loop (shell.go).
// The SAFETY invariant is structural now: /clear is reachable ONLY through
// AwaitModelDone, which is reachable only via the nonce-confirmed or the
// freshness-recovered edge — the abort path never clears (SK-INV-001).

// RecoverFromCrash checks for an in-progress cycle journal on boot and takes
// corrective action based on the last recorded phase.
//
//   - phase "cleared": /clear was issued before the crash; inject briefRestartCmd
//     to complete the interrupted cycle, update journal to "complete", emit
//     session_keeper_cycle_recovered.
//   - phase "opened" / "handoff_injected" / "confirmed": /clear was NOT issued;
//     update journal to "aborted" with reason "crash_before_clear", no injection.
//   - phase "resumed": brief was already injected; close journal.
//   - phase "complete" / "aborted": terminal state; no-op.
//
// Respects the .managed guard: no injection if not managed.
// Does NOT reuse the stale cycle_id nonce from the journal (DEFECT-2): the
// recovery path injects briefRestartCmd directly without a nonce poll, so a
// stale nonce cannot trigger unintended behaviour.
func (c *Cycler) RecoverFromCrash(ctx context.Context) error {
	// Fail-closed: only act on a managed agent. Boot-time entry point (the
	// reactor's CrashJournal event): the gate input comes from the same
	// per-entry GateSnapshot burst as the tick entry points.
	if !c.gauge.Snapshot("").Managed {
		return nil
	}

	j, err := c.handoff.ReadJournal()
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no journal = no crash to recover
		}
		return fmt.Errorf("keeper: read recovery journal: %w", err)
	}

	// One-shot fast-forward/close-out per the crash-recovery matrix
	// (stepIdleCrashJournal, step.go): no drive loop — the machine never
	// leaves Idle on this event.
	return c.feed(ctx, Event{Kind: EvCrashJournal, At: c.cfg.Clock.Now(), Journal: j})
}

// RunForPrecompact is the PreCompact-backstop entry point. It is called by the
// watcher when it detects the .precompact trigger marker written by
// keeper-precompact-hook.sh. Unlike MaybeRun, it skips the CrispIdle and
// act_pct gates — the PreCompact hook implies the context window is at or
// near the compaction threshold, and the agent is not necessarily at an
// await-input boundary.
//
// Gate order (subset of MaybeRun):
//  1. .managed opt-in guard.
//  2. Non-empty session_id (anti-loop identity requires it).
//     2b. Boot-grace: if the session is within its grace window, defer (clear
//     marker so the next PreCompact fire gets a clean slate). The grace state
//     is populated by MaybeRun, which the watcher ALWAYS calls before
//     RunForPrecompact (watcher.go:568 before :580) — that ordering is the
//     load-bearing invariant keeping this state current without re-computing it
//     here. Force-path exception: above ForceActPct, bypass grace.
//  3. NOT HoldingDispatch (fail-closed: skip cycle, clear marker → next PreCompact is fail-open).
//     3b. NOT operator HOLD — skip cycle while co-working hold is active (clear
//     marker so the next PreCompact fire gets a clean slate). Refs: hk-4rago.
//  4. Anti-loop suppression (same policy as MaybeRun).
//
// The .precompact marker is ALWAYS cleared regardless of which gate fires, so
// the shell script's "marker present → fail-open" bounded-fallback kicks in
// cleanly: the keeper makes exactly one decision per marker write, then the
// next PreCompact fire starts fresh.
//
// Emits session_keeper_precompact_blocked with the action taken.
//
// Spec ref: keeper-precompact-hook.sh; docs/components/internal/keeper-precompact.md.
// Refs: hk-aalsm.
func (c *Cycler) RunForPrecompact(ctx context.Context, cf *CtxFile) error {
	sessionID := ""
	if cf != nil {
		sessionID = cf.SessionID
	}

	// Per-entry gate-input read-burst (T6): sampled fresh at THIS entry point —
	// not shared with a MaybeRun that may have run a full blocking cycle on the
	// same tick — so gate values match the old live reads. The gate subset,
	// the per-gate precompact_blocked emissions, and the always-clear-marker
	// contract live in the pure reactor (stepIdlePrecompact, step.go).
	snap := c.gauge.Snapshot(sessionID)

	return c.runEntry(ctx, Event{
		Kind:  EvPrecompactTrigger,
		At:    c.cfg.Clock.Now(),
		CF:    cf,
		Gates: snap,
	})
}

// RunForIdle is the idle-large-context entry point. Called by the watcher
// on each fresh-gauge tick for sessions below the act threshold. When the
// session is CrispIdle, not HoldingDispatch, and tokens >= IdleRestartAbsTokens,
// triggers a full handoff cycle to restart the agent to a small context.
//
// Gate order:
//  1. cf != nil (requires a live gauge).
//  2. Tokens >= IdleRestartAbsTokens: if below, emit session_keeper_idle_crew
//     and return (no restart; captain may reap).
//  3. Tokens < effective act threshold: don't double-fire with MaybeRun.
//  4. CrispIdle (pane quiescent).
//  5. NOT HoldingDispatch (fail-closed: in-flight work → skip).
//     5b. NOT operator HOLD — skip idle restart while co-working hold is active.
//     Refs: hk-4rago.
//  6. IdleRestartCooldown: time since last idle restart >= cooldown.
//  7. Anti-loop: session_id must differ from lastFiredSID.
//
// Refs: hk-ee81.
func (c *Cycler) RunForIdle(ctx context.Context, cf *CtxFile) error {
	if cf == nil {
		return nil
	}

	// Per-entry gate-input read-burst (T6) — see RunForPrecompact for why each
	// entry point samples its own snapshot. The idle gate ladder (incl. the
	// hk-4i0s stamp-then-unwind cooldown discipline and the hk-qshh8
	// once-per-SID idle_crew notification) lives in the pure reactor
	// (stepIdleRestartTick, step.go).
	snap := c.gauge.Snapshot(cf.SessionID)

	return c.runEntry(ctx, Event{
		Kind:  EvIdleRestartTick,
		At:    c.cfg.Clock.Now(),
		CF:    cf,
		Gates: snap,
	})
}

// NOTE (T7): the emit* helpers, the Gate-7 operator-attached throttle, and
// the two blocking poll loops (pollForNonce, waitForNewSessionID /
// waitForNewSessionIDWithBackstop) are DISSOLVED: emissions are pure Emit
// actions built in step.go (emitOperatorAttached stays a deliberate NO-OP —
// logmine TA3/F55 — represented by Gate 7 emitting NOTHING while still
// advancing the hk-2yvx sample throttle in CycleState); the poll loops are
// the shell drive loop's detection ticks + armed-timer deadlines (shell.go,
// SK-010).
