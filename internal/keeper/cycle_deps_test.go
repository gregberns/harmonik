package keeper

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

type cycleDepsStub struct{}

func (*cycleDepsStub) Next() string                                         { return "cycle" }
func (*cycleDepsStub) Inject(context.Context, string, string) error         { return nil }
func (*cycleDepsStub) SendEscape(context.Context, string) error             { return nil }
func (*cycleDepsStub) SetEnv(context.Context, string, string, string) error { return nil }
func (*cycleDepsStub) ReadGauge() (*CtxFile, time.Time, error)              { return nil, time.Time{}, nil }
func (*cycleDepsStub) SetManagedSession(string) error                       { return nil }
func (*cycleDepsStub) ClearPrecompactTrigger() error                        { return nil }
func (*cycleDepsStub) IdleMarkerModTime() (time.Time, bool)                 { return time.Time{}, false }
func (*cycleDepsStub) LastUserTurn(string) (time.Time, bool)                { return time.Time{}, false }
func (*cycleDepsStub) LastAssistantTurn(string) (time.Time, bool)           { return time.Time{}, false }
func (*cycleDepsStub) IsManaged() bool                                      { return true }
func (*cycleDepsStub) CrispIdle() bool                                      { return true }
func (*cycleDepsStub) HoldingDispatch() bool                                { return false }
func (*cycleDepsStub) Sleeping(string) bool                                 { return false }
func (*cycleDepsStub) Held() bool                                           { return false }
func (*cycleDepsStub) Attached(string) bool                                 { return false }
func (*cycleDepsStub) Path() string                                         { return "/tmp/handoff" }
func (*cycleDepsStub) Read() (string, error)                                { return "", nil }
func (*cycleDepsStub) ModTime() (time.Time, bool)                           { return time.Time{}, false }
func (*cycleDepsStub) ScrubNonce() error                                    { return nil }
func (*cycleDepsStub) Write(*CycleJournal) error                            { return nil }

type journalDepsStub struct{ *cycleDepsStub }

func (*journalDepsStub) Read() (*CycleJournal, error) { return nil, nil }

func completeCycleDeps() CycleDeps {
	s := &cycleDepsStub{}
	j := &journalDepsStub{cycleDepsStub: s}
	return CycleDeps{
		Clock: substrate.NewFakeClock(time.Unix(1_700_000_000, 0)), CycleIDs: s,
		Pane: s, Context: s, Activity: s, Managed: s, Idle: s, Dispatch: s,
		Sleep: s, Hold: s, Operator: s, Handoff: s, Journal: j,
	}
}

func TestNewCyclerWithDepsListsMissingDependenciesInStableOrder(t *testing.T) {
	deps := completeCycleDeps()
	deps.Clock = nil
	deps.Pane = nil
	deps.Journal = nil
	_, err := NewCyclerWithDeps(CyclePolicyFromConfig(CyclerConfig{}), CycleEnv{}, deps)
	if err == nil || !strings.Contains(err.Error(), "Clock, Pane, Journal") {
		t.Fatalf("error = %v", err)
	}
}

func TestNewCyclerWithDepsRejectsTypedNil(t *testing.T) {
	deps := completeCycleDeps()
	var pane *cycleDepsStub
	deps.Pane = pane
	_, err := NewCyclerWithDeps(CyclePolicyFromConfig(CyclerConfig{}), CycleEnv{}, deps)
	if err == nil || !strings.Contains(err.Error(), "Pane") {
		t.Fatalf("typed nil error = %v", err)
	}
}

func TestNewCyclerWithDepsAllowsNilEmitterAndRespawn(t *testing.T) {
	c, err := NewCyclerWithDeps(CyclePolicyFromConfig(CyclerConfig{}), CycleEnv{}, completeCycleDeps())
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.respawn != nil {
		t.Fatalf("cycler=%v respawn=%v", c, c.respawn)
	}
}
