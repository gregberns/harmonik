package codexdriver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/apptap"
	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/codexwire"
	"github.com/gregberns/harmonik/internal/handler"
)

// scannerBufCap mirrors the RS-010 1 MiB line bound for the stdout scanner.
const scannerBufCap = 1 << 20

// stderrTailCap bounds the captured stderr tail (parity with handler's ring).
const stderrTailCap = 4 * 1024

// evChanCap buffers the internal event channel so producers (scanner, timers,
// SubmitInput/CloseInput callers, and the effector's own fault events) never
// block the loop goroutine against itself.
const evChanCap = 128

// ErrInputStale is wrapped by SubmitInput's stale terminal (AIS-INV-001): the
// submission reached the agent_input_stale terminal instead of an ack.
var ErrInputStale = errors.New("codexdriver: input stale")

// ErrSessionClosed is returned by SubmitInput/CloseInput once the driver loop
// has wound down (child exited or wire closed).
var ErrSessionClosed = errors.New("codexdriver: session closed")

// pendingKind classifies an in-flight JSON-RPC client request so the response
// can be correlated by id.
type pendingKind int

const (
	pendingInitialize pendingKind = iota
	pendingThreadStart
	pendingTurnStart
	pendingInterrupt
)

type pendingReq struct {
	kind pendingKind
	seq  uint64 // input seq for pendingTurnStart
}

type submitResult struct {
	ack handler.Ack
	err error
}

// codexSession is the driver's session: handler.SubstrateSession +
// handler.InputPort over one codex app-server child.
type codexSession struct {
	opts       Options
	cmd        *exec.Cmd
	procCancel context.CancelFunc

	stdinPipe io.WriteCloser // raw pipe (for Close)
	stdinW    io.Writer      // tee'd write path
	stdout    io.Reader      // tee'd read path
	stderr    *ringWriter

	evCh     chan codexinput.Event
	wireDone chan struct{} // closed by the scanner after EOF/read error
	loopDone chan struct{} // closed when the reactor loop has exited
	failCh   chan struct{} // closed on launch failure / wind-down before ready
	waitDone chan struct{} // closed when cmd.Wait has returned

	failOnce sync.Once

	// phaseMu guards the reactor-phase mirror the front-stop reads. The reactor
	// itself is single-goroutine-owned (runLoop); runLoop publishes its phase
	// after every Step so SubmitInput can gate on the reactor being genuinely
	// Ready (idle) without touching the reactor concurrently.
	phaseMu      sync.Mutex
	phase        codexinput.DriverState // reactor phase mirror (Spawning at rest)
	phaseChanged chan struct{}          // closed+replaced on each phase publish

	// submitMu serializes SubmitInput callers: exactly one uncorrelated input
	// in flight per session (the reactor's AwaitingAck invariant).
	submitMu sync.Mutex

	mu    sync.Mutex
	seq   uint64
	reqID int64
	// pending is keyed by the JSON-RPC request id's verbatim string form (the id
	// is a json.RawMessage on the wire per H11 — a number here, but a string or
	// other shape round-trips too). We mint integer ids but correlate responses
	// by string(f.ID) so a server that echoes the id in any valid JSON form still
	// matches.
	pending  map[string]pendingReq
	payloads map[uint64][]byte
	waiters  map[uint64]chan submitResult
	timers   map[codexinput.TimerKind]context.CancelFunc
	// turnSeqByID binds a codex turn id → the input seq that opened it. The
	// binding is created from the turn/start RESPONSE (correlated to its seq by
	// the JSON-RPC request id, s.pending), which the server sends carrying the
	// created turn's id BEFORE the turn/started notification (corpus
	// raw-session-01 lines 11→13). The turn/started notification then correlates
	// to a seq by its OWN turn id — never by a mutable in-flight pointer — so a
	// genuinely-late turn/started for an abandoned turn cannot bind to a fresh
	// submission (AIS-INV-001 no-mis-ack). Entries are pruned when the seq
	// resolves or the turn completes.
	turnSeqByID map[string]uint64
	threadID    string
	failure     error
	stdinClosed bool

	killOnce sync.Once

	outcomeMu sync.Mutex
	outcome   handler.Outcome
	started   time.Time
}

// Compile-time seam assertions.
var (
	_ handler.SubstrateSession = (*codexSession)(nil)
	_ handler.InputPort        = (*codexSession)(nil)
)

