package daemon

// remote_ack_conformance_m4c2_test.go — T2 (M4-C2) Ack-on-remote conformance.
//
// Gate-runnable unit tests on the perRunSubstrate.SubmitInput seam
// (tmuxsubstrate.go:2245-2258), the interim tmux/paste implementation of
// handler.InputPort for the REMOTE Claude path. No real tmux, SSH, or git; the
// remote adapter is a fake and the "SSH runner failure" is a synthesized
// exit-255 *exec.ExitError (what tmux.IsSSHConnectionFailure classifies as a
// transport disconnect).
//
// The three assertions the M4 merges gate on (07-tasks.md T2 / 03-components.md
// M4-C2, specs/agent-input.md §4 AIS-003/AIS-INV-001, specs/handler-contract.md
// §5 HC-INV-008):
//
//   Assertion 1 — TestT2_RemoteSubmitInput_DeliveredNeverSynthesized:
//     Remote Claude SubmitInput returns Ack{Outcome: Delivered} on a successful
//     pane write and NEVER a synthesized positive acceptance (Delivered is
//     delivery, not acceptance; Seq/Token stay zero — the paste path fabricates
//     no acceptance token). The write rides the run's REMOTE adapter.
//
//   Assertion 2 — TestT2_PositiveAcceptance_OnlyAsyncAcked:
//     Positive acceptance is decoupled from the synchronous Ack and travels ONLY
//     as the async core.AgentInputAckedPayload (agent_input_acked) — over the
//     reverse tunnel on the remote path — never fabricated by SubmitInput. The
//     Ack carries no acceptance the seam did not observe.
//
//   Assertion 3 — TestT2_PartitionedWorker_ReachesStale_NoSilentWedge:
//     A dropped / partitioned worker (SSH runner failure / disconnect) drives the
//     submission to a terminal (a returned error — the tmux-path front-stop that
//     upstream turns into the agent_input_stale-class reason) within the
//     AIS-INV-001 bound — SubmitInput NEVER returns silently in flight and NEVER
//     wedges. Deterministic: no real sleep — the transport error is returned
//     immediately, and the bounded-liveness case is driven by context
//     cancellation, not a wall-clock timeout.
//
// Task: T2 (M4-C2), codename:remote-substrate.

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fake remote adapter — records WriteToPane and injects controlled outcomes.
// ─────────────────────────────────────────────────────────────────────────────

// remoteAckFixtureAdapter is a minimal fake [tmux.Adapter] standing in for the
// SSH-backed adapter a remote run caches in perRunSubstrate.remoteAdapter. Only
// WriteToPane carries behaviour; every other method is an inert stub.
type remoteAckFixtureAdapter struct {
	// writeErr, when non-nil, is returned by WriteToPane (models a transport
	// failure surfaced through the adapter).
	writeErr error
	// blockUntilCtx, when true, makes WriteToPane block until ctx is done and
	// return ctx.Err() — models a partitioned worker whose write never lands and
	// whose only terminal is the context deadline/cancel (AIS-INV-001 bound).
	blockUntilCtx bool

	// calls records each WriteToPane invocation for assertion.
	calls []remoteAckWrite
}

type remoteAckWrite struct {
	bufferName string
	paneTarget string
	payload    string
}

func (a *remoteAckFixtureAdapter) WriteToPane(ctx context.Context, bufferName, paneTarget string, payload []byte) error {
	if a.blockUntilCtx {
		<-ctx.Done()
		return ctx.Err()
	}
	a.calls = append(a.calls, remoteAckWrite{
		bufferName: bufferName,
		paneTarget: paneTarget,
		payload:    string(payload),
	})
	return a.writeErr
}

