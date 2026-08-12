package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

func TestAcknowledgeSessionStartInstallsReceiptAfterExactLiveProof(t *testing.T) {
	projectDir := t.TempDir()
	intent, record, receipt := sessionStartAckFixture(t)
	persistSessionStartAuthority(t, projectDir, intent, record)
	adapter := &dispatchTargetProbeAdapter{probe: sessionStartAckProbe(intent, ltmux.TargetProbeExact)}
	if err := acknowledgeSessionStart(t.Context(), projectDir, adapter, receipt); err != nil {
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
			probe := sessionStartAckProbe(intent, ltmux.TargetProbeExact)
			tc.mutate(&receipt, &probe)
			adapter := &dispatchTargetProbeAdapter{probe: probe}
			if err := acknowledgeSessionStart(t.Context(), projectDir, adapter, receipt); err == nil {
				t.Fatal("acknowledgeSessionStart() = nil error")
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
			adapter := &dispatchTargetProbeAdapter{probe: sessionStartAckProbe(intent, ltmux.TargetProbeExact)}
			if err := acknowledgeSessionStart(t.Context(), projectDir, adapter, receipt); err == nil {
				t.Fatal("acknowledgeSessionStart() = nil error")
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

func sessionStartAckFixture(t *testing.T) (dispatch.Intent, runpkg.DispatchRecord, dispatch.SessionStartReceipt) {
	t.Helper()
	intent := replayOwnershipIntent(t, dispatch.PhaseHandoffDurable)
	startedAt := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	base, err := runpkg.NewDispatchRecord(intent.Binding, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	located, err := base.BindLocation(runpkg.ExecutionLocation{Kind: runpkg.ExecutionLocalIndependent})
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

func sessionStartAckProbe(intent dispatch.Intent, status ltmux.TargetProbeStatus) ltmux.TargetProbe {
	return ltmux.TargetProbe{
		Status: status, RunID: intent.Binding.RunID.String(),
		ClaimTransitionID: intent.Binding.ClaimTransitionID.String(),
		SessionName:       intent.Handoff.SessionName, WindowName: intent.Handoff.WindowName,
		PanePID: "1234", PaneDead: "0",
	}
}