func newCodexSession(opts Options, cmd *exec.Cmd, procCancel context.CancelFunc, stdin io.WriteCloser, stdout io.Reader, stderr *ringWriter) *codexSession {
	return &codexSession{
		opts:       opts,
		cmd:        cmd,
		procCancel: procCancel,
		stdinPipe:  stdin,
		// Best-effort capture tee (AIS-013 / AIS-INV-002): a capture-disk fault
		// degrades to uncaptured and MUST NOT abort or back-pressure the live
		// agent wire. onErr logs once and drops capture for the rest of the run.
		stdinW:       apptap.BestEffortCaptureWriter(stdin, opts.InCapture, captureDegradeLogger("input")),
		stdout:       apptap.BestEffortCaptureReader(stdout, opts.OutCapture, captureDegradeLogger("output")),
		stderr:       stderr,
		evCh:         make(chan codexinput.Event, evChanCap),
		wireDone:     make(chan struct{}),
		loopDone:     make(chan struct{}),
		failCh:       make(chan struct{}),
		waitDone:     make(chan struct{}),
		phaseChanged: make(chan struct{}),
		pending:      make(map[string]pendingReq),
		payloads:     make(map[uint64][]byte),
		waiters:      make(map[uint64]chan submitResult),
		timers:       make(map[codexinput.TimerKind]context.CancelFunc),
		turnSeqByID:  make(map[string]uint64),
		started:      opts.Clock.Now(),
	}
}

// captureDegradeLogger returns the apptap best-effort onErr hook for one
// capture direction: it logs the degrade-to-uncaptured event exactly once (the
// tee fires it once per stream) so an operator can see capture was lost without
// the live run ever noticing (AIS-INV-002).
func captureDegradeLogger(dir string) func(error) {
	return func(err error) {
		slog.WarnContext(context.Background(), "codexdriver_capture_degraded",
			"direction", dir,
			"error", err.Error(),
			"note", "capture dropped; live agent stream unaffected (AIS-INV-002)",
		)
	}
}

// start launches the three session goroutines: the child reaper, the stdout
// scanner (EventSource producer), and the reactor loop. It then injects the
// Spawned event that kicks the handshake.
func (s *codexSession) start(_ context.Context) {
	go s.reapLoop()
	go s.readLoop()
	go s.runLoop() //nolint:contextcheck // the loop is session-lifetime-owned (ends on wire close), deliberately not spawn-ctx-scoped
	s.sendEvent(codexinput.Event{Type: codexinput.EventTypeSpawned})
}

// ─── SubstrateSession ─────────────────────────────────────────────────────────

// Kill terminates the child. Idempotent: the ctx cancel kills the
// exec.CommandContext-spawned process; the reaper observes the exit.
func (s *codexSession) Kill(_ context.Context) error {
	s.killOnce.Do(s.procCancel)
	return nil
}

