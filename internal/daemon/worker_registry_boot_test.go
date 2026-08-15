package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/workers"
)

func TestEnsureWorkerRegistryBuildsOneBootSingleton(t *testing.T) {
	observed := make([]*workers.Registry, 0, 2)
	bs := &bootState{cfg: Config{
		Workers: workers.Config{Version: 1, Workers: []workers.Worker{{
			Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/repo", MaxSlots: 1,
		}}},
		WorkerRegistryObserver: func(registry *workers.Registry) { observed = append(observed, registry) },
	}}
	if err := bs.ensureWorkerRegistry(t.Context()); err != nil {
		t.Fatal(err)
	}
	first := bs.workerRegistry
	if err := bs.ensureWorkerRegistry(t.Context()); err != nil {
		t.Fatal(err)
	}
	if first == nil || bs.workerRegistry != first || len(observed) != 1 || observed[0] != first {
		t.Fatalf("registry singleton = %p then %p, observations %+v", first, bs.workerRegistry, observed)
	}
}

func TestStartupReconcileBuildsWorkerRegistryBeforeReplay(t *testing.T) {
	var observed *workers.Registry
	projectDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectDir, ".harmonik"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".harmonik", "dispatch-intents"), []byte("wrong type"), 0o600); err != nil {
		t.Fatal(err)
	}
	bs := &bootState{cfg: Config{
		ProjectDir: projectDir,
		Workers: workers.Config{Version: 1, Workers: []workers.Worker{{
			Name: "worker-a", Transport: "ssh", Host: "worker.example", RepoPath: "/repo", MaxSlots: 1,
		}}},
		WorkerRegistryObserver: func(registry *workers.Registry) { observed = registry },
	}}
	if err := bs.runStartupReconcile(t.Context(), time.Now(), "main"); err == nil {
		t.Fatal("runStartupReconcile() accepted an invalid dispatch authority root")
	}
	if observed == nil || observed != bs.workerRegistry || !bs.workerRegistryBuilt {
		t.Fatalf("startup registry = %p, observed %p, built %v", bs.workerRegistry, observed, bs.workerRegistryBuilt)
	}
}
