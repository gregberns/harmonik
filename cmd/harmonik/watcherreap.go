package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

type reapPriorAgentWatchersFn func(agent string)

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