// Wait blocks until the child has exited and been reaped.
func (s *codexSession) Wait(ctx context.Context) error {
	select {
	case <-s.waitDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Outcome returns exit metadata once Wait has returned (zero value before).
func (s *codexSession) Outcome() handler.Outcome {
	s.outcomeMu.Lock()
	defer s.outcomeMu.Unlock()
	return s.outcome
}

// PID returns the child's PID, or 0 when unknown.
func (s *codexSession) PID() int {
	if s.cmd.Process == nil {
		return 0
	}
	return s.cmd.Process.Pid
}

// Stdout returns nil: the structured wire is consumed by the driver itself;
// observation happens through the capture tee (AIS-010), not a raw pipe.
func (s *codexSession) Stdout() io.Reader { return nil }

// ─── InputPort ────────────────────────────────────────────────────────────────

// SubmitInput delivers one submission and blocks to exactly ONE terminal
// (AIS-INV-001 / HC-INV-008): the async agent_input_acked resolves
// Ack{Delivered, Seq, Token=turn id}; a protocol-level refusal resolves
// Ack{Rejected}; the agent_input_stale terminal resolves as an error wrapping
// ErrInputStale. Never silence. Callers serialize (one uncorrelated input in
// flight); ctx cancellation abandons the park without leaking the waiter.
func (s *codexSession) SubmitInput(ctx context.Context, req handler.InputRequest) (handler.Ack, error) {
	s.submitMu.Lock()
	defer s.submitMu.Unlock()

	// Front-stop: block (bounded) until the reactor is genuinely Ready (idle) —
	// NOT merely handshake-done. Gating on the reactor phase, not just channels,
	// is load-bearing (AIS-INV-001): if a PRIOR caller cancelled mid-flight, its
	// turn is still live on the child and the reactor is still AwaitingAck/InTurn;
	// a fresh submit into that state would be SILENTLY DROPPED by the reactor
	// (stepInputSubmitted accepts only from Ready/InTurn). Correlation-wise a
	// late turn/started for the abandoned turn is separately fenced by turn-id
	// binding (handleNotification), but waiting for the abandoned turn's REAL
	// terminal (its
	// turn/completed, or the InputAck timer → stale) before proceeding closes
	// both holes. The wait is bounded by that terminal, never a hang.
	if err := s.awaitReady(ctx); err != nil {
		return handler.Ack{}, err
	}

	s.mu.Lock()
	if s.stdinClosed {
		s.mu.Unlock()
		return handler.Ack{}, fmt.Errorf("codexdriver: submit after CloseInput: %w", ErrSessionClosed)
	}
	s.seq++
	seq := s.seq
	s.payloads[seq] = req.Payload
	ch := make(chan submitResult, 1)
	s.waiters[seq] = ch
	s.mu.Unlock()

	s.sendEvent(codexinput.Event{Type: codexinput.EventTypeInputSubmitted, InputSeq: seq})

	select {
	case res := <-ch:
		return res.ack, res.err
	case <-s.loopDone:
		// Backstop: finalize() resolves waiters before closing loopDone, so
		// prefer the waiter result if it raced in.
		select {
		case res := <-ch:
			return res.ack, res.err
		default:
			return handler.Ack{}, ErrSessionClosed
		}
	case <-s.failCh:
		// Reactor reached a terminal (launch failure / wind-down) while the
		// wire is still open: resolve rather than park forever (AIS-INV-001).
		select {
		case res := <-ch:
			return res.ack, res.err
		default:
			s.dropWaiter(seq)
			return handler.Ack{}, fmt.Errorf("codexdriver: submit aborted: %w", s.failureErr())
		}
	case <-ctx.Done():
		// Abandon the park: drop THIS caller's waiter ONLY. We deliberately do
		// NOT touch the reactor — the abandoned turn is still live on the child,
		// so it MUST resolve through its own REAL terminal (turn/started → InTurn
		// → turn/completed → Ready, or the InputAck timer → stale → Ready). The
		// abandoned turn's turn-id binding still correlates its (possibly late)
		// turn/started to THIS seq, never to a subsequent caller's turn; the
		// reactor's PendingSeq guard drops that ack since this seq is no longer
		// pending. The reactor's later agent_input_acked /
		// agent_input_stale emit for this seq becomes a no-op resolve (the waiter
		// is gone) — verified non-panicking in resolveWaiter. The next caller
		// blocks in awaitReady until that real terminal returns the reactor to
		// Ready (bounded wait), so it never slips in against a live turn.
		s.dropWaiter(seq)
		return handler.Ack{}, ctx.Err()
	}
}

// awaitReady blocks until the reactor phase is Ready (idle and able to accept a
// fresh, correctly-correlated submission), or a terminal channel fires. It is
// the phase-gated front-stop: waiting for Ready — not just handshake-done —
// prevents both the silent-drop and the mis-ack that a submit into a non-idle
// reactor would cause (AIS-INV-001). The wait is bounded by the in-flight turn's
// real terminal or by ctx.
func (s *codexSession) awaitReady(ctx context.Context) error {
	for {
		s.phaseMu.Lock()
		p := s.phase
		changed := s.phaseChanged
		s.phaseMu.Unlock()

		switch p {
		case codexinput.Ready:
			return nil
		case codexinput.Draining, codexinput.Exited:
			return fmt.Errorf("codexdriver: submit refused: %w", ErrSessionClosed)
		case codexinput.Spawning, codexinput.Handshaking, codexinput.AwaitingAck, codexinput.InTurn:
			// Not yet idle: fall through and wait for the next phase change.
		}

		select {
		case <-changed:
			// phase advanced; re-read.
		case <-s.failCh:
			return fmt.Errorf("codexdriver: submit refused: %w", s.failureErr())
		case <-s.loopDone:
			return ErrSessionClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// publishPhase mirrors the reactor's current phase for awaitReady and wakes any
// waiters. Called from runLoop only (single writer), after every Step.
func (s *codexSession) publishPhase(p codexinput.DriverState) {
	s.phaseMu.Lock()
	s.phase = p
	close(s.phaseChanged)
	s.phaseChanged = make(chan struct{})
	s.phaseMu.Unlock()
}

// CloseInput signals end-of-input. The reactor gracefully interrupts an open
// turn (turn/interrupt, AIS-017) before the stdin close, and resolves any
// pending submission to its stale terminal — never silence.
func (s *codexSession) CloseInput(_ context.Context) error {
	s.mu.Lock()
	already := s.stdinClosed
	s.stdinClosed = true
	s.mu.Unlock()
	if already {
		return nil
	}
	s.sendEvent(codexinput.Event{Type: codexinput.EventTypeCloseRequested})
	return nil
}

// ─── Goroutines ───────────────────────────────────────────────────────────────

// reapLoop waits for the child and records the Outcome.
func (s *codexSession) reapLoop() {
	err := s.cmd.Wait()
	out := handler.Outcome{
		Duration:   s.opts.Clock.Since(s.started),
		Signal:     syscall.Signal(-1),
		StderrTail: s.stderr.Bytes(),
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		out.ExitCode = 0
	case errors.As(err, &exitErr):
		out.ExitCode = exitErr.ExitCode()
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			out.Signal = ws.Signal()
		}
	default:
		out.ExitCode = -1
	}
	s.outcomeMu.Lock()
	s.outcome = out
	s.outcomeMu.Unlock()
	s.procCancel() // release the exec ctx either way
	close(s.waitDone)
}

// readLoop is the stdout scanner: the wire-side Event producer. One NDJSON
// line → codexwire.Parse → frame handling → typed Events. On EOF/read error
// it injects the codec disconnect terminal (RS-009) and closes wireDone,
// which drains and ends the reactor loop.
func (s *codexSession) readLoop() {
	sc := bufio.NewScanner(s.stdout)
	sc.Buffer(make([]byte, 0, 64*1024), scannerBufCap)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		frame, err := codexwire.Parse(line)
		if err != nil {
			// Fatal decode: the codec's ErrorEvent terminal (RS-009).
			s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: err.Error()})
			continue // reactor is terminal; keep draining bytes harmlessly.
		}
		s.handleFrame(frame)
	}
	reason := "disconnected"
	if err := sc.Err(); err != nil {
		reason = err.Error()
	}
	s.sendEvent(codexinput.Event{Type: codexinput.EventTypeDisconnected, Reason: reason})
	close(s.wireDone)
}

// runLoop drives the pure reactor, publishing the reactor phase after every
// Step so the phase-gated front-stop (awaitReady) can observe the reactor
// returning to Ready — including the silent InTurn→Ready transition on
// turn/completed, which produces no Action. This is a deliberate minimal
// in-line of substrate.Run (same range-Step-Execute shape) rather than r.Run:
// the generic loop exposes no per-event state hook, and the AIS-INV-001
// front-stop MUST see every phase change, not only the ones that emit an
// Action. Step stays pure; only the phase read is added.
//
// On loop end it finalizes: every parked caller resolves, every timer stops
// (AIS-INV-001 — wind-down is a terminal, not silence).
func (s *codexSession) runLoop() {
	ctx := context.Background()
	r := codexinput.New(s.opts.Config)
	src := (*sessionSource)(s)
	eff := (*sessionEffector)(s)
	for ev := range src.Events(ctx) {
		for _, a := range r.Step(ev) {
			// Execute never returns an error today (faults funnel back as
			// Events); guard anyway so a future fault can't pass silently.
			if err := eff.Execute(ctx, a); err != nil {
				s.setFailure("effector: " + err.Error())
			}
		}
		s.publishPhase(r.State().Phase)
	}
	s.finalize()
	close(s.loopDone)
}

// finalize resolves every remaining waiter to the stale terminal, fails the
// readiness gate if the handshake never completed, and cancels timers.
func (s *codexSession) finalize() {
	s.mu.Lock()
	waiters := s.waiters
	s.waiters = make(map[uint64]chan submitResult)
	timers := s.timers
	s.timers = make(map[codexinput.TimerKind]context.CancelFunc)
	s.mu.Unlock()
	for seq, ch := range waiters {
		ch <- submitResult{err: fmt.Errorf("%w (seq %d): session wind-down", ErrInputStale, seq)}
	}
	for _, cancel := range timers {
		cancel()
	}
	s.setFailure("session wind-down before handshake")
}

// ─── EventSource ─────────────────────────────────────────────────────────────

// sessionSource adapts the session's internal event channel to the
// codexinput.EventSource seam. The forwarder ends (closing its output, which
// ends substrate.Run) when the wire has closed and the buffer is drained.
type sessionSource codexSession

// Events returns the reactor's event channel (codexinput.EventSource).
func (ss *sessionSource) Events(ctx context.Context) <-chan codexinput.Event {
	s := (*codexSession)(ss)
	out := make(chan codexinput.Event)
	forward := func(ev codexinput.Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	// drainBuffered forwards whatever is already buffered, then reports done.
	drainBuffered := func() {
		for {
			select {
			case ev := <-s.evCh:
				if !forward(ev) {
					return
				}
			default:
				return
			}
		}
	}
	go func() {
		defer close(out)
		for {
			select {
			case ev := <-s.evCh:
				if !forward(ev) {
					return
				}
			case <-s.wireDone:
				drainBuffered()
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// sendEvent delivers an event to the reactor loop without wedging a producer
// after wind-down (the loopDone guard). The channel buffer keeps the loop
// goroutine's own effector-side sends from self-blocking.
func (s *codexSession) sendEvent(ev codexinput.Event) {
	select {
	case s.evCh <- ev:
	case <-s.loopDone:
	}
}

// ─── Effector ────────────────────────────────────────────────────────────────

// sessionEffector maps reactor Actions to IO (codexinput.Effector). Faults are
// funneled back in as Events (never as Execute errors) so the loop always
// processes the wire's own terminal.
type sessionEffector codexSession

// Execute performs one reactor Action.
func (se *sessionEffector) Execute(_ context.Context, a codexinput.Action) error {
	s := (*codexSession)(se)
	switch a.Type {
	case codexinput.ActionTypeSendHandshake:
		s.writeInitialize()
	case codexinput.ActionTypeWriteInput:
		s.writeTurnStart(a.InputSeq)
	case codexinput.ActionTypeCloseInput:
		_ = s.stdinPipe.Close()
	case codexinput.ActionTypeInterrupt:
		s.writeInterrupt(a.TurnID)
	case codexinput.ActionTypeArmTimer:
		s.armTimer(a.Kind, a.Duration) //nolint:contextcheck // timer lifetime is reactor-owned (cancel_timer), deliberately not caller-ctx-scoped
	case codexinput.ActionTypeCancelTimer:
		s.cancelTimer(a.Kind)
	case codexinput.ActionTypeEmit:
		s.handleEmit(a)
	}
	return nil
}

// handleEmit resolves the parked SubmitInput caller (the sync terminal) and
// forwards the durable emission to the composition root's hook.
func (s *codexSession) handleEmit(a codexinput.Action) {
	switch a.Emit {
	case codexinput.EmitInputAcked:
		s.resolveWaiter(a.InputSeq, submitResult{ack: handler.Ack{
			Outcome: handler.Delivered,
			Seq:     a.InputSeq,
			Token:   a.TurnID,
		}})
	case codexinput.EmitInputStale:
		s.resolveWaiter(a.InputSeq, submitResult{err: fmt.Errorf("%w (seq %d): %s", ErrInputStale, a.InputSeq, a.Reason)})
	case codexinput.EmitLaunchFailure:
		s.setFailure(a.Reason)
	case codexinput.EmitInputSubmitted:
		// Front-stop entry marker only; the caller stays parked.
	}
	if s.opts.Emit != nil {
		s.opts.Emit(Emission{Type: a.Emit, InputSeq: a.InputSeq, TurnID: a.TurnID, Reason: a.Reason})
	}
}

// ─── Timers (ClockPort-only; RS-015) ─────────────────────────────────────────

func (s *codexSession) armTimer(kind codexinput.TimerKind, d time.Duration) {
	tctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if prev, ok := s.timers[kind]; ok {
		prev()
	}
	s.timers[kind] = cancel
	s.mu.Unlock()
	go func() {
		if s.opts.Clock.Sleep(tctx, d) {
			s.sendEvent(codexinput.Event{Type: codexinput.EventTypeTimerFired, Kind: kind})
		}
	}()
}

func (s *codexSession) cancelTimer(kind codexinput.TimerKind) {
	s.mu.Lock()
	cancel, ok := s.timers[kind]
	if ok {
		delete(s.timers, kind)
	}
	s.mu.Unlock()
	if ok {
		cancel()
	}
}

// ─── Outbound frames (codexwire.Marshal + stdin write) ──────────────────────

// nextReqID mints the next integer request id, records its pending correlation
// keyed by the id's verbatim JSON string form, and returns the id as the
// json.RawMessage the wire frame carries (H11: Frame.ID is raw so string/number
// ids round-trip verbatim).
func (s *codexSession) nextReqID(kind pendingKind, seq uint64) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqID++
	raw := json.RawMessage(strconv.FormatInt(s.reqID, 10))
	s.pending[string(raw)] = pendingReq{kind: kind, seq: seq}
	return raw
}

// writeFrame marshals and writes one NDJSON line. Write failures are funneled
// back as the codec error terminal so a pending submission still reaches its
// stale terminal (AIS-INV-001).
func (s *codexSession) writeFrame(f codexwire.Frame) {
	b, err := codexwire.Marshal(f)
	if err != nil {
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: "marshal: " + err.Error()})
		return
	}
	if _, err := s.stdinW.Write(append(b, '\n')); err != nil {
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: "stdin write: " + err.Error()})
	}
}

func (s *codexSession) writeInitialize() {
	id := s.nextReqID(pendingInitialize, 0)
	s.writeFrame(codexwire.Frame{
		Kind:    codexwire.FrameKindClientRequest,
		JSONRPC: "2.0",
		ID:      id,
		Method:  "initialize",
		Params: &codexwire.InitializeParams{
			ClientInfo:   codexwire.ClientInfo{Name: "harmonik", Title: "harmonik", Version: "codexdriver"},
			Capabilities: json.RawMessage("null"),
		},
	})
}

func (s *codexSession) writeThreadStart(cwd string) {
	id := s.nextReqID(pendingThreadStart, 0)
	s.writeFrame(codexwire.Frame{
		Kind:    codexwire.FrameKindClientRequest,
		JSONRPC: "2.0",
		ID:      id,
		Method:  "thread/start",
		Params:  &codexwire.ThreadStartParams{CWD: cwd},
	})
}

func (s *codexSession) writeTurnStart(seq uint64) {
	s.mu.Lock()
	payload := s.payloads[seq]
	delete(s.payloads, seq)
	threadID := s.threadID
	s.mu.Unlock()

	// The turn id this submission opens is not known until the turn/start
	// RESPONSE arrives (handleResponse → pendingTurnStart), which binds it to
	// this seq via s.pending[id]. No mutable in-flight pointer is set here.
	id := s.nextReqID(pendingTurnStart, seq)
	s.writeFrame(codexwire.Frame{
		Kind:    codexwire.FrameKindClientRequest,
		JSONRPC: "2.0",
		ID:      id,
		Method:  "turn/start",
		Params: &codexwire.TurnStartParams{
			ThreadID: threadID,
			Input: []codexwire.InputItem{{
				Type:         "text",
				Text:         string(payload),
				TextElements: json.RawMessage("[]"), // REQUIRED in the text variant (T0 finding)
			}},
		},
	})
}

// writeInterrupt sends turn/interrupt (graceful mid-turn wind-down, AIS-017).
// The method is not in the codexwire registry, so the params go raw.
func (s *codexSession) writeInterrupt(turnID string) {
	id := s.nextReqID(pendingInterrupt, 0)
	params, err := json.Marshal(map[string]string{"threadId": s.currentThreadID(), "turnId": turnID})
	if err != nil {
		return // cannot happen for a map[string]string; interrupt is best-effort
	}
	s.writeFrame(codexwire.Frame{
		Kind:      codexwire.FrameKindClientRequest,
		JSONRPC:   "2.0",
		ID:        id,
		Method:    "turn/interrupt",
		RawParams: params,
	})
}

func (s *codexSession) currentThreadID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.threadID
}

