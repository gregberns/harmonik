//go:build integration

package keeper_test

// scenario_restartnow_integration_qji8g_test.go — T9 scenario (b), restart-now
// leg. Integration tier (build tag: integration, mirroring
// restartnow_smoke_integration_test.go). Bead
// hk-keeper-delivery-scenario-tests-qji8g.
//
// After the 300s handoff window has ABORTED (the harness tier proves the abort in
// scenario_delivery_harness_qji8g_test.go), the leader self-restarts on its own
// schedule via `keeper restart-now --nonce <cycle_id>`. This test drives that
// T+301 leg on a REAL tmux pane: RestartNow runs the full synchronous
// verify → ACK → /clear → brief sequence to a clean /clear, wholly independent of
// the aborted cycle's timer (SC-4), and the supplied nonce is CARRIED into the
// emitted ACK line (SK-030, T5). It reuses the smoke harness helpers
// (smokeStartSession / smokeWriteGaugeAndSID / smokeWriteFreshHandoff) and their
// exact-name, all-paths-cleanup safety contract.

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func TestScenario_RestartNow_AfterAbort_CarriesNonce_qji8g(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("qji8g: tmux not found on PATH; skipping restart-now T+301 integration test")
	}

	sessName := smokeSessionName() + "-qji8g-t301"
	t.Cleanup(func() { smokeKillSession(sessName) })
	smokeStartSession(t, sessName)
	if !smokeSessionAlive(sessName) {
		t.Fatalf("qji8g: tmux session %q not alive after new-session", sessName)
	}

	project := t.TempDir()
	agent := "qji8g-t301-" + strconv.Itoa(os.Getpid())

	// Valid UUIDv4 (lowercase) → IsPrimarySID == true.
	const primarySID = "aaaabbbb-cccc-4ddd-8eee-ffffffffffff"
	smokeWriteGaugeAndSID(t, project, agent, primarySID)
	smokeWriteFreshHandoff(t, project, agent)

	// The nonce is the originating cycle id — carried for audit (SK-030).
	const nonce = "cyc-qji8g-t301-000001"

	var injected []string
	spyInject := func(_ context.Context, _ string, text string) error {
		injected = append(injected, text)
		return nil
	}

	// RequestedAt models T+301 — 301s after the cycle opened. RestartNow does NOT
	// consult the aborted cycle timer; it runs its own synchronous pre-flight.
	err := keeper.RestartNow(context.Background(), keeper.RestartNowConfig{
		ProjectDir:  project,
		AgentName:   agent,
		TmuxTarget:  sessName,
		Inject:      spyInject,
		RequestedAt: time.Now().Add(301 * time.Second),
	}, nonce)
	if err != nil {
		t.Fatalf("qji8g: RestartNow (T+301 leg) returned error: %v", err)
	}

	// Clean sequence: ACK(nonce) → /clear → agent brief.
	if len(injected) != 3 {
		t.Fatalf("qji8g: got %d injections %v, want 3 (ack + /clear + brief)", len(injected), injected)
	}
	if want := keeper.AckLine(nonce, "restart"); injected[0] != want {
		t.Errorf("qji8g: inject[0] = %q, want %q (the nonce must be carried into the ACK)", injected[0], want)
	}
	if injected[1] != "/clear" {
		t.Errorf("qji8g: inject[1] = %q, want \"/clear\"", injected[1])
	}
	if !strings.Contains(injected[2], "agent brief") || !strings.Contains(injected[2], "keeper-restart") {
		t.Errorf("qji8g: inject[2] = %q, want 'agent brief ... keeper-restart'", injected[2])
	}
}
