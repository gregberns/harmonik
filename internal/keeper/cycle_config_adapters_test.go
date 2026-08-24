package keeper

import (
	"context"
	"os"
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
				AgentName:            "adapter",
				ProjectDir:           t.TempDir(),
				TmuxTarget:           "adapter:0",
				OperatorTurnLookback: tc.lookback,
			}
			deps := CycleDepsFromConfig(cfg, nil)
			deps.Operator = operatorProbeFunc(func(string) bool { return false })
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
	handoffPath := filepath.Join(project, "HANDOFF-adapter.md")
	if err := os.WriteFile(handoffPath, []byte("handoff"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := CyclerConfig{
		AgentName: "adapter", ProjectDir: project,
	}
	deps := CycleDepsFromConfig(cfg, nil)
	got, err := deps.Handoff.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got != "handoff" || deps.Handoff.Path() != handoffPath {
		t.Fatalf("handoff read = %q at %q", got, deps.Handoff.Path())
	}
	journal := &CycleJournal{CycleID: "path-test", Phase: "opened"}
	if err := deps.Journal.Write(journal); err != nil {
		t.Fatal(err)
	}
	wantJournal := journalFilePath(project, "adapter")
	if _, err := os.Stat(wantJournal); err != nil {
		t.Fatalf("journal was not written at %q: %v", wantJournal, err)
	}
	read, err := deps.Journal.Read()
	if err != nil {
		t.Fatal(err)
	}
	if read.CycleID != journal.CycleID || read.Phase != journal.Phase {
		t.Fatalf("journal = %+v, want %+v", read, journal)
	}
}