// ─── Inbound frames → Events ─────────────────────────────────────────────────

// handleFrame maps one parsed wire frame to reactor Events and drives the
// driver-internal handshake plumbing (initialize → initialized + thread/start
// → handshake_ok). The ack anchor is the turn/started notification: its turn
// id is the acceptance token (AIS-003c).
func (s *codexSession) handleFrame(f codexwire.Frame) {
	switch f.Kind {
	case codexwire.FrameKindServerResponse:
		s.handleResponse(f)
	case codexwire.FrameKindServerNotification:
		s.handleNotification(f)
	case codexwire.FrameKindServerRequest:
		s.handleServerRequest(f)
	default:
		// Client echoes / raw unknowns: preserved by the tee, ignored here.
	}
}

// handleServerRequest handles a JSON-RPC request the app-server sends TO us —
// an exec / apply-patch approval prompt carrying its own id + method (RU-07).
// These MUST NOT be dropped: the server blocks the turn until it receives a
// response for that id, so a silently-discarded request hangs the turn forever.
//
// This driver does not yet negotiate approval capabilities, so no interactive
// approval flow exists. The correct JSON-RPC-conformant answer is therefore to
// reply immediately with a "method not found" error (JSON-RPC -32601) for the
// request id, which unblocks the server's wait deterministically instead of
// leaving it parked. The event is also surfaced so an operator can see an
// approval was requested and auto-declined.
func (s *codexSession) handleServerRequest(f codexwire.Frame) {
	slog.WarnContext(context.Background(), "codexdriver_server_request_declined",
		"method", f.Method,
		"id", string(f.ID),
		"note", "app-server request auto-declined (no approval negotiation); replying method-not-found to unblock the turn (RU-07)",
	)
	// Reply with a JSON-RPC error for this id so the server's wait resolves.
	s.writeFrame(codexwire.Frame{
		Kind:    codexwire.FrameKindServerResponse,
		JSONRPC: "2.0",
		ID:      f.ID,
		Error:   json.RawMessage(`{"code":-32601,"message":"method not supported by client: ` + jsonEscape(f.Method) + `"}`),
	})
	// Deliberately no fatal Event: the wire reply unblocks the server, which then
	// drives the turn to its own terminal (turn/completed) normally. Injecting an
	// EventTypeError here would wind the whole session down for a single declined
	// approval, which is the opposite of the desired non-hang behaviour.
}

