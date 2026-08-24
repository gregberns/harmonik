package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

const commandRestartSID = "11111111-1111-4111-8111-111111111111"

func prepareRestartNowCommand(t *testing.T, project, agent string, withLock bool) {
	t.Helper()
	dir := filepath.Join(project, ".harmonik", "keeper")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".harmonik", "events"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := `{"pct":50,"session_id":"` + commandRestartSID + `","ts":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	if err := os.WriteFile(filepath.Join(dir, agent+".ctx"), []byte(ctx+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, agent+".sid"), []byte(commandRestartSID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "HANDOFF-"+agent+".md"), []byte("handoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !withLock {
		return
	}
	lock, err := keeper.AcquireLock(project, agent)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lock.Release(); err != nil {
			t.Errorf("release keeper lock: %v", err)
		}
	})
}

//nolint:gosec // Test reads its own temporary event log.
func readRestartNowCommandEvents(t *testing.T, project string) []core.Event {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(project, ".harmonik", core.EventsJSONLPath))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var events []core.Event
	for _, line := range bytesLines(raw) {
		var event core.Event
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == core.EventTypeSessionKeeperRestartNow {
			events = append(events, event)
		}
	}
	return events
}

func bytesLines(raw []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range raw {
		if b == '\n' {
			if i > start {
				lines = append(lines, raw[start:i])
			}
			start = i + 1
		}
	}
	if start < len(raw) {
		lines = append(lines, raw[start:])
	}
	return lines
}

func TestRunKeeperRestartNow_PostStartReleaseFailureStillEmitsAcceptedEvent(t *testing.T) {
	project := t.TempDir()
	agent := "captain"
	prepareRestartNowCommand(t, project, agent, true)

	originalStart := startKeeperRestartDriverFn
	originalRelease := releaseKeeperRestartDriverProcess
	t.Cleanup(func() {
		startKeeperRestartDriverFn = originalStart
		releaseKeeperRestartDriverProcess = originalRelease
	})
	releaseKeeperRestartDriverProcess = func(*os.Process) error { return errors.New("release failed after start") }
	startKeeperRestartDriverFn = func(_, _, _, _, _ string) error {
		logFile, err := os.CreateTemp(t.TempDir(), "restart-driver-log")
		if err != nil {
			return err
		}
		finishKeeperRestartDriverStart(&os.Process{Pid: 99999}, logFile, logFile.Name())
		return nil
	}

	if code := runKeeperRestartNow([]string{"--project", project, "--agent", agent, "--tmux", "%1"}); code != 0 {
		t.Fatalf("restart-now exit = %d; want accepted start", code)
	}
	if events := readRestartNowCommandEvents(t, project); len(events) != 1 {
		t.Fatalf("accepted events = %d; want 1 after post-start release failure", len(events))
	}
}

func TestRunKeeperRestartNow_EmitsAcceptedEventAfterDriverStarts(t *testing.T) {
	project := t.TempDir()
	agent := "captain"
	prepareRestartNowCommand(t, project, agent, true)
	original := startKeeperRestartDriverFn
	t.Cleanup(func() { startKeeperRestartDriverFn = original })
	var starts int
	startKeeperRestartDriverFn = func(_, _, _, sid, nonce string) error {
		starts++
		if sid != commandRestartSID || nonce != "command-nonce" {
			t.Fatalf("driver got sid=%q nonce=%q", sid, nonce)
		}
		return nil
	}

	if code := runKeeperRestartNow([]string{"--project", project, "--agent", agent, "--tmux", "pane", "--nonce", "command-nonce"}); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if starts != 1 {
		t.Fatalf("driver starts = %d, want 1", starts)
	}
	events := readRestartNowCommandEvents(t, project)
	if len(events) != 1 {
		t.Fatalf("accepted events = %d, want 1", len(events))
	}
	var payload core.SessionKeeperRestartNowPayload
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SessionID != commandRestartSID || payload.Nonce != "command-nonce" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestRunKeeperRestartNow_NoEventWhenValidationOrDriverStartFails(t *testing.T) {
	for _, tc := range []struct {
		name     string
		withLock bool
		startErr error
	}{
		{name: "no live keeper", withLock: false},
		{name: "driver start fails", withLock: true, startErr: errors.New("start failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := t.TempDir()
			prepareRestartNowCommand(t, project, "captain", tc.withLock)
			original := startKeeperRestartDriverFn
			t.Cleanup(func() { startKeeperRestartDriverFn = original })
			var starts int
			startKeeperRestartDriverFn = func(string, string, string, string, string) error {
				starts++
				return tc.startErr
			}
			if code := runKeeperRestartNow([]string{"--project", project, "--agent", "captain", "--tmux", "pane"}); code == 0 {
				t.Fatal("failure returned exit 0")
			}
			if !tc.withLock && starts != 0 {
				t.Fatalf("validation failure started driver %d times", starts)
			}
			if events := readRestartNowCommandEvents(t, project); len(events) != 0 {
				t.Fatalf("failure emitted %d accepted events", len(events))
			}
		})
	}
}
