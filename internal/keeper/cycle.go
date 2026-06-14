package keeper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

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
	// wins (300k) — preventing the 90%-pct gate from firing at ~900k tokens.
	// Refs: hk-cl74g.
	ActAbsTokens  int64   // absolute cycle threshold; default 300000
	ActPctCeil    float64 // pct-of-window cap for cycle gate; default 0.85
	WarnAbsTokens int64   // absolute warn/re-arm threshold; default 270000
	WarnPctCeil   float64 // pct-of-window cap for warn gate; default 0.70

	// ForceActAbsTokens / ForceActPctCeil define the hard upper threshold above
	// which the cycle fires unconditionally, bypassing the CrispIdle gate. This
	// ensures a perpetually-busy crew (one that never satisfies CrispIdle) still
	// gets cleared before context exhaustion. Effective threshold is
	// min(ForceActAbsTokens, ForceActPctCeil * WindowSize).
	// Refs: hk-0uu.
	ForceActAbsTokens int64   // default 380000
	ForceActPctCeil   float64 // default 0.95

	// Pct-based fallbacks used when CtxFile.Tokens == 0 or WindowSize == 0
	// (older Claude Code versions that do not emit absolute token counts).
	ActPct      float64 // threshold to fire; default 90
	WarnPct     float64 // re-arm threshold; default 80
	ForceActPct float64 // forced-clear fallback pct; default 95

	HandoffTimeout time.Duration // wait for handoff nonce; default 180s
	ClearSettle    time.Duration // best-effort wait for new session_id; default 3s
	PollInterval   time.Duration // polling cadence for nonce + settle; default 200ms

	// Injectable dependencies. Nil → production default.
	CycleIDGen               func() string
	IsManagedFn              func(projectDir, agentName string) bool
	HandoffFilePath          func(projectDir, agentName string) string
	ReadHandoff              func(path string) (string, error)
	TruncateHandoffFn        func(path string) error
	InjectFn                 func(ctx context.Context, target, text string) error
	ReadGaugeFn              func(projectDir, agentName string) (*CtxFile, time.Time, error)
	CrispIdleFn              func(projectDir, agentName string) bool
	HoldingDispatchFn        func(projectDir, agentName string) bool
	WriteJournalFn           func(path string, j *CycleJournal) error
	ReadJournalFn            func(path string) (*CycleJournal, error)
	ClearPrecompactTriggerFn func(projectDir, agentName string) error

	// ClearRestartNowTriggerFn removes the .restart-now marker for the given
	// agent. Called at entry in RunOnDemand (consume-once contract). Nil →
	// ClearRestartNowTrigger. Refs: hk-wjzf, ON-059.
	ClearRestartNowTriggerFn func(projectDir, agentName string) error

	// ReadRestartNowMarkerFn reads and parses the .restart-now marker JSON for
	// the given agent. Called at entry in RunOnDemand before the marker is
	// cleared. Nil → ReadRestartNowMarker. Refs: hk-wjzf, ON-059.
	ReadRestartNowMarkerFn func(projectDir, agentName string) (*RestartNowMarker, error)

	// AppendHandoffFn appends text to the handoff file after the nonce is
	// confirmed. Used to pin keeper-authoritative identity into the handoff so
	// the resumed agent reads the correct name rather than guessing from context.
	// Nil → default OS append.
	AppendHandoffFn func(path, text string) error

	// SetManagedSessionFn writes the new session_id into .managed after a cycle
	// completes post-/clear. This unblocks the watcher's session_id binding so it
	// resumes monitoring the resumed session. Called unconditionally: an empty
	// sessionID clears the binding so the watcher re-latches on the next valid
	// gauge tick (IsManaged stays true; only the binding is cleared). Nil →
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

	// BootGracePeriod is the minimum duration after a session_id CHANGE before
	// the keeper starts a cycle on the new session. During the grace window all
	// cycle-gate checks return immediately, preventing forced-clear cycles from
	// firing while the agent is still booting after a /session-resume (the
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
	// /clear, /session-resume) is suppressed so the keeper never races the
	// operator's own keystrokes and clobbers an in-flight turn. The watcher's
	// warn/gauge emissions continue, and the cycle resumes on the next tick
	// once the operator detaches. Nil → OperatorAttached (real tmux
	// list-clients). The check is skipped entirely when TmuxTarget is empty
	// (no pane to inject into). Refs: hk-6qf.
	OperatorAttachedFn func(target string) bool
}

