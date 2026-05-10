package operatornfr_test

// attachSurfaceFixture — spec-level harness for hk-sx9r.61.
//
// Covers: ON-050 (`harmonik attach` minimum surface — five sub-rules) with
// specific focus on T_attach_status default, inline operator-command subset,
// and event-tap event-class subset. The five-clause fixture and session_id
// tests live in multidaemon_sx9r83_test.go (the §10.2 parent harness); this
// file adds the sub-rule detail tests scoped to hk-sx9r.61.
//
// Spec ref: specs/operator-nfr.md §4.10 ON-050.

import (
	"strings"
	"testing"
)

// attachSurfaceFixtureInlineCommand models one inline operator command that
// ON-050(d) requires `harmonik attach` to accept.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(d) accept operator commands
// inline (subset of `pause`, `resume`, `stop`, `enqueue`)."
type attachSurfaceFixtureInlineCommand struct {
	Command string
	SpecRef string
}

// attachSurfaceFixtureInlineCommands is the authoritative fixture encoding of
// the four inline commands declared by ON-050(d).
var attachSurfaceFixtureInlineCommands = []attachSurfaceFixtureInlineCommand{
	{"pause", "ON-050(d) — pause the daemon"},
	{"resume", "ON-050(d) — resume the daemon"},
	{"stop", "ON-050(d) — stop the daemon"},
	{"enqueue", "ON-050(d) — enqueue a new run"},
}

// attachSurfaceFixtureEventClass models one event class that ON-050(b) requires
// in the live event tap.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(b) stream a live event tap
// (subset of `daemon_*`, `run_*`, `node_*`, `operator_*`, `error` events)."
type attachSurfaceFixtureEventClass struct {
	Class   string
	SpecRef string
}

// attachSurfaceFixtureEventClasses is the authoritative fixture encoding of
// the five event-class prefixes declared by ON-050(b).
var attachSurfaceFixtureEventClasses = []attachSurfaceFixtureEventClass{
	{"daemon_*", "ON-050(b) — daemon lifecycle events"},
	{"run_*", "ON-050(b) — run lifecycle events"},
	{"node_*", "ON-050(b) — node lifecycle events"},
	{"operator_*", "ON-050(b) — operator-command events"},
	{"error", "ON-050(b) — error class events"},
}

// TestON050_TAttachStatusDefaultIs10s verifies that the spec declares
// T_attach_status with a default of 10 seconds.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(c) present a periodic status
// snapshot … every T_attach_status (default 10s, operator-tunable)."
func TestON050_TAttachStatusDefaultIs10s(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "T_attach_status") {
		t.Error("ON-050: specs/operator-nfr.md missing 'T_attach_status' parameter name in ON-050(c)")
	}
	if !strings.Contains(content, "default 10s") {
		t.Error("ON-050: specs/operator-nfr.md missing 'default 10s' for T_attach_status in ON-050(c)")
	}
	if !strings.Contains(content, "operator-tunable") {
		t.Error("ON-050: specs/operator-nfr.md missing 'operator-tunable' qualifier for T_attach_status in ON-050(c)")
	}
}

// TestON050_InlineCommandSubsetIsFour verifies the fixture encodes exactly four
// inline operator commands per ON-050(d).
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(d) accept operator commands
// inline (subset of `pause`, `resume`, `stop`, `enqueue`)."
func TestON050_InlineCommandSubsetIsFour(t *testing.T) {
	t.Parallel()

	const wantCommands = 4
	if len(attachSurfaceFixtureInlineCommands) != wantCommands {
		t.Errorf("ON-050: inline-command fixture has %d entries, want %d (pause, resume, stop, enqueue)",
			len(attachSurfaceFixtureInlineCommands), wantCommands)
	}

	required := map[string]bool{
		"pause":   false,
		"resume":  false,
		"stop":    false,
		"enqueue": false,
	}
	for _, cmd := range attachSurfaceFixtureInlineCommands {
		required[cmd.Command] = true
	}
	for cmd, found := range required {
		if !found {
			t.Errorf("ON-050: inline operator command %q missing from fixture; ON-050(d) requires all four", cmd)
		}
	}
}