// Inert stubs — SubmitInput touches none of these.
func (a *remoteAckFixtureAdapter) ProbeTmux(context.Context) error                { return nil }
func (a *remoteAckFixtureAdapter) ListSessions(context.Context) ([]string, error) { return nil, nil }
func (a *remoteAckFixtureAdapter) ListWindows(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *remoteAckFixtureAdapter) NewWindowIn(context.Context, tmux.NewWindowIn) tmux.Outcome {
	return tmux.Outcome{}
}
func (a *remoteAckFixtureAdapter) KillWindow(context.Context, tmux.WindowHandle) error { return nil }
func (a *remoteAckFixtureAdapter) WindowPanePID(context.Context, tmux.WindowHandle) (int, error) {
	return 0, nil
}

func (a *remoteAckFixtureAdapter) WindowPaneID(context.Context, tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *remoteAckFixtureAdapter) KillSession(context.Context, string) error         { return nil }
func (a *remoteAckFixtureAdapter) LoadBuffer(context.Context, string, []byte) error  { return nil }
func (a *remoteAckFixtureAdapter) PasteBuffer(context.Context, string, string) error { return nil }
func (a *remoteAckFixtureAdapter) SendKeysLiteral(context.Context, string, string) error {
	return nil
}
func (a *remoteAckFixtureAdapter) SendKeysEnter(context.Context, string) error { return nil }
func (a *remoteAckFixtureAdapter) SendKeysQuit(context.Context, string) error  { return nil }

// Compile-time assertion: remoteAckFixtureAdapter satisfies tmux.Adapter.
var _ tmux.Adapter = (*remoteAckFixtureAdapter)(nil)

// newRemoteRunSubstrate builds a perRunSubstrate wired for the REMOTE path: a
// genuine SSHRunner (so commandRunner() is remote), a cached remote adapter, and
// a pre-captured pane target so SubmitInput → WriteLastPane → pasteAdapter()
// routes the write through the remote adapter exactly as production does.
func newRemoteRunSubstrate(t *testing.T, adapter *remoteAckFixtureAdapter) *perRunSubstrate {
	t.Helper()
	const worker = "worker@gb-mbp"
	ts := &tmuxSubstrate{sessionName: "harmonik-remote"}
	prs := newPerRunSubstrate(ts, "claude", tmux.SSHRunner{Host: worker})
	if prs == nil {
		t.Fatal("T2: newPerRunSubstrate(*tmuxSubstrate, SSHRunner) = nil, want non-nil remote substrate")
	}
	// Cache the remote adapter (production sets this in spawnWindowRemote) so
	// pasteAdapter() routes to the worker's tmux server.
	prs.remoteAdapter = adapter
	// Pre-capture the pane target this run's SpawnWindow would have set.
	prs.paneTargetMu.Lock()
	prs.cachedPaneTarget = "%4242"
	prs.paneTargetMu.Unlock()
	return prs
}

// sshDisconnectErr returns a *exec.ExitError with code 255 — the exact shape an
// `ssh` transport failure produces and that tmux.IsSSHConnectionFailure
// classifies as a connection failure. Real subprocess, deterministic, no sleep.
func sshDisconnectErr(t *testing.T) error {
	t.Helper()
	err := exec.CommandContext(t.Context(), "sh", "-c", "exit 255").Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 255 {
		t.Fatalf("T2: could not synthesize exit-255 ExitError: %v", err)
	}
	if !tmux.IsSSHConnectionFailure(err) {
		t.Fatal("T2: synthesized error not classified as SSH connection failure — precondition broken")
	}
	return err
}

// ─────────────────────────────────────────────────────────────────────────────
// Assertion 1 — Delivered on success, never a synthesized positive acceptance.
// ─────────────────────────────────────────────────────────────────────────────

