package ratelimiter_test

import (
	"sync"
	"testing"
	"time"

	ratelimiter "github.com/gregberns/harmonik/evaltasks/eval-bugfix-rate-limiter"
)

// TestLimiter is the held-out test suite for the token-bucket rate limiter.
// It passes only after both bugs in limiter.go are fixed:
//
//  1. off-by-one: initial token count must not exceed capacity.
//  2. lost-update race: Allow must be safe under concurrent callers.
//
// Run with: go test -race ./evaltasks/eval-bugfix-rate-limiter/... -run TestLimiter
// Skipped under -short so the scenario-gate does not see the intentional failure.
func TestLimiter(t *testing.T) {
	if testing.Short() {
		t.Skip("held-out eval test — run explicitly without -short and with -race")
	}

	t.Run("burst_does_not_exceed_capacity", func(t *testing.T) {
		const cap = 5
		// Slow refill ensures no tokens are added during the burst.
		l := ratelimiter.New(cap, 1, time.Hour)

		got := 0
		for i := 0; i < cap+10; i++ {
			if l.Allow() {
				got++
			}
		}
		if got > cap {
			t.Errorf("Allow() returned true %d times; capacity is %d, burst must not exceed it", got, cap)
		}
	})

	t.Run("concurrent_allow_no_race", func(t *testing.T) {
		// Designed for -race: any unsynchronised access inside Allow triggers the detector.
		l := ratelimiter.New(1000, 100, time.Millisecond)

		const goroutines = 50
		const callsEach = 40
		var wg sync.WaitGroup
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < callsEach; j++ {
					l.Allow()
				}
			}()
		}
		wg.Wait()
	})
}
