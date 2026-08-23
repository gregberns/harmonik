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

const scannerBufCap = 1 << 20

const stderrTailCap = 4 * 1024

const evChanCap = 128

// ErrInputStale is wrapped by SubmitInput's stale terminal (AIS-INV-001): the
// submission reached the agent_input_stale terminal instead of an ack.
var ErrInputStale = errors.New("codexdriver: input stale")

// ErrSessionClosed is returned by SubmitInput/CloseInput once the driver loop
// has wound down (child exited or wire closed).
var ErrSessionClosed = errors.New("codexdriver: session closed")

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

type codexSession struct {
	opts       Options
	cmd        *exec.Cmd
	procCancel context.CancelFunc

	stdinPipe io.WriteCloser // raw pipe (for Close)
	stdinW    io.Writer      // tee'd write path
	stdout    io.Reader      // tee'd read path
	stderr    *ringWriter

	// wmu serializes every write to the child's stdin AND the pipe close, so a
	// server-request reply written on readLoop (handleServerRequest) can never
	// tear against, or write after, a close driven on runLoop. stdinWClosed
	// latches once the pipe is closed; a write after it is dropped silently
	// (the child is gone) rather than surfacing a spurious transport error.
	wmu          sync.Mutex
	stdinWClosed bool

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
	// handshakeDone latches true the first time the reactor reaches Ready, i.e.
	// the launch handshake completed. finalize reads it to tell a clean
	// wind-down (ran, then the wire closed) from a genuine launch failure
	// (never reached Ready). Written only from runLoop (publishPhase); guarded
	// by phaseMu because awaitReady's goroutine may also observe the phase.
	handshakeDone bool

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
	// resumeThreadID, when non-empty, makes the launch handshake re-attach to an
	// existing server-side thread via `thread/resume` instead of opening a fresh
	// one via `thread/start` (hk-160yb G1 reconnect). Set once by the substrate
	// before start() and never mutated afterward; read only on the readLoop
	// goroutine (handleResponse pendingInitialize), so it needs no lock.
	resumeThreadID string
	// spawnCwd is the worktree working directory this session was spawned into
	// (SubstrateSpawn.Cwd). Unlike cmd.Dir it is populated on BOTH the local and
	// the remote (ssh) spawn paths — the remote path leaves cmd.Dir UNSET (the cwd
	// is applied on the worker via `cd`), so cmd.Dir cannot be used to derive the
	// worktree's git common dir. hk-daegv: the input to Options.WritableRoots. Set
	// once before start(); read only on the readLoop handshake goroutine.
	spawnCwd    string
	failure     error
	stdinClosed bool
	// drainClose latches when a mid-turn CloseInput deferred the stdin close so
	// the interrupted turn can wind down first (RU-07: a server-originated
	// approval request the child sends AFTER we started closing still needs its
	// reply written, which requires stdin to stay open). The pipe is then closed
	// on the turn's terminal (turn/completed) or by the drain backstop timer,
	// whichever comes first. drainCancel stops that backstop when the terminal
	// wins the race.
	drainClose  bool
	drainCancel context.CancelFunc

	killOnce sync.Once

	outcomeMu sync.Mutex
	outcome   handler.Outcome
	started   time.Time
}

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

func captureDegradeLogger(dir string) func(error) {
	return func(err error) {
		slog.WarnContext(context.Background(), "codexdriver_capture_degraded",
			"direction", dir,
			"error", err.Error(),
			"note", "capture dropped; live agent stream unaffected (AIS-INV-002)",
		)
	}
}

func (s *codexSession) start(_ context.Context) {
	go s.reapLoop()
	go s.readLoop() //nolint:contextcheck // readLoop→handleFrame→handleServerRequest is session-lifetime-owned (ends on wire close), deliberately not spawn-ctx-scoped
	go s.runLoop()  //nolint:contextcheck // the loop is session-lifetime-owned (ends on wire close), deliberately not spawn-ctx-scoped
	s.sendEvent(codexinput.Event{Type: codexinput.EventTypeSpawned})
}

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