func (c *CyclerConfig) applyDefaults() {
	if c.ActAbsTokens <= 0 {
		c.ActAbsTokens = 300_000
	}
	if c.ActPctCeil <= 0 {
		c.ActPctCeil = 0.85
	}
	if c.WarnAbsTokens <= 0 {
		c.WarnAbsTokens = 270_000
	}
	if c.WarnPctCeil <= 0 {
		c.WarnPctCeil = 0.70
	}
	if c.ActPct <= 0 {
		c.ActPct = 90.0
	}
	if c.WarnPct <= 0 {
		c.WarnPct = 80.0
	}
	// ForceAct thresholds are derived from their corresponding act thresholds so
	// that a custom --act-pct/--act-abs-tokens never creates a dead zone where
	// context is above the act gate but below the force-clear gate (hk-6el).
	if c.ForceActAbsTokens <= 0 {
		c.ForceActAbsTokens = c.ActAbsTokens + 80_000
	}
	if c.ForceActPctCeil <= 0 {
		c.ForceActPctCeil = c.ActPctCeil + 0.10
	}
	if c.ForceActPct <= 0 {
		c.ForceActPct = c.ActPct + 5.0
	}
	if c.ForceRetryInterval <= 0 {
		c.ForceRetryInterval = 120 * time.Second
	}
	if c.BootGracePeriod > 0 && c.MaxBootGraceTotal <= 0 {
		c.MaxBootGraceTotal = 2 * c.BootGracePeriod
	}
	if c.MaxHandoffTimeouts <= 0 {
		c.MaxHandoffTimeouts = 3
	}
	if c.HandoffTimeout <= 0 {
		c.HandoffTimeout = 180 * time.Second
	}
	if c.ClearSettle <= 0 {
		c.ClearSettle = 3 * time.Second
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 200 * time.Millisecond
	}
	if c.CycleIDGen == nil {
		c.CycleIDGen = newCycleIDGen()
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
	if c.TruncateHandoffFn == nil {
		c.TruncateHandoffFn = defaultTruncateHandoff
	}
	if c.InjectFn == nil {
		c.InjectFn = InjectText
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
	if c.ClearRestartNowTriggerFn == nil {
		c.ClearRestartNowTriggerFn = ClearRestartNowTrigger
	}
	if c.ReadRestartNowMarkerFn == nil {
		c.ReadRestartNowMarkerFn = ReadRestartNowMarker
	}
	if c.AppendHandoffFn == nil {
		c.AppendHandoffFn = defaultAppendHandoff
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
}

// actThreshold returns the effective absolute-token cycle threshold for the
// given windowSize. It returns min(ActAbsTokens, int64(ActPctCeil * windowSize))
// when windowSize > 0, ensuring the gate fires early enough on both 200k and 1M
// windows. When windowSize == 0 (old .ctx without window data) returns ActAbsTokens
// so callers can still apply it as a hard cap if they have a token count.
func (c *CyclerConfig) actThreshold(windowSize int64) int64 {
	if windowSize > 0 {
		pctBased := int64(c.ActPctCeil * float64(windowSize))
		if pctBased < c.ActAbsTokens {
			return pctBased
		}
	}
	return c.ActAbsTokens
}

// warnThreshold returns the effective absolute-token warn/re-arm threshold for
// the given windowSize, using the same min(abs, pct*window) formula as actThreshold.
func (c *CyclerConfig) warnThreshold(windowSize int64) int64 {
	if windowSize > 0 {
		pctBased := int64(c.WarnPctCeil * float64(windowSize))
		if pctBased < c.WarnAbsTokens {
			return pctBased
		}
	}
	return c.WarnAbsTokens
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
func (c *CyclerConfig) belowWarnThreshold(cf *CtxFile) bool {
	if cf.Tokens > 0 && cf.WindowSize > 0 {
		return cf.Tokens < c.warnThreshold(cf.WindowSize)
	}
	return cf.Pct < c.WarnPct
}

// forceActThreshold returns the effective absolute-token forced-clear threshold
// using the same min(abs, pct*window) formula as actThreshold.
func (c *CyclerConfig) forceActThreshold(windowSize int64) int64 {
	if windowSize > 0 {
		pctBased := int64(c.ForceActPctCeil * float64(windowSize))
		if pctBased < c.ForceActAbsTokens {
			return pctBased
		}
	}
	return c.ForceActAbsTokens
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

// onDemandSettle is the dwell period in RunOnDemand after the freshness gate
// initially passes, before the keeper re-stats the HANDOFF mtime to confirm
// the file was not written concurrently (stability check). Sibling to the
// ClearSettle config field which governs the post-/clear session_id wait.
// Refs: hk-wjzf, ON-059.
const onDemandSettle = 3 * time.Second

// newCycleIDGen returns a closure that generates collision-resistant cycle IDs.
// The ID includes a startup-time timestamp prefix so IDs issued by different
// process instances never collide, addressing DEFECT-2 (stale on-disk nonce).
func newCycleIDGen() func() string {
	prefix := time.Now().UTC().Format("20060102T150405")
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

func defaultTruncateHandoff(path string) error {
	//nolint:gosec // G304,G306: path is operator-controlled; 0600 — keeper-owned
	return os.WriteFile(path, []byte{}, 0o600)
}

func defaultAppendHandoff(path, text string) error {
	//nolint:gosec // G304,G306: path derived from operator-controlled projectDir + agentName; 0600 keeper-owned
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("keeper: append handoff %q: %w", path, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck
	if _, err := fmt.Fprint(f, text); err != nil {
		return fmt.Errorf("keeper: write handoff append %q: %w", path, err)
	}
	return nil
}

// identityBlock returns the keeper-authoritative identity section to be
// appended to the handoff file after the nonce is confirmed.
// The resumed agent reads this and anchors its self-concept to the correct name.
func identityBlock(agentName string) string {
	return fmt.Sprintf("\n\n<!-- KEEPER-IDENTITY -->\n"+
		"**Agent identity (keeper-authoritative):** You are `%s`. "+
		"Your HARMONIK_AGENT environment variable is `%s`. "+
		"Use `harmonik comms send --from %s` (or rely on $HARMONIK_AGENT). "+
		"Do not reconstruct identity from conversation history — trust this line.\n"+
		"<!-- /KEEPER-IDENTITY -->\n",
		agentName, agentName, agentName)
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

// Cycler runs the Phase-2 intent-preserving reset cycle when gate conditions
// are met. It is safe to call MaybeRun on every watcher tick.
//
// Spec ref: codename:session-keeper §4.3 Phase-2 cycle core (hk-22i70, hk-kct9t).
type Cycler struct {
	cfg     CyclerConfig
	emitter Emitter

	// Anti-loop state. The cycle is suppressed on a session_id after firing
	// until BOTH (1) a new session_id is observed AND (2) pct has been seen
	// below WarnPct on that new session_id.
	lastFiredSID            string // session_id of the last completed or aborted cycle
	seenLowPctAfterLastFire bool   // true once pct < WarnPct is observed on the new session

	// Forced-clear retry state (Refs: hk-qoz).
	// lastForcedAttemptAt is set at the start of any runCycle call when
	// aboveForceThreshold. Gate 6 uses it to rate-limit same-session forced
	// retries after an abort without permanently blocking them.
	lastForcedAttemptAt time.Time

	// consecutiveHandoffTimeouts counts consecutive handoff timeouts while
	// above the force threshold. Reset to 0 on a successful cycle or on a
	// below-force-threshold abort. When it reaches MaxHandoffTimeouts,
	// ForceRestartFn is called to hard-restart the agent.
	consecutiveHandoffTimeouts int

	// Boot-grace tracking (Refs: hk-4f8, hk-ibb).
	// currentSessionID is the session_id most recently seen in MaybeRun.
	// currentSessionIDSince is the time the session_id last CHANGED to a
	// never-before-seen SID (set only when a non-empty previous session_id was
	// evicted AND the new SID is novel). Zero on first boot, meaning the grace
	// does not apply when the Cycler has never seen a prior session.
	// seenSessionIDs tracks all session_ids ever observed — a SID already in this
	// set does NOT re-arm the grace timer (prevents flapping SIDs from perpetually
	// extending the grace window). bootGraceFirstArmAt is the timestamp of the
	// most recent burst's first grace arm; used to enforce MaxBootGraceTotal ceiling.
	//
	// TODO(hk-hz9 fix 5): seenSessionIDs grows unbounded. If keeper becomes
	// permanently resident, cap it with an LRU (e.g. 128 entries).
	currentSessionID      string
	currentSessionIDSince time.Time
	seenSessionIDs        map[string]struct{}
	bootGraceFirstArmAt   time.Time
}

// NewCycler constructs a Cycler. Defaults are applied to zero-valued config fields.
func NewCycler(cfg CyclerConfig, emitter Emitter) *Cycler {
	cfg.applyDefaults()
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	return &Cycler{cfg: cfg, emitter: emitter}
}

// journalPath returns the path to the cycle journal file for the agent.
func (c *Cycler) journalPath() string {
	return filepath.Join(c.cfg.ProjectDir, ".harmonik", "keeper", c.cfg.AgentName+".cycle")
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
// It is safe to call on every watcher tick; gating is done internally.
func (c *Cycler) MaybeRun(ctx context.Context, cf *CtxFile) error {
	// Gate 1: .managed opt-in — co-located with the destructive action (DEFECT-3).
	if !c.cfg.IsManagedFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		return nil
	}
	// Gate 2: empty session_id → cannot establish anti-loop identity (DEFECT-1).
	if cf.SessionID == "" {
		return nil
	}

	// Observe context level for re-arm: track the first below-warn reading on a
	// new session_id after a cycle has fired. This happens regardless of other
	// gates so a brief low-context window is never missed.
	if c.lastFiredSID != "" && cf.SessionID != c.lastFiredSID && c.cfg.belowWarnThreshold(cf) {
		c.seenLowPctAfterLastFire = true
	}

	// Anti-loop escape hatch: if the session_id hasn't changed since the last
	// cycle (ClearSettle timeout — no new SID observed) but the context has
	// genuinely dropped below WarnPct, a real /clear happened and the keeper
	// must be allowed to re-arm. Reset lastFiredSID so subsequent ticks on the
	// same session_id can pass Gate 6 once the context climbs again.
	// (Refs: hk-uxu)
	if c.lastFiredSID != "" && cf.SessionID == c.lastFiredSID && c.cfg.belowWarnThreshold(cf) {
		c.lastFiredSID = ""
		c.seenLowPctAfterLastFire = false
	}

	// Boot-grace gate (Refs: hk-4f8 — bad-trigger-timing fix, hk-ibb — follow-up).
	// Track when the session_id last changed to a NEVER-SEEN SID. Apply a grace
	// window after each novel session_id transition so cycles cannot fire while
	// an agent is still booting after a /session-resume. The grace applies ONLY
	// when a previous session_id was evicted AND the new SID is novel (never
	// observed before), preventing flapping SIDs from perpetually re-arming the
	// timer. On initial Cycler startup (currentSessionID == "") no grace is armed
	// so an already-running agent is monitored without delay on keeper boot.
	if cf.SessionID != c.currentSessionID {
		if c.currentSessionID != "" {
			// Session changed: arm grace only for a never-seen SID.
			// Already-seen SIDs (e.g. a flapping session_id) do not re-arm the
			// timer — the prior grace period was sufficient. (Refs: hk-ibb fix 2)
			if _, alreadySeen := c.seenSessionIDs[cf.SessionID]; !alreadySeen {
				now := time.Now()
				c.currentSessionIDSince = now
				// Burst-relative cap (Refs: hk-hz9 fix 1): reset the grace burst
				// window on first arm OR when the prior burst's MaxBootGraceTotal has
				// already elapsed. This ensures a new boot burst gets a fresh total
				// window rather than inheriting accumulated time from a prior burst.
				if c.bootGraceFirstArmAt.IsZero() ||
					(c.cfg.MaxBootGraceTotal > 0 && time.Since(c.bootGraceFirstArmAt) >= c.cfg.MaxBootGraceTotal) {
					c.bootGraceFirstArmAt = now
				}
			}
		}
		if c.seenSessionIDs == nil {
			c.seenSessionIDs = make(map[string]struct{})
		}
		c.seenSessionIDs[cf.SessionID] = struct{}{}
		c.currentSessionID = cf.SessionID
	}
	// Force-path exemption (Refs: hk-ibb fix 1): an agent above ForceActPct
	// bypasses the boot grace entirely — pane-overflow risk outweighs the
	// boot-timing false-positive risk.
	if c.cfg.BootGracePeriod > 0 && !c.currentSessionIDSince.IsZero() &&
		!c.cfg.aboveForceThreshold(cf) &&
		time.Since(c.currentSessionIDSince) < c.cfg.BootGracePeriod {
		// MaxBootGraceTotal ceiling: if total time since first grace-arm exceeds
		// the cap, skip the grace gate regardless of per-SID timer. (hk-ibb fix 2)
		totalExceeded := c.cfg.MaxBootGraceTotal > 0 &&
			!c.bootGraceFirstArmAt.IsZero() &&
			time.Since(c.bootGraceFirstArmAt) >= c.cfg.MaxBootGraceTotal
		if !totalExceeded {
			slog.DebugContext(ctx, "keeper: boot grace active — deferring cycle for new session",
				"agent", c.cfg.AgentName, "session_id", cf.SessionID,
				"grace_remaining", c.cfg.BootGracePeriod-time.Since(c.currentSessionIDSince))
			return nil
		}
	}

	// Gate 3: context must reach the act threshold.
	// Uses absolute tokens when available (min(ActAbsTokens, ActPctCeil*window));
	// falls back to percentage when Tokens/WindowSize are absent (old .ctx files).
	if c.cfg.belowActThreshold(cf) {
		return nil
	}
	// Gate 4: agent must be at a crisp await-input boundary — UNLESS context
	// has breached the hard force-act threshold. Above that threshold the cycle
	// fires unconditionally so a perpetually-busy crew (one that never satisfies
	// CrispIdle) is still cleared before context exhaustion. (Refs: hk-0uu)
	if !c.cfg.CrispIdleFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		if !c.cfg.aboveForceThreshold(cf) {
			return nil
		}
		slog.WarnContext(ctx, "keeper: forced-clear: bypassing CrispIdle above hard threshold",
			"agent", c.cfg.AgentName, "pct", cf.Pct, "tokens", cf.Tokens)
	}
	// Gate 5: no in-flight queue work (fail-closed via HoldingDispatchFn).
	if c.cfg.HoldingDispatchFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		return nil
	}
	// Gate 6: full anti-loop suppression (only applies after the first fire).
	//
	// Forced-clear exception (Refs: hk-qoz): when above the hard force threshold
	// and the same session_id is still present (DEFECT-4 abort set lastFiredSID),
	// allow a retry once ForceRetryInterval has elapsed from the last forced
	// attempt. This breaks the catch-22 where an aborted forced-clear permanently
	// blocks further attempts on a session whose context never drops below WarnPct.
	if c.lastFiredSID != "" {
		if cf.SessionID == c.lastFiredSID {
			if !c.cfg.aboveForceThreshold(cf) {
				// Normal path: same session always suppressed until context drops.
				return nil
			}
			// Above force threshold: allow retry after ForceRetryInterval.
			if !c.lastForcedAttemptAt.IsZero() && time.Since(c.lastForcedAttemptAt) < c.cfg.ForceRetryInterval {
				return nil
			}
			// Fall through: retry the forced-clear.
		} else if !c.seenLowPctAfterLastFire {
			// Different session: suppress until pct has been seen below WarnPct.
			// Force-retry exception (Refs: hk-hz9 fix 2): mirrors the same-SID path
			// above. A novel session staying above WarnPct would otherwise wedge
			// indefinitely — it can never arm seenLowPctAfterLastFire and the normal
			// suppression never lifts. When above the hard force threshold and
			// ForceRetryInterval has elapsed, allow a retry to break the stall.
			if c.cfg.aboveForceThreshold(cf) {
				if !c.lastForcedAttemptAt.IsZero() && time.Since(c.lastForcedAttemptAt) < c.cfg.ForceRetryInterval {
					return nil
				}
				// Fall through: retry the forced-clear.
			} else {
				return nil
			}
		}
	}

	// Gate 7: operator-attached guard (warn-only). If a human operator is
	// attached to the target tmux session, suppress the destructive injection so
	// the keeper never races the operator's keystrokes and clobbers an in-flight
	// turn. The watcher keeps emitting warn/gauge; the cycle resumes on a later
	// tick once the operator detaches. Skipped when TmuxTarget is empty (nothing
	// to inject into). Refs: hk-6qf.
	if c.operatorAttached() {
		c.emitOperatorAttached(ctx, cf.SessionID, "cycle")
		return nil
	}

	return c.runCycle(ctx, cf)
}

// operatorAttached reports whether a human operator is attached to the target
// tmux session. Returns false (proceed) when TmuxTarget is empty — there is no
// pane to race over — matching the test/warn-only-injection convention used
// elsewhere in the cycle core.
func (c *Cycler) operatorAttached() bool {
	if c.cfg.TmuxTarget == "" {
		return false
	}
	return c.cfg.OperatorAttachedFn(c.cfg.TmuxTarget)
}

// runCycle executes the full 7-step reset cycle.
// SAFETY: /clear is ONLY issued after the handoff nonce is positively confirmed.
func (c *Cycler) runCycle(ctx context.Context, cf *CtxFile) error {
	cycleID := c.cfg.CycleIDGen()
	now := time.Now().UTC()
	journalPath := c.journalPath()
	handoffPath := c.cfg.HandoffFilePath(c.cfg.ProjectDir, c.cfg.AgentName)

	// Record forced-attempt timestamp BEFORE injecting so Gate 6 can rate-limit
	// same-session retries after this cycle completes or aborts. Set regardless
	// of CrispIdle path so any forced-threshold cycle is rate-limited. (hk-qoz)
	if c.cfg.aboveForceThreshold(cf) {
		c.lastForcedAttemptAt = time.Now().UTC()
	}

	// Step 1: open journal BEFORE any injection.
	j := &CycleJournal{
		CycleID:   cycleID,
		Phase:     "opened",
		OpenedAt:  now,
		UpdatedAt: now,
	}
	if err := c.cfg.WriteJournalFn(journalPath, j); err != nil {
		return err
	}

	// Emit session_keeper_handoff_started so the cycle is auditable.
	c.emitHandoffStarted(ctx, cycleID, cf.SessionID)

	// Step 2: truncate handoff file BEFORE injecting /session-handoff.
	// This prevents a stale nonce from a pre-crash cycle from pre-satisfying
	// the poll in step 3 (DEFECT-2).
	_ = c.cfg.TruncateHandoffFn(handoffPath) //nolint:errcheck // non-fatal; poll will fail gracefully

	// Step 2b: inject /session-handoff with nonce directive.
	// Send Escape first to preempt any in-progress input on a busy pane so the
	// injected command lands cleanly at the REPL prompt. (Refs: hk-qoz)
	if c.cfg.TmuxTarget != "" {
		if c.cfg.SendEscapeFn != nil {
			_ = c.cfg.SendEscapeFn(ctx, c.cfg.TmuxTarget) //nolint:errcheck // non-fatal; clears partial input
		}
		handoffCmd := fmt.Sprintf(
			"/session-handoff %s\n\nIMPORTANT: include exactly this line verbatim in the handoff file: %s",
			handoffPath, nonceMarker(cycleID),
		)
		if err := c.cfg.InjectFn(ctx, c.cfg.TmuxTarget, handoffCmd); err != nil {
			// Non-fatal: the confirm step will catch any delivery failure.
			_ = err //nolint:errcheck
		}
	}
	j.Phase = "handoff_injected"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck

	// Step 3: confirm — poll until nonce appears or timeout elapses.
	if !c.pollForNonce(ctx, handoffPath, nonceMarker(cycleID)) {
		// ABORT — NEVER /clear an unconfirmed handoff.
		j.Phase = "aborted"
		j.UpdatedAt = time.Now().UTC()
		j.Reason = "handoff_timeout"
		_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck
		c.emitCycleAborted(ctx, cycleID, cf.SessionID, "handoff_timeout")
		// DEFECT-4: record suppression on abort to prevent re-fire on next tick.
		c.lastFiredSID = cf.SessionID
		c.seenLowPctAfterLastFire = false

		// Re-arm: clear .managed so the watcher re-latches on the next valid
		// gauge after this abort — but ONLY when a real session-id change was
		// previously observed (currentSessionIDSince non-zero). When the keeper
		// has never seen a session change (first monitored session), clearing
		// .managed prematurely allows a new SID to latch and trigger boot-grace,
		// creating a Gate-6 suppression stall where the force-retry exception
		// never fires (different SID, no low-pct observation). Gating this on
		// !currentSessionIDSince.IsZero() ensures the watcher stays bound to the
		// original session and Gate-6 same-SID force-retry handles the retry loop.
		// Refs: hk-4f8 (no-re-arm fix), hk-ibb (fix 3 — gate abort-clear).
		if !c.currentSessionIDSince.IsZero() {
			if setErr := c.cfg.SetManagedSessionFn(c.cfg.ProjectDir, c.cfg.AgentName, ""); setErr != nil {
				slog.WarnContext(ctx, "keeper: clear managed session_id after handoff_timeout abort",
					"agent", c.cfg.AgentName, "err", setErr)
			}
		}

		// Escalation path: track consecutive timeouts above the force threshold
		// and call ForceRestartFn after MaxHandoffTimeouts. This handles the case
		// where the pane is permanently unresponsive (process loop, frozen REPL).
		// Refs: hk-qoz.
		if c.cfg.aboveForceThreshold(cf) {
			c.consecutiveHandoffTimeouts++
			if c.cfg.ForceRestartFn != nil && c.cfg.MaxHandoffTimeouts > 0 &&
				c.consecutiveHandoffTimeouts >= c.cfg.MaxHandoffTimeouts {
				slog.WarnContext(ctx, "keeper: escalating to hard restart after repeated handoff timeouts",
					"agent", c.cfg.AgentName, "timeouts", c.consecutiveHandoffTimeouts)
				if restartErr := c.cfg.ForceRestartFn(ctx, c.cfg.AgentName); restartErr != nil {
					slog.WarnContext(ctx, "keeper: hard restart failed",
						"agent", c.cfg.AgentName, "err", restartErr)
				}
				c.consecutiveHandoffTimeouts = 0
			}
		} else {
			c.consecutiveHandoffTimeouts = 0
		}
		return nil
	}

	j.Phase = "confirmed"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck

	// Step 3b: pin identity — append keeper-authoritative identity block to the
	// handoff file so the resumed agent reads the correct name rather than
	// reconstructing it from context. Non-fatal: /session-resume can still run.
	_ = c.cfg.AppendHandoffFn(handoffPath, identityBlock(c.cfg.AgentName)) //nolint:errcheck

	// Step 3c: set HARMONIK_AGENT in the tmux session environment so the new
	// Claude process started after /clear inherits the correct agent name.
	// Non-fatal: the handoff identity block is the primary anchor.
	if c.cfg.TmuxTarget != "" {
		_ = c.cfg.SetTmuxEnvFn(ctx, c.cfg.TmuxTarget, "HARMONIK_AGENT", c.cfg.AgentName) //nolint:errcheck
	}

	// Step 4: inject /clear.
	if c.cfg.TmuxTarget != "" {
		_ = c.cfg.InjectFn(ctx, c.cfg.TmuxTarget, "/clear") //nolint:errcheck
	}
	j.Phase = "cleared"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck

	// Step 5: clear-readiness — wait for a new session_id (best-effort, NOT a hard gate).
	// Spec note: spike proved resume survives /clear FIFO; absent new session_id is non-fatal.
	newSID := c.waitForNewSessionID(ctx, cf.SessionID)
	if newSID == "" {
		c.emitClearUnconfirmed(ctx, cycleID, cf.SessionID)
	}

	// Step 5b: update the .managed session binding so the watcher accepts the
	// new session's gauge data after /clear. Without this update the watcher would
	// continue filtering the new session as "foreign". Called unconditionally:
	// when newSID=="" (ClearSettle timeout), writing "" clears the stale binding
	// so the watcher re-latches on the next valid gauge tick. (Refs: hk-igt, hk-uxu)
	if err := c.cfg.SetManagedSessionFn(c.cfg.ProjectDir, c.cfg.AgentName, newSID); err != nil {
		slog.WarnContext(ctx, "keeper: update managed session_id after cycle",
			"agent", c.cfg.AgentName, "new_sid", newSID, "err", err)
		// Non-fatal: watcher falls back to accepting the session_id via
		// the latch path on the next tick.
	}

	// Step 6: inject /session-resume.
	if c.cfg.TmuxTarget != "" {
		_ = c.cfg.InjectFn(ctx, c.cfg.TmuxTarget, fmt.Sprintf("/session-resume %s", handoffPath)) //nolint:errcheck
	}
	j.Phase = "resumed"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck

	// Step 7: close journal; emit session_keeper_cycle_complete.
	j.Phase = "complete"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck
	c.emitCycleComplete(ctx, cycleID, cf.SessionID, newSID)

	// Anti-loop: record the session_id so we do not re-fire for it until both
	// a new session_id is observed AND pct drops below WarnPct on it.
	c.lastFiredSID = cf.SessionID
	c.seenLowPctAfterLastFire = false

	// Successful cycle: reset the consecutive-timeout counter and the grace
	// burst window so the next novel SID after this /clear gets a fresh total
	// window, not residual time from the current burst. (Refs: hk-hz9 fix 1)
	c.consecutiveHandoffTimeouts = 0
	c.bootGraceFirstArmAt = time.Time{}

	return nil
}

// RecoverFromCrash checks for an in-progress cycle journal on boot and takes
// corrective action based on the last recorded phase.
//
//   - phase "cleared": /clear was issued before the crash; inject /session-resume
//     to complete the interrupted cycle, update journal to "complete", emit
//     session_keeper_cycle_recovered.
//   - phase "opened" / "handoff_injected" / "confirmed": /clear was NOT issued;
//     update journal to "aborted" with reason "crash_before_clear", no injection.
//   - phase "resumed": /session-resume was already injected; close journal.
//   - phase "complete" / "aborted": terminal state; no-op.
//
// Respects the .managed guard: no injection if not managed.
// Does NOT reuse the stale cycle_id nonce from the journal (DEFECT-2): the
// recovery path injects /session-resume directly without a nonce poll, so a
// stale nonce cannot trigger unintended behaviour.
func (c *Cycler) RecoverFromCrash(ctx context.Context) error {
	// Fail-closed: only act on a managed agent.
	if !c.cfg.IsManagedFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		return nil
	}

	journalPath := c.journalPath()
	j, err := c.cfg.ReadJournalFn(journalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no journal = no crash to recover
		}
		return fmt.Errorf("keeper: read recovery journal: %w", err)
	}

	handoffPath := c.cfg.HandoffFilePath(c.cfg.ProjectDir, c.cfg.AgentName)

	switch j.Phase {
	case "cleared":
		// /clear was issued; agent needs resume injected.
		if c.cfg.TmuxTarget != "" {
			_ = c.cfg.InjectFn(ctx, c.cfg.TmuxTarget, fmt.Sprintf("/session-resume %s", handoffPath)) //nolint:errcheck
		}
		j.Phase = "complete"
		j.UpdatedAt = time.Now().UTC()
		j.Reason = "recovered_from_crash"
		_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck
		c.emitCycleRecovered(ctx, j.CycleID, "cleared")

	case "resumed":
		// /session-resume was already injected; just close the journal.
		j.Phase = "complete"
		j.UpdatedAt = time.Now().UTC()
		j.Reason = "recovered_from_crash"
		_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck
		c.emitCycleRecovered(ctx, j.CycleID, "resumed")

	case "opened", "handoff_injected", "confirmed":
		// /clear was NOT issued; discard (abort) the journal safely.
		j.Phase = "aborted"
		j.UpdatedAt = time.Now().UTC()
		j.Reason = "crash_before_clear"
		_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck

	case "complete", "aborted":
		// Terminal state — nothing to recover.
	}

	return nil
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
//  2b. Boot-grace: if the session is within its grace window, defer (clear
//      marker so the next PreCompact fire gets a clean slate). The grace state
//      is populated by MaybeRun, which the watcher ALWAYS calls before
//      RunForPrecompact (watcher.go:568 before :580) — that ordering is the
//      load-bearing invariant keeping this state current without re-computing it
//      here. Force-path exception: above ForceActPct, bypass grace.
//  3. NOT HoldingDispatch (fail-closed: skip cycle, clear marker → next PreCompact is fail-open).
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

	// Gate 1: .managed opt-in (defensive; the shell script checks this too).
	if !c.cfg.IsManagedFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		c.emitPrecompactBlocked(ctx, sessionID, "not_managed")
		_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck
		return nil
	}

	// Gate 2: empty session_id → cannot establish anti-loop identity.
	if sessionID == "" {
		// Don't cycle but clear marker — let native compaction proceed next time.
		c.emitPrecompactBlocked(ctx, sessionID, "hold_dispatch_skip")
		_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck
		return nil
	}

	// Gate 2b: boot-grace (Refs: hk-hz9 fix 3). The grace state is kept current
	// by MaybeRun, which the watcher calls immediately before RunForPrecompact
	// (watcher.go:568 before :580) — that ordering is the load-bearing invariant.
	// Force-path exception: cf above ForceActPct bypasses grace (pane-overflow
	// risk outweighs boot-timing false-positive risk).
	if c.cfg.BootGracePeriod > 0 && !c.currentSessionIDSince.IsZero() &&
		(cf == nil || !c.cfg.aboveForceThreshold(cf)) &&
		time.Since(c.currentSessionIDSince) < c.cfg.BootGracePeriod {
		totalExceeded := c.cfg.MaxBootGraceTotal > 0 &&
			!c.bootGraceFirstArmAt.IsZero() &&
			time.Since(c.bootGraceFirstArmAt) >= c.cfg.MaxBootGraceTotal
		if !totalExceeded {
			c.emitPrecompactBlocked(ctx, sessionID, "boot_grace")
			_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck
			return nil
		}
	}

	// Observe context level for re-arm tracking (mirrors MaybeRun side-effect).
	if cf != nil && c.lastFiredSID != "" && cf.SessionID != c.lastFiredSID && c.cfg.belowWarnThreshold(cf) {
		c.seenLowPctAfterLastFire = true
	}

	// Anti-loop escape hatch (mirrors MaybeRun): same-session + below WarnPct
	// means a real /clear happened with ClearSettle timeout; reset so re-arm
	// is possible. (Refs: hk-uxu)
	if cf != nil && c.lastFiredSID != "" && cf.SessionID == c.lastFiredSID && c.cfg.belowWarnThreshold(cf) {
		c.lastFiredSID = ""
		c.seenLowPctAfterLastFire = false
	}

	// Gate 3: HoldingDispatch — fail-closed; skip cycle.
	if c.cfg.HoldingDispatchFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		c.emitPrecompactBlocked(ctx, sessionID, "hold_dispatch_skip")
		_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck
		return nil
	}

	// Gate 4: anti-loop suppression.
	if c.lastFiredSID != "" {
		if sessionID == c.lastFiredSID {
			c.emitPrecompactBlocked(ctx, sessionID, "anti_loop_suppressed")
			_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck
			return nil
		}
		if !c.seenLowPctAfterLastFire {
			c.emitPrecompactBlocked(ctx, sessionID, "anti_loop_suppressed")
			_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck
			return nil
		}
	}

	// Gate 5: operator-attached guard (warn-only). Even under PreCompact, if a
	// human operator is attached we must not race their keystrokes with a /clear.
	// Emit the precompact decision (operator_attached) AND the operator_attached
	// event, clear the marker (bounded-fallback: native compaction proceeds next
	// time), and suppress the cycle. The keeper retries on a later PreCompact fire
	// once the operator detaches. Refs: hk-6qf.
	if c.operatorAttached() {
		c.emitPrecompactBlocked(ctx, sessionID, "operator_attached")
		c.emitOperatorAttached(ctx, sessionID, "precompact")
		_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck
		return nil
	}

	// All gates passed: emit the precompact event, clear the marker, run cycle.
	c.emitPrecompactBlocked(ctx, sessionID, "cycle_triggered")
	_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck

	if cf == nil {
		// Construct a minimal CtxFile so runCycle has a session_id.
		cf = &CtxFile{SessionID: sessionID}
	}
	return c.runCycle(ctx, cf)
}

// RunOnDemand executes the on-demand clear→resume cycle initiated by the
// captain via `harmonik keeper restart-now`. Unlike MaybeRun, it bypasses the
// act-pct threshold gate only — ALL other safety gates are kept. The captain
// already wrote HANDOFF-<agent>.md; this method skips the handoff-truncate and
// /session-handoff-inject steps and instead validates the freshness gate before
// issuing /clear.
//
// Gate order (ON-059):
//  1. Read marker JSON, then ClearRestartNowTrigger at entry (consume-once).
//  2. Gate 1: .managed opt-in guard.
//  3. Gate 2: non-empty session_id (anti-loop identity).
//  4. Gate 3: NOT HoldingDispatch (fail-closed).
//  5. Gate 4: CrispIdle.
//  6. Gate 5: anti-loop lastFiredSID suppression.
//  7. Gate 6: operator-attached guard.
//  8. Freshness gate: marker.session_id == cf.SessionID; handoff contains
//     <!-- KEEPER:<marker.nonce> -->; handoff mtime >= marker.requested_at;
//     after onDemandSettle re-stat mtime must be unchanged.
//  9. On any gate/freshness failure: emit session_keeper_restart_now_blocked,
//     return nil (non-destructive).
// 10. On freshness-gate pass: execute /clear → /session-resume tail.
//
// Refs: hk-wjzf, hk-xjlq, ON-059.
func (c *Cycler) RunOnDemand(ctx context.Context, cf *CtxFile) error {
	sessionID := ""
	if cf != nil {
		sessionID = cf.SessionID
	}

	// Step 1: read marker BEFORE clearing (consume-once at entry).
	marker, markerErr := c.cfg.ReadRestartNowMarkerFn(c.cfg.ProjectDir, c.cfg.AgentName)
	// Always clear the marker at entry, even on gate failure, so no re-fire occurs.
	if clearErr := c.cfg.ClearRestartNowTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName); clearErr != nil {
		slog.WarnContext(ctx, "keeper: restart-now: clear marker", "agent", c.cfg.AgentName, "err", clearErr)
	}

	emitBlocked := func(reason string) {
		c.emitRestartNowBlocked(ctx, sessionID, reason)
	}

	if markerErr != nil {
		slog.WarnContext(ctx, "keeper: restart-now: read marker", "agent", c.cfg.AgentName, "err", markerErr)
		emitBlocked("marker_read_error")
		return nil
	}

	// Gate 1: .managed opt-in.
	if !c.cfg.IsManagedFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		emitBlocked("not_managed")
		return nil
	}
	// Gate 2: non-empty session_id.
	if sessionID == "" {
		emitBlocked("empty_session_id")
		return nil
	}
	// Gate 3: HoldingDispatch fail-closed.
	if c.cfg.HoldingDispatchFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		emitBlocked("hold_dispatch")
		return nil
	}
	// Gate 4: CrispIdle.
	if !c.cfg.CrispIdleFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		emitBlocked("not_crisp_idle")
		return nil
	}
	// Gate 5: anti-loop lastFiredSID suppression.
	if c.lastFiredSID != "" && c.lastFiredSID == sessionID {
		emitBlocked("anti_loop_suppressed")
		return nil
	}
	// Gate 6: operator-attached guard.
	if c.operatorAttached() {
		c.emitOperatorAttached(ctx, sessionID, "restart_now")
		emitBlocked("operator_attached")
		return nil
	}

	// Freshness gate.
	handoffPath := c.cfg.HandoffFilePath(c.cfg.ProjectDir, c.cfg.AgentName)

	// Check session_id matches.
	if marker.SessionID != sessionID {
		slog.WarnContext(ctx, "keeper: restart-now: session_id mismatch",
			"agent", c.cfg.AgentName, "marker_sid", marker.SessionID, "current_sid", sessionID)
		emitBlocked("session_id_mismatch")
		return nil
	}

	// Read handoff file and check nonce.
	handoffContent, readErr := c.cfg.ReadHandoff(handoffPath)
	if readErr != nil {
		slog.WarnContext(ctx, "keeper: restart-now: read handoff", "agent", c.cfg.AgentName, "err", readErr)
		emitBlocked("handoff_read_error")
		return nil
	}
	expectedNonce := nonceMarker(marker.Nonce)
	if !strings.Contains(handoffContent, expectedNonce) {
		slog.WarnContext(ctx, "keeper: restart-now: nonce mismatch",
			"agent", c.cfg.AgentName, "expected", expectedNonce)
		emitBlocked("nonce_mismatch")
		return nil
	}

	// Check handoff mtime >= marker.requested_at.
	handoffStat, statErr := os.Stat(handoffPath)
	if statErr != nil {
		slog.WarnContext(ctx, "keeper: restart-now: stat handoff", "agent", c.cfg.AgentName, "err", statErr)
		emitBlocked("handoff_stat_error")
		return nil
	}
	if handoffStat.ModTime().Before(marker.RequestedAt) {
		slog.WarnContext(ctx, "keeper: restart-now: handoff predates marker",
			"agent", c.cfg.AgentName,
			"handoff_mtime", handoffStat.ModTime(), "requested_at", marker.RequestedAt)
		emitBlocked("handoff_stale")
		return nil
	}
	handoffMtime := handoffStat.ModTime()

	// Settle dwell: wait onDemandSettle then re-stat handoff mtime.
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(onDemandSettle):
	}

	handoffStat2, statErr2 := os.Stat(handoffPath)
	if statErr2 != nil {
		slog.WarnContext(ctx, "keeper: restart-now: re-stat handoff after settle",
			"agent", c.cfg.AgentName, "err", statErr2)
		emitBlocked("handoff_stat_error")
		return nil
	}
	if !handoffStat2.ModTime().Equal(handoffMtime) {
		slog.WarnContext(ctx, "keeper: restart-now: handoff mtime changed during settle",
			"agent", c.cfg.AgentName, "before", handoffMtime, "after", handoffStat2.ModTime())
		emitBlocked("handoff_modified_during_settle")
		return nil
	}

	// All gates and freshness checks passed. Execute the /clear → /session-resume
	// tail, reusing runCycle's post-confirm logic. The captain already wrote the
	// HANDOFF; we skip the truncate + /session-handoff-inject steps.
	return c.runOnDemandCycleTail(ctx, cf)
}

