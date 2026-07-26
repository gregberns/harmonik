package supervise

import (
	"sync"
	"testing"
	"time"
)

// TestStopIsConcurrentSafe is the supervisor shutdown contract: several
// independent shutdown paths may request a stop at once, and all must observe
// the same idempotent result rather than racing to close stopCh.
func TestStopIsConcurrentSafe(t *testing.T) {
	s := New(Spec{StopTimeout: time.Second}, nil)

	const callers = 128
	start := make(chan struct{})
	panics := make(chan any, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panics <- r
				}
			}()
			<-start
			if err := s.Stop(0); err != nil {
				t.Errorf("Stop: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(panics)
	for p := range panics {
		t.Errorf("concurrent Stop panicked: %v", p)
	}
}