// SubmitInput delivers one submission and blocks to exactly ONE terminal
// (AIS-INV-001 / HC-INV-008): the async agent_input_acked resolves
// Ack{Delivered, Seq, Token=turn id}; a protocol-level refusal resolves
// Ack{Rejected}; the agent_input_stale terminal resolves as an error wrapping
// ErrInputStale. Never silence. Callers serialize (one uncorrelated input in
// flight); ctx cancellation abandons the park without leaking the waiter.
func (s *codexSession) SubmitInput(ctx context.Context, req handler.InputRequest) (handler.Ack, error) {
	s.submitMu.Lock()
	defer s.submitMu.Unlock()

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
		select {
		case res := <-ch:
			return res.ack, res.err
		default:
			return handler.Ack{}, ErrSessionClosed
		}
	case <-s.failCh:
		select {
		case res := <-ch:
			return res.ack, res.err
		default:
			s.dropWaiter(seq)
			return handler.Ack{}, fmt.Errorf("codexdriver: submit aborted: %w", s.failureErr())
		}
	case <-ctx.Done():
		s.dropWaiter(seq)
		return handler.Ack{}, ctx.Err()
	}
}

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
		}

		select {
		case <-changed:
		case <-s.failCh:
			return fmt.Errorf("codexdriver: submit refused: %w", s.failureErr())
		case <-s.loopDone:
			return ErrSessionClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *codexSession) publishPhase(p codexinput.DriverState) {
	s.phaseMu.Lock()
	s.phase = p
	if p == codexinput.Ready {
		s.handshakeDone = true
	}
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

func (s *codexSession) runLoop() {
	ctx := context.Background()
	r := codexinput.New(s.opts.Config)
	src := (*sessionSource)(s)
	eff := (*sessionEffector)(s)
	for ev := range src.Events(ctx) {
		for _, a := range r.Step(ev) {
			if err := eff.Execute(ctx, a); err != nil {
				s.setFailure("effector: " + err.Error())
			}
		}
		s.publishPhase(r.State().Phase)
	}
	s.finalize()
	close(s.loopDone)
}

func (s *codexSession) finalize() {
	s.finishDeferredClose()
	s.closeStdin()
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
	s.phaseMu.Lock()
	handshakeDone := s.handshakeDone
	s.phaseMu.Unlock()
	if !handshakeDone {
		s.setFailure("session wind-down before handshake")
	}
}

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

func (s *codexSession) sendEvent(ev codexinput.Event) {
	select {
	case s.evCh <- ev:
	case <-s.loopDone:
	}
}

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
		s.closeInputAction() //nolint:contextcheck // the effector deliberately ignores its ctx (Execute takes _ context.Context); close is reactor-owned lifecycle, not caller-ctx-scoped
	case codexinput.ActionTypeInterrupt:
		s.mu.Lock()
		s.drainClose = true
		s.mu.Unlock()
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
	}
	if s.opts.Emit != nil {
		s.opts.Emit(Emission{Type: a.Emit, InputSeq: a.InputSeq, TurnID: a.TurnID, Reason: a.Reason})
	}
}

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

func (s *codexSession) nextReqID(kind pendingKind, seq uint64) json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqID++
	raw := json.RawMessage(strconv.FormatInt(s.reqID, 10))
	s.pending[string(raw)] = pendingReq{kind: kind, seq: seq}
	return raw
}

func (s *codexSession) writeFrame(f codexwire.Frame) {
	b, err := codexwire.Marshal(f)
	if err != nil {
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: "marshal: " + err.Error()})
		return
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.stdinWClosed {
		return // child stdin already closed; the child is gone. Drop, don't fault.
	}
	if _, err := s.stdinW.Write(append(b, '\n')); err != nil {
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: "stdin write: " + err.Error()})
	}
}

func (s *codexSession) closeStdin() {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.stdinWClosed {
		return
	}
	s.stdinWClosed = true
	if closeErr := s.stdinPipe.Close(); closeErr != nil {
		slog.WarnContext(context.Background(), "codexdriver_close_stdin", "err", closeErr)
	}
}

const drainTimeout = 30 * time.Second

func (s *codexSession) closeInputAction() {
	tctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if !s.drainClose {
		s.mu.Unlock()
		cancel()
		s.closeStdin()
		return
	}
	s.drainCancel = cancel
	s.mu.Unlock()
	go func() {
		if s.opts.Clock.Sleep(tctx, drainTimeout) {
			s.finishDeferredClose()
		}
	}()
}

func (s *codexSession) finishDeferredClose() {
	s.mu.Lock()
	if !s.drainClose {
		s.mu.Unlock()
		return
	}
	s.drainClose = false
	cancel := s.drainCancel
	s.drainCancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.closeStdin()
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
		Params:  &codexwire.ThreadStartParams{CWD: cwd, Extra: s.postureExtra()},
	})
}

