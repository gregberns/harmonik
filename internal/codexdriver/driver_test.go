package codexdriver_test

// L2-style isolation tests for the structured Codex driver. Twin-blindness
// (AIS-015): the driver is exercised through its exported surface against a
// twin process speaking the same NDJSON wire on stdio — the test binary
// re-execs itself as the twin (TestMain helper-process pattern). The driver
// never sees a test branch.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/handler"
)

const (
	twinEnv     = "CODEXDRIVER_TWIN"
	twinModeEnv = "CODEXDRIVER_TWIN_MODE"
)

func TestMain(m *testing.M) {
	if os.Getenv(twinEnv) == "1" {
		runTwin(os.Getenv(twinModeEnv))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runTwin speaks the codex app-server NDJSON wire on stdio.
//
// Modes:
//   - "happy":       full handshake; turn/start → turn/started + delta + turn/completed
//   - "reject":      full handshake; turn/start → JSON-RPC error response
//   - "silent":      full handshake; turn/start → nothing (stale probe)
//   - "nohandshake": never answers initialize (handshake-timeout probe)
//   - "openturn":    full handshake; turn/start → turn/started + delta but NO
//     turn/completed (turn stays OPEN so a mid-turn CloseInput exercises the
//     graceful turn/interrupt path, AIS-017). On turn/interrupt the twin marks
//     stderr TWIN_INTERRUPT_RECEIVED, replies, then completes the turn.
//   - "silentthenhappy": first turn/start → silent (caller A parks in
//     AwaitingAck); second turn/start → full happy turn (caller B). Drives the
//     ctx-cancel-then-resubmit regression (A resolves via its stale timeout).
//   - "latedistinctturn": every turn/start emits a DELAYED full turn whose turn
//     id is turn_<N> (distinct per turn). The delay lets caller A be cancelled
//     while AwaitingAck, then turn_1's turn/started arrives LATE (after abandon).
//     Drives the mis-attribution regression: B must ack with turn_2, never turn_1.
//   - "stalethenlate": the stale-then-revive mis-attribution race. Caller A's
//     turn/start gets a RESPONSE (turn_1) but NO turn/started, so A stales and
//     frees the reactor; caller B's turn/start then emits turn_1's genuinely-late
//     turn/started BEFORE B's own turn_2. B must ack with turn_2, never turn_1.
func runTwin(mode string) {
	turnStarts := 0
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1<<20)
	emit := func(format string, args ...any) {
		if _, err := fmt.Fprintf(os.Stdout, format+"\n", args...); err != nil {
			os.Exit(1) // driver closed our stdout; twin is done
		}
	}
	// emitTurn plays a full turn (started → delta → completed) with the given
	// turn id — the ack anchor is turn/started, so the driver's Ack.Token is tid.
	emitTurn := func(tid string) {
		emit(`{"method":"turn/started","params":{"threadId":"th_1","turn":{"id":%q,"items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, tid)
		emit(`{"method":"item/agentMessage/delta","params":{"threadId":"th_1","turnId":%q,"itemId":"msg_1","delta":"ok"}}`, tid)
		emit(`{"method":"turn/completed","params":{"threadId":"th_1","turn":{"id":%q,"items":[],"itemsView":"notLoaded","status":"completed","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, tid)
	}
	for in.Scan() {
		line := in.Bytes()
		var env struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(line, &env) != nil {
			continue
		}
		// Approval-reply detection: in "approval" mode the twin sends the driver a
		// server-originated JSON-RPC request (id 999). The driver MUST answer it —
		// a dropped request would hang the turn. When the driver's response for id
		// 999 arrives (empty method), record a positive stderr marker and complete
		// the outstanding turn so the session drains cleanly (RU-07).
		if mode == "approval" && env.Method == "" && env.ID != nil && *env.ID == 999 {
			fmt.Fprintln(os.Stderr, "TWIN_APPROVAL_REPLY_RECEIVED")
			emit(`{"method":"turn/completed","params":{"threadId":"th_1","turn":{"id":"turn_%d","items":[],"itemsView":"notLoaded","status":"completed","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, turnStarts)
			continue
		}
		switch env.Method {
		case "initialize":
			if mode == "nohandshake" {
				continue
			}
			emit(`{"id":%d,"result":{"userAgent":"twin","codexHome":"/tmp","platformFamily":"test","platformOs":"test"}}`, *env.ID)
		case "initialized":
			// notification; no reply
		case "thread/start":
			emit(`{"id":%d,"result":{"thread":{"id":"th_1"},"model":"twin"}}`, *env.ID)
		case "thread/resume":
			// Re-attach to an existing thread (hk-160yb G1). Positive stderr
			// marker proves the driver took the resume branch (not thread/start),
			// and the reply echoes the REQUESTED threadId so a test can assert the
			// session re-adopted the prior thread. Shape mirrors thread/start
			// (ThreadResumeResult == ThreadStartResult).
			var rp struct {
				ThreadID string `json:"threadId"`
			}
			_ = json.Unmarshal(env.Params, &rp)
			fmt.Fprintln(os.Stderr, "TWIN_RESUME_RECEIVED "+rp.ThreadID)
			emit(`{"id":%d,"result":{"thread":{"id":%q},"model":"twin"}}`, *env.ID, rp.ThreadID)
		case "turn/start":
			turnStarts++
			tid := fmt.Sprintf("turn_%d", turnStarts)
			reqID := *env.ID
			// respondTurnStart replays the real server's turn/start RESPONSE,
			// which carries the created turn's id and arrives BEFORE the
			// turn/started notification (corpus raw-session-01 lines 11→13). The
			// driver binds this turn id → the submission seq for correlation.
			respondTurnStart := func(id string) {
				emit(`{"id":%d,"result":{"turn":{"id":%q,"items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, reqID, id)
			}
			effMode := mode
			switch mode {
			case "openturn":
				// Open the turn (response + started + delta) but DO NOT complete
				// it: the driver stays InTurn so a subsequent CloseInput
				// exercises the graceful turn/interrupt path (AIS-017), not a
				// SIGKILL.
				respondTurnStart(tid)
				emit(`{"method":"turn/started","params":{"threadId":"th_1","turn":{"id":%q,"items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, tid)
				emit(`{"method":"item/agentMessage/delta","params":{"threadId":"th_1","turnId":%q,"itemId":"msg_1","delta":"working"}}`, tid)
				continue
			case "dieafterturn":
				// Play one full turn, then exit — the child death that drives the
				// resident owner's respawn+resume path (hk-160yb G1b). A respawned
				// twin re-execs in this same mode: it re-attaches via thread/resume
				// (top-level case above) and then dies again after its own turn.
				respondTurnStart(tid)
				emitTurn(tid)
				os.Exit(0)
			case "approval":
				// Ack the submission (turn/started), then send a server-originated
				// approval request (id+method) that the driver must answer. The turn
				// is completed only once the driver's reply for id 999 arrives (see
				// the approval-reply detection above) — proving the request was NOT
				// silently dropped (RU-07).
				respondTurnStart(tid)
				emit(`{"method":"turn/started","params":{"threadId":"th_1","turn":{"id":%q,"items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, tid)
				emit(`{"jsonrpc":"2.0","id":999,"method":"execCommandApproval","params":{"command":"ls"}}`)
				continue
			case "silentthenhappy":
				if turnStarts == 1 {
					effMode = "silent" // caller A: never acked → resolves via stale
				} else {
					effMode = "happy" // caller B: served
				}
			case "latedistinctturn":
				// Delay so caller A is cancelled while AwaitingAck; turn_1's
				// response+turn/started then arrive late (after abandon). turn_2
				// for B.
				time.Sleep(400 * time.Millisecond)
				respondTurnStart(tid)
				emitTurn(tid)
				continue
			case "stalethenlate":
				// The stale-then-revive mis-attribution race (AIS-INV-001).
				//   turn/start #1 (caller A): send the turn/start RESPONSE (so the
				//     turn_1 binding exists) but NEVER the turn/started anchor —
				//     A stales via a short InputAckTimeout, freeing the reactor.
				//   turn/start #2 (caller B): send B's response (turn_2), THEN
				//     turn_1's genuinely-LATE turn/started (the abandoned turn's
				//     anchor), THEN B's own full turn_2. A mutable-inFlightSeq
				//     driver mis-acks B with turn_1; the turn-id-correlated driver
				//     FENCES the late turn_1 anchor and acks B with turn_2.
				if turnStarts == 1 {
					respondTurnStart(tid) // turn_1 bound to A; no anchor → A stales
					continue
				}
				respondTurnStart(tid) // turn_2 bound to B
				// The late anchor for the abandoned turn_1, injected AFTER B is
				// AwaitingAck and BEFORE B's own anchor.
				emit(`{"method":"turn/started","params":{"threadId":"th_1","turn":{"id":"turn_1","items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`)
				emitTurn(tid) // B's own turn_2
				continue
			}
			switch effMode {
			case "reject":
				emit(`{"id":%d,"error":{"code":-32000,"message":"twin says no"}}`, *env.ID)
			case "silent":
				// stale probe: no ack anchor ever arrives.
			default: // happy
				respondTurnStart(tid)
				emitTurn(tid)
			}
		case "turn/interrupt":
			// Positive marker on stderr (captured into Outcome.StderrTail) so a
			// test can PROVE the graceful turn/interrupt frame arrived — i.e. the
			// driver wound the turn down via interrupt, not a SIGKILL.
			fmt.Fprintln(os.Stderr, "TWIN_INTERRUPT_RECEIVED")
			emit(`{"id":%d,"result":{}}`, *env.ID)
			// Graceful drain: complete the interrupted turn so the reactor leaves
			// InTurn, then the stdin EOF (from CloseInput) ends the run at exit 0.
			emit(`{"method":"turn/completed","params":{"threadId":"th_1","turn":{"id":"turn_%d","items":[],"itemsView":"notLoaded","status":"completed","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, turnStarts)
		}
	}
	// stdin EOF: exit 0 (graceful end-of-input).
}

// emitRecorder collects the driver's durable-event emissions.
type emitRecorder struct {
	mu    sync.Mutex
	emits []codexdriver.Emission
}

func (r *emitRecorder) record(e codexdriver.Emission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emits = append(r.emits, e)
}

func (r *emitRecorder) types() []codexinput.EmitType {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]codexinput.EmitType, len(r.emits))
	for i, e := range r.emits {
		out[i] = e.Type
	}
	return out
}

// countType returns how many recorded emissions have the given type.
func (r *emitRecorder) countType(ty codexinput.EmitType) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.emits {
		if e.Type == ty {
			n++
		}
	}
	return n
}

// spawnTwin spawns a driver session against the twin in the given mode.
func spawnTwin(t *testing.T, mode string, cfg codexinput.Config, rec *emitRecorder) handler.SubstrateSession {
	t.Helper()
	opts := codexdriver.Options{Config: cfg}
	if rec != nil {
		opts.Emit = rec.record
	}
	sub := codexdriver.NewCodexSubstrate(opts)
	sess, err := sub.SpawnWindow(context.Background(), handler.SubstrateSpawn{
		WindowName: "twin",
		Argv:       []string{os.Args[0], "-test.run=NONE"},
		Env: append(os.Environ(),
			twinEnv+"=1",
			twinModeEnv+"="+mode,
		),
	})
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}
	t.Cleanup(func() {
		if err := sess.Kill(context.Background()); err != nil {
			t.Logf("cleanup Kill: %v", err)
		}
		waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := sess.Wait(waitCtx); err != nil {
			t.Logf("cleanup Wait: %v", err)
		}
	})
	return sess
}

func asPort(t *testing.T, sess handler.SubstrateSession) handler.InputPort {
	t.Helper()
	port, ok := handler.AsInputPort(sess)
	if !ok {
		t.Fatalf("session does not satisfy handler.InputPort (AIS-001 seam)")
	}
	return port
}

func TestSubmitAckedDelivered(t *testing.T) {
	rec := &emitRecorder{}
	sess := spawnTwin(t, "happy", codexinput.Config{}, rec)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ack, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("hello twin")})
	if err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if ack.Outcome != handler.Delivered {
		t.Fatalf("outcome = %v, want Delivered", ack.Outcome)
	}
	if ack.Seq != 1 {
		t.Fatalf("seq = %d, want 1", ack.Seq)
	}
	if ack.Token != "turn_1" {
		t.Fatalf("token = %q, want turn_1 (turn/started ack anchor)", ack.Token)
	}

	// Second submission on the same session (Ready again after turn_completed).
	ack2, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("again")})
	if err != nil {
		t.Fatalf("second SubmitInput: %v", err)
	}
	if ack2.Seq != 2 || ack2.Outcome != handler.Delivered {
		t.Fatalf("second ack = %+v", ack2)
	}

	if err := port.CloseInput(ctx); err != nil {
		t.Fatalf("CloseInput: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if got := sess.Outcome().ExitCode; got != 0 {
		t.Fatalf("exit code = %d, want 0 (graceful stdin-close drain)", got)
	}

	types := rec.types()
	wantPrefix := []codexinput.EmitType{
		codexinput.EmitInputSubmitted, codexinput.EmitInputAcked,
		codexinput.EmitInputSubmitted, codexinput.EmitInputAcked,
	}
	if len(types) < len(wantPrefix) {
		t.Fatalf("emissions = %v, want prefix %v", types, wantPrefix)
	}
	for i, w := range wantPrefix {
		if types[i] != w {
			t.Fatalf("emissions = %v, want prefix %v", types, wantPrefix)
		}
	}
}

// TestServerRequestAnswered proves that a JSON-RPC request the app-server sends
// TO the driver mid-turn (an exec/apply-patch approval prompt, id+method) is
// answered rather than silently dropped. The twin withholds turn/completed until
// it receives the driver's reply for the approval id; if the driver dropped the
// request (the pre-fix behaviour), the turn would never complete and the session
// would not drain to exit 0 with the positive stderr marker (RU-07).
func TestServerRequestAnswered(t *testing.T) {
	sess := spawnTwin(t, "approval", codexinput.Config{}, nil)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ack, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("do the thing")})
	if err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if ack.Outcome != handler.Delivered {
		t.Fatalf("outcome = %v, want Delivered", ack.Outcome)
	}

	if err := port.CloseInput(ctx); err != nil {
		t.Fatalf("CloseInput: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v (driver likely dropped the server request → turn never completed)", err)
	}
	if got := sess.Outcome().ExitCode; got != 0 {
		t.Fatalf("exit code = %d, want 0", got)
	}
	if tail := string(sess.Outcome().StderrTail); !strings.Contains(tail, "TWIN_APPROVAL_REPLY_RECEIVED") {
		t.Fatalf("twin never received the driver's approval reply — server request was dropped (RU-07). stderr: %q", tail)
	}
}

func TestSubmitRejected(t *testing.T) {
	sess := spawnTwin(t, "reject", codexinput.Config{}, nil)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ack, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("nope?")})
	if err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if ack.Outcome != handler.Rejected {
		t.Fatalf("outcome = %v, want Rejected (protocol-level refusal)", ack.Outcome)
	}
	if ack.Seq != 1 {
		t.Fatalf("seq = %d, want 1", ack.Seq)
	}
}

func TestSubmitStaleTerminal(t *testing.T) {
	rec := &emitRecorder{}
	// Short ack bound so the stale terminal fires fast (bounded liveness,
	// AIS-INV-001). The window is honored via ClockPort inside the driver.
	sess := spawnTwin(t, "silent", codexinput.Config{InputAckTimeout: 200 * time.Millisecond}, rec)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("into the void")})
	if !errors.Is(err, codexdriver.ErrInputStale) {
		t.Fatalf("err = %v, want ErrInputStale", err)
	}

	// The session stays usable after a stale terminal (reactor returns Ready);
	// a second submission must again reach a terminal, not silence.
	_, err = port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("again")})
	if !errors.Is(err, codexdriver.ErrInputStale) {
		t.Fatalf("second err = %v, want ErrInputStale", err)
	}

	types := rec.types()
	var stales int
	for _, ty := range types {
		if ty == codexinput.EmitInputStale {
			stales++
		}
	}
	if stales != 2 {
		t.Fatalf("stale emissions = %d (all: %v), want 2", stales, types)
	}
}

func TestHandshakeTimeoutLaunchFailure(t *testing.T) {
	rec := &emitRecorder{}
	sess := spawnTwin(t, "nohandshake", codexinput.Config{HandshakeTimeout: 200 * time.Millisecond}, rec)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("never ready")})
	if err == nil || !strings.Contains(err.Error(), "launch failure") {
		t.Fatalf("err = %v, want launch-failure refusal (AIS-017 fast-fail)", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		found := false
		for _, ty := range rec.types() {
			if ty == codexinput.EmitLaunchFailure {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no agent_launch_failure emission; got %v", rec.types())
		}
		time.Sleep(10 * time.Millisecond) // test scaffolding poll, not driver timing
	}
}

// TestCloseMidTurnGracefulInterrupt is the driver-level AIS-017 reduce-the-need
// acceptance: a CloseInput issued while a turn is still OPEN winds the session
// down via the graceful turn/interrupt frame and a stdin-close drain — NOT a
// SIGKILL. An ungraceful kill is exactly what leaves a stale codex
// state_*.sqlite-wal behind (the WAL-guard's whole reason to exist); the
// graceful path avoids it. Proven two ways: the twin records a positive
// TWIN_INTERRUPT_RECEIVED marker (the interrupt frame arrived), and the child
// exits 0 UNSIGNALED (no kill signal).
func TestCloseMidTurnGracefulInterrupt(t *testing.T) {
	rec := &emitRecorder{}
	sess := spawnTwin(t, "openturn", codexinput.Config{}, rec)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Submit: the turn opens (turn/started) and stays open — the driver is InTurn.
	ack, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("start work")})
	if err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if ack.Outcome != handler.Delivered || ack.Token != "turn_1" {
		t.Fatalf("ack = %+v, want Delivered/turn_1", ack)
	}

	// Close mid-turn: reactor emits turn/interrupt (graceful) + stdin close.
	if err := port.CloseInput(ctx); err != nil {
		t.Fatalf("CloseInput: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	out := sess.Outcome()
	if out.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0 (graceful turn/interrupt + stdin-close drain, not SIGKILL)", out.ExitCode)
	}
	if out.Signal != syscall.Signal(-1) {
		t.Fatalf("child was signaled (%v); graceful close must NOT SIGKILL (AIS-017)", out.Signal)
	}
	if !strings.Contains(string(out.StderrTail), "TWIN_INTERRUPT_RECEIVED") {
		t.Fatalf("turn/interrupt frame never reached the child (stderr tail: %q); "+
			"graceful interrupt path not exercised", string(out.StderrTail))
	}
}

func TestKillAndWait(t *testing.T) {
	sess := spawnTwin(t, "happy", codexinput.Config{}, nil)
	if sess.PID() == 0 {
		t.Fatalf("PID = 0, want live child pid")
	}
	if sess.Stdout() != nil {
		t.Fatalf("Stdout() must be nil for the structured driver (wire is driver-owned)")
	}
	if err := sess.Kill(context.Background()); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if err := sess.Kill(context.Background()); err != nil { // idempotent
		t.Fatalf("second Kill: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait after Kill: %v", err)
	}
}

func TestSubmitAfterCloseInputRefused(t *testing.T) {
	sess := spawnTwin(t, "happy", codexinput.Config{}, nil)
	port := asPort(t, sess)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("one")}); err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if err := port.CloseInput(ctx); err != nil {
		t.Fatalf("CloseInput: %v", err)
	}
	if err := port.CloseInput(ctx); err != nil { // idempotent
		t.Fatalf("second CloseInput: %v", err)
	}
	if _, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("two")}); err == nil {
		t.Fatalf("SubmitInput after CloseInput succeeded, want refusal")
	}
}

func TestConcurrentSubmitsSerialize(t *testing.T) {
	sess := spawnTwin(t, "happy", codexinput.Config{}, nil)
	port := asPort(t, sess)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const n = 5
	var wg sync.WaitGroup
	errs := make([]error, n)
	acks := make([]handler.Ack, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			acks[i], errs[i] = port.SubmitInput(ctx, handler.InputRequest{Payload: []byte(fmt.Sprintf("msg-%d", i))})
		}(i)
	}
	wg.Wait()

	seen := map[uint64]bool{}
	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("submit %d: %v", i, errs[i])
		}
		if acks[i].Outcome != handler.Delivered {
			t.Fatalf("submit %d outcome = %v", i, acks[i].Outcome)
		}
		if seen[acks[i].Seq] {
			t.Fatalf("duplicate seq %d", acks[i].Seq)
		}
		seen[acks[i].Seq] = true
	}
}

// TestCancelThenResubmitReachesTerminal is the AIS-INV-001 regression for the
// ctx-cancel-then-resubmit hang: caller A cancels its SubmitInput while the
// reactor is in AwaitingAck (before ack/reject/stale), then caller B submits.
// B MUST reach a terminal (here: Delivered) within a bounded time — never park
// against a still-AwaitingAck reactor with no scheduled terminal.
func TestCancelThenResubmitReachesTerminal(t *testing.T) {
	rec := &emitRecorder{}
	// Short ack timeout: caller A's abandoned (silent) turn resolves via its
	// REAL stale terminal, which returns the reactor to Ready and unblocks
	// caller B's phase-gated front-stop. B's wait is bounded by this timeout,
	// which is the correct behaviour (Option B) — NOT a hang.
	sess := spawnTwin(t, "silentthenhappy", codexinput.Config{InputAckTimeout: 1500 * time.Millisecond}, rec)
	port := asPort(t, sess)

	// Caller A: submit under a cancelable ctx, then cancel once the reactor is
	// confirmed in AwaitingAck (agent_input_submitted emitted for seq 1).
	ctxA, cancelA := context.WithCancel(context.Background())
	aDone := make(chan error, 1)
	go func() {
		_, err := port.SubmitInput(ctxA, handler.InputRequest{Payload: []byte("caller A")})
		aDone <- err
	}()

	deadline := time.Now().Add(10 * time.Second)
	for rec.countType(codexinput.EmitInputSubmitted) < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("caller A never reached AwaitingAck (no agent_input_submitted)")
		}
		time.Sleep(5 * time.Millisecond) // test scaffolding poll, not driver timing
	}
	cancelA()

	select {
	case err := <-aDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("caller A err = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("caller A did not return after cancel")
	}

	// Caller B: MUST reach a terminal (not hang) even though A left the reactor
	// in AwaitingAck. Bound B's own ctx generously; the assertion is that B
	// resolves well within it.
	ctxB, cancelB := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelB()
	bDone := make(chan struct {
		ack handler.Ack
		err error
	}, 1)
	go func() {
		ack, err := port.SubmitInput(ctxB, handler.InputRequest{Payload: []byte("caller B")})
		bDone <- struct {
			ack handler.Ack
			err error
		}{ack, err}
	}()

	select {
	case res := <-bDone:
		if res.err != nil {
			t.Fatalf("caller B err = %v, want a clean terminal (delivered)", res.err)
		}
		if res.ack.Outcome != handler.Delivered {
			t.Fatalf("caller B outcome = %v, want Delivered", res.ack.Outcome)
		}
	case <-time.After(12 * time.Second):
		t.Fatalf("caller B HUNG against an AwaitingAck reactor (AIS-INV-001 violation)")
	}
}

// TestCancelThenLateTurnStartedNoMisAck is the mis-attribution regression: a
// NON-silent abandoned turn. Caller A is cancelled while AwaitingAck; the child
// THEN emits turn_1's turn/started LATE. Caller B submits and MUST resolve with
// its OWN turn id (turn_2), never A's turn_1. This guards against optimistically
// freeing the reactor while A's turn is still live (the mutable-inFlightSeq
// mis-correlation the reviewer named). B waits for A's real terminal before
// proceeding, so turn_1's late turn/started resolves against seq 1 (no waiter)
// and B only ever sees turn_2.
func TestCancelThenLateTurnStartedNoMisAck(t *testing.T) {
	rec := &emitRecorder{}
	// Long ack timeout: A's turn is merely SLOW (400 ms twin delay), not silent,
	// so A must not stale first — its late turn_1 must actually arrive.
	sess := spawnTwin(t, "latedistinctturn", codexinput.Config{InputAckTimeout: 30 * time.Second}, rec)
	port := asPort(t, sess)

	ctxA, cancelA := context.WithCancel(context.Background())
	aDone := make(chan error, 1)
	go func() {
		_, err := port.SubmitInput(ctxA, handler.InputRequest{Payload: []byte("caller A")})
		aDone <- err
	}()

	// Cancel A as soon as it is AwaitingAck (turn_1's turn/started is still 400 ms
	// out in the twin), so A is genuinely abandoned mid-flight.
	deadline := time.Now().Add(10 * time.Second)
	for rec.countType(codexinput.EmitInputSubmitted) < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("caller A never reached AwaitingAck")
		}
		time.Sleep(5 * time.Millisecond) // test scaffolding poll, not driver timing
	}
	cancelA()
	if err := <-aDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("caller A err = %v, want context.Canceled", err)
	}

	// Caller B: blocks until A's real terminal (turn_1 completed) returns the
	// reactor to Ready, then runs turn_2. Its token MUST be turn_2.
	ctxB, cancelB := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelB()
	ack, err := port.SubmitInput(ctxB, handler.InputRequest{Payload: []byte("caller B")})
	if err != nil {
		t.Fatalf("caller B err = %v, want Delivered", err)
	}
	if ack.Outcome != handler.Delivered {
		t.Fatalf("caller B outcome = %v, want Delivered", ack.Outcome)
	}
	if ack.Token == "turn_1" {
		t.Fatalf("caller B MIS-ACKED with A's turn id turn_1 (AIS-INV-001 wrong-ack)")
	}
	if ack.Token != "turn_2" {
		t.Fatalf("caller B token = %q, want turn_2 (its own turn)", ack.Token)
	}
	if ack.Seq != 2 {
		t.Fatalf("caller B seq = %d, want 2", ack.Seq)
	}
}

// TestStaleThenLateTurnStartedNoMisAck is the design-inherent AIS-INV-001
// no-mis-ack regression the response-id correlation fixes (bead hk-9rrzi). It is
// the STALE-then-revive variant the front-stop alone cannot cover: caller A's
// submission is slower than InputAckTimeout, so A reaches its REAL stale terminal
// and the reactor legitimately returns to Ready (front-stop satisfied). Caller B
// then submits — and A's genuinely-late turn/started (for the now-abandoned
// turn_1) arrives while B is AwaitingAck. A driver that correlated turn/started
// by a mutable in-flight seq would stamp turn_1 onto B (mis-ack). With turn-id
// correlation (turn/started bound to the turn/start response's turn id), the late
// turn_1 anchor matches no live binding and is FENCED; B acks with its own turn_2.
func TestStaleThenLateTurnStartedNoMisAck(t *testing.T) {
	rec := &emitRecorder{}
	// Short ack timeout so caller A genuinely stales (its turn_1 anchor never
	// arrives) and the reactor returns to Ready before caller B submits.
	sess := spawnTwin(t, "stalethenlate", codexinput.Config{InputAckTimeout: 300 * time.Millisecond}, rec)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Caller A: MUST reach the stale terminal (never an ack) — turn_1 has no
	// anchor within the bound.
	_, errA := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("caller A")})
	if !errors.Is(errA, codexdriver.ErrInputStale) {
		t.Fatalf("caller A err = %v, want ErrInputStale (no anchor within bound)", errA)
	}

	// Caller B: the twin now injects turn_1's LATE turn/started (abandoned-turn
	// anchor) before B's own turn_2. B MUST ack with turn_2, never turn_1.
	ack, errB := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("caller B")})
	if errB != nil {
		t.Fatalf("caller B err = %v, want Delivered", errB)
	}
	if ack.Outcome != handler.Delivered {
		t.Fatalf("caller B outcome = %v, want Delivered", ack.Outcome)
	}
	if ack.Token == "turn_1" {
		t.Fatalf("caller B MIS-ACKED with abandoned turn_1 (AIS-INV-001 wrong-ack); " +
			"late turn/started was stamped onto a fresh submission")
	}
	if ack.Token != "turn_2" {
		t.Fatalf("caller B token = %q, want turn_2 (its own turn)", ack.Token)
	}
	if ack.Seq != 2 {
		t.Fatalf("caller B seq = %d, want 2", ack.Seq)
	}
}

func TestCaptureTee(t *testing.T) {
	inCap := &lockedBuffer{}
	outCap := &lockedBuffer{}
	sub := codexdriver.NewCodexSubstrate(codexdriver.Options{InCapture: inCap, OutCapture: outCap})
	sess, err := sub.SpawnWindow(context.Background(), handler.SubstrateSpawn{
		Argv: []string{os.Args[0], "-test.run=NONE"},
		Env:  append(os.Environ(), twinEnv+"=1", twinModeEnv+"=happy"),
	})
	if err != nil {
		t.Fatalf("SpawnWindow: %v", err)
	}
	port := asPort(t, sess)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if _, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("teed payload")}); err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if err := port.CloseInput(ctx); err != nil {
		t.Fatalf("CloseInput: %v", err)
	}
	if err := sess.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !strings.Contains(inCap.String(), "teed payload") {
		t.Fatalf("InCapture missing the submitted payload; got %q", inCap.String())
	}
	if !strings.Contains(outCap.String(), "turn/started") {
		t.Fatalf("OutCapture missing the ack-anchor frame; got %q", outCap.String())
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
