package codexdigitaltwin_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexreactor"
)

func corpusPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(
		filepath.Dir(thisFile), "..", "..", "testdata",
		"codex-app-server", "corpus", "raw-session-01.jsonl",
	)
}

func openCorpus(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(corpusPath())
	if err != nil {
		t.Fatalf("open corpus: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := f.Close(); closeErr != nil {
			t.Errorf("close corpus: %v", closeErr)
		}
	})
	return f
}

func runTwin(t *testing.T, fault codexdigitaltwin.FaultConfig, ctx context.Context) []codexreactor.Action {
	t.Helper()
	twin := codexdigitaltwin.New(openCorpus(t), fault)
	eff := &codexreactor.FakeEffector{}
	r := codexreactor.New()
	if err := r.Run(ctx, twin, eff); err != nil {
		t.Fatalf("reactor.Run: %v", err)
	}
	return eff.Actions()
}

const (
	corpusThreadID = "019f5489-8dde-7ed2-81c3-5848fe26f1ac"
	corpusTurnID   = "019f5489-8e9f-7d62-b86c-6020273ed855"
	corpusItemID   = "msg_0bb2d88f02914c01016a5314e2fb5c819ab15adab033e890c5"
)

// TestTwin_HappyPath verifies that a clean corpus replay produces the five
// expected reactor actions in order.
func TestTwin_HappyPath(t *testing.T) {
	got := runTwin(t, codexdigitaltwin.FaultConfig{}, context.Background())

	want := []codexreactor.Action{
		{
			Type:     codexreactor.ActionTypeNotifyStatus,
			ThreadID: corpusThreadID,
			Status:   "active",
		},
		{
			Type:     codexreactor.ActionTypeEmitOutput,
			ThreadID: corpusThreadID,
			TurnID:   corpusTurnID,
			ItemID:   corpusItemID,
			Delta:    "ok",
		},
		{
			Type:          codexreactor.ActionTypeNotifyTokenUsage,
			ThreadID:      corpusThreadID,
			TurnID:        corpusTurnID,
			TotalTokens:   15825,
			ContextWindow: 258400,
		},
		{
			Type:     codexreactor.ActionTypeNotifyStatus,
			ThreadID: corpusThreadID,
			Status:   "idle",
		},
		{
			Type:     codexreactor.ActionTypeCompleteTurn,
			ThreadID: corpusThreadID,
			TurnID:   corpusTurnID,
			Status:   "completed",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("happy path actions mismatch\n  want: %v\n  got:  %v", want, got)
	}
}

// TestTwin_FaultDropAfter verifies that FaultDropAfter at event 2 (turn_started)
// emits an EmitError action because the disconnect arrives during an in-flight turn.
func TestTwin_FaultDropAfter(t *testing.T) {
	fault := codexdigitaltwin.FaultConfig{
		Mode:   codexdigitaltwin.FaultDropAfter,
		EventN: 2, // drop after ev2 (turn/started); turn is now in-flight
	}
	got := runTwin(t, fault, context.Background())

	want := []codexreactor.Action{
		{
			Type:     codexreactor.ActionTypeNotifyStatus,
			ThreadID: corpusThreadID,
			Status:   "active",
		},
		{
			Type:    codexreactor.ActionTypeEmitError,
			Message: "disconnected during turn",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("drop-after actions mismatch\n  want: %v\n  got:  %v", want, got)
	}
}

// TestTwin_FaultStall verifies that FaultStall at event 3 blocks until context
// cancellation, after which Reactor.Run returns nil.
func TestTwin_FaultStall(t *testing.T) {
	fault := codexdigitaltwin.FaultConfig{
		Mode:   codexdigitaltwin.FaultStall,
		EventN: 3, // stall before ev3 (item/agentMessage/delta)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	got := runTwin(t, fault, ctx)

	want := []codexreactor.Action{
		{
			Type:     codexreactor.ActionTypeNotifyStatus,
			ThreadID: corpusThreadID,
			Status:   "active",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("stall actions mismatch\n  want: %v\n  got:  %v", want, got)
	}
}

// TestTwin_FaultTruncate verifies that FaultTruncate at event 3 (item delta)
// replaces it with an Error event. The reactor's error handler terminates the
// in-flight turn, producing an EmitError action.
func TestTwin_FaultTruncate(t *testing.T) {
	fault := codexdigitaltwin.FaultConfig{
		Mode:   codexdigitaltwin.FaultTruncate,
		EventN: 3, // truncate ev3 (item/agentMessage/delta); turn is in-flight
	}
	got := runTwin(t, fault, context.Background())

	want := []codexreactor.Action{
		{
			Type:     codexreactor.ActionTypeNotifyStatus,
			ThreadID: corpusThreadID,
			Status:   "active",
		},
		{
			Type:    codexreactor.ActionTypeEmitError,
			Message: "twin: truncated frame",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("truncate actions mismatch\n  want: %v\n  got:  %v", want, got)
	}
}

// TestTwin_FaultDup verifies that FaultDup at event 3 emits the event twice
// with the same Seq. The reactor's I2 dedup invariant must drop the second
// copy, yielding the same final action set as the happy path.
func TestTwin_FaultDup(t *testing.T) {
	fault := codexdigitaltwin.FaultConfig{
		Mode:   codexdigitaltwin.FaultDup,
		EventN: 3, // duplicate ev3 (item/agentMessage/delta, seq=3)
	}
	got := runTwin(t, fault, context.Background())

	want := []codexreactor.Action{
		{
			Type:     codexreactor.ActionTypeNotifyStatus,
			ThreadID: corpusThreadID,
			Status:   "active",
		},
		{
			Type:     codexreactor.ActionTypeEmitOutput,
			ThreadID: corpusThreadID,
			TurnID:   corpusTurnID,
			ItemID:   corpusItemID,
			Delta:    "ok",
		},
		{
			Type:          codexreactor.ActionTypeNotifyTokenUsage,
			ThreadID:      corpusThreadID,
			TurnID:        corpusTurnID,
			TotalTokens:   15825,
			ContextWindow: 258400,
		},
		{
			Type:     codexreactor.ActionTypeNotifyStatus,
			ThreadID: corpusThreadID,
			Status:   "idle",
		},
		{
			Type:     codexreactor.ActionTypeCompleteTurn,
			ThreadID: corpusThreadID,
			TurnID:   corpusTurnID,
			Status:   "completed",
		},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("dup actions mismatch\n  want: %v\n  got:  %v", want, got)
	}
}
