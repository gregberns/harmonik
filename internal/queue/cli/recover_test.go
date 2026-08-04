package cli_test

// recover_test.go — claims defended for `harmonik queue recover`.
//
// The verb must send `queue-recover`, which is the op the daemon registers. A
// CLI that sends a name the daemon does not route reaches the unknown-op path
// and the operator gets no recovery at all.

import (
	"context"
	"strings"
	"testing"

	cli "github.com/gregberns/harmonik/internal/queue/cli"
)

func TestRunQueueRecover_SendsTheQueueRecoverOp(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedOp string
	var capturedQueue string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		msg := queueCliFixtureDecodeRequest(t, raw)
		queueCliFixtureCapture(t, msg, "op", &capturedOp)
		queueCliFixtureCapture(t, msg, "queue", &capturedQueue)
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue":         "investigate",
			"queue_id":      "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0300",
			"rearmed":       []any{"hk-canary"},
			"rearmed_count": 1,
		})
	})

	var out, errOut strings.Builder
	got := cli.RunQueueRecover(context.Background(), []string{"--project", projectDir, "investigate"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedOp != "queue-recover" {
		t.Errorf("op = %q, want %q — the daemon routes only %q", capturedOp, "queue-recover", "queue-recover")
	}
	if capturedQueue != "investigate" {
		t.Errorf("queue = %q, want %q", capturedQueue, "investigate")
	}
	if !strings.Contains(out.String(), "hk-canary") {
		t.Errorf("stdout = %q, want the re-armed bead listed", out.String())
	}
}

func TestRunQueueRecover_AcceptsTheQueueFlagForm(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	var capturedQueue string
	queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
		msg := queueCliFixtureDecodeRequest(t, raw)
		queueCliFixtureCapture(t, msg, "queue", &capturedQueue)
		return queueCliFixtureSuccessResponse(t, map[string]any{"queue": "investigate", "rearmed_count": 0})
	})

	var out, errOut strings.Builder
	got := cli.RunQueueRecover(context.Background(), []string{"--project", projectDir, "--queue=investigate"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedQueue != "investigate" {
		t.Errorf("queue = %q, want %q", capturedQueue, "investigate")
	}
}

func TestRunQueueRecover_ExitsSeventeenWhenTheDaemonIsDown(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)

	var out, errOut strings.Builder
	got := cli.RunQueueRecover(context.Background(), []string{"--project", projectDir, "investigate"}, &out, &errOut)

	if got != 17 {
		t.Fatalf("exit = %d, want 17 (daemon not running); stderr=%q", got, errOut.String())
	}
}
