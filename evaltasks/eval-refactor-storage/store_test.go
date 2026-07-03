package refactorstorage

import (
	"os"
	"strings"
	"testing"
)

// TestStore is the GOLDEN behavior contract. It exercises the observable public
// API only (via Service) and must pass BOTH before and after the refactor.
func TestStore(t *testing.T) {
	svc := NewService()

	if got := svc.Count(); got != 0 {
		t.Fatalf("fresh store Count()=%d, want 0", got)
	}
	if _, ok := svc.Fetch("missing"); ok {
		t.Fatalf("Fetch(missing) reported present on empty store")
	}

	svc.Put("a", "1")
	svc.Put("b", "2")
	if got, ok := svc.Fetch("a"); !ok || got != "1" {
		t.Fatalf("Fetch(a)=(%q,%v), want (\"1\",true)", got, ok)
	}
	if got := svc.Count(); got != 2 {
		t.Fatalf("Count()=%d after 2 puts, want 2", got)
	}

	// Overwrite must not grow the store.
	svc.Put("a", "9")
	if got, _ := svc.Fetch("a"); got != "9" {
		t.Fatalf("Fetch(a) after overwrite=%q, want \"9\"", got)
	}
	if got := svc.Count(); got != 2 {
		t.Fatalf("Count()=%d after overwrite, want 2", got)
	}

	// Remove is idempotent and only affects the named key.
	svc.Remove("a")
	if _, ok := svc.Fetch("a"); ok {
		t.Fatalf("Fetch(a) present after Remove")
	}
	svc.Remove("a") // no-op, must not panic or change count
	if got := svc.Count(); got != 1 {
		t.Fatalf("Count()=%d after remove, want 1", got)
	}

	if got := svc.Summary(); got != "entries=1" {
		t.Fatalf("Summary()=%q, want \"entries=1\"", got)
	}
}

// TestStoreDecoupled is the held-out decoupling gate (the grep-zero check): the
// caller files must depend on the Store interface, not the concrete memStore
// type. It FAILS on the un-refactored starting code, so it is skipped under
// -short to keep the scenario-gate green (mirrors eval-bugfix-rate-limiter).
func TestStoreDecoupled(t *testing.T) {
	if testing.Short() {
		t.Skip("held-out eval gate — run explicitly without -short")
	}

	// The concrete memStore type may be named ONLY in store.go (its definition).
	// No caller file may reference it.
	callers := []string{"service.go", "report.go"}
	for _, f := range callers {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(src), "memStore") {
			t.Errorf("%s still references the concrete memStore type; "+
				"callers must depend on the Store interface", f)
		}
	}

	// A Store interface must exist and *memStore must satisfy it.
	var _ Store = newMemStore()
}