// TestON050_InlineCommandsHaveSpecRefs verifies every inline command has a
// non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.10 ON-050.
func TestON050_InlineCommandsHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, cmd := range attachSurfaceFixtureInlineCommands {
		cmd := cmd
		t.Run(cmd.Command, func(t *testing.T) {
			t.Parallel()

			if cmd.SpecRef == "" {
				t.Errorf("ON-050: inline command %q has empty SpecRef", cmd.Command)
			}
		})
	}
}

// TestON050_EventTapClassesAreFive verifies the fixture encodes exactly five
// event-class prefixes for the live event tap per ON-050(b).
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(b) stream a live event tap
// (subset of `daemon_*`, `run_*`, `node_*`, `operator_*`, `error` events)."
func TestON050_EventTapClassesAreFive(t *testing.T) {
	t.Parallel()

	const wantClasses = 5
	if len(attachSurfaceFixtureEventClasses) != wantClasses {
		t.Errorf("ON-050: event-tap class fixture has %d entries, want %d (daemon_*, run_*, node_*, operator_*, error)",
			len(attachSurfaceFixtureEventClasses), wantClasses)
	}

	required := map[string]bool{
		"daemon_*":   false,
		"run_*":      false,
		"node_*":     false,
		"operator_*": false,
		"error":      false,
	}
	for _, ec := range attachSurfaceFixtureEventClasses {
		required[ec.Class] = true
	}
	for class, found := range required {
		if !found {
			t.Errorf("ON-050: event-tap class %q missing from fixture; ON-050(b) requires all five", class)
		}
	}
}

// TestON050_EventTapClassesHaveSpecRefs verifies every event-tap class has a
// non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.10 ON-050.
func TestON050_EventTapClassesHaveSpecRefs(t *testing.T) {
	t.Parallel()

	for _, ec := range attachSurfaceFixtureEventClasses {
		ec := ec
		t.Run(ec.Class, func(t *testing.T) {
			t.Parallel()

			if ec.SpecRef == "" {
				t.Errorf("ON-050: event-tap class %q has empty SpecRef", ec.Class)
			}
		})
	}
}

// TestON050_EventTapClassesInSpec verifies that the spec names the five event
// class prefixes that ON-050(b) requires in the live event tap.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "subset of `daemon_*`, `run_*`,
// `node_*`, `operator_*`, `error` events per [event-model.md §8]."
func TestON050_EventTapClassesInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	for _, ec := range attachSurfaceFixtureEventClasses {
		ec := ec
		// Strip trailing wildcard for contains check since the spec uses both
		// forms (e.g., "daemon_*" and plain "daemon_").
		needle := strings.TrimSuffix(ec.Class, "*")
		if !strings.Contains(content, needle) {
			t.Errorf("ON-050: specs/operator-nfr.md missing event-tap class %q in ON-050(b)", ec.Class)
		}
	}
}

// TestON050_DetachOnSIGINTOrDetachCommandInSpec verifies that the spec names
// both SIGINT and `:detach` as detach triggers for ON-050(e).
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(e) detach cleanly on operator
// SIGINT or `:detach` command without affecting the daemon's state."
func TestON050_DetachOnSIGINTOrDetachCommandInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "SIGINT") {
		t.Error("ON-050: specs/operator-nfr.md missing 'SIGINT' detach trigger in ON-050(e)")
	}
	if !strings.Contains(content, ":detach") {
		t.Error("ON-050: specs/operator-nfr.md missing ':detach' command as detach trigger in ON-050(e)")
	}
}

// TestON050_DetachDoesNotAffectDaemonStateInSpec verifies that the spec
// explicitly requires detach to not affect daemon state.
//
// Spec ref: operator-nfr.md §4.10 ON-050 — "(e) … without affecting the
// daemon's state."
func TestON050_DetachDoesNotAffectDaemonStateInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "without affecting") {
		t.Error("ON-050: specs/operator-nfr.md missing 'without affecting' daemon-state invariant in ON-050(e)")
	}
}
