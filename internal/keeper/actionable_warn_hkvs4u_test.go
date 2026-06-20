package keeper

import (
	"strings"
	"testing"
)

// hk-vs4u — R3 actionable warn → self-service restart handshake.
//
// These tests pin the three load-bearing contracts:
//   1. ActionableWarnText ALWAYS contains the verbatim restart-now command and the
//      /session-handoff step (the two-step procedure), the live token count, the
//      band, and the auto-restart fall-through line.
//   2. selectWarnText picks the actionable form ONLY when the gate holds (captain
//      OR crew-with-crews-enabled, primary SID, CrispIdle, self_service.enabled),
//      and a custom ActionableWarnText override that DROPS the command falls back
//      to the compiled text (the required token can never be silently lost).
//   3. The lighter advisory is used otherwise.

const restartNowStem = "harmonik keeper restart-now"

func TestActionableWarnText_ContainsTwoStepProcedureAndFigures(t *testing.T) {
	t.Parallel()
	txt := ActionableWarnText("captain", 205_000, 200_000, 215_000)

	wantSubstrs := []string{
		"/session-handoff", // step (a)
		"harmonik keeper restart-now --agent captain", // step (b), verbatim, templated-in
		"205k",                      // live token count (tokens/1000)
		"warn 200k",                 // band
		"act 215k",                  // band
		"auto-restarts at 215k",     // fall-through line
		"Only at a clean stop",      // clean-stop qualifier
		"if mid-task, finish first", // mid-task instruction
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
		txt := ActionableWarnText(agent, 100_000, 200_000, 215_000)
		want := "harmonik keeper restart-now --agent " + agent
		if !strings.Contains(txt, want) {
			t.Errorf("agent %q: ActionableWarnText must contain %q, got: %s", agent, want, txt)
		}
	}
}

func ctxWith(sid string, tokens int64) *CtxFile {
	return &CtxFile{SessionID: sid, Tokens: tokens, WindowSize: 200_000, Pct: 85}
}

const primarySID = "11111111-2222-4333-8444-555555555555" // lowercase UUIDv4
const brokenSID = "NOT-A-UUID"

func TestSelectWarnText_CaptainActionableWhenIdleAndPrimary(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true /*crispIdle*/)
	if !strings.Contains(txt, restartNowStem) {
		t.Fatalf("captain idle+primary: want actionable (restart-now), got: %s", txt)
	}
}

func TestSelectWarnText_CrewActionableWhenCrewsEnabledDefault(t *testing.T) {
	t.Parallel()
	// crews_enabled DEFAULT is true (operator decision); modeled here by setting
	// SelfServiceCrewsEnabled=true (the resolver fills unset→true upstream).
	c := WatcherConfig{
		AgentName:               "crew-paul",
		SelfServiceEnabled:      true,
		SelfServiceCrewsEnabled: true,
		WarnAbsTokens:           200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true)
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
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true)
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
	txt := c.selectWarnText(ctxWith(brokenSID, 205_000), true)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("broken/non-primary SID: want lighter advisory, got actionable: %s", txt)
	}
}

func TestSelectWarnText_BusyCaptainStillGetsLighterAdvisory(t *testing.T) {
	t.Parallel()
	// Not CrispIdle: the actionable form is suppressed, but the lighter advisory is
	// still returned (and the watcher injects it once gaugeQuiesced) — no session
	// loses its warn.
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), false /*busy*/)
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
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true)
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
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true)
	if txt != custom {
		t.Errorf("custom actionable text carrying the command must be honored verbatim, got: %s", txt)
	}
}

func TestSelectWarnText_CustomActionableDroppingCommandFallsBackToCompiled(t *testing.T) {
	t.Parallel()
	// A custom override that DROPS the required command token MUST NOT be used; the
	// compiled ActionableWarnText (which always carries the command) is used instead.
	custom := "[CUSTOM] just wrap up, no command here"
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		ActionableWarnText: custom,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true)
	if txt == custom {
		t.Fatal("custom override dropping the command must NOT be honored")
	}
	if !strings.Contains(txt, restartNowStem) {
		t.Errorf("fallback compiled text must contain the verbatim command, got: %s", txt)
	}
}
