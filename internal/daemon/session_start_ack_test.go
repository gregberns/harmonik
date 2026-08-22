package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workers"
)

type recordingSessionStartAcknowledgementHandler struct {
	payload json.RawMessage
}

func (h *recordingSessionStartAcknowledgementHandler) HandleSessionStartAcknowledgement(
	_ context.Context,
	payload json.RawMessage,
) (json.RawMessage, error) {
	h.payload = append(h.payload[:0], payload...)
	return json.RawMessage(`{"acknowledged":true}`), nil
}

func TestSessionStartAcknowledgementRoutesPayloadThroughSocketControlChannel(t *testing.T) {
	handler := &recordingSessionStartAcknowledgementHandler{}
	router := buildSocketRouter(&socketDispatch{sessionStarth: handler})
	result := router.Dispatch(t.Context(), "session-start-ack", json.RawMessage(
		`{"op":"session-start-ack","payload":{"schema_version":1}}`,
	))
	if !result.OK || string(result.Payload) != `{"acknowledged":true}` {
		t.Fatalf("result = %+v; want explicit success", result)
	}
	if string(handler.payload) != `{"schema_version":1}` {
		t.Fatalf("handler payload = %s; want exact request payload", handler.payload)
	}
}

func TestSessionStartAcknowledgementHandlerInstallsExactReceipt(t *testing.T) {
	projectDir := t.TempDir()
	intent, record, receipt := sessionStartAckFixture(t)
	persistSessionStartAuthority(t, projectDir, intent, record)
	handler := sessionStartAcknowledgementHandler{
		projectDir: projectDir,
		resolveAdapter: func(runpkg.ExecutionLocation) (ltmux.Adapter, error) {
			return &dispatchTargetProbeAdapter{probe: sessionStartAckProbe(intent)}, nil
		},
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	result, err := handler.HandleSessionStartAcknowledgement(t.Context(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != `{"acknowledged":true}` {
		t.Fatalf("result = %s; want explicit acknowledgement", result)
	}
	got, err := dispatchstore.New(projectDir).LoadSessionStartReceipt(intent.Binding.RunID)
	if err != nil || got != receipt {
		t.Fatalf("receipt = (%+v, %v), want exact", got, err)
	}
}

func TestSessionStartAcknowledgementHandlerRejectsInvalidPayloadBeforeIO(t *testing.T) {
	projectDir := t.TempDir()
	handler := sessionStartAcknowledgementHandler{projectDir: projectDir}
	if _, err := handler.HandleSessionStartAcknowledgement(t.Context(), json.RawMessage(`{"schema_version":1}`)); err == nil {
		t.Fatal("invalid receipt returned nil error")
	}
	root := filepath.Join(projectDir, ".harmonik", "dispatch-session-starts")
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("receipt IO occurred: %v", err)
	}
}

func TestSessionStartAcknowledgementResolvesRemoteWorkerAdapterFromDurableLocation(t *testing.T) {
	projectDir := t.TempDir()
	intent, _, receipt := sessionStartAckFixture(t)
	startedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	base, err := runpkg.NewDispatchRecord(intent.Binding, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	located, err := base.BindLocation(runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionRemote, WorkerName: "worker-a", Transport: "ssh",
		Host: "worker.example", RepositoryPath: "/srv/harmonik/worker-a/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := located.BindSession(intent.Handoff.SessionName, intent.Handoff.WindowName)
	if err != nil {
		t.Fatal(err)
	}
	persistSessionStartAuthority(t, projectDir, intent, record)

	local := &dispatchTargetProbeAdapter{probe: ltmux.TargetProbe{Status: ltmux.TargetProbeSessionAbsent}}
	remote := &dispatchTargetProbeAdapter{probe: sessionStartAckProbe(intent)}
	remoteFactoryCalls := 0
	resolver := newSessionStartAdapterResolverWithFactory(local, workers.Config{Workers: []workers.Worker{{
		Name: "worker-a", Transport: "ssh", Host: "worker.example",
		RepoPath: "/srv/harmonik/worker-a/project",
	}}}, func(worker workers.Worker) ltmux.Adapter {
		remoteFactoryCalls++
		if worker.Name != "worker-a" || worker.Host != "worker.example" {
			t.Fatalf("worker = %+v; want durable location worker", worker)
		}
		return remote
	})
	if err := acknowledgeSessionStartWithResolver(t.Context(), projectDir, resolver, receipt); err != nil {
		t.Fatal(err)
	}
	if remoteFactoryCalls != 1 {
		t.Fatalf("remote adapter calls = %d; want 1", remoteFactoryCalls)
	}
	got, err := dispatchstore.New(projectDir).LoadSessionStartReceipt(intent.Binding.RunID)
	if err != nil || got != receipt {
		t.Fatalf("receipt = (%+v, %v), want exact remote acknowledgement", got, err)
	}
}

func TestAcknowledgeSessionStartInstallsReceiptAfterExactLiveProof(t *testing.T) {
	projectDir := t.TempDir()
	intent, record, receipt := sessionStartAckFixture(t)
	persistSessionStartAuthority(t, projectDir, intent, record)
	adapter := &dispatchTargetProbeAdapter{probe: sessionStartAckProbe(intent)}
	if err := acknowledgeSessionStartLocally(t, projectDir, adapter, receipt); err != nil {
		t.Fatal(err)
	}
	got, err := dispatchstore.New(projectDir).LoadSessionStartReceipt(intent.Binding.RunID)
	if err != nil || got != receipt {
		t.Fatalf("receipt = (%+v, %v), want exact", got, err)
	}
}

func TestAcknowledgeSessionStartRejectsBeforeReceiptIO(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*dispatch.SessionStartReceipt, *ltmux.TargetProbe)
	}{
		{name: "receipt", mutate: func(r *dispatch.SessionStartReceipt, _ *ltmux.TargetProbe) {
			r.WindowName = "other"
		}},
		{name: "target absent", mutate: func(_ *dispatch.SessionStartReceipt, p *ltmux.TargetProbe) {
			*p = ltmux.TargetProbe{Status: ltmux.TargetProbeSessionAbsent}
		}},
		{name: "target dead", mutate: func(_ *dispatch.SessionStartReceipt, p *ltmux.TargetProbe) {
			p.PaneDead = "1"
		}},
		{name: "target conflict", mutate: func(_ *dispatch.SessionStartReceipt, p *ltmux.TargetProbe) {
			p.WindowName = "other"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			intent, record, receipt := sessionStartAckFixture(t)
			persistSessionStartAuthority(t, projectDir, intent, record)
			probe := sessionStartAckProbe(intent)
			tc.mutate(&receipt, &probe)
			adapter := &dispatchTargetProbeAdapter{probe: probe}
			if err := acknowledgeSessionStartLocally(t, projectDir, adapter, receipt); err == nil {
				t.Fatal("acknowledgeSessionStartWithResolver() = nil error")
			}
			root := filepath.Join(projectDir, ".harmonik", "dispatch-session-starts")
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("receipt IO occurred: %v", err)
			}
		})
	}
}

func TestAcknowledgeSessionStartRequiresDurableAuthority(t *testing.T) {
	intent, record, receipt := sessionStartAckFixture(t)
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, projectDir string)
	}{
		{name: "intent absent", prepare: func(*testing.T, string) {}},
		{name: "record absent", prepare: func(t *testing.T, projectDir string) {
			store := dispatchstore.New(projectDir)
			if err := store.Create(replayOwnershipIntent(t, dispatch.PhasePrepared)); err != nil {
				t.Fatal(err)
			}
			if err := advanceReplayOwnershipIntent(t, projectDir, intent); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "corrupt intent", prepare: func(t *testing.T, projectDir string) {
			root := filepath.Join(projectDir, ".harmonik", "dispatch-intents")
			if err := os.MkdirAll(root, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, intent.Binding.RunID.String()+".json"), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong-path run", prepare: func(t *testing.T, projectDir string) {
			persistSessionStartAuthority(t, projectDir, intent, record)
			path := filepath.Join(projectDir, ".harmonik", "runs", intent.Binding.RunID.String()+".json")
			other := filepath.Join(projectDir, ".harmonik", "runs", "0197d100-0000-7000-8000-000000000099.json")
			if err := os.Rename(path, other); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			tc.prepare(t, projectDir)
			adapter := &dispatchTargetProbeAdapter{probe: sessionStartAckProbe(intent)}
			if err := acknowledgeSessionStartLocally(t, projectDir, adapter, receipt); err == nil {
				t.Fatal("acknowledgeSessionStartWithResolver() = nil error")
			}
			root := filepath.Join(projectDir, ".harmonik", "dispatch-session-starts")
			if _, err := os.Lstat(root); !os.IsNotExist(err) {
				t.Fatalf("receipt IO occurred: %v", err)
			}
		})
	}
}

