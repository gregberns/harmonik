package main

import (
	"strings"
	"testing"
)

// TestUsageListsEveryCommandVerb drives the production usage renderer. It
// fails if either the dispatcher table or rendered usage gains a verb alone.
func TestUsageListsEveryCommandVerb(t *testing.T) {
	_, stderr := captureUsageIO(t, harmonikUsage)
	usageVerbs := usageCommandVerbs(stderr)
	tableVerbs := make(map[string]struct{}, len(commandVerbs))
	for _, command := range commandVerbs {
		tableVerbs[command.name] = struct{}{}
	}

	if len(usageVerbs) != len(commandVerbs) {
		t.Fatalf("usage lists %d verbs; command table holds %d", len(usageVerbs), len(commandVerbs))
	}
	for _, command := range commandVerbs {
		if _, ok := usageVerbs[command.name]; !ok {
			t.Errorf("command table verb %q is absent from production usage", command.name)
		}
	}
	for verb := range usageVerbs {
		if _, ok := tableVerbs[verb]; !ok {
			t.Errorf("production usage verb %q has no command table entry", verb)
		}
	}
}

func usageCommandVerbs(usage string) map[string]struct{} {
	verbs := make(map[string]struct{})
	inCommands := false
	for _, line := range strings.Split(usage, "\n") {
		switch strings.TrimSpace(line) {
		case "SUBCOMMANDS":
			inCommands = true
			continue
		case "DAEMON FLAGS (used with \"start daemon\")":
			inCommands = false
		}
		if inCommands && strings.HasPrefix(line, "  ") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				verbs[fields[0]] = struct{}{}
			}
		}
	}
	return verbs
}
