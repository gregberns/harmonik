package queue_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

// TestPersistConcurrentSameQueueNoTempCollision exercises the real unlocked
// path: many goroutines persisting the SAME queue concurrently. With the old
// PID-only temp name, two racing writers derived an identical tmpPath and the
// second O_EXCL create failed with ErrPersistFailed. The per-write unique
// suffix must eliminate that collision. Run under -race.
func TestPersistConcurrentSameQueueNoTempCollision(t *testing.T) {
	t.Parallel()

	projectDir := persistFixtureProjectDir(t)
	ctx := context.Background()

	const goroutines = 32
	var wg sync.WaitGroup
	errCh := make(chan error, goroutines)
	start := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q := persistFixtureQueue() // independent copy per goroutine
			<-start                    // maximise the race window
			if err := queue.Persist(ctx, projectDir, &q); err != nil {
				errCh <- err
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatalf("concurrent Persist collided: %v", err)
	}

	got, err := queue.Load(ctx, projectDir, queue.QueueNameMain)
	if err != nil {
		t.Fatalf("Load after concurrent Persist: %v", err)
	}
	if got == nil {
		t.Fatal("Load after concurrent Persist returned nil queue")
	}
}

// TestAppendItemsGroupIndexOutOfRangeReturnsError verifies AppendItems returns a
// typed validation error (never an out-of-range panic) for GroupIndex values
// that fall outside the group slice — including negative indices from malformed
// decoded JSON.
func TestAppendItemsGroupIndexOutOfRangeReturnsError(t *testing.T) {
	t.Parallel()

	for _, idx := range []int{-1, 1, 99} {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			q := appendFixtureStreamQueue(queue.GroupStatusActive, nil) // single group at index 0
			ledger := appendFixtureOpenLedger("hk-aaa01")

			_, _, err := queue.AppendItems(context.Background(), q, idx, []string{"hk-aaa01"}, ledger, time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC))
			if err == nil {
				t.Fatalf("GroupIndex %d: expected error, got nil", idx)
			}
			if !queue.IsValidationError(err) {
				t.Fatalf("GroupIndex %d: expected a ValidationError, got %T: %v", idx, err, err)
			}
			if got := queue.ValidationReason(err); got != queue.ReasonAppendTargetInvalid {
				t.Fatalf("GroupIndex %d: reason = %q, want %q", idx, got, queue.ReasonAppendTargetInvalid)
			}
		})
	}
}
