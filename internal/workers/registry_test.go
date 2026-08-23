package workers_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workers"
)

func boundRunID(t *testing.T, value string) core.RunID {
	t.Helper()
	var runID core.RunID
	if err := runID.UnmarshalText([]byte(value)); err != nil {
		t.Fatal(err)
	}
	return runID
}

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
	if r.SelectWorker() == nil {
		t.Fatal("after release: expected slot available again")
	}
	r.ReleaseSlot()
}

func TestRegistry_AcquireBoundWorkerOwnsOneSlotPerRun(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(true, 1))
	route := workers.BoundWorker{
		Name: "test-worker", Transport: "ssh", Host: "host.example.com", RepositoryPath: "/repo",
	}
	firstRun := boundRunID(t, "0197d200-0000-7000-8000-000000000001")
	secondRun := boundRunID(t, "0197d200-0000-7000-8000-000000000002")
	first, err := r.AcquireBoundWorker(firstRun, route)
	if err != nil || first == nil {
		t.Fatalf("first acquire = (%+v, %v)", first, err)
	}
	replay, err := r.AcquireBoundWorker(firstRun, route)
	if err != nil || replay == nil || r.InFlight() != 1 {
		t.Fatalf("replay acquire = (%+v, %v), in-flight %d", replay, err, r.InFlight())
	}
	blocked, err := r.AcquireBoundWorker(secondRun, route)
	if err != nil || blocked != nil {
		t.Fatalf("full acquire = (%+v, %v)", blocked, err)
	}
	if !r.ReleaseBoundWorker(firstRun) || r.ReleaseBoundWorker(firstRun) || r.InFlight() != 0 {
		t.Fatalf("release state: in-flight %d", r.InFlight())
	}
}

func TestRegistry_AcquireBoundWorkerFailsClosedOnRouteChange(t *testing.T) {
	base := workers.BoundWorker{
		Name: "test-worker", Transport: "ssh", Host: "host.example.com", RepositoryPath: "/repo",
	}
	tests := []struct {
		name   string
		mutate func(*workers.BoundWorker)
	}{
		{name: "name", mutate: func(v *workers.BoundWorker) { v.Name = "other" }},
		{name: "transport", mutate: func(v *workers.BoundWorker) { v.Transport = "other" }},
		{name: "host", mutate: func(v *workers.BoundWorker) { v.Host = "other.example" }},
		{name: "repository", mutate: func(v *workers.BoundWorker) { v.RepositoryPath = "/other" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := workers.NewRegistry(newRegistryCfg(true, 1))
			route := base
			tc.mutate(&route)
			if worker, err := r.AcquireBoundWorker(boundRunID(t, "0197d200-0000-7000-8000-000000000001"), route); err == nil || worker != nil || r.InFlight() != 0 {
				t.Fatalf("AcquireBoundWorker() = (%+v, %v), in-flight %d", worker, err, r.InFlight())
			}
		})
	}
}

func TestRegistry_AcquireBoundWorkerLeavesDisabledAndFullRunsPending(t *testing.T) {
	route := workers.BoundWorker{
		Name: "test-worker", Transport: "ssh", Host: "host.example.com", RepositoryPath: "/repo",
	}
	disabled := workers.NewRegistry(newRegistryCfg(false, 1))
	if worker, err := disabled.AcquireBoundWorker(boundRunID(t, "0197d200-0000-7000-8000-000000000001"), route); err != nil || worker != nil || disabled.InFlight() != 0 {
		t.Fatalf("disabled acquire = (%+v, %v), in-flight %d", worker, err, disabled.InFlight())
	}
	full := workers.NewRegistry(newRegistryCfg(true, 1))
	if full.SelectWorker() == nil {
		t.Fatal("fill slot")
	}
	if worker, err := full.AcquireBoundWorker(boundRunID(t, "0197d200-0000-7000-8000-000000000002"), route); err != nil || worker != nil || full.InFlight() != 1 {
		t.Fatalf("full acquire = (%+v, %v), in-flight %d", worker, err, full.InFlight())
	}
	full.ReleaseSlot()
}

func TestRegistry_AcquireBoundWorkerRejectsInvalidRunID(t *testing.T) {
	r := workers.NewRegistry(newRegistryCfg(true, 1))
	route := workers.BoundWorker{
		Name: "test-worker", Transport: "ssh", Host: "host.example.com", RepositoryPath: "/repo",
	}
	if worker, err := r.AcquireBoundWorker(core.RunID{}, route); err == nil || worker != nil || r.InFlight() != 0 {
		t.Fatalf("invalid acquire = (%+v, %v), in-flight %d", worker, err, r.InFlight())
	}
}

func TestRegistry_SelectBoundWorkerOwnsOneSlotPerRun(t *testing.T) {
	r := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
		Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/project",
		Enabled: true, MaxSlots: 1,
	}}})
	runID := boundRunID(t, "0197d200-0000-7000-8000-000000000001")
	first, err := r.SelectBoundWorker(runID, "")
	if err != nil || first == nil || first.Name != "worker-a" || r.InFlight() != 1 {
		t.Fatalf("first selection = (%+v, %v), in-flight %d", first, err, r.InFlight())
	}
	replay, err := r.SelectBoundWorker(runID, "worker-a")
	if err != nil || replay == nil || replay.Name != first.Name || r.InFlight() != 1 {
		t.Fatalf("repeat selection = (%+v, %v), in-flight %d", replay, err, r.InFlight())
	}
	other, err := r.SelectBoundWorker(boundRunID(t, "0197d200-0000-7000-8000-000000000002"), "")
	if err != nil || other != nil || r.InFlight() != 1 {
		t.Fatalf("full selection = (%+v, %v), in-flight %d", other, err, r.InFlight())
	}
}