// jsonEscape returns s escaped for embedding inside a JSON string literal.
func jsonEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	// Strip the surrounding quotes json.Marshal adds.
	return string(b[1 : len(b)-1])
}

func (s *codexSession) handleResponse(f codexwire.Frame) {
	s.mu.Lock()
	req, ok := s.pending[string(f.ID)]
	if ok {
		delete(s.pending, string(f.ID))
	}
	s.mu.Unlock()
	if !ok {
		return // uncorrelated response; drop.
	}

	switch req.kind {
	case pendingInitialize:
		if len(f.Error) > 0 {
			s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: "initialize error: " + string(f.Error)})
			return
		}
		// Complete the client side of the handshake, then open the thread.
		s.writeFrame(codexwire.Frame{Kind: codexwire.FrameKindClientNotification, JSONRPC: "2.0", Method: "initialized"})
		s.writeThreadStart(s.cmd.Dir)

	case pendingThreadStart:
		if len(f.Error) > 0 {
			s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: "thread/start error: " + string(f.Error)})
			return
		}
		var res struct {
			Thread struct {
				ID string `json:"id"`
			} `json:"thread"`
		}
		if err := json.Unmarshal(f.RawResult, &res); err != nil || res.Thread.ID == "" {
			s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: "thread/start: missing thread id"})
			return
		}
		s.mu.Lock()
		s.threadID = res.Thread.ID
		s.mu.Unlock()
		// Handshake complete: the reactor's HandshakeOK step moves it to Ready,
		// which runLoop publishes — that is what unblocks awaitReady (no separate
		// ready channel needed).
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeHandshakeOK})

	case pendingTurnStart:
		if len(f.Error) > 0 {
			// Protocol-level refusal: the SYNC Ack{Rejected} terminal
			// (AIS-003). The reactor's input_rejected edge only cleans state
			// (no emit), so the waiter resolves here at the wire.
			s.resolveWaiter(req.seq, submitResult{ack: handler.Ack{Outcome: handler.Rejected, Seq: req.seq}})
			s.sendEvent(codexinput.Event{Type: codexinput.EventTypeInputRejected, InputSeq: req.seq, Reason: string(f.Error)})
			return
		}
		// Success response: it carries the created turn's id (corpus
		// raw-session-01 line 11). Bind that turn id → this seq so the
		// subsequent turn/started notification (the ack anchor) correlates by
		// its OWN turn id, not a mutable in-flight pointer — the fence against
		// a late turn/started for an abandoned turn mis-acking a fresh
		// submission (AIS-INV-001). The ack itself is still the turn/started.
		var res codexwire.TurnStartResult
		if err := json.Unmarshal(f.RawResult, &res); err == nil && res.Turn.ID != "" {
			s.mu.Lock()
			s.turnSeqByID[res.Turn.ID] = req.seq
			s.mu.Unlock()
		}

	case pendingInterrupt:
		// Best-effort graceful interrupt; nothing to resolve.
	}
}