// runOnDemandCycleTail executes the post-confirm tail of the reset cycle for
// an on-demand (restart-now) invocation. Corresponds to runCycle steps 3b–7:
// identity-block append, HARMONIK_AGENT env, /clear, settle, .managed update,
// /session-resume, journal close, anti-loop update.
//
// Called only after RunOnDemand's freshness gate passes. The handoff file
// already contains the nonce and identity; we do NOT truncate or re-inject it.
func (c *Cycler) runOnDemandCycleTail(ctx context.Context, cf *CtxFile) error {
	cycleID := c.cfg.CycleIDGen()
	now := time.Now().UTC()
	journalPath := c.journalPath()
	handoffPath := c.cfg.HandoffFilePath(c.cfg.ProjectDir, c.cfg.AgentName)

	sessionID := ""
	if cf != nil {
		sessionID = cf.SessionID
	}

	// Open journal.
	j := &CycleJournal{
		CycleID:   cycleID,
		Phase:     "confirmed", // freshness gate = confirmation; no handoff-inject needed
		OpenedAt:  now,
		UpdatedAt: now,
	}
	if err := c.cfg.WriteJournalFn(journalPath, j); err != nil {
		return err
	}
	c.emitHandoffStarted(ctx, cycleID, sessionID)

	// Step 3b: pin identity — append keeper-authoritative identity block.
	_ = c.cfg.AppendHandoffFn(handoffPath, identityBlock(c.cfg.AgentName)) //nolint:errcheck

	// Step 3c: set HARMONIK_AGENT in the tmux session environment.
	if c.cfg.TmuxTarget != "" {
		_ = c.cfg.SetTmuxEnvFn(ctx, c.cfg.TmuxTarget, "HARMONIK_AGENT", c.cfg.AgentName) //nolint:errcheck
	}

	// Step 4: inject /clear.
	if c.cfg.TmuxTarget != "" {
		_ = c.cfg.InjectFn(ctx, c.cfg.TmuxTarget, "/clear") //nolint:errcheck
	}
	j.Phase = "cleared"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck

	// Step 5: wait for new session_id (best-effort).
	newSID := c.waitForNewSessionID(ctx, sessionID)
	if newSID == "" {
		c.emitClearUnconfirmed(ctx, cycleID, sessionID)
	}

	// Step 5b: update .managed so the watcher accepts the new session.
	if err := c.cfg.SetManagedSessionFn(c.cfg.ProjectDir, c.cfg.AgentName, newSID); err != nil {
		slog.WarnContext(ctx, "keeper: restart-now: update managed session_id",
			"agent", c.cfg.AgentName, "new_sid", newSID, "err", err)
	}

	// Step 6: inject /session-resume.
	if c.cfg.TmuxTarget != "" {
		_ = c.cfg.InjectFn(ctx, c.cfg.TmuxTarget, fmt.Sprintf("/session-resume %s", handoffPath)) //nolint:errcheck
	}
	j.Phase = "resumed"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck

	// Step 7: close journal; emit cycle_complete.
	j.Phase = "complete"
	j.UpdatedAt = time.Now().UTC()
	_ = c.cfg.WriteJournalFn(journalPath, j) //nolint:errcheck
	c.emitCycleComplete(ctx, cycleID, sessionID, newSID)

	// Anti-loop: record the session_id so we do not re-fire until both a new
	// session_id is observed AND pct drops below WarnPct on it.
	c.lastFiredSID = sessionID
	c.seenLowPctAfterLastFire = false

	// Reset the consecutive-timeout counter and grace burst window.
	c.consecutiveHandoffTimeouts = 0
	c.bootGraceFirstArmAt = time.Time{}

	return nil
}

