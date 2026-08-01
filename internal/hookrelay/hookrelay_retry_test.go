package hookrelay

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestWaitForRetry_StopsAtContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := waitForRetry(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForRetry error = %v, want context canceled", err)
	}
}
