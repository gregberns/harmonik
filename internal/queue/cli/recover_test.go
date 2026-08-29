package cli_test

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
			"result":        "accepted",
			"receipt":       map[string]any{"receipt_id": "0190b3c4-9001-7000-8000-000000000300"},
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
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue":         "investigate",
			"result":        "accepted",
			"receipt":       map[string]any{"receipt_id": "0190b3c4-9001-7000-8000-000000000301"},
			"rearmed_count": 0,
		})
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

func TestRunQueueRecover_DropSendsTheQueueDropOp(t *testing.T) {
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
			"dropped":       []any{"hk-canary"},
			"dropped_count": 1,
			"archive_path":  "/tmp/investigate.json.failed-20260824000000",
		})
	})

	var out, errOut strings.Builder
	got := cli.RunQueueRecover(context.Background(), []string{"--project", projectDir, "--drop", "investigate"}, &out, &errOut)

	if got != 0 {
		t.Fatalf("exit = %d, want 0; stderr=%q", got, errOut.String())
	}
	if capturedOp != "queue-drop" {
		t.Errorf("op = %q, want %q — --drop must route to queue-drop, not queue-recover", capturedOp, "queue-drop")
	}
	if capturedQueue != "investigate" {
		t.Errorf("queue = %q, want %q", capturedQueue, "investigate")
	}
	if !strings.Contains(out.String(), "hk-canary") {
		t.Errorf("stdout = %q, want the dropped bead listed", out.String())
	}
}

func TestRunQueueRecover_RefusesASuccessThatCarriesNoReceipt(t *testing.T) {
	t.Parallel()

	projectDir := queueCliFixtureTempDir(t)
	queueCliFixtureStartEchoServer(t, projectDir, func(_ []byte) []byte {
		return queueCliFixtureSuccessResponse(t, map[string]any{
			"queue":         "investigate",
			"result":        "accepted",
			"rearmed":       []any{"hk-canary"},
			"rearmed_count": 1,
		})
	})

	var out, errOut strings.Builder
	got := cli.RunQueueRecover(context.Background(), []string{"--project", projectDir, "investigate"}, &out, &errOut)

	if got == 0 {
		t.Fatalf("exit = 0 for a success with no receipt; stdout=%q — an unprovable recovery must not read as a success", out.String())
	}
	if strings.Contains(out.String(), "recovery: accepted") {
		t.Errorf("stdout = %q, want no accepted line for a payload with no receipt", out.String())
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
