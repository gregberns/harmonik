package codexdriver_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
)

type fakeInputPort struct {
	gate    chan struct{} // received-from once per SubmitInput; nil ⇒ never blocks
	started chan struct{} // non-blocking signal at the top of each SubmitInput; nil ⇒ off

	mu    sync.Mutex
	order []string
	err   error
}

func (f *fakeInputPort) SubmitInput(ctx context.Context, req handler.InputRequest) (handler.Ack, error) {
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default: // non-blocking: only the first waiter needs the signal
		}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return handler.Ack{}, ctx.Err()
		}
	}
	f.mu.Lock()
	f.order = append(f.order, string(req.Payload))
	seq := uint64(len(f.order))
	f.mu.Unlock()
	return handler.Ack{Outcome: handler.Delivered, Seq: seq}, f.err
}

func (f *fakeInputPort) CloseInput(context.Context) error { return nil }

func (f *fakeInputPort) snapshotOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.order...)
}

// TestBoundedInputQueue_FIFODelivery proves submissions reach the port in strict
// enqueue order and each caller gets its own Ack.
func TestBoundedInputQueue_FIFODelivery_HK160YB(t *testing.T) {
	port := &fakeInputPort{} // no gate: SubmitInput returns immediately
	q := codexdriver.NewBoundedInputQueue(port, 8)
	defer q.Close()

	var chans []<-chan codexdriver.QueuedResult
	for i := 0; i < 5; i++ {
		ch, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte(fmt.Sprintf("m%d", i))})
		if err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
		chans = append(chans, ch)
	}
	for i, ch := range chans {
		select {
		case res := <-ch:
			if res.Err != nil {
				t.Fatalf("submission %d err: %v", i, res.Err)
			}
			if res.Ack.Outcome != handler.Delivered {
				t.Fatalf("submission %d outcome = %v, want Delivered", i, res.Ack.Outcome)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("submission %d never resolved", i)
		}
	}
	want := []string{"m0", "m1", "m2", "m3", "m4"}
	got := port.snapshotOrder()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("delivery order = %v, want %v (FIFO violated)", got, want)
	}
}

// TestBoundedInputQueue_CapEnforced proves the backlog is bounded: with one turn
// held in flight and the buffer full, the next Enqueue returns ErrQueueFull
// rather than parking an unbounded goroutine.
func TestBoundedInputQueue_CapEnforced_HK160YB(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	port := &fakeInputPort{gate: gate, started: started}
	const capacity = 2
	q := codexdriver.NewBoundedInputQueue(port, capacity)
	defer func() {
		close(gate) // release the in-flight + buffered submits so Close can drain
		q.Close()
	}()

	if _, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte("inflight")}); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("drainer never dispatched the in-flight item")
	}

	for i := 0; i < capacity; i++ {
		if _, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte(fmt.Sprintf("b%d", i))}); err != nil {
			t.Fatalf("Enqueue b%d within capacity: %v", i, err)
		}
	}
	_, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte("overflow")})
	if !errors.Is(err, codexdriver.ErrQueueFull) {
		t.Fatalf("Enqueue past capacity err = %v, want ErrQueueFull", err)
	}
}

// TestBoundedInputQueue_CtxCancelShortCircuits proves a submission whose context
// is already cancelled when the drainer reaches it resolves with the ctx error
// and never opens a turn on the port.
func TestBoundedInputQueue_CtxCancelShortCircuits_HK160YB(t *testing.T) {
	gate := make(chan struct{}, 8)
	port := &fakeInputPort{gate: gate}
	q := codexdriver.NewBoundedInputQueue(port, 8)
	defer q.Close()

	gate <- struct{}{} // allow exactly the first submit to proceed after it starts
	firstCtx := context.Background()
	if _, err := q.Enqueue(firstCtx, handler.InputRequest{Payload: []byte("first")}); err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	ch, err := q.Enqueue(cancelCtx, handler.InputRequest{Payload: []byte("cancelled")})
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	cancel() // cancel before the drainer reaches it

	select {
	case res := <-ch:
		if !errors.Is(res.Err, context.Canceled) {
			t.Fatalf("cancelled submission err = %v, want context.Canceled", res.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled submission never resolved")
	}
	for _, p := range port.snapshotOrder() {
		if p == "cancelled" {
			t.Fatal("cancelled submission was delivered to the port — short-circuit failed")
		}
	}
}

// TestBoundedInputQueue_EnqueueAfterClose proves Enqueue after Close is rejected
// with ErrQueueClosed and Close is idempotent.
func TestBoundedInputQueue_EnqueueAfterClose_HK160YB(t *testing.T) {
	port := &fakeInputPort{}
	q := codexdriver.NewBoundedInputQueue(port, 4)
	q.Close()
	q.Close() // idempotent; must not panic

	_, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte("x")})
	if !errors.Is(err, codexdriver.ErrQueueClosed) {
		t.Fatalf("Enqueue after Close err = %v, want ErrQueueClosed", err)
	}
}

// TestBoundedInputQueue_CloseDrainsBuffered proves the documented graceful-drain
// semantics: submissions already buffered when Close is called are still
// delivered to the port (in FIFO order), not rejected. Close blocks until the
// drainer has processed them.
func TestBoundedInputQueue_CloseDrainsBuffered_HK160YB(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 1)
	port := &fakeInputPort{gate: gate, started: started}
	q := codexdriver.NewBoundedInputQueue(port, 8)

	if _, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte("d0")}); err != nil {
		t.Fatalf("Enqueue d0: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("drainer never dispatched the in-flight item")
	}
	for _, p := range []string{"d1", "d2"} {
		if _, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte(p)}); err != nil {
			t.Fatalf("Enqueue %s: %v", p, err)
		}
	}

	closed := make(chan struct{})
	go func() { q.Close(); close(closed) }()
	close(gate) // let every (in-flight + buffered) submit proceed

	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the buffered items drained")
	}
	want := []string{"d0", "d1", "d2"}
	got := port.snapshotOrder()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("delivered %v, want %v — buffered-at-Close items were not gracefully drained in FIFO order", got, want)
	}
}

// TestBoundedInputQueue_ConcurrentEnqueue proves concurrent Enqueue is safe and
// every accepted submission is delivered exactly once (no loss, no dup).
func TestBoundedInputQueue_ConcurrentEnqueue_HK160YB(t *testing.T) {
	port := &fakeInputPort{}
	q := codexdriver.NewBoundedInputQueue(port, 64)
	defer q.Close()

	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch, err := q.Enqueue(context.Background(), handler.InputRequest{Payload: []byte(fmt.Sprintf("c%d", i))})
			if err != nil {
				return // ErrQueueFull is acceptable under contention; count only accepted
			}
			mu.Lock()
			accepted++
			mu.Unlock()
			select {
			case <-ch:
			case <-time.After(2 * time.Second):
				t.Errorf("submission %d never resolved", i)
			}
		}(i)
	}
	wg.Wait()
	if got := len(port.snapshotOrder()); got != accepted {
		t.Fatalf("delivered %d submissions, accepted %d (loss or duplication)", got, accepted)
	}
}