func persistSessionStartAuthority(t *testing.T, projectDir string, intent dispatch.Intent, record runpkg.DispatchRecord) {
	t.Helper()
	store := dispatchstore.New(projectDir)
	if err := store.Create(replayOwnershipIntent(t, dispatch.PhasePrepared)); err != nil {
		t.Fatal(err)
	}
	if err := advanceReplayOwnershipIntent(t, projectDir, intent); err != nil {
		t.Fatal(err)
	}
	base := record
	base.Location, base.SessionName, base.WindowName = nil, "", ""
	if err := runpkg.CreateDispatchRecord(projectDir, base); err != nil {
		t.Fatal(err)
	}
	located := record
	located.SessionName, located.WindowName = "", ""
	if err := runpkg.AdvanceDispatchRecord(projectDir, base, located); err != nil {
		t.Fatal(err)
	}
	if err := runpkg.AdvanceDispatchRecord(projectDir, located, record); err != nil {
		t.Fatal(err)
	}
}

// acknowledgeSessionStartLocally runs the acknowledgement over the production
// adapter resolver for a local execution location. The resolver is the same one
// the daemon builds at boot, so the test proves the path production takes rather
// than a shortcut that hands the adapter straight to the acknowledgement.
func acknowledgeSessionStartLocally(
	t *testing.T,
	projectDir string,
	adapter ltmux.Adapter,
	receipt dispatch.SessionStartReceipt,
) error {
	t.Helper()
	resolver := newSessionStartAdapterResolverWithFactory(adapter, workers.Config{},
		func(worker workers.Worker) ltmux.Adapter {
			t.Fatalf("remote adapter factory ran for a local location: %+v", worker)
			return nil
		})
	return acknowledgeSessionStartWithResolver(t.Context(), projectDir, resolver, receipt)
}

func sessionStartAckFixture(t *testing.T) (dispatch.Intent, runpkg.DispatchRecord, dispatch.SessionStartReceipt) {
	t.Helper()
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	startedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	base, err := runpkg.NewDispatchRecord(intent.Binding, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	located, err := base.BindLocation(runpkg.ExecutionLocation{
		Kind: runpkg.ExecutionLocalIndependent, RepositoryPath: intent.Binding.RepositoryPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := located.BindSession(intent.Handoff.SessionName, intent.Handoff.WindowName)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	return intent, record, receipt
}

// sessionStartAckProbe returns the probe an exact live target reports for one
// intent. A test that needs a different target state changes a field of the
// result, or replaces the whole probe.
func sessionStartAckProbe(intent dispatch.Intent) ltmux.TargetProbe {
	return ltmux.TargetProbe{
		Status: ltmux.TargetProbeExact, RunID: intent.Binding.RunID.String(),
		ClaimTransitionID: intent.Binding.ClaimTransitionID.String(),
		SessionName:       intent.Handoff.SessionName, WindowName: intent.Handoff.WindowName,
		PanePID: "1234", PaneDead: "0",
	}
}
