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
//   - "silentthenhappy": first turn/start → silent (caller A parks in
//     AwaitingAck); second turn/start → full happy turn (caller B). Drives the
//     ctx-cancel-then-resubmit regression (A resolves via its stale timeout).
//   - "latedistinctturn": every turn/start emits a DELAYED full turn whose turn
//     id is turn_<N> (distinct per turn). The delay lets caller A be cancelled
//     while AwaitingAck, then turn_1's turn/started arrives LATE (after abandon).
//     Drives the mis-attribution regression: B must ack with turn_2, never turn_1.
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
		case "turn/start":
			turnStarts++
			tid := fmt.Sprintf("turn_%d", turnStarts)
			effMode := mode
			switch mode {
			case "silentthenhappy":
				if turnStarts == 1 {
					effMode = "silent" // caller A: never acked → resolves via stale
				} else {
					effMode = "happy" // caller B: served
				}
			case "latedistinctturn":
				// Delay so caller A is cancelled while AwaitingAck; turn_1's
				// turn/started then arrives late (after abandon). turn_2 for B.
				time.Sleep(400 * time.Millisecond)
				emitTurn(tid)
				continue
			}
			switch effMode {
			case "reject":
				emit(`{"id":%d,"error":{"code":-32000,"message":"twin says no"}}`, *env.ID)
			case "silent":
				// stale probe: no ack anchor ever arrives.
			default: // happy
				emitTurn(tid)
			}
		case "turn/interrupt":
			emit(`{"id":%d,"result":{}}`, *env.ID)
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
