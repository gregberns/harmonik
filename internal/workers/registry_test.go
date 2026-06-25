package workers_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/workers"
)

func newRegistryCfg(enabled bool, maxSlots int) workers.Config {
	return workers.Config{
		Version: 1,
		Workers: []workers.Worker{
			{
				Name:      "test-worker",
				Transport: "ssh",
				Host:      "host.example.com",
				OS:        "darwin",
				RepoPath:  "/repo",
				MaxSlots:  maxSlots,
				Enabled:   enabled,
			},
		},
	}
}

func TestRegistry_EnabledWorkerSelected(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(true, 4))
	w := r.SelectWorker()
	if w == nil {
		t.Fatal("SelectWorker: expected non-nil for enabled worker, got nil")
	}
	if w.Name != "test-worker" {
		t.Fatalf("SelectWorker: got name %q, want %q", w.Name, "test-worker")
	}
	r.ReleaseSlot()
}

func TestRegistry_DisabledWorkerNil(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(false, 4))
	if w := r.SelectWorker(); w != nil {
		t.Fatalf("SelectWorker with Enabled=false: expected nil, got %+v", *w)
	}
}

// TestRegistry_SetEnabledByName_FlipsSelectabilityLive proves the operator-facing
// live toggle (hk-xjbvi): a disabled worker is not selectable; SetEnabledByName
// with the matching name flips it selectable immediately (no rebuild); a second
// flip back to false makes it unselectable again.
func TestRegistry_SetEnabledByName_FlipsSelectabilityLive(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(false, 4))
	if w := r.SelectWorker(); w != nil {
		t.Fatalf("precondition: disabled worker must not be selectable, got %+v", *w)
	}

	name, err := r.SetEnabledByName("test-worker", true)
	if err != nil {
		t.Fatalf("SetEnabledByName(test-worker, true): unexpected error %v", err)
	}
	if name != "test-worker" {
		t.Fatalf("SetEnabledByName resolved name = %q, want %q", name, "test-worker")
	}
	w := r.SelectWorker()
	if w == nil {
		t.Fatal("SelectWorker after live enable: expected non-nil, got nil")
	}
	r.ReleaseSlot()

	if _, err := r.SetEnabledByName("test-worker", false); err != nil {
		t.Fatalf("SetEnabledByName(test-worker, false): unexpected error %v", err)
	}
	if w := r.SelectWorker(); w != nil {
		t.Fatalf("SelectWorker after live disable: expected nil, got %+v", *w)
	}
}

// TestRegistry_SetEnabledByName_UnknownName proves an unknown worker name is
// rejected (not a silent flip of the only configured worker).
func TestRegistry_SetEnabledByName_UnknownName(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(true, 4))
	if _, err := r.SetEnabledByName("ghost", false); err == nil {
		t.Fatal("SetEnabledByName(ghost): expected an error for an unknown name, got nil")
	}
	// The real worker is untouched (still selectable).
	if w := r.SelectWorker(); w == nil {
		t.Fatal("SelectWorker: the configured worker must be unaffected by a rejected unknown-name toggle")
	} else {
		r.ReleaseSlot()
	}
}

// TestRegistry_SetEnabledByName_NoWorkerConfigured proves a registry built from
// an empty config rejects any toggle with a clear error.
func TestRegistry_SetEnabledByName_NoWorkerConfigured(t *testing.T) {
	r := workers.NewRegistry(workers.Config{})
	if _, err := r.SetEnabledByName("anything", true); err == nil {
		t.Fatal("SetEnabledByName on empty registry: expected an error, got nil")
	}
}

func TestRegistry_SlotsExhaustedNil(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(true, 2))
	w1 := r.SelectWorker()
	if w1 == nil {
		t.Fatal("slot 1/2: expected non-nil")
	}
	w2 := r.SelectWorker()
	if w2 == nil {
		t.Fatal("slot 2/2: expected non-nil")
	}
	if w3 := r.SelectWorker(); w3 != nil {
		t.Fatalf("slots exhausted: expected nil, got %+v", *w3)
	}
	r.ReleaseSlot()
	r.ReleaseSlot()
}

func TestRegistry_FlipEnabledFlipsResult(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(true, 4))

	w1 := r.SelectWorker()
	if w1 == nil {
		t.Fatal("before disable: expected non-nil")
	}
	r.ReleaseSlot()

	r.SetEnabled(false)
	if w := r.SelectWorker(); w != nil {
		t.Fatalf("after SetEnabled(false): expected nil, got %+v", *w)
	}

	r.SetEnabled(true)
	w3 := r.SelectWorker()
	if w3 == nil {
		t.Fatal("after SetEnabled(true): expected non-nil")
	}
	r.ReleaseSlot()
}

func TestRegistry_NoConfigNil(t *testing.T) {
	r := workers.NewRegistry(workers.Config{})
	if w := r.SelectWorker(); w != nil {
		t.Fatalf("empty config: expected nil, got %+v", *w)
	}
}

func TestRegistry_ReleaseSlotDecrementsInFlight(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(true, 1))
	if r.SelectWorker() == nil {
		t.Fatal("expected slot to be available")
	}
	if r.InFlight() != 1 {
		t.Fatalf("InFlight: got %d, want 1", r.InFlight())
	}
	r.ReleaseSlot()
	if r.InFlight() != 0 {
		t.Fatalf("after ReleaseSlot: InFlight got %d, want 0", r.InFlight())
	}
	// slot freed — SelectWorker should succeed again
	if r.SelectWorker() == nil {
		t.Fatal("after release: expected slot available again")
	}
	r.ReleaseSlot()
}
