package queue_test

// w4_regression_test.go — regressions for the Wave-4 mega-review §c queue fixes.
//
// Coverage:
//   - Concurrent Persist for the SAME queue must not collide on the temp
//     filename (previously keyed only on PID → O_EXCL clash → ErrPersistFailed).
//   - AppendItems / HandleQueueAppend must return a typed error, never panic,
//     when GroupIndex is out of range (untrusted decoded JSON).

import (
	"context"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/queue" //nolint:depguard // external test package (queue_test) self-import; queue allow-list omits self (cf. eventbus/mergeq leaf pattern)
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

	// The final file must be present and loadable.
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

			_, _, err := queue.AppendItems(context.Background(), q, idx, []string{"hk-aaa01"}, ledger)
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