func TestT2_RemoteSubmitInput_DeliveredNeverSynthesized(t *testing.T) {
	t.Parallel()

	adapter := &remoteAckFixtureAdapter{} // WriteToPane succeeds.
	prs := newRemoteRunSubstrate(t, adapter)

	// Precondition: this really is the remote path (commandRunner is SSHRunner,
	// pasteAdapter is the cached remote adapter).
	if _, ok := prs.commandRunner().(tmux.SSHRunner); !ok {
		t.Fatalf("T2: commandRunner() = %T, want tmux.SSHRunner (remote path)", prs.commandRunner())
	}
	if prs.pasteAdapter() != tmux.Adapter(adapter) {
		t.Fatal("T2: pasteAdapter() did not route to the cached remote adapter")
	}

	ack, err := prs.SubmitInput(t.Context(), handler.InputRequest{Payload: []byte("do the thing")})
	if err != nil {
		t.Fatalf("T2: SubmitInput on healthy remote worker returned error: %v", err)
	}

	// Delivered — delivery, not acceptance.
	if ack.Outcome != handler.Delivered {
		t.Errorf("T2: Ack.Outcome = %v, want Delivered", ack.Outcome)
	}
	// NEVER a synthesized positive acceptance: the interim paste path supplies no
	// wire protocol, so Seq and the acceptance Token stay zero. A non-zero Token
	// here would be a fabricated acceptance the seam never observed.
	if ack.Seq != 0 {
		t.Errorf("T2: Ack.Seq = %d, want 0 (paste path is codec-blind; no synthesized seq)", ack.Seq)
	}
	if ack.Token != "" {
		t.Errorf("T2: Ack.Token = %q, want empty (SubmitInput must not synthesize an acceptance token)", ack.Token)
	}

	// The write rode the remote adapter, targeting this run's pane via the
	// input buffer.
	if len(adapter.calls) != 1 {
		t.Fatalf("T2: remote adapter WriteToPane calls = %d, want exactly 1", len(adapter.calls))
	}
	c := adapter.calls[0]
	if c.paneTarget != "%4242" {
		t.Errorf("T2: write paneTarget = %q, want the run's captured pane %%4242", c.paneTarget)
	}
	if c.bufferName != inputBufferName {
		t.Errorf("T2: write bufferName = %q, want %q", c.bufferName, inputBufferName)
	}
	if c.payload != "do the thing" {
		t.Errorf("T2: write payload = %q, want the submitted payload", c.payload)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Assertion 2 — positive acceptance arrives ONLY as the async agent_input_acked.
// ─────────────────────────────────────────────────────────────────────────────

func TestT2_PositiveAcceptance_OnlyAsyncAcked(t *testing.T) {
	t.Parallel()

	adapter := &remoteAckFixtureAdapter{}
	prs := newRemoteRunSubstrate(t, adapter)

	ack, err := prs.SubmitInput(t.Context(), handler.InputRequest{Payload: []byte("payload")})
	if err != nil {
		t.Fatalf("T2: SubmitInput returned error: %v", err)
	}

	// The synchronous return is delivery only — "delivered", never "accepted".
	if ack.Outcome.String() != "delivered" {
		t.Errorf("T2: Ack.Outcome.String() = %q, want %q (delivery, not acceptance)", ack.Outcome.String(), "delivered")
	}
	// The Ack carries no acceptance the seam observed: the acceptance token is the
	// wire form of a protocol turn id, which the paste path never receives.
	if ack.Token != "" {
		t.Errorf("T2: synchronous Ack.Token = %q, want empty — acceptance must not ride the Ack", ack.Token)
	}

	// Positive acceptance is a DISTINCT async event, not a DeliveryOutcome value.
	// Delivered and Rejected are the only outcomes; neither means "accepted".
	if handler.Delivered == handler.Rejected {
		t.Fatal("T2: Delivered and Rejected collapsed — the binary outcome model is broken")
	}

	// The sole positive-acceptance carrier is core.AgentInputAckedPayload
	// (agent_input_acked), delivered asynchronously — on the remote path over the
	// reverse tunnel from the worker's Claude-hook-bridge (outcome_emitted /
	// agent_ready). Its existence IS the ack, and it carries a run_id + acked_at
	// the synchronous Ack never supplies.
	acked := core.AgentInputAckedPayload{
		RunID:         "019ec897-0000-7000-8000-000000000042",
		InputSeq:      int64(ack.Seq), //nolint:gosec // G115: test ack.Seq is a small controlled value, no uint64→int64 overflow
		AcceptanceRef: ack.Token,      // empty here — the paste path supplies no turn id.
		AckedAt:       "2026-07-16T00:00:00Z",
	}
	if !acked.Valid() {
		t.Fatal("T2: async agent_input_acked carrier is not well-formed — acceptance channel broken")
	}
	// The event carrier requires a run_id; the synchronous Ack has no such field,
	// so acceptance provably cannot be fabricated inside SubmitInput's return.
	if (core.AgentInputAckedPayload{}).Valid() {
		t.Fatal("T2: empty agent_input_acked reported valid — acceptance identity is not enforced")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Assertion 3 — partitioned worker reaches a terminal, never a silent wedge.
// ─────────────────────────────────────────────────────────────────────────────

func TestT2_PartitionedWorker_ReachesStale_NoSilentWedge(t *testing.T) {
	t.Parallel()

	// Case A — SSH transport failure surfaced immediately: SubmitInput returns
	// the error terminal (NOT a Delivered Ack), classified as an SSH disconnect
	// which upstream maps to the agent_input_stale-class reason.
	t.Run("ssh_disconnect_returns_error_terminal", func(t *testing.T) {
		t.Parallel()
		adapter := &remoteAckFixtureAdapter{writeErr: sshDisconnectErr(t)}
		prs := newRemoteRunSubstrate(t, adapter)

		ack, err := prs.SubmitInput(t.Context(), handler.InputRequest{Payload: []byte("payload")})
		if err == nil {
			t.Fatal("T2: SubmitInput on a partitioned worker returned nil error — silent wedge / lost submission")
		}
		if !tmux.IsSSHConnectionFailure(err) {
			t.Errorf("T2: SubmitInput error = %v, want an SSH-connection-failure terminal (feeds agent_input_stale)", err)
		}
		// The error IS the terminal; the Ack is meaningless on this path and MUST
		// be the zero value (the seam returns handler.Ack{} — it fabricates no
		// delivery it never achieved). No acceptance seq/token leaks out.
		if ack != (handler.Ack{}) {
			t.Errorf("T2: SubmitInput returned non-zero Ack %+v on transport failure, want zero Ack", ack)
		}
	})

	// Case B — bounded liveness (AIS-INV-001): a worker whose write never lands
	// must NOT wedge SubmitInput forever. When the bounding context is torn down
	// (deadline/cancel — the daemon's submission bound), SubmitInput returns the
	// context terminal PROMPTLY. Deterministic: the return is driven by
	// cancellation, not a real-time sleep; the 5s guard only trips on a genuine
	// hang (a real AIS-INV-001 violation).
	t.Run("partition_bounded_by_context_no_hang", func(t *testing.T) {
		t.Parallel()
		adapter := &remoteAckFixtureAdapter{blockUntilCtx: true}
		prs := newRemoteRunSubstrate(t, adapter)

		ctx, cancel := context.WithCancel(t.Context())
		type result struct {
			ack handler.Ack
			err error
		}
		done := make(chan result, 1)
		go func() {
			ack, err := prs.SubmitInput(ctx, handler.InputRequest{Payload: []byte("payload")})
			done <- result{ack, err}
		}()

		// Tear down the transport bound (models the daemon's AIS-INV-001 cutoff).
		cancel()

		select {
		case r := <-done:
			if r.err == nil {
				t.Fatal("T2: SubmitInput returned nil error after its bound elapsed — silent wedge")
			}
			if !errors.Is(r.err, context.Canceled) {
				t.Errorf("T2: SubmitInput error = %v, want context.Canceled terminal", r.err)
			}
			// The error is the terminal; the Ack is the zero value (no fabricated
			// delivery for a write that never landed).
			if r.ack != (handler.Ack{}) {
				t.Errorf("T2: SubmitInput returned non-zero Ack %+v on a never-landed write, want zero Ack", r.ack)
			}
		case <-time.After(5 * time.Second):
			// Hang detector, not a timing assertion — the happy path resolves on
			// cancellation, so this never contributes to flakiness.
			t.Fatal("T2: SubmitInput did not return within the guard window after cancel — AIS-INV-001 violation (silent wedge)")
		}
	})
}
