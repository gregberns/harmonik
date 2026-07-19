package keeper

// delivery_decision_0nlqs.go — T7 (hk-keeper-delivery-decision-0nlqs): the K1
// leader-warn delivery decision (session-keeper.md §4.11 SK-022/023/024/025 +
// §5 SK-INV-006). At a leader warn tick the keeper routes deterministically to
// EXACTLY ONE channel:
//
//	leader + presence-Online (age < 120s) → comms agent_message (NO pane write, NO --wake)
//	leader + Stale/Offline                 → terminal fallback (the existing warn pane
//	                                          path, hk-89g settle+retry-Enter loop preserved)
//
// A fired leader warn tick that resolves to NEITHER is a conformance failure
// (SK-INV-006 delivery totality). Crew role is unchanged this work.
//
// Consumes T1's presence read (AIS-020) and T3's selectLeaderDeferText body (SK-026).
//
// WIRING NOTE: this file holds the decision LOGIC + its seams only. The one call
// site in Watcher.Run (the pane-inject block ~watcher.go:1608) is sequenced AFTER
// charlie's T4 lands to avoid the shared-index hazard on watcher.go; the logic and
// its acceptance are unit-testable here without that hook (via the tmuxRunFn +
// commsSendFn seams), mirroring how T1/T3 staged selectLeaderDeferText.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/presence"
	"github.com/gregberns/harmonik/internal/substrate"
)

// leaderDeliveryChannel names the channel a leader warn tick resolved to. The
// decision always returns one of these two — never empty (SK-INV-006).
type leaderDeliveryChannel string

const (
	leaderDeliveryComms    leaderDeliveryChannel = "comms"
	leaderDeliveryTerminal leaderDeliveryChannel = "terminal"
)

// isLeaderRole reports whether agent is a leader session (captain / admiral) — the
// roles the K1 comms-delivery path applies to. Crew keeps the existing pane path
// (crew role unchanged this work).
func isLeaderRole(agent string) bool {
	return agent == "captain" || agent == "admiral"
}

// commsSendArgs builds the argv (after the binary) for the leader comms nudge:
// `comms send --from keeper --to <agent> --topic keeper -- <body>`. It NEVER
// includes --wake (SK-022): the leader reads the message via its armed
// recv-follow, not a pane poke. Extracted so a test can assert the arg shape
// without executing a subprocess.
func commsSendArgs(agent, body string) []string {
	return []string{"comms", "send", "--from", "keeper", "--to", agent, "--topic", "keeper", "--", body}
}

// commsSendFn is the seam through which the keeper shells `harmonik comms send`
// for the leader comms nudge. Package-level var (mirrors tmuxRunFn) so tests swap
// it and assert the send WITHOUT a real daemon; prod = runCommsSend. Refs: T7.
var commsSendFn = runCommsSend

// runCommsSend is the production commsSendFn: it shells the current harmonik binary
// with commsSendArgs — fire-and-forget, no join, no subscription, no --wake
// (SK-022). os.Executable() keeps this PATH-independent: the keeper runs inside the
// harmonik binary, so it re-invokes itself.
func runCommsSend(ctx context.Context, agent, body string) error {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = "harmonik"
	}
	//nolint:gosec // G204: bin is os.Executable() (the keeper's own harmonik binary); the args are fixed keeper-owned literals plus the nudge body, not user input.
	cmd := exec.CommandContext(ctx, bin, commsSendArgs(agent, body)...)
	if out, runErr := cmd.CombinedOutput(); runErr != nil {
		return fmt.Errorf("keeper: comms send --from keeper --to %s: %w (out: %s)",
			agent, runErr, strings.TrimSpace(string(out)))
	}
	return nil
}

// leaderPresenceOnline reads the leader's in-process presence-Online reachability
// (SK-023 / AIS-020): ComputeRegistry over events.jsonl → GetState == Online (age
// < presence.TTL = 120s). A missing record or empty events path reads as NOT
// Online, so the decision fails toward the terminal fallback — a warn is never
// lost. AIS-020 necessary-but-not-sufficient: a present recv-follow is the sharper
// signal, recorded as future work; presence-Online is the read available today.
func leaderPresenceOnline(eventsPath, agent string) bool {
	if eventsPath == "" {
		return false
	}
	rec, known := presence.ComputeRegistry(eventsPath)[agent]
	if !known {
		return false
	}
	return presence.GetState(rec) == presence.StateOnline
}