// emitRestartNowBlocked emits session_keeper_restart_now_blocked.
func (c *Cycler) emitRestartNowBlocked(ctx context.Context, sessionID, reason string) {
	payload := core.SessionKeeperRestartNowBlockedPayload{
		AgentName: c.cfg.AgentName,
		SessionID: sessionID,
		Reason:    reason,
	}
	raw, _ := json.Marshal(payload)                                                                        //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperRestartNowBlocked, raw) //nolint:errcheck
}

// emitPrecompactBlocked emits session_keeper_precompact_blocked.
func (c *Cycler) emitPrecompactBlocked(ctx context.Context, sessionID, action string) {
	payload := core.SessionKeeperPrecompactBlockedPayload{
		AgentName: c.cfg.AgentName,
		SessionID: sessionID,
		Action:    action,
	}
	raw, _ := json.Marshal(payload)                                                                   //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperPrecompactBlocked, raw) //nolint:errcheck
}

// emitOperatorAttached emits session_keeper_operator_attached when a reset-cycle
// injection is suppressed because a human operator is attached to the target
// tmux session. Phase is "cycle" (MaybeRun) or "precompact" (RunForPrecompact).
// Refs: hk-6qf.
func (c *Cycler) emitOperatorAttached(ctx context.Context, sessionID, phase string) {
	payload := core.SessionKeeperOperatorAttachedPayload{
		AgentName: c.cfg.AgentName,
		SessionID: sessionID,
		Phase:     phase,
	}
	raw, _ := json.Marshal(payload)                                                                  //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperOperatorAttached, raw) //nolint:errcheck
}

