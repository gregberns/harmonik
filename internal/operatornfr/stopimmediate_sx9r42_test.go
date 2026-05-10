package operatornfr_test

// stopImmediateFixture — spec-level harness for hk-sx9r.42.
//
// Covers: ON-028 (`stop --immediate` skips drain steps 2–3), with specific
// focus on the subprocess-kill sequence (SIGTERM → bounded window → SIGKILL)
// and the recoverability claim for in-flight run state.
//
// The steps-2-and-3 skippability test and spec-section-exists test live in
// securitydrain_sx9r80_test.go (the §10.2 parent harness). This file adds the
// subprocess-kill and recoverability sub-rule tests scoped to hk-sx9r.42.
//
// Spec ref: specs/operator-nfr.md §4.7 ON-028.

import (
	"strings"
	"testing"
)

// stopImmediateFixtureKillStep models one step in the SIGTERM → SIGKILL kill
// sequence that ON-028 requires for in-flight agent subprocesses.
//
// Spec ref: operator-nfr.md §4.7 ON-028 — "In-flight agent subprocesses MUST
// be killed (SIGTERM with a short bounded window, then SIGKILL)."
type stopImmediateFixtureKillStep struct {
	Signal      string // "SIGTERM" or "SIGKILL"
	Description string
	SpecRef     string
}

// stopImmediateFixtureKillSequence is the authoritative two-step fixture
// encoding of the ON-028 subprocess-kill protocol.
var stopImmediateFixtureKillSequence = []stopImmediateFixtureKillStep{
	{"SIGTERM", "initial graceful-termination signal with short bounded window", "ON-028"},
	{"SIGKILL", "unconditional kill if subprocess survives SIGTERM window", "ON-028"},
}

// TestON028_SubprocessKillSequenceIsTwoStep verifies the fixture encodes
// exactly two steps (SIGTERM then SIGKILL) as declared by ON-028.
//
// Spec ref: operator-nfr.md §4.7 ON-028 — "(SIGTERM with a short bounded
// window, then SIGKILL)."
func TestON028_SubprocessKillSequenceIsTwoStep(t *testing.T) {
	t.Parallel()

	const wantSteps = 2
	if len(stopImmediateFixtureKillSequence) != wantSteps {
		t.Errorf("ON-028: kill-sequence fixture has %d steps, want %d (SIGTERM then SIGKILL)",
			len(stopImmediateFixtureKillSequence), wantSteps)
	}

	if len(stopImmediateFixtureKillSequence) >= 2 {
		if stopImmediateFixtureKillSequence[0].Signal != "SIGTERM" {
			t.Errorf("ON-028: kill-sequence step 0 = %q, want SIGTERM", stopImmediateFixtureKillSequence[0].Signal)
		}
		if stopImmediateFixtureKillSequence[1].Signal != "SIGKILL" {
			t.Errorf("ON-028: kill-sequence step 1 = %q, want SIGKILL", stopImmediateFixtureKillSequence[1].Signal)
		}
	}
}

// TestON028_KillSequenceHasSpecRefs verifies every kill-sequence step has a
// non-empty SpecRef.
//
// Spec ref: operator-nfr.md §4.7 ON-028.
func TestON028_KillSequenceHasSpecRefs(t *testing.T) {
	t.Parallel()

	for _, step := range stopImmediateFixtureKillSequence {
		step := step
		t.Run(step.Signal, func(t *testing.T) {
			t.Parallel()

			if step.SpecRef == "" {
				t.Errorf("ON-028: kill-sequence step %q has empty SpecRef", step.Signal)
			}
		})
	}
}

// TestON028_SubprocessKillSequenceInSpec verifies that the spec explicitly
// declares the SIGTERM → SIGKILL kill sequence for in-flight subprocesses.
//
// Spec ref: operator-nfr.md §4.7 ON-028 — "In-flight agent subprocesses MUST
// be killed (SIGTERM with a short bounded window, then SIGKILL)."
func TestON028_SubprocessKillSequenceInSpec(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "SIGTERM") {
		t.Error("ON-028: specs/operator-nfr.md missing 'SIGTERM' in ON-028 subprocess-kill sequence")
	}
	if !strings.Contains(content, "SIGKILL") {
		t.Error("ON-028: specs/operator-nfr.md missing 'SIGKILL' in ON-028 subprocess-kill sequence")
	}
	if !strings.Contains(content, "short bounded window") {
		t.Error("ON-028: specs/operator-nfr.md missing 'short bounded window' qualifier for the SIGTERM phase in ON-028")
	}
}

// TestON028_InFlightRunStateIsRecoverableViaCheckpoint verifies that the spec
// explicitly states that in-flight run state is recoverable on next startup via
// checkpoint + reconciliation.
//
// Spec ref: operator-nfr.md §4.7 ON-028 — "In-flight run state is recoverable
// on next startup via checkpoint + reconciliation per
// [reconciliation/spec.md §4.2]."
func TestON028_InFlightRunStateIsRecoverableViaCheckpoint(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "recoverable on next startup") {
		t.Error("ON-028: specs/operator-nfr.md missing 'recoverable on next startup' recoverability claim in ON-028")
	}
	if !strings.Contains(content, "checkpoint + reconciliation") {
		t.Error("ON-028: specs/operator-nfr.md missing 'checkpoint + reconciliation' as the recovery mechanism in ON-028")
	}
}

// TestON028_InFlightSubprocessesNotGracefullyStopped verifies that the spec
// explicitly acknowledges that in-flight agent subprocesses are NOT gracefully
// stopped on stop --immediate.
//
// Spec ref: operator-nfr.md §4.7 ON-028 — "but the in-flight agent
// subprocesses are not gracefully stopped."
func TestON028_InFlightSubprocessesNotGracefullyStopped(t *testing.T) {
	t.Parallel()

	root := obligationsFixtureRepoRoot(t)
	data := securityDrainFixtureReadSpec(t, root)
	content := string(data)

	if !strings.Contains(content, "not gracefully stopped") {
		t.Error("ON-028: specs/operator-nfr.md missing 'not gracefully stopped' acknowledgment for subprocesses in ON-028")
	}
}