func (s *codexSession) handleNotification(f codexwire.Frame) {
	switch f.Method {
	case "turn/started":
		// The ack anchor: the turn id is the acceptance token the input opened.
		// Correlate to the originating seq by THIS turn's id (bound from the
		// turn/start response), never by a mutable in-flight pointer. A
		// turn/started whose turn id we never opened — a genuinely-late anchor
		// for an abandoned/stale turn — matches no binding and is FENCED
		// (dropped), so it can never be stamped onto a fresh submission
		// (AIS-INV-001 no-mis-ack). The binding is one-shot: consumed here.
		turnID := ""
		if p, ok := f.Params.(*codexwire.TurnStartedParams); ok {
			turnID = p.Turn.ID
		}
		s.mu.Lock()
		seq, ok := s.turnSeqByID[turnID]
		if ok {
			delete(s.turnSeqByID, turnID)
		}
		s.mu.Unlock()
		if !ok || turnID == "" {
			return // fenced: unknown/late turn/started, no submission owns it.
		}
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeInputAcked, InputSeq: seq, TurnID: turnID})
	case "turn/completed":
		// Prune any lingering binding for the completed turn (e.g. an abandoned
		// turn whose late started was fenced without consuming its binding).
		if p, ok := f.Params.(*codexwire.TurnCompletedParams); ok && p.Turn.ID != "" {
			s.mu.Lock()
			delete(s.turnSeqByID, p.Turn.ID)
			s.mu.Unlock()
		}
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeTurnCompleted})
	case "item/agentMessage/delta":
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeDelta})
	default:
		// Status/config/token-usage notifications: observation-only here.
	}
}