func (s *codexSession) postureExtra() map[string]json.RawMessage {
	extra := map[string]json.RawMessage{}
	if s.opts.Sandbox != "" {
		if b, err := json.Marshal(s.opts.Sandbox); err == nil {
			extra["sandbox"] = b
		}
	}
	if s.opts.ApprovalPolicy != "" {
		if b, err := json.Marshal(s.opts.ApprovalPolicy); err == nil {
			extra["approvalPolicy"] = b
		}
	}
	if s.opts.WritableRoots != nil && s.spawnCwd != "" {
		if roots := s.opts.WritableRoots(s.spawnCwd); len(roots) > 0 {
			if b, err := json.Marshal(roots); err == nil {
				extra["runtimeWorkspaceRoots"] = b
			}
		}
	}
	if len(extra) == 0 {
		return nil
	}
	return extra
}

func (s *codexSession) writeThreadResume(threadID string) {
	id := s.nextReqID(pendingThreadStart, 0)
	s.writeFrame(codexwire.Frame{
		Kind:    codexwire.FrameKindClientRequest,
		JSONRPC: "2.0",
		ID:      id,
		Method:  "thread/resume",
		Params:  &codexwire.ThreadResumeParams{ThreadID: threadID, Extra: s.postureExtra()},
	})
}

func (s *codexSession) writeTurnStart(seq uint64) {
	s.mu.Lock()
	payload := s.payloads[seq]
	delete(s.payloads, seq)
	threadID := s.threadID
	s.mu.Unlock()

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

func (s *codexSession) handleFrame(f codexwire.Frame) {
	switch f.Kind {
	case codexwire.FrameKindServerResponse:
		s.handleResponse(f)
	case codexwire.FrameKindServerNotification:
		s.handleNotification(f)
	case codexwire.FrameKindServerRequest:
		s.handleServerRequest(f)
	default:
	}
}

func (s *codexSession) handleServerRequest(f codexwire.Frame) {
	slog.WarnContext(context.Background(), "codexdriver_server_request_declined",
		"method", f.Method,
		"id", string(f.ID),
		"note", "app-server request auto-declined (no approval negotiation); replying method-not-found to unblock the turn (RU-07)",
	)
	s.writeFrame(codexwire.Frame{
		Kind:    codexwire.FrameKindServerResponse,
		JSONRPC: "2.0",
		ID:      f.ID,
		Error:   json.RawMessage(`{"code":-32601,"message":"method not supported by client: ` + jsonEscape(f.Method) + `"}`),
	})
}

func jsonEscape(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
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
		s.writeFrame(codexwire.Frame{Kind: codexwire.FrameKindClientNotification, JSONRPC: "2.0", Method: "initialized"})
		if s.resumeThreadID != "" {
			s.writeThreadResume(s.resumeThreadID)
		} else {
			s.writeThreadStart(s.cmd.Dir)
		}

	case pendingThreadStart:
		s.handleThreadStartResponse(f)

	case pendingTurnStart:
		if len(f.Error) > 0 {
			s.resolveWaiter(req.seq, submitResult{ack: handler.Ack{Outcome: handler.Rejected, Seq: req.seq}})
			s.sendEvent(codexinput.Event{Type: codexinput.EventTypeInputRejected, InputSeq: req.seq, Reason: string(f.Error)})
			return
		}
		var res codexwire.TurnStartResult
		if err := json.Unmarshal(f.RawResult, &res); err == nil && res.Turn.ID != "" {
			s.mu.Lock()
			s.turnSeqByID[res.Turn.ID] = req.seq
			s.mu.Unlock()
		}

	case pendingInterrupt:
	}
}

func (s *codexSession) handleThreadStartResponse(f codexwire.Frame) {
	method := "thread/start"
	if s.resumeThreadID != "" {
		method = "thread/resume"
	}
	if len(f.Error) > 0 {
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: method + " error: " + string(f.Error)})
		return
	}
	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(f.RawResult, &res); err != nil || res.Thread.ID == "" {
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeError, Reason: method + ": missing thread id"})
		return
	}
	s.mu.Lock()
	s.threadID = res.Thread.ID
	s.mu.Unlock()
	s.sendEvent(codexinput.Event{Type: codexinput.EventTypeHandshakeOK})
}

func (s *codexSession) handleNotification(f codexwire.Frame) {
	switch f.Method {
	case "turn/started":
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
		if p, ok := f.Params.(*codexwire.TurnCompletedParams); ok && p.Turn.ID != "" {
			s.mu.Lock()
			delete(s.turnSeqByID, p.Turn.ID)
			s.mu.Unlock()
		}
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeTurnCompleted})
		s.finishDeferredClose()
	case "item/agentMessage/delta":
		s.sendEvent(codexinput.Event{Type: codexinput.EventTypeDelta})
	default:
	}
}

func (s *codexSession) resolveWaiter(seq uint64, res submitResult) {
	s.mu.Lock()
	ch, ok := s.waiters[seq]
	if ok {
		delete(s.waiters, seq)
	}
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
