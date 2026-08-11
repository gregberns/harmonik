package keeper

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

type countingActivity struct {
	ActivityProbe
	roles map[string]int
}

func (a countingActivity) LastUserTurn(string) (time.Time, bool) {
	a.roles["user"]++
	return time.Time{}, false
}

func (a countingActivity) LastAssistantTurn(string) (time.Time, bool) {
	a.roles["assistant"]++
	return time.Time{}, false
}

func TestConfigAdaptersKeepEntryTranscriptReadsLazy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lookback time.Duration
		wantUser int
	}{
		{name: "disabled", wantUser: 0},
		{name: "enabled", lookback: 5 * time.Minute, wantUser: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roles := map[string]int{}
			cfg := CyclerConfig{
				AgentName: "adapter", ProjectDir: t.TempDir(), TmuxTarget: "adapter:0",
				OperatorTurnLookback: tc.lookback,
				IsManagedFn:          func(string, string) bool { return false },
			}
			deps := CycleDepsFromConfig(cfg, nil)
			deps.Activity = countingActivity{ActivityProbe: deps.Activity, roles: roles}
			cycler, err := NewCyclerWithDeps(
				CyclePolicyFromConfig(cfg), CycleEnvFromConfig(cfg), deps,
			)
			if err != nil {
				t.Fatal(err)
			}
			if err := cycler.MaybeRun(context.Background(), &CtxFile{Pct: 90, SessionID: "sid"}); err != nil {
				t.Fatal(err)
			}
			if roles["user"] != tc.wantUser || roles["assistant"] != 0 {
				t.Fatalf("transcript reads = %v, want user=%d assistant=0", roles, tc.wantUser)
			}
		})
	}
}

func TestConfigAdaptersSeparateHandoffAndJournalPaths(t *testing.T) {
	project := t.TempDir()
	handoffPath := filepath.Join(project, "HANDOFF-custom.md")
	var readHandoffPath, readJournalPath string
	cfg := CyclerConfig{
		AgentName: "adapter", ProjectDir: project,
		HandoffFilePath: func(string, string) string { return handoffPath },
		ReadHandoff: func(path string) (string, error) {
			readHandoffPath = path
			return "handoff", nil
		},
		ReadJournalFn: func(path string) (*CycleJournal, error) {
			readJournalPath = path
			return nil, nil
		},
	}
	deps := CycleDepsFromConfig(cfg, nil)
	if _, err := deps.Handoff.Read(); err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Journal.Read(); err != nil {
		t.Fatal(err)
	}
	if readHandoffPath != handoffPath {
		t.Fatalf("handoff path = %q, want %q", readHandoffPath, handoffPath)
	}
	wantJournal := journalFilePath(project, "adapter")
	if readJournalPath != wantJournal {
		t.Fatalf("journal path = %q, want %q", readJournalPath, wantJournal)
	}
}
