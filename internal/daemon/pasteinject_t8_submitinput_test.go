package daemon_test

// T8 (codename:agent-input-substrate) — observation-only tmux acceptance proof.
//
// Asserts that the daemon-run review-loop input path delivers the EM-015d
// implementer-resume + reviewer-start instructions via the AIS structured input
// port (handler.InputPort.SubmitInput → Ack), NOT via a direct tmux paste
// (pasteInjecter.WriteLastPane) on this code path. This is the tmux-write-free
// daemon-run input path proof for AIS-011 / AIS-012 + EM-015d-RFD/RIA: when the
// substrate exposes InputPort, the daemon-run delivery routes through it and the
// direct WriteLastPane paste verb is never reached.
//
// The interim tmux driver's SubmitInput still performs the bracketed paste
// internally (PL-021d demoted-not-deleted), but that is encapsulated behind the
// port — the daemon-run delivery code depends on the port, not the write verb.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// t8InputPortSubstrate satisfies handler.Substrate, the daemon's (unexported)
// pasteInjecter / enterSender / paneCapturer interfaces, AND handler.InputPort.
// Because it exposes InputPort, the daemon-run delivery MUST route through
// SubmitInput and MUST NOT call WriteLastPane directly.
type t8InputPortSubstrate struct {
	mu sync.Mutex
	// submitPayloads records every payload delivered via the AIS input port.
	submitPayloads [][]byte
	// writeLastPaneCalls counts DIRECT tmux paste calls — MUST stay 0 on the
	// daemon-run input path once SubmitInput is the delivery seam.
	writeLastPaneCalls int
	// lastPayload is echoed back by CaptureLastPane so the seed-verify loop sees
	// the marker and passes on the first attempt (models a landed paste).
	lastPayload []byte
}

func (s *t8InputPortSubstrate) SpawnWindow(_ context.Context, _ handler.SubstrateSpawn) (handler.SubstrateSession, error) {
	// Not exercised by pasteInjectOnLaunch (it only needs pasteInjecter).
	return nil, nil //nolint:nilnil // stub: SpawnWindow is never called on this path
}

// SubmitInput is the AIS structured input port — the delivery seam the
// daemon-run input path MUST use.
func (s *t8InputPortSubstrate) SubmitInput(_ context.Context, req handler.InputRequest) (handler.Ack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]byte, len(req.Payload))
	copy(cp, req.Payload)
	s.submitPayloads = append(s.submitPayloads, cp)
	s.lastPayload = cp
	return handler.Ack{Outcome: handler.Delivered}, nil
}

func (s *t8InputPortSubstrate) CloseInput(_ context.Context) error { return nil }

// WriteLastPane is the DIRECT tmux paste verb. It MUST NOT be reached on the
// daemon-run input path while SubmitInput is available.
func (s *t8InputPortSubstrate) WriteLastPane(_ context.Context, _ string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeLastPaneCalls++
	s.lastPayload = payload
	return nil
}

func (s *t8InputPortSubstrate) SendEnterToLastPane(_ context.Context) error { return nil }

func (s *t8InputPortSubstrate) CaptureLastPane(_ context.Context, _ int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.lastPayload), nil
}

func (s *t8InputPortSubstrate) snapshot() (submits [][]byte, writes int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.submitPayloads))
	copy(out, s.submitPayloads)
	return out, s.writeLastPaneCalls
}

var (
	_ handler.Substrate = (*t8InputPortSubstrate)(nil)
	_ handler.InputPort = (*t8InputPortSubstrate)(nil)
)

func t8Worktree(t *testing.T) string {
	t.Helper()
	wt := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(wt, ".harmonik"), 0o755); err != nil {
		t.Fatalf("t8Worktree: mkdir .harmonik: %v", err)
	}
	write := func(name, body string) {
		//nolint:gosec // G306: test-only .harmonik fixture file; not production.
		if err := os.WriteFile(filepath.Join(wt, ".harmonik", name), []byte(body), 0o644); err != nil {
			t.Fatalf("t8Worktree: write %s: %v", name, err)
		}
	}
	write("agent-task.md", "T8 proof task\n")
	write("review-target.md", "T8 proof review target\n")
	return wt
}

func t8DrainOrTimeout(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("t8: pasteInjectOnLaunch did not complete within 5s")
	}
}

// TestT8ReviewerStartDeliveredViaSubmitInput asserts the reviewer-start
// instruction (EM-015d-RIA step 3) arrives via SubmitInput with an Ack and the
// direct tmux paste verb is never reached.
func TestT8ReviewerStartDeliveredViaSubmitInput(t *testing.T) {
	sub := &t8InputPortSubstrate{}
	wt := t8Worktree(t)

	ch := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, "t8-session-reviewer",
		handlercontract.ReviewLoopPhaseReviewer, 1, wt,
	)
	t8DrainOrTimeout(t, ch)

	submits, writes := sub.snapshot()
	if writes != 0 {
		t.Errorf("T8: reviewer daemon-run input path called WriteLastPane %d times; want 0 (tmux-write-free — must route via SubmitInput)", writes)
	}
	if len(submits) == 0 {
		t.Fatalf("T8: reviewer instruction was not delivered via SubmitInput (0 submissions)")
	}
	if got := string(submits[0]); !strings.Contains(got, "review-target.md") {
		t.Errorf("T8: reviewer SubmitInput payload = %q; want it to carry the review-target.md read instruction", got)
	}
}

// TestT8ImplementerResumeDeliveredViaSubmitInput asserts the implementer-resume
// read instruction (EM-015d-RFD step 2) arrives via SubmitInput with an Ack and
// the direct tmux paste verb is never reached.
func TestT8ImplementerResumeDeliveredViaSubmitInput(t *testing.T) {
	sub := &t8InputPortSubstrate{}
	wt := t8Worktree(t)

	ch := daemon.ExportedPasteInjectOnLaunch(
		t.Context(), sub, "t8-session-resume",
		handlercontract.ReviewLoopPhaseImplementerResume, 2, wt,
	)
	t8DrainOrTimeout(t, ch)

	submits, writes := sub.snapshot()
	if writes != 0 {
		t.Errorf("T8: implementer-resume daemon-run input path called WriteLastPane %d times; want 0 (tmux-write-free — must route via SubmitInput)", writes)
	}
	if len(submits) == 0 {
		t.Fatalf("T8: implementer-resume instruction was not delivered via SubmitInput (0 submissions)")
	}
	if got := string(submits[0]); !strings.Contains(got, "agent-task.md") {
		t.Errorf("T8: implementer-resume SubmitInput payload = %q; want it to carry the agent-task.md read instruction", got)
	}
}
