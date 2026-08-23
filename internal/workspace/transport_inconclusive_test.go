package workspace

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"testing"
	"time"
)

type exitCodeRunner struct{ code int }

func (r exitCodeRunner) Command(ctx context.Context, _ string, _ ...string) *exec.Cmd {
	//nolint:gosec // G204: fixed shell literal, test-controlled exit code
	return exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("exit %d", r.code))
}

// TestH4_ReadReviewVerdictVia_TransportFailure_Inconclusive verifies an ssh
// transport failure (exit 255) surfaces ErrRemoteTransport, not absent.
func TestH4_ReadReviewVerdictVia_TransportFailure_Inconclusive(t *testing.T) {
	origBudget, origBase := reviewVerdictRemoteRetryBudget, reviewVerdictRemoteBaseBackoff
	reviewVerdictRemoteRetryBudget = 40 * time.Millisecond
	reviewVerdictRemoteBaseBackoff = 5 * time.Millisecond
	t.Cleanup(func() {
		reviewVerdictRemoteRetryBudget = origBudget
		reviewVerdictRemoteBaseBackoff = origBase
	})

	v, err := ReadReviewVerdictVia(context.Background(), exitCodeRunner{code: 255}, t.TempDir())
	if v != nil {
		t.Errorf("verdict = %+v; want nil on transport failure", v)
	}
	if !errors.Is(err, ErrRemoteTransport) {
		t.Errorf("err = %v; want ErrRemoteTransport (inconclusive, not absent)", err)
	}
}

// TestH4_ReadReviewVerdictVia_ConfirmedAbsent_NilNil verifies a non-transport cat
// failure (exit 1: no such file) is still treated as confirmed-absent.
func TestH4_ReadReviewVerdictVia_ConfirmedAbsent_NilNil(t *testing.T) {
	v, err := ReadReviewVerdictVia(context.Background(), exitCodeRunner{code: 1}, t.TempDir())
	if v != nil || err != nil {
		t.Errorf("(v,err) = (%+v,%v); want (nil,nil) for a confirmed-absent verdict", v, err)
	}
}

// TestH5_ReadAutoStatusMarkerVia_TransportFailure_Inconclusive verifies an ssh
// transport failure surfaces ErrRemoteTransport on the marker path too.
func TestH5_ReadAutoStatusMarkerVia_TransportFailure_Inconclusive(t *testing.T) {
	t.Parallel()

	m, err := ReadAutoStatusMarkerVia(context.Background(), exitCodeRunner{code: 255}, t.TempDir())
	if m != nil {
		t.Errorf("marker = %+v; want nil on transport failure", m)
	}
	if !errors.Is(err, ErrRemoteTransport) {
		t.Errorf("err = %v; want ErrRemoteTransport (inconclusive, not absent)", err)
	}
}

// TestH5_ReadAutoStatusMarkerVia_ConfirmedAbsent_NilNil verifies a non-transport
// cat failure is still confirmed-absent.
func TestH5_ReadAutoStatusMarkerVia_ConfirmedAbsent_NilNil(t *testing.T) {
	t.Parallel()

	m, err := ReadAutoStatusMarkerVia(context.Background(), exitCodeRunner{code: 1}, t.TempDir())
	if m != nil || err != nil {
		t.Errorf("(m,err) = (%+v,%v); want (nil,nil) for a confirmed-absent marker", m, err)
	}
}
