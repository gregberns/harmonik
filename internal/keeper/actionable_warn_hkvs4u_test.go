package keeper

import (
	"strings"
	"testing"
)

const restartNowStem = "harmonik keeper restart-now"

func TestActionableWarnText_ContainsTwoStepProcedureAndFigures(t *testing.T) {
	t.Parallel()
	txt := ActionableWarnText("captain", 175_000, 170_000, 200_000)

	wantSubstrs := []string{
		"/session-handoff", // step (a)
		"harmonik keeper restart-now --agent captain", // step (b), verbatim, templated-in
		"175k", // live token count (tokens/1000)
		"before 200k",
		"notice band is 170k",
		"As you continue, shape the work toward a state that a fresh session can resume without losing decisions or repeating work.",
		"so we can continue in the fresh session",
	}
	for _, sub := range wantSubstrs {
		if !strings.Contains(txt, sub) {
			t.Errorf("ActionableWarnText missing %q\ngot: %s", sub, txt)
		}
	}
}

func TestActionableWarnText_AlwaysCarriesCommand_AnyAgent(t *testing.T) {
	t.Parallel()
	for _, agent := range []string{"captain", "crew-paul", "-weird-name"} {
		txt := ActionableWarnText(agent, 100_000, 170_000, 200_000)
		want := "harmonik keeper restart-now --agent " + agent
		if !strings.Contains(txt, want) {
			t.Errorf("agent %q: ActionableWarnText must contain %q, got: %s", agent, want, txt)
		}
	}
}

func TestSettleWarnText_KeepsCheckpointAgentOwned(t *testing.T) {
	t.Parallel()
	got := SettleWarnText("alpha", 201_000, 220_000)
	for _, want := range []string{
		"[KEEPER WARN]", "201k tokens", "220k hard band", "durable checkpoint",
		"/session-handoff", "harmonik keeper restart-now --agent alpha",
		"continue in the fresh session",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("SettleWarnText() missing %q: %q", want, got)
		}
	}
}

func ctxWith(sid string) *CtxFile {
	return &CtxFile{SessionID: sid, Tokens: 205_000, WindowSize: 200_000, Pct: 85}
}

const (
	primarySID = "11111111-2222-4333-8444-555555555555" // lowercase UUIDv4
	brokenSID  = "NOT-A-UUID"
)

func TestSelectWarnText_CaptainActionableWhenIdleAndPrimary(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID), true /*crispIdle*/, false /*operatorAttached*/)
	if !strings.Contains(txt, restartNowStem) {
		t.Fatalf("captain idle+primary: want actionable (restart-now), got: %s", txt)
	}
}

func TestSelectWarnText_CrewActionableWhenCrewsEnabledDefault(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:               "crew-paul",
		SelfServiceEnabled:      true,
		SelfServiceCrewsEnabled: true,
		WarnAbsTokens:           200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID), true, false)
	if !strings.Contains(txt, restartNowStem) {
		t.Fatalf("crew with crews_enabled=true: want actionable, got: %s", txt)
	}
	if !strings.Contains(txt, "--agent crew-paul") {
		t.Errorf("crew actionable text must name the crew agent, got: %s", txt)
	}
}

func TestSelectWarnText_CrewLighterWhenCrewsDisabled(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:               "crew-paul",
		SelfServiceEnabled:      true,
		SelfServiceCrewsEnabled: false, // explicit false
		WarnAbsTokens:           200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID), true, false)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("crew with crews_enabled=false: want lighter advisory, got actionable: %s", txt)
	}
	if txt != wrapUpWarningText {
		t.Errorf("want compiled lighter advisory, got: %s", txt)
	}
}

func TestSelectWarnText_BrokenSIDFallsToLighter(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(brokenSID), true, false)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("broken/non-primary SID: want lighter advisory, got actionable: %s", txt)
	}
}

func TestSelectWarnText_BusyCaptainStillGetsLighterAdvisory(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID), false /*busy*/, false /*operatorAttached*/)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("busy captain: want lighter advisory, got actionable: %s", txt)
	}
	if txt == "" {
		t.Fatal("busy captain: still must receive a (lighter) warn, got empty")
	}
}

func TestSelectWarnText_SelfServiceDisabledAlwaysLighter(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: false, // off
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID), true, false)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("self_service disabled: want lighter advisory, got actionable: %s", txt)
	}
}

func TestSelectWarnText_CustomActionableHonoredWhenItKeepsCommand(t *testing.T) {
	t.Parallel()
	custom := "[CUSTOM] please run harmonik keeper restart-now --agent captain now"
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		ActionableWarnText: custom,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID), true, false)
	if txt != custom {
		t.Errorf("custom actionable text carrying the command must be honored verbatim, got: %s", txt)
	}
}

func TestSelectWarnText_CustomActionableDroppingCommandFallsBackToCompiled(t *testing.T) {
	t.Parallel()
	custom := "[CUSTOM] just wrap up, no command here"
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		ActionableWarnText: custom,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID), true, false)
	if txt == custom {
		t.Fatal("custom override dropping the command must NOT be honored")
	}
	if !strings.Contains(txt, restartNowStem) {
		t.Errorf("fallback compiled text must contain the verbatim command, got: %s", txt)
	}
}
