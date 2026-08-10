package keeper

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFunctionAdaptersKeepLazyGateReads(t *testing.T) {
	t.Parallel()

	var sleeping, attached, userTurns, assistantTurns int
	cfg := CyclerConfig{
		ProjectDir: "project",
		AgentName:  "agent",
		SleepingCheckFn: func(string, string) bool {
			sleeping++
			return true
		},
		OperatorAttachedFn: func(string) bool {
			attached++
			return true
		},
		RecentTranscriptTurnFn: func(_, _, role string) (time.Time, bool) {
			switch role {
			case "user":
				userTurns++
			case "assistant":
				assistantTurns++
			}
			return time.Unix(1, 0), true
		},
	}
	cfg.applyDefaults()
	c := NewCycler(cfg, nil)

	c.sampleGates("")
	if sleeping != 0 || attached != 0 || userTurns != 0 || assistantTurns != 0 {
		t.Fatalf("disabled gate reads ran: sleeping=%d attached=%d user=%d assistant=%d", sleeping, attached, userTurns, assistantTurns)
	}

	c.cfg.TmuxTarget = "pane"
	c.cfg.OperatorTurnLookback = time.Minute
	c.cfg.PostAnswerGrace = time.Minute
	c.sampleGates("session")
	if sleeping != 1 || attached != 1 || userTurns != 1 || assistantTurns != 1 {
		t.Fatalf("enabled gate reads = sleeping:%d attached:%d user:%d assistant:%d; want one each", sleeping, attached, userTurns, assistantTurns)
	}
}

func TestHandoffAdapterScrubsOnlyKeeperNonces(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "HANDOFF.md")
	want := "crew context\nnext action\n"
	if err := os.WriteFile(path, []byte("crew context\n<!-- KEEPER:cycle-1 -->\nnext action\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := CyclerConfig{HandoffFilePath: func(string, string) string { return path }}
	cfg.applyDefaults()
	if err := (fnHandoff{cfg: &cfg}).ScrubNonce(); err != nil {
		t.Fatalf("ScrubNonce: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("scrubbed handoff = %q; want %q", got, want)
	}
}

func TestActivityAdapterReadsAssistantTurnWithoutAnswerGrace(t *testing.T) {
	t.Parallel()

	want := time.Unix(42, 0)
	cfg := CyclerConfig{
		RecentTranscriptTurnFn: func(_, _, role string) (time.Time, bool) {
			if role != "assistant" {
				t.Fatalf("role = %q; want assistant", role)
			}
			return want, true
		},
	}
	cfg.applyDefaults()
	got, ok := (fnGauge{cfg: &cfg}).LastAssistantTurn("session")
	if !ok || !got.Equal(want) {
		t.Fatalf("LastAssistantTurn = (%v, %v); want (%v, true)", got, ok, want)
	}
}

func TestJournalAdapterKeepsOpenedFailureFatalAndLaterFailureBestEffort(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("journal unavailable")
	c := NewCycler(CyclerConfig{
		WriteJournalFn: func(string, *CycleJournal) error { return wantErr },
	}, nil)

	if err := c.executeWriteJournal(Action{Journal: CycleJournal{Phase: "opened"}}); !errors.Is(err, wantErr) {
		t.Fatalf("opened journal error = %v; want %v", err, wantErr)
	}
	if err := c.executeWriteJournal(Action{Journal: CycleJournal{Phase: "confirmed"}}); err != nil {
		t.Fatalf("later journal error = %v; want nil", err)
	}
}