// ─── Waiter plumbing ─────────────────────────────────────────────────────────

func (s *codexSession) resolveWaiter(seq uint64, res submitResult) {
	s.mu.Lock()
	ch, ok := s.waiters[seq]
	if ok {
		delete(s.waiters, seq)
	}
	// Prune any turn-id binding this seq still owns (e.g. resolved via stale
	// before its turn/started arrived) so a genuinely-late turn/started for the
	// abandoned turn finds no binding and is fenced (AIS-INV-001 no-mis-ack).
	for tid, s2 := range s.turnSeqByID {
		if s2 == seq {
			delete(s.turnSeqByID, tid)
		}
	}
	s.mu.Unlock()
	if ok {
		ch <- res // buffered(1); never blocks.
	}
}

func (s *codexSession) dropWaiter(seq uint64) {
	s.mu.Lock()
	delete(s.waiters, seq)
	delete(s.payloads, seq)
	s.mu.Unlock()
}

func (s *codexSession) setFailure(reason string) {
	s.failOnce.Do(func() {
		s.mu.Lock()
		s.failure = fmt.Errorf("codexdriver: launch failure: %s", reason)
		s.mu.Unlock()
		close(s.failCh)
	})
}

func (s *codexSession) failureErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure != nil {
		return s.failure
	}
	return errors.New("codexdriver: launch failure")
}

// ─── stderr ring ─────────────────────────────────────────────────────────────

// ringWriter keeps the last cap bytes written (Outcome.StderrTail parity).
type ringWriter struct {
	mu  sync.Mutex
	cap int
	buf []byte
}

func newRingWriter(capacity int) *ringWriter { return &ringWriter{cap: capacity} }

func (r *ringWriter) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	if len(r.buf) > r.cap {
		r.buf = r.buf[len(r.buf)-r.cap:]
	}
	return len(p), nil
}

func (r *ringWriter) Bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}
