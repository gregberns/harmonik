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
	WarnAbsTokens int64   // absolute warn/re-arm threshold; default 240000
	WarnPctCeil   float64 // pct-of-window cap for warn gate; default 0.70

	// Pct-based fallbacks used when CtxFile.Tokens == 0 or WindowSize == 0
	// (older Claude Code versions that do not emit absolute token counts).
	ActPct  float64 // threshold to fire; default 90
	WarnPct float64 // re-arm threshold; default 80

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

	// AppendHandoffFn appends text to the handoff file after the nonce is
	// confirmed. Used to pin keeper-authoritative identity into the handoff so
	// the resumed agent reads the correct name rather than guessing from context.
	// Nil → default OS append.
	AppendHandoffFn func(path, text string) error

	// SetTmuxEnvFn sets a key=value in the tmux session that owns TmuxTarget.
	// Called after nonce confirmation so HARMONIK_AGENT is inherited by the
	// new Claude process started after /clear. Nil → default tmux setenv call.
	// No-op when TmuxTarget is empty.
	SetTmuxEnvFn func(ctx context.Context, target, key, value string) error
}

func (c *CyclerConfig) applyDefaults() {
	if c.ActAbsTokens <= 0 {
		c.ActAbsTokens = 300_000
	}
	if c.ActPctCeil <= 0 {
		c.ActPctCeil = 0.85
	}
	if c.WarnAbsTokens <= 0 {
		c.WarnAbsTokens = 240_000
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
	if c.AppendHandoffFn == nil {
		c.AppendHandoffFn = defaultAppendHandoff
	}
	if c.SetTmuxEnvFn == nil {
		c.SetTmuxEnvFn = SetTmuxEnv
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

	// Gate 3: context must reach the act threshold.
	// Uses absolute tokens when available (min(ActAbsTokens, ActPctCeil*window));
	// falls back to percentage when Tokens/WindowSize are absent (old .ctx files).
	if c.cfg.belowActThreshold(cf) {
		return nil
	}
	// Gate 4: agent must be at a crisp await-input boundary.
	if !c.cfg.CrispIdleFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		return nil
	}
	// Gate 5: no in-flight queue work (fail-closed via HoldingDispatchFn).
	if c.cfg.HoldingDispatchFn(c.cfg.ProjectDir, c.cfg.AgentName) {
		return nil
	}
	// Gate 6: full anti-loop suppression (only applies after the first fire).
	if c.lastFiredSID != "" {
		// Suppress if still on the same session as the last cycle.
		if cf.SessionID == c.lastFiredSID {
			return nil
		}
		// Suppress on the new session until pct has been observed below WarnPct.
		if !c.seenLowPctAfterLastFire {
			return nil
		}
	}

	return c.runCycle(ctx, cf)
}

// runCycle executes the full 7-step reset cycle.
// SAFETY: /clear is ONLY issued after the handoff nonce is positively confirmed.
func (c *Cycler) runCycle(ctx context.Context, cf *CtxFile) error {
	cycleID := c.cfg.CycleIDGen()
	now := time.Now().UTC()
	journalPath := c.journalPath()
	handoffPath := c.cfg.HandoffFilePath(c.cfg.ProjectDir, c.cfg.AgentName)

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
	if c.cfg.TmuxTarget != "" {
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

	// Observe context level for re-arm tracking (mirrors MaybeRun side-effect).
	if cf != nil && c.lastFiredSID != "" && cf.SessionID != c.lastFiredSID && c.cfg.belowWarnThreshold(cf) {
		c.seenLowPctAfterLastFire = true
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

	// All gates passed: emit the precompact event, clear the marker, run cycle.
	c.emitPrecompactBlocked(ctx, sessionID, "cycle_triggered")
	_ = c.cfg.ClearPrecompactTriggerFn(c.cfg.ProjectDir, c.cfg.AgentName) //nolint:errcheck

	if cf == nil {
		// Construct a minimal CtxFile so runCycle has a session_id.
		cf = &CtxFile{SessionID: sessionID}
	}
	return c.runCycle(ctx, cf)
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
			if err == nil && cf.SessionID != "" && cf.SessionID != prevSID {
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
