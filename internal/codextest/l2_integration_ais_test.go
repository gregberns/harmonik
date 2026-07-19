package codextest_test

// L2 integration tier for the INPUT driver (T9): synthesized stimulus → InputTwin
// → codexinput reactor → InputBridgeSink over the §2.2 discrete-event harness.
// Asserts the exact port-effect shape per stratum on the no-fault path, plus one
// fault case per substrate mode asserting terminal-never-silence (RS-017). The
// exhaustive 4×strata×EventN matrix lives in l2_fault_matrix_ais_test.go.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

// TestL2AIS_AckedEffects — the happy path drives a positive ack terminal.
func TestL2AIS_AckedEffects(t *testing.T) {
	t.Parallel()
	sink, elapsed := runInputDiscrete(t, codexdigitaltwin.StratumAcked, codexdigitaltwin.FaultConfig{}, false)

	if got := sink.emitCount(codexinput.EmitInputSubmitted); got != 1 {
		t.Errorf("agent_input_submitted = %d, want 1", got)
	}
	if got := sink.emitCount(codexinput.EmitInputAcked); got != 1 {
		t.Errorf("agent_input_acked = %d, want 1", got)
	}
	if got := sink.emitCount(codexinput.EmitInputStale); got != 0 {
		t.Errorf("agent_input_stale = %d, want 0 on the acked path", got)
	}
	ack, ok := sink.firstEmit(codexinput.EmitInputAcked)
	if !ok || ack.InputSeq != 1 || ack.TurnID == "" {
		t.Errorf("acked emit = %+v, want seq 1 with a non-empty acceptance token", ack)
	}
	if sink.WriteInput != 1 {
		t.Errorf("write_input = %d, want 1", sink.WriteInput)
	}
	if sink.Handshakes != 1 {
		t.Errorf("send_handshake = %d, want 1", sink.Handshakes)
	}
	// No timer fired: elapsed is virtual-zero on the fully-acked path.
	if elapsed != 0 {
		t.Errorf("elapsed = %v, want 0 (no timer should fire when the ack lands)", elapsed)
	}
}

// TestL2AIS_RejectedEffects — a protocol rejection resolves the sync Ack{Rejected}
// (timer cancelled, no positive/stale emit), returning the reactor to Ready.
func TestL2AIS_RejectedEffects(t *testing.T) {
	t.Parallel()
	sink, _ := runInputDiscrete(t, codexdigitaltwin.StratumRejected, codexdigitaltwin.FaultConfig{}, false)

	if got := sink.emitCount(codexinput.EmitInputSubmitted); got != 1 {
		t.Errorf("agent_input_submitted = %d, want 1", got)
	}
	if got := sink.emitCount(codexinput.EmitInputAcked); got != 0 {
		t.Errorf("agent_input_acked = %d, want 0 on the rejected path", got)
	}
	if got := sink.emitCount(codexinput.EmitInputStale); got != 0 {
		t.Errorf("agent_input_stale = %d, want 0 (rejection is a definite terminal, not silence)", got)
	}
}

// TestL2AIS_StaleTimeoutEffects — a submission that never gets its ack reaches
// agent_input_stale via the armed input_ack_timeout (the resume-hang front-stop).
func TestL2AIS_StaleTimeoutEffects(t *testing.T) {
	t.Parallel()
	sink, elapsed := runInputDiscrete(t, codexdigitaltwin.StratumStaleTimeout, codexdigitaltwin.FaultConfig{}, false)

	if got := sink.emitCount(codexinput.EmitInputSubmitted); got != 1 {
		t.Errorf("agent_input_submitted = %d, want 1", got)
	}
	if got := sink.emitCount(codexinput.EmitInputStale); got != 1 {
		t.Errorf("agent_input_stale = %d, want 1", got)
	}
	if got := sink.emitCount(codexinput.EmitInputAcked); got != 0 {
		t.Errorf("agent_input_acked = %d, want 0 on the timeout path", got)
	}
	stale, ok := sink.firstEmit(codexinput.EmitInputStale)
	if !ok || stale.InputSeq != 1 || stale.Reason != "input_ack_timeout" {
		t.Errorf("stale emit = %+v, want seq 1 reason input_ack_timeout", stale)
	}
	if elapsed != aisConfig().InputAckTimeout {
		t.Errorf("elapsed = %v, want the InputAckTimeout window %v", elapsed, aisConfig().InputAckTimeout)
	}
}

