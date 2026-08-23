package daemon

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

var _ tmux.Adapter = (*remoteAckFixtureAdapter)(nil)

func newRemoteRunSubstrate(t *testing.T, adapter *remoteAckFixtureAdapter) *perRunSubstrate {
	t.Helper()
	const worker = "worker@gb-mbp"
	ts := &tmuxSubstrate{sessionName: "harmonik-remote"}
	prs := newPerRunSubstrate(ts, "claude", tmux.SSHRunner{Host: worker})
	if prs == nil {
		t.Fatal("T2: newPerRunSubstrate(*tmuxSubstrate, SSHRunner) = nil, want non-nil remote substrate")
	}
	prs.remoteAdapter = adapter
	prs.paneTargetMu.Lock()
	prs.cachedPaneTarget = "%4242"
	prs.paneTargetMu.Unlock()
	return prs
}

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

func TestT2_RemoteSubmitInput_DeliveredNeverSynthesized(t *testing.T) {
	t.Parallel()

	adapter := &remoteAckFixtureAdapter{} // WriteToPane succeeds.
	prs := newRemoteRunSubstrate(t, adapter)

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

	if ack.Outcome != handler.Delivered {
		t.Errorf("T2: Ack.Outcome = %v, want Delivered", ack.Outcome)
	}
	if ack.Seq != 0 {
		t.Errorf("T2: Ack.Seq = %d, want 0 (paste path is codec-blind; no synthesized seq)", ack.Seq)
	}
	if ack.Token != "" {
		t.Errorf("T2: Ack.Token = %q, want empty (SubmitInput must not synthesize an acceptance token)", ack.Token)
	}

	if len(adapter.calls) != 1 {
		t.Fatalf("T2: remote adapter WriteToPane calls = %d, want exactly 1", len(adapter.calls))
	}
	c := adapter.calls[0]
	if c.paneTarget != "%4242" {
		t.Errorf("T2: write paneTarget = %q, want the run's captured pane %%4242", c.paneTarget)
	}
	if want := prs.inputBufferName(); c.bufferName != want {
		t.Errorf("T2: write bufferName = %q, want %q", c.bufferName, want)
	}
	if c.payload != "do the thing" {
		t.Errorf("T2: write payload = %q, want the submitted payload", c.payload)
	}
}

func TestT2_PositiveAcceptance_OnlyAsyncAcked(t *testing.T) {
	t.Parallel()

	adapter := &remoteAckFixtureAdapter{}
	prs := newRemoteRunSubstrate(t, adapter)

	ack, err := prs.SubmitInput(t.Context(), handler.InputRequest{Payload: []byte("payload")})
	if err != nil {
		t.Fatalf("T2: SubmitInput returned error: %v", err)
	}

	if ack.Outcome.String() != "delivered" {
		t.Errorf("T2: Ack.Outcome.String() = %q, want %q (delivery, not acceptance)", ack.Outcome.String(), "delivered")
	}
	if ack.Token != "" {
		t.Errorf("T2: synchronous Ack.Token = %q, want empty — acceptance must not ride the Ack", ack.Token)
	}

	if handler.Delivered == handler.Rejected {
		t.Fatal("T2: Delivered and Rejected collapsed — the binary outcome model is broken")
	}

	acked := core.AgentInputAckedPayload{
		RunID:         "019ec897-0000-7000-8000-000000000042",
		InputSeq:      int64(ack.Seq), //nolint:gosec // G115: test ack.Seq is a small controlled value, no uint64→int64 overflow
		AcceptanceRef: ack.Token,      // empty here — the paste path supplies no turn id.
		AckedAt:       "2026-07-16T00:00:00Z",
	}
	if !acked.Valid() {
		t.Fatal("T2: async agent_input_acked carrier is not well-formed — acceptance channel broken")
	}
	if (core.AgentInputAckedPayload{}).Valid() {
		t.Fatal("T2: empty agent_input_acked reported valid — acceptance identity is not enforced")
	}
}

func TestT2_PartitionedWorker_ReachesStale_NoSilentWedge(t *testing.T) {
	t.Parallel()

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
		if ack != (handler.Ack{}) {
			t.Errorf("T2: SubmitInput returned non-zero Ack %+v on transport failure, want zero Ack", ack)
		}
	})

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

		cancel()

		select {
		case r := <-done:
			if r.err == nil {
				t.Fatal("T2: SubmitInput returned nil error after its bound elapsed — silent wedge")
			}
			if !errors.Is(r.err, context.Canceled) {
				t.Errorf("T2: SubmitInput error = %v, want context.Canceled terminal", r.err)
			}
			if r.ack != (handler.Ack{}) {
				t.Errorf("T2: SubmitInput returned non-zero Ack %+v on a never-landed write, want zero Ack", r.ack)
			}
		case <-time.After(daemonExitHangBudget):
			t.Fatalf("T2: SubmitInput did not return within %s after cancel — AIS-INV-001 violation (silent wedge)", daemonExitHangBudget)
		}
	})
}
