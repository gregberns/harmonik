package main

// watcherreap.go — hk-6629b: launch-path reap of prior same-agent
// `comms recv --follow` / `subscribe --follow` watchers, wired into both the
// captain launcher (captain.go) and crew start (crew.go). See
// internal/lifecycle/agentwatcherreap.go for the enumeration/kill mechanics
// and the captain ruling this implements (bead comment 2176): reap prior
// same-agent watchers REGARDLESS OF LIVENESS, on the launch path only — no
// daemon subscribe-server edit (that is the separate follow-up bead,
// hk-f9gna).
//
// Bead: hk-6629b.

import (
	"context"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// reapPriorAgentWatchersFn is the seam tests inject into captainReapPriorWatchers /
// crewReapPriorWatchers to observe or no-op the reap call without touching the
// real process table.
type reapPriorAgentWatchersFn func(agent string)

// reapPriorAgentWatchers is the production reap hook: it enumerates and
// terminates every prior --follow watcher process addressed to agent,
// regardless of liveness (see lifecycle.ReapPriorAgentFollowWatchers for why),
// and reports survivors/errors to stderr. Best-effort: never blocks or fails
// the launch — a missed reap just leaves the pre-existing leak in place,
// which is the status quo, not a regression introduced by this call.
func reapPriorAgentWatchers(agent string) {
	survived, err := lifecycle.ReapPriorAgentFollowWatchers(context.Background(), nil, agent, os.Getpid(), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik: reap prior --follow watcher(s) for %q: %v\n", agent, err)
		return
	}
	if len(survived) > 0 {
		fmt.Fprintf(os.Stderr, "harmonik: prior --follow watcher(s) for %q survived SIGKILL: %v\n", agent, survived)
	}
}
