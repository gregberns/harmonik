package keeper

// leader_defer_template_xi7p8_test.go — T3 (hk-keeper-delivery-templated-slots-xi7p8)
// acceptance for the K2 leader defer template: four normative structural slots
// (SK-026), the verbatim four-part good-stopping-point self-test (SK-027), and the
// structure-normative / prose-tunable override validation with compiled-default
// fallback (SK-033).
//
// Substrate/template only — NOT the K1 delivery decision (T7). No threshold change.

import (
	"fmt"
	"strings"
	"testing"
)

// TestLeaderDeferBody_CompiledDefaultHasAllFourSlots — the compiled default carries
// all four SK-026 elements, the verbatim SK-027 self-test, and renders the SK-030
// restart-now command with --agent and --nonce.
func TestLeaderDeferBody_CompiledDefaultHasAllFourSlots(t *testing.T) {
	body := LeaderDeferBody("captain", "cyc-123")

	// Slots 1, 2, 3-anchor, 4 (SK-026).
	for _, slot := range []string{
		deferOperatorExchangeToken,    // slot 1
		deferInflightUnitToken,        // slot 2
		goodStoppingPointToken,        // slot 3 anchor
		"harmonik keeper restart-now", // slot 4 stem
	} {
		if !strings.Contains(body, slot) {
			t.Errorf("compiled default missing SK-026 slot %q:\n%s", slot, body)
		}
	}

	// Verbatim four-part SK-027 self-test (i)–(iv).
	for _, part := range []string{
		"mid-edit / mid-plan / mid-tool-sequence", // (i)
		"trivially re-derivable",                  // (ii)
		"no unanswered operator question",         // (iii)
		"no redo and no lost decision",            // (iv)
	} {
		if !strings.Contains(body, part) {
			t.Errorf("compiled default missing SK-027 self-test criterion %q:\n%s", part, body)
		}
	}

	// SK-030 restart-now slot renders with --agent <name> --nonce <cycle_id>.
	wantCmd := "harmonik keeper restart-now --agent captain --nonce cyc-123"
	if !strings.Contains(body, wantCmd) {
		t.Errorf("restart-now slot did not render %q:\n%s", wantCmd, body)
	}

	// T7 nonce model: the body instructs the agent to write the SAME cycle_id as
	// the handoff KEEPER:<id> marker, so nudge == handoff marker == restart-now
	// event is one join key (SK-030/SK-031).
	wantMarker := nonceMarker("cyc-123") // <!-- KEEPER:cyc-123 -->
	if !strings.Contains(body, wantMarker) {
		t.Errorf("body did not instruct the handoff marker %q:\n%s", wantMarker, body)
	}

	// The compiled default must itself be structurally complete (else selection loops).
	if !leaderDeferHasAllSlots(body) {
		t.Errorf("compiled default fails its own four-slot completeness check:\n%s", body)
	}
}

// TestSelectLeaderDeferText_FallbackPerMissingSlot — an override that DROPS any one
// of the four slots independently falls back to the compiled default; a full,
// structurally-complete override ships the operator's prose verbatim; an empty
// override uses the compiled default. (SK-033.)
func TestSelectLeaderDeferText_FallbackPerMissingSlot(t *testing.T) {
	const agent, nonce = "captain", "cyc-777"
	compiled := LeaderDeferBody(agent, nonce)

	// A structurally-complete operator override: all four slots present, custom prose.
	fullOverride := fmt.Sprintf(
		"Operator note: please %s, then %s; only stop at a %s; then run harmonik keeper restart-now --agent %s.",
		deferOperatorExchangeToken, deferInflightUnitToken, goodStoppingPointToken, agent,
	)
	if !leaderDeferHasAllSlots(fullOverride) {
		t.Fatalf("test bug: fullOverride is not structurally complete: %q", fullOverride)
	}

	cases := []struct {
		name     string
		override string
		wantBody string // fullOverride (ships prose) or compiled (fallback)
	}{
		{"empty -> compiled default", "", compiled},
		{"full override -> ships operator prose", fullOverride, fullOverride},
		{"missing slot 1 (defer-A) -> fallback",
			strings.Replace(fullOverride, deferOperatorExchangeToken, "wrap things up", 1), compiled},
		{"missing slot 2 (defer-B) -> fallback",
			strings.Replace(fullOverride, deferInflightUnitToken, "wrap up the task", 1), compiled},
		{"missing slot 3 (self-test) -> fallback",
			strings.Replace(fullOverride, goodStoppingPointToken, "a clean break", 1), compiled},
		{"missing slot 4 (restart-now) -> fallback",
			strings.Replace(fullOverride, "harmonik keeper restart-now", "restart yourself", 1), compiled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := WatcherConfig{AgentName: agent, LeaderDeferText: tc.override}
			got := cfg.selectLeaderDeferText(nonce)
			if got != tc.wantBody {
				t.Errorf("selectLeaderDeferText:\n got  = %q\n want = %q", got, tc.wantBody)
			}
		})
	}
}