// pollForNonce polls handoffPath until the nonce appears or the handoff
// timeout (or ctx) elapses.
func (c *Cycler) pollForNonce(parentCtx context.Context, handoffPath, nonce string) bool {
	ctx, cancel := context.WithTimeout(parentCtx, c.cfg.HandoffTimeout)
	defer cancel()

	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			content, err := c.cfg.ReadHandoff(handoffPath)
			if err == nil && strings.Contains(content, nonce) {
				return true
			}
		}
	}
}

// waitForNewSessionID polls the gauge until its session_id differs from
// prevSID or the clear-settle timeout (or ctx) elapses.
func (c *Cycler) waitForNewSessionID(parentCtx context.Context, prevSID string) string {
	ctx, cancel := context.WithTimeout(parentCtx, c.cfg.ClearSettle)
	defer cancel()

	ticker := time.NewTicker(c.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ""
		case <-ticker.C:
			cf, _, err := c.cfg.ReadGaugeFn(c.cfg.ProjectDir, c.cfg.AgentName)
			// Skip UUIDv7: daemon-spawned implementers write UUIDv7 session IDs.
			// Binding to one after /clear would make the watcher treat the
			// captain's real UUIDv4 session as foreign. (Refs: hk-lap)
			if err == nil && cf.SessionID != "" && cf.SessionID != prevSID && !isUUIDv7(cf.SessionID) {
				return cf.SessionID
			}
		}
	}
}

