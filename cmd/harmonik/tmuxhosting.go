package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

func bootNotef(out io.Writer, format string, a ...any) {
	fmt.Fprintf(out, format, a...) //nolint:errcheck // diagnostic write to stderr; error non-actionable
}

type tmuxHosting struct {
	// Available reports that tmux is usable AND a spawn-target session is ready.
	// When false, SessionName is empty and nothing may be spawned via tmux.
	Available bool

	// SessionName is the resolved spawn-target session name.
	SessionName string

	// NeedKeepalive is true when the daemon created the session itself and is
	// therefore responsible for keeping it alive for its whole lifetime
	// (hk-9ptu). False when the daemon is spawning into a session it does not
	// own — the operator's ambient one.
	NeedKeepalive bool

	// Ambient reports that $TMUX was set at boot.
	Ambient bool

	// Err is the reason tmux hosting is unavailable. Non-nil iff !Available.
	Err error
}

func resolveTmuxHosting(ctx context.Context, projectDir string, adapter tmux.OSAdapter, out io.Writer) tmuxHosting {
	h := tmuxHosting{Ambient: os.Getenv("TMUX") != ""}

	if probeErr := adapter.ProbeTmux(ctx); probeErr != nil {
		h.Err = probeErr
		return h
	}

	liveSession := ""
	if h.Ambient {
		if outBytes, dmErr := exec.CommandContext(ctx, "tmux", "display-message", "-p", "#{session_name}").Output(); dmErr != nil {
			bootNotef(out, "harmonik: tmux display-message failed (%v); falling back to deterministic daemon session\n", dmErr)
		} else {
			liveSession = strings.TrimSpace(string(outBytes))
		}
	}

	sessionName, needEnsure := tmux.ResolveDaemonSpawnSession(projectDir, liveSession)

	if needEnsure {
		if ensErr := adapter.EnsureSession(ctx, sessionName, projectDir); ensErr != nil {
			h.Err = fmt.Errorf("cannot ensure daemon tmux session %q: %w", sessionName, ensErr)
			return h
		}
		if h.Ambient {
			bootNotef(out, "harmonik: spawning implementer windows into daemon-owned session %q (ambient session was supervisor/flywheel/empty)\n", sessionName)
		} else {
			bootNotef(out, "harmonik: no ambient tmux client ($TMUX unset) — created daemon-owned session %q; inspect live agents with: tmux attach -t %s\n", sessionName, sessionName)
		}
	}

	h.Available = true
	h.SessionName = sessionName
	h.NeedKeepalive = needEnsure
	return h
}

func reportNoTmuxHosting(h tmuxHosting, tmuxSubstrate bool, out io.Writer) int {
	if tmuxSubstrate {
		bootNotef(out, "harmonik: FATAL — tmux hosting is unavailable (%v) and the tmux substrate is selected.\n"+
			"  Every agent spawn routes through `tmux new-window`, so the daemon would boot and then fail on the first dispatch.\n"+
			"  Fix one of:\n"+
			"    - install tmux >= 3.0 on PATH (an ambient $TMUX session is NOT required — the daemon creates its own)\n"+
			"    - select the structured Codex driver, which owns child stdio and needs no tmux: %s=codexdriver\n",
			h.Err, substrateSelectEnv)
		return 1
	}
	bootNotef(out, "harmonik: WARNING — running WITHOUT tmux hosting (%v).\n"+
		"  The structured Codex driver is selected, so agent dispatch works: it owns child stdio directly.\n"+
		"  DEGRADED, until tmux >= 3.0 is on PATH:\n"+
		"    - NO SUPERVISOR: `harmonik supervise start` builds its flywheel session with tmux, so this\n"+
		"      daemon has no auto-revive — if it dies, nothing restarts it.\n"+
		"    - `harmonik start crew` / `start captain` / keeper nudges cannot create their tmux sessions\n"+
		"    - no attachable agent panes; live inspection is `harmonik subscribe` only, plus the\n"+
		"      capture-tee IF you opt in by setting %s (it is off by default)\n",
		h.Err, captureDirEnv)
	return 0
}
