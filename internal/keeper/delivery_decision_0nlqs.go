package keeper

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

type leaderDeliveryChannel string

const (
	leaderDeliveryComms    leaderDeliveryChannel = "comms"
	leaderDeliveryTerminal leaderDeliveryChannel = "terminal"
)

func isLeaderRole(agent string) bool {
	return agent == "captain" || agent == "admiral"
}

func commsSendArgs(agent, body string) []string {
	return []string{"comms", "send", "--from", "keeper", "--to", agent, "--topic", "keeper", "--", body}
}

var commsSendFn = runCommsSend

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

func (w *Watcher) deliverLeaderWarn(ctx context.Context, ctxFile *CtxFile, crispIdle, operatorAttached bool, nonce string) (leaderDeliveryChannel, error) {
	if leaderPresenceOnline(w.cfg.EventsJSONLPath, w.cfg.AgentName) {
		body := w.cfg.selectLeaderDeferText(nonce)
		if err := commsSendFn(ctx, w.cfg.AgentName, body); err != nil {
			slog.WarnContext(ctx, "keeper: leader comms nudge failed; falling back to terminal",
				"agent", w.cfg.AgentName, "err", err)
			return leaderDeliveryTerminal, w.deliverTerminalWarn(ctx, ctxFile, crispIdle, operatorAttached)
		}
		return leaderDeliveryComms, nil
	}
	return leaderDeliveryTerminal, w.deliverTerminalWarn(ctx, ctxFile, crispIdle, operatorAttached)
}

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

func (w *Watcher) deliverTerminalWarn(ctx context.Context, ctxFile *CtxFile, crispIdle, operatorAttached bool) error {
	inject := w.cfg.InjectFn
	if inject == nil {
		text := w.cfg.selectWarnText(ctxFile, crispIdle, operatorAttached)
		inject = func(ctx context.Context, target string) error {
			return InjectText(ctx, target, AutomationMessage("keeper", text))
		}
	}
	return inject(ctx, w.cfg.TmuxTarget)
}