func TestRegistry_SelectBoundWorkerPreservesTargetAndFallbackRules(t *testing.T) {
	newRegistry := func(enabled bool) *workers.Registry {
		return workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
			Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/project",
			Enabled: enabled, MaxSlots: 1,
		}}})
	}
	tests := []struct {
		name    string
		target  string
		enabled bool
		want    bool
	}{
		{name: "default worker", enabled: true, want: true},
		{name: "named worker", target: "worker-a", enabled: true, want: true},
		{name: "unknown target falls back", target: "worker-b", enabled: true},
		{name: "disabled worker falls back", enabled: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry(tc.enabled)
			got, err := r.SelectBoundWorker(boundRunID(t, "0197d200-0000-7000-8000-000000000003"), tc.target)
			if err != nil {
				t.Fatal(err)
			}
			if (got != nil) != tc.want {
				t.Fatalf("SelectBoundWorker() = %+v, want selected %v", got, tc.want)
			}
			wantInFlight := 0
			if tc.want {
				wantInFlight = 1
			}
			if r.InFlight() != wantInFlight {
				t.Fatalf("in-flight = %d, want %d", r.InFlight(), wantInFlight)
			}
		})
	}
}

func TestRegistry_SelectBoundWorkerRejectsInvalidRunBeforeSlot(t *testing.T) {
	r := workers.NewRegistry(workers.Config{Workers: []workers.Worker{{Name: "worker-a", Enabled: true, MaxSlots: 1}}})
	if got, err := r.SelectBoundWorker(core.RunID{}, ""); err == nil || got != nil || r.InFlight() != 0 {
		t.Fatalf("SelectBoundWorker() = (%+v, %v), in-flight %d", got, err, r.InFlight())
	}
}

func TestRegistry_SelectBoundWorkerRejectsInvalidRoutesBeforeSlot(t *testing.T) {
	base := workers.Worker{
		Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/project",
		Enabled: true, MaxSlots: 1,
	}
	tests := []struct {
		name   string
		mutate func(*workers.Worker)
	}{
		{name: "name", mutate: func(worker *workers.Worker) { worker.Name = "" }},
		{name: "transport", mutate: func(worker *workers.Worker) { worker.Transport = "https" }},
		{name: "host", mutate: func(worker *workers.Worker) { worker.Host = "" }},
		{name: "repository", mutate: func(worker *workers.Worker) { worker.RepoPath = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			worker := base
			tc.mutate(&worker)
			r := workers.NewRegistry(workers.Config{Workers: []workers.Worker{worker}})
			got, err := r.SelectBoundWorker(boundRunID(t, "0197d200-0000-7000-8000-000000000004"), "")
			if err == nil || got != nil || r.InFlight() != 0 {
				t.Fatalf("SelectBoundWorker() = (%+v, %v), in-flight %d", got, err, r.InFlight())
			}
		})
	}
}

func TestRegistry_BoundSelectionAndAcquisitionShareOneOwnership(t *testing.T) {
	newRegistry := func() *workers.Registry {
		return workers.NewRegistry(workers.Config{Workers: []workers.Worker{{
			Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/srv/project",
			Enabled: true, MaxSlots: 1,
		}}})
	}
	route := workers.BoundWorker{
		Name: "worker-a", Transport: "ssh", Host: "worker.example", RepositoryPath: "/srv/project",
	}
	runID := boundRunID(t, "0197d200-0000-7000-8000-000000000005")
	for _, tc := range []struct {
		name  string
		first func(*workers.Registry) (*workers.Worker, error)
		next  func(*workers.Registry) (*workers.Worker, error)
	}{
		{
			name:  "select then acquire",
			first: func(r *workers.Registry) (*workers.Worker, error) { return r.SelectBoundWorker(runID, "") },
			next:  func(r *workers.Registry) (*workers.Worker, error) { return r.AcquireBoundWorker(runID, route) },
		},
		{
			name:  "acquire then select",
			first: func(r *workers.Registry) (*workers.Worker, error) { return r.AcquireBoundWorker(runID, route) },
			next:  func(r *workers.Registry) (*workers.Worker, error) { return r.SelectBoundWorker(runID, "worker-a") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRegistry()
			first, err := tc.first(r)
			if err != nil || first == nil || r.InFlight() != 1 {
				t.Fatalf("first = (%+v, %v), in-flight %d", first, err, r.InFlight())
			}
			next, err := tc.next(r)
			if err != nil || next == nil || r.InFlight() != 1 {
				t.Fatalf("next = (%+v, %v), in-flight %d", next, err, r.InFlight())
			}
			if !r.ReleaseBoundWorker(runID) || r.InFlight() != 0 {
				t.Fatalf("release left in-flight %d", r.InFlight())
			}
			reused, err := r.SelectBoundWorker(boundRunID(t, "0197d200-0000-7000-8000-000000000006"), "")
			if err != nil || reused == nil || r.InFlight() != 1 {
				t.Fatalf("capacity reuse = (%+v, %v), in-flight %d", reused, err, r.InFlight())
			}
		})
	}
}
