package core

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// TestTransitionIDGenerator_EM018a_StrictlyMonotonic verifies that 10 consecutive
// calls to Next() produce strictly increasing TransitionID values when their 16-byte
// big-endian representations are compared (EM-018a, execution-model.md §4.4).
func TestTransitionIDGenerator_EM018a_StrictlyMonotonic(t *testing.T) {
	g := NewTransitionIDGenerator()

	const n = 10
	ids := make([]TransitionID, n)
	for i := range ids {
		id, err := g.Next()
		if err != nil {
			t.Fatalf("Next() call %d: %v", i, err)
		}
		ids[i] = id
	}

	for i := 1; i < n; i++ {
		prev := uuid.UUID(ids[i-1])
		curr := uuid.UUID(ids[i])
		if bytes.Compare(prev[:], curr[:]) >= 0 {
			t.Errorf("EM-018a: ids[%d] (%v) >= ids[%d] (%v); must be strictly less",
				i-1, ids[i-1], i, ids[i])
		}
	}
}

// TestTransitionIDGenerator_EM018a_SameMillisecondMonotonic verifies that when the
// underlying clock returns the same UUIDv7 value twice (simulating same-millisecond
// emission), the generator still produces strictly monotonic TransitionID values
// via the increment path (EM-018a, execution-model.md §4.4).
func TestTransitionIDGenerator_EM018a_SameMillisecondMonotonic(t *testing.T) {
	fixed, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7(): %v", err)
	}

	// newV7 always returns the same UUID — simulates same-millisecond clock.
	callCount := 0
	g := &TransitionIDGenerator{
		newV7: func() (uuid.UUID, error) {
			callCount++
			return fixed, nil
		},
	}

	id1, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a: first Next(): %v", err)
	}
	id2, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a: second Next(): %v", err)
	}

	u1 := uuid.UUID(id1)
	u2 := uuid.UUID(id2)
	if bytes.Compare(u1[:], u2[:]) >= 0 {
		t.Errorf("EM-018a: same-millisecond: id1 (%v) >= id2 (%v); must be strictly less", id1, id2)
	}

	// Confirm the second value is exactly id1 + 1.
	expected := increment128(u1)
	if u2 != expected {
		t.Errorf("EM-018a: same-millisecond: id2 = %v, want %v (id1+1)", id2, TransitionID(expected))
	}
}

// TestTransitionIDGenerator_EM018a_ConcurrentMonotonic verifies that N concurrent
// goroutines each calling Next() K times collectively produce a set of strictly
// monotonic, duplicate-free TransitionID values (EM-018a, execution-model.md §4.4).
func TestTransitionIDGenerator_EM018a_ConcurrentMonotonic(t *testing.T) {
	const (
		goroutines = 8
		callsEach  = 100
		total      = goroutines * callsEach
	)

	g := NewTransitionIDGenerator()

	// errCh carries the first Next() error from any goroutine back to the test.
	errCh := make(chan error, goroutines)

	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		all = make([]TransitionID, 0, total)
	)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make([]TransitionID, 0, callsEach)
			for j := 0; j < callsEach; j++ {
				id, err := g.Next()
				if err != nil {
					// t.Fatal cannot be called from a non-test goroutine;
					// send the error to the main goroutine via errCh.
					errCh <- fmt.Errorf("EM-018a: goroutine Next() call %d: %w", j, err)
					return
				}
				local = append(local, id)
			}
			mu.Lock()
			all = append(all, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("EM-018a: concurrent: goroutine error: %v", err)
	}

	if len(all) != total {
		t.Fatalf("EM-018a: concurrent: got %d results, want %d", len(all), total)
	}

	// Sort lexicographically to check for duplicates across all goroutines.
	sort.Slice(all, func(i, j int) bool {
		ui := uuid.UUID(all[i])
		uj := uuid.UUID(all[j])
		return bytes.Compare(ui[:], uj[:]) < 0
	})

	for i := 1; i < len(all); i++ {
		prev := uuid.UUID(all[i-1])
		curr := uuid.UUID(all[i])
		if bytes.Compare(prev[:], curr[:]) >= 0 {
			t.Errorf("EM-018a: concurrent: duplicate or out-of-order at position %d: prev=%v curr=%v",
				i, all[i-1], all[i])
		}
	}
}

// TestTransitionIDGenerator_EM018a_VersionPreservedNormalCase verifies that in the
// normal (no clock rollback) case, the TransitionID produced by Next() has version 7
// (EM-018a, execution-model.md §4.4).
func TestTransitionIDGenerator_EM018a_VersionPreservedNormalCase(t *testing.T) {
	g := NewTransitionIDGenerator()

	id, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a: Next(): %v", err)
	}

	if !id.IsUUIDv7() {
		t.Errorf("EM-018a: version-preserved: IsUUIDv7() = false for first Next() call (%v); expected true in normal (no-rollback) case", id)
	}
}

// TestTransitionIDGenerator_EM018a_ClockRollbackMonotonic verifies that when the
// underlying clock returns a value LESS than the last issued value (clock rollback),
// the generator still produces strictly monotonic TransitionID values via the
// increment path (EM-018a, execution-model.md §4.4, RFC 9562 §6.2 method 1).
func TestTransitionIDGenerator_EM018a_ClockRollbackMonotonic(t *testing.T) {
	hi, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7() hi: %v", err)
	}
	// lo is hi - 1: guaranteed to be strictly less than hi.
	lo := hi
	for i := 15; i >= 0; i-- {
		if lo[i] > 0 {
			lo[i]--
			break
		}
		lo[i] = 0xff
	}

	// First call returns hi; second call returns lo (simulated clock rollback).
	seq := []uuid.UUID{hi, lo}
	idx := 0
	g := &TransitionIDGenerator{
		newV7: func() (uuid.UUID, error) {
			v := seq[idx%len(seq)]
			idx++
			return v, nil
		},
	}

	id1, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a: clock-rollback: first Next(): %v", err)
	}
	id2, err := g.Next()
	if err != nil {
		t.Fatalf("EM-018a: clock-rollback: second Next(): %v", err)
	}

	u1 := uuid.UUID(id1)
	u2 := uuid.UUID(id2)
	if bytes.Compare(u1[:], u2[:]) >= 0 {
		t.Errorf("EM-018a: clock-rollback: id1 (%v) >= id2 (%v); must be strictly less", id1, id2)
	}
}

// TestTransitionIDGenerator_EM018a_NewV7Error verifies that Next() propagates an
// error returned by the underlying UUIDv7 generator (EM-018a, execution-model.md §4.4).
func TestTransitionIDGenerator_EM018a_NewV7Error(t *testing.T) {
	sentinel := errors.New("EM-018a: synthetic clock error")
	g := &TransitionIDGenerator{
		newV7: func() (uuid.UUID, error) {
			return uuid.UUID{}, sentinel
		},
	}

	_, err := g.Next()
	if err == nil {
		t.Fatal("EM-018a: Next() returned nil error; expected error propagation from newV7")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("EM-018a: Next() error = %v, want %v", err, sentinel)
	}
}
