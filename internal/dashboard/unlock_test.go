package dashboard_test

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dashboard"
)

func TestUnlockRoundtrip(t *testing.T) {
	dir := t.TempDir()

	u, err := dashboard.ReadUnlock(dir)
	if err != nil {
		t.Fatalf("ReadUnlock on absent file: unexpected error: %v", err)
	}
	if u != nil {
		t.Fatalf("ReadUnlock on absent file: got %+v, want nil", u)
	}
	if u.Active(time.Now()) {
		t.Error("nil UnlockState.Active: got true, want false")
	}

	until := time.Now().Add(1 * time.Hour).Truncate(time.Second)
	if err := dashboard.WriteUnlock(dir, until, "operator"); err != nil {
		t.Fatalf("WriteUnlock: %v", err)
	}

	got, err := dashboard.ReadUnlock(dir)
	if err != nil {
		t.Fatalf("ReadUnlock: %v", err)
	}
	if got == nil {
		t.Fatal("ReadUnlock: got nil, want non-nil after WriteUnlock")
	}
	if !got.UnlockedUntil.Equal(until) {
		t.Errorf("UnlockedUntil: got %v, want %v", got.UnlockedUntil, until)
	}
	if got.By != "operator" {
		t.Errorf("By: got %q, want %q", got.By, "operator")
	}
	if !got.Active(time.Now()) {
		t.Error("Active: got false, want true (before expiry)")
	}
	if got.Active(until.Add(1 * time.Second)) {
		t.Error("Active: got true, want false (after expiry)")
	}

	if err := dashboard.ClearUnlock(dir); err != nil {
		t.Fatalf("ClearUnlock: %v", err)
	}
	cleared, err := dashboard.ReadUnlock(dir)
	if err != nil {
		t.Fatalf("ReadUnlock after ClearUnlock: %v", err)
	}
	if cleared != nil {
		t.Errorf("ReadUnlock after ClearUnlock: got %+v, want nil", cleared)
	}

	// ClearUnlock on an already-absent file is a no-op, not an error.
	if err := dashboard.ClearUnlock(dir); err != nil {
		t.Fatalf("ClearUnlock on absent file: unexpected error: %v", err)
	}
}