// emitHandoffStarted emits session_keeper_handoff_started.
func (c *Cycler) emitHandoffStarted(ctx context.Context, cycleID, sessionID string) {
	payload := core.SessionKeeperHandoffStartedPayload{
		AgentName: c.cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
	}
	raw, _ := json.Marshal(payload)                                                                //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperHandoffStarted, raw) //nolint:errcheck
}

// emitCycleComplete emits session_keeper_cycle_complete.
func (c *Cycler) emitCycleComplete(ctx context.Context, cycleID, prevSID, newSID string) {
	payload := core.SessionKeeperCycleCompletePayload{
		AgentName:     c.cfg.AgentName,
		CycleID:       cycleID,
		PrevSessionID: prevSID,
		NewSessionID:  newSID,
	}
	raw, _ := json.Marshal(payload)                                                               //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperCycleComplete, raw) //nolint:errcheck
}

// emitCycleAborted emits session_keeper_cycle_aborted.
func (c *Cycler) emitCycleAborted(ctx context.Context, cycleID, sessionID, reason string) {
	payload := core.SessionKeeperCycleAbortedPayload{
		AgentName: c.cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
		Reason:    reason,
	}
	raw, _ := json.Marshal(payload)                                                              //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperCycleAborted, raw) //nolint:errcheck
}

// emitClearUnconfirmed emits session_keeper_clear_unconfirmed.
func (c *Cycler) emitClearUnconfirmed(ctx context.Context, cycleID, sessionID string) {
	payload := core.SessionKeeperClearUnconfirmedPayload{
		AgentName: c.cfg.AgentName,
		CycleID:   cycleID,
		SessionID: sessionID,
	}
	raw, _ := json.Marshal(payload)                                                                  //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperClearUnconfirmed, raw) //nolint:errcheck
}

// emitCycleRecovered emits session_keeper_cycle_recovered.
func (c *Cycler) emitCycleRecovered(ctx context.Context, cycleID, phaseAtCrash string) {
	payload := core.SessionKeeperCycleRecoveredPayload{
		AgentName:    c.cfg.AgentName,
		CycleID:      cycleID,
		PhaseAtCrash: phaseAtCrash,
	}
	raw, _ := json.Marshal(payload)                                                                //nolint:errcheck
	_ = c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperCycleRecovered, raw) //nolint:errcheck
}
