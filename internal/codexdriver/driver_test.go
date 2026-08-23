package codexdriver_test

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

func emitPostureMarker(tag string, params json.RawMessage) {
	var p struct {
		Sandbox        string `json:"sandbox"`
		ApprovalPolicy string `json:"approvalPolicy"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		fmt.Fprintf(os.Stderr, "%s decode-error: %v\n", tag, err)
	}
	fmt.Fprintf(os.Stderr, "%s sandbox=%s approval=%s\n", tag, p.Sandbox, p.ApprovalPolicy)
}

func emitWritableRootsMarker(tag string, params json.RawMessage) {
	var p struct {
		RuntimeWorkspaceRoots []string `json:"runtimeWorkspaceRoots"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		fmt.Fprintf(os.Stderr, "%s decode-error: %v\n", tag, err)
	}
	fmt.Fprintf(os.Stderr, "%s roots=%s\n", tag, strings.Join(p.RuntimeWorkspaceRoots, ","))
}

func runTwin(mode string) {
	turnStarts := 0
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1<<20)
	emit := func(format string, args ...any) {
		if _, err := fmt.Fprintf(os.Stdout, format+"\n", args...); err != nil {
			os.Exit(1) // driver closed our stdout; twin is done
		}
	}
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
		case "thread/start":
			emitPostureMarker("TWIN_POSTURE_START", env.Params)
			emitWritableRootsMarker("TWIN_WRITABLE_ROOTS_START", env.Params)
			emit(`{"id":%d,"result":{"thread":{"id":"th_1"},"model":"twin"}}`, *env.ID)
			if mode == "dieafterhandshake" {
				time.Sleep(200 * time.Millisecond)
				os.Exit(0)
			}
		case "thread/resume":
			var rp struct {
				ThreadID string `json:"threadId"`
			}
			if err := json.Unmarshal(env.Params, &rp); err != nil {
				fmt.Fprintf(os.Stderr, "twin thread/resume decode-error: %v\n", err)
			}
			emitPostureMarker("TWIN_POSTURE_RESUME", env.Params)
			emitWritableRootsMarker("TWIN_WRITABLE_ROOTS_RESUME", env.Params)
			fmt.Fprintln(os.Stderr, "TWIN_RESUME_RECEIVED "+rp.ThreadID)
			emit(`{"id":%d,"result":{"thread":{"id":%q},"model":"twin"}}`, *env.ID, rp.ThreadID)
		case "turn/start":
			turnStarts++
			tid := fmt.Sprintf("turn_%d", turnStarts)
			reqID := *env.ID
			respondTurnStart := func(id string) {
				emit(`{"id":%d,"result":{"turn":{"id":%q,"items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, reqID, id)
			}
			effMode := mode
			switch mode {
			case "openturn":
				respondTurnStart(tid)
				emit(`{"method":"turn/started","params":{"threadId":"th_1","turn":{"id":%q,"items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, tid)
				emit(`{"method":"item/agentMessage/delta","params":{"threadId":"th_1","turnId":%q,"itemId":"msg_1","delta":"working"}}`, tid)
				continue
			case "dieafterturn":
				respondTurnStart(tid)
				emitTurn(tid)
				os.Exit(0)
			case "approval":
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
				time.Sleep(400 * time.Millisecond)
				respondTurnStart(tid)
				emitTurn(tid)
				continue
			case "stalethenlate":
				if turnStarts == 1 {
					respondTurnStart(tid) // turn_1 bound to A; no anchor → A stales
					continue
				}
				respondTurnStart(tid) // turn_2 bound to B
				emit(`{"method":"turn/started","params":{"threadId":"th_1","turn":{"id":"turn_1","items":[],"itemsView":"notLoaded","status":"inProgress","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`)
				emitTurn(tid) // B's own turn_2
				continue
			}
			switch effMode {
			case "reject":
				emit(`{"id":%d,"error":{"code":-32000,"message":"twin says no"}}`, *env.ID)
			case "silent":
			default: // happy
				respondTurnStart(tid)
				emitTurn(tid)
			}
		case "turn/interrupt":
			fmt.Fprintln(os.Stderr, "TWIN_INTERRUPT_RECEIVED")
			emit(`{"id":%d,"result":{}}`, *env.ID)
			emit(`{"method":"turn/completed","params":{"threadId":"th_1","turn":{"id":"turn_%d","items":[],"itemsView":"notLoaded","status":"completed","error":null,"startedAt":null,"completedAt":null,"durationMs":null}}}`, turnStarts)
		}
	}
}

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
	sess := spawnTwin(t, "silent", codexinput.Config{InputAckTimeout: 200 * time.Millisecond}, rec)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("into the void")})
	if !errors.Is(err, codexdriver.ErrInputStale) {
		t.Fatalf("err = %v, want ErrInputStale", err)
	}

	_, err = port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("again")})
	if !errors.Is(err, codexdriver.ErrInputStale) {
		t.Fatalf("second err = %v, want ErrInputStale", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for rec.countType(codexinput.EmitInputStale) < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("stale emissions = %d (all: %v), want 2",
				rec.countType(codexinput.EmitInputStale), rec.types())
		}
		time.Sleep(5 * time.Millisecond) // test scaffolding poll, not driver timing
	}
	if got := rec.countType(codexinput.EmitInputStale); got != 2 {
		t.Fatalf("stale emissions = %d (all: %v), want exactly 2", got, rec.types())
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

	ack, err := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("start work")})
	if err != nil {
		t.Fatalf("SubmitInput: %v", err)
	}
	if ack.Outcome != handler.Delivered || ack.Token != "turn_1" {
		t.Fatalf("ack = %+v, want Delivered/turn_1", ack)
	}

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
	sess := spawnTwin(t, "silentthenhappy", codexinput.Config{InputAckTimeout: 1500 * time.Millisecond}, rec)
	port := asPort(t, sess)

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
	sess := spawnTwin(t, "latedistinctturn", codexinput.Config{InputAckTimeout: 30 * time.Second}, rec)
	port := asPort(t, sess)

	ctxA, cancelA := context.WithCancel(context.Background())
	aDone := make(chan error, 1)
	go func() {
		_, err := port.SubmitInput(ctxA, handler.InputRequest{Payload: []byte("caller A")})
		aDone <- err
	}()

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
	sess := spawnTwin(t, "stalethenlate", codexinput.Config{InputAckTimeout: 300 * time.Millisecond}, rec)
	port := asPort(t, sess)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, errA := port.SubmitInput(ctx, handler.InputRequest{Payload: []byte("caller A")})
	if !errors.Is(errA, codexdriver.ErrInputStale) {
		t.Fatalf("caller A err = %v, want ErrInputStale (no anchor within bound)", errA)
	}

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