// deliverLeaderWarn executes the K1 delivery decision for a leader warn tick and
// returns the channel it resolved to. It ALWAYS resolves to exactly one of
// {comms, terminal} (SK-INV-006) — a comms-send error is not a silent no-op: it
// falls back to the terminal path so the warn still lands. nonce is the keeper
// cycle id carried in the K2 body (SK-030). operatorAttached gates ONLY the
// terminal fallback text (the comms path writes no pane, so an attached operator
// is harmless there — SK-022; the in-cycle re-check for the fallback is T8/SK-035).
func (w *Watcher) deliverLeaderWarn(ctx context.Context, ctxFile *CtxFile, crispIdle, operatorAttached bool, nonce string) (leaderDeliveryChannel, error) {
	if leaderPresenceOnline(w.cfg.EventsJSONLPath, w.cfg.AgentName) {
		// Comms path: fire-and-forget agent_message carrying the K2 defer body +
		// the K3 restart-now command. ZERO pane write, no --wake (SK-022).
		body := w.cfg.selectLeaderDeferText(nonce)
		if err := commsSendFn(ctx, w.cfg.AgentName, body); err != nil {
			slog.WarnContext(ctx, "keeper: leader comms nudge failed; falling back to terminal",
				"agent", w.cfg.AgentName, "err", err)
			return leaderDeliveryTerminal, w.deliverTerminalWarn(ctx, ctxFile, crispIdle, operatorAttached)
		}
		return leaderDeliveryComms, nil
	}
	// Stale/Offline leader: terminal fallback (the existing warn pane path).
	return leaderDeliveryTerminal, w.deliverTerminalWarn(ctx, ctxFile, crispIdle, operatorAttached)
}

// mintCycleID mints ONE cyc-id for a leader warn nudge (the confirmed nonce
// model): the SAME id is threaded into the nudge's restart-now --nonce AND the
// handoff KEEPER:<id> marker instruction, so nudge == handoff marker ==
// restart-now event is a single audit join key (SK-030 / SK-031). It reuses the
// Cycler's generator when present (shared monotonic seq — no duplicate mint), and
// falls back to a one-shot generator only for a Cycler-less (WarnOnly) leader.
func (w *Watcher) mintCycleID() string {
	if w.cfg.Cycler != nil {
		return w.cfg.Cycler.MintCycleID()
	}
	clock := w.cfg.Clock
	if clock == nil {
		clock = substrate.SystemClock{}
	}
	return newCycleIDGen(clock)()
}

// maybeDeliverLeaderWarn applies the T7 K1 delivery decision at the warn tick.
// It returns handled=true when this session takes the leader delivery path — a
// leader (isLeaderRole) on the PRODUCTION path (the InjectFn test seam unset) —
// in which case the Run caller skips the pane-inject fallback; cleared reports
// whether delivery succeeded so the caller clears pendingInject (a failure leaves
// it set to retry). It returns (false,false) for crew OR any InjectFn-set path,
// which keeps the existing pane behavior byte-identical (crew unchanged; tests
// that set InjectFn still exercise the pane path directly). One minted cyc-id is
// threaded into the nudge's restart-now --nonce AND its handoff KEEPER:<id> marker
// instruction (SK-030/SK-031). Refs: T7.
func (w *Watcher) maybeDeliverLeaderWarn(ctx context.Context, ctxFile *CtxFile, crispIdle bool) (handled, cleared bool) {
	if w.cfg.InjectFn != nil || !isLeaderRole(w.cfg.AgentName) {
		return false, false
	}
	operatorAttached := w.cfg.TmuxTarget != "" && w.cfg.OperatorAttachedFn(w.cfg.TmuxTarget)
	ch, err := w.deliverLeaderWarn(ctx, ctxFile, crispIdle, operatorAttached, w.mintCycleID())
	if err != nil {
		slog.WarnContext(ctx, "keeper: leader warn delivery", "agent", w.cfg.AgentName, "channel", string(ch), "err", err)
		return true, false
	}
	return true, true
}

// deliverTerminalWarn runs the existing terminal warn injection: selectWarnText +
// InjectText, with the hk-89g 750ms-settle + retry-Enter loop preserved verbatim
// (via injectTextClocked). It honors the InjectFn test seam exactly like the
// Watcher.Run pane-inject block, and is the SINGLE terminal path shared by the
// leader Stale/Offline fallback and (post-wire) the Run loop. Refs: SK-025, T7.
func (w *Watcher) deliverTerminalWarn(ctx context.Context, ctxFile *CtxFile, crispIdle, operatorAttached bool) error {
	inject := w.cfg.InjectFn
	if inject == nil {
		text := w.cfg.selectWarnText(ctxFile, crispIdle, operatorAttached)
		inject = func(ctx context.Context, target string) error {
			return InjectText(ctx, target, text)
		}
	}
	return inject(ctx, w.cfg.TmuxTarget)
}