// TestL2AIS_HandshakeFailEffects — the handshake never completes: the armed
// handshake_timeout fires → agent_launch_failure (AIS-017 fast-fail), never a
// silent exit-0.
func TestL2AIS_HandshakeFailEffects(t *testing.T) {
	t.Parallel()
	sink, elapsed := runInputDiscrete(t, codexdigitaltwin.StratumHandshakeFail, codexdigitaltwin.FaultConfig{}, false)

	if got := sink.emitCount(codexinput.EmitLaunchFailure); got != 1 {
		t.Errorf("agent_launch_failure = %d, want 1", got)
	}
	if got := sink.emitCount(codexinput.EmitInputSubmitted); got != 0 {
		t.Errorf("agent_input_submitted = %d, want 0 (no submission ever opened)", got)
	}
	if sink.Handshakes != 1 {
		t.Errorf("send_handshake = %d, want 1", sink.Handshakes)
	}
	if elapsed != aisConfig().HandshakeTimeout {
		t.Errorf("elapsed = %v, want the HandshakeTimeout window %v", elapsed, aisConfig().HandshakeTimeout)
	}
}

// TestL2AIS_FaultSmoke asserts RS-INV-003 for one representative cell per fault
// mode over the acked stratum: every fault yields exactly one explicit terminal
// (or the no-submit shape) within the virtual bound — never silence. EventN
// indexes the stripped stimulus (1=Spawned, 2=HandshakeOK, 3=InputSubmitted,
// 4=InputAcked, 5=TurnCompleted).
func TestL2AIS_FaultSmoke(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		fault         codexdigitaltwin.FaultConfig
		stallExpected bool
		wantStale     int
		wantAcked     int
	}{
		{
			// Child stdout closes right after submit (before the ack): the ignored
			// disconnect sentinel leaves the submission in flight; the armed
			// input_ack_timeout carries it to a bounded stale terminal.
			name:      "drop_after_submit",
			fault:     codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultDropAfter, EventN: 3},
			wantStale: 1,
		},
		{
			// The ack frame stalls (rendered, never delivered): the timeout front-
			// stop fires stale.
			name:          "stall_before_ack",
			fault:         codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultStall, EventN: 4},
			stallExpected: true,
			wantStale:     1,
		},
		{
			// The ack frame is truncated → ignored transport sentinel → timeout →
			// stale (a partial frame is never swallowed into silence).
			name:      "truncate_at_ack",
			fault:     codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultTruncate, EventN: 4},
			wantStale: 1,
		},
		{
			// The ack is delivered twice (double-submit / re-delivery probe): the
			// reactor's seq/phase guards absorb the second copy — one acked, no stale.
			name:      "dup_ack",
			fault:     codexdigitaltwin.FaultConfig{Mode: codexdigitaltwin.FaultDup, EventN: 4},
			wantAcked: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sink, elapsed := runInputDiscrete(t, codexdigitaltwin.StratumAcked, tc.fault, tc.stallExpected)
			if got := sink.emitCount(codexinput.EmitInputStale); got != tc.wantStale {
				t.Errorf("stale = %d, want %d", got, tc.wantStale)
			}
			if got := sink.emitCount(codexinput.EmitInputAcked); got != tc.wantAcked {
				t.Errorf("acked = %d, want %d", got, tc.wantAcked)
			}
			if bound := aisVirtualBound(aisConfig()); elapsed > bound {
				t.Errorf("terminal at %v beyond bound %v", elapsed, bound)
			}
		})
	}
}
