package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
)

func commsServeSendingDaemon(t *testing.T, projectDir string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"ok":     true,
		"result": map[string]any{"event_id": "019fef4e-9fbd-7ed0-9184-646377e53d07"},
	})
	if err != nil {
		t.Fatalf("marshal canned send reply: %v", err)
	}
	sockPath := filepath.Join(projectDir, ".harmonik", "send.sock")
	serveCannedUnixSocket(t, sockPath, body)
	return sockPath
}

func commsSendTo(t *testing.T, projectDir, sockPath, to string) (stdout, stderr string, code int) {
	t.Helper()
	stdout, stderr = captureStd(t, func() {
		code = runCommsSendSubcommand([]string{
			"--to", to, "--from", "alpha", "--no-wake", "--topic", "status",
			"--project", projectDir, "--socket", sockPath, "epic complete",
		})
	})
	return stdout, stderr, code
}

func commsSendProject(t *testing.T) string {
	t.Helper()
	dir := shortProjectDir(t)
	if err := crew.Write(dir, crew.Record{
		SchemaVersion: 1,
		Name:          "bravo",
		SessionID:     "00000000-0000-4000-8000-00000000000a",
		Queue:         "bravo",
		StartedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("write crew record: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "agents", "captain"), 0o750); err != nil {
		t.Fatalf("mkdir agent manifest folder: %v", err)
	}
	return dir
}

// TestCommsSend_UnknownRecipientIsReportedAsUndelivered is the defect itself.
func TestCommsSend_UnknownRecipientIsReportedAsUndelivered(t *testing.T) {
	dir := commsSendProject(t)
	sock := commsServeSendingDaemon(t, dir)

	stdout, stderr, code := commsSendTo(t, dir, sock, "bravoo")
	if code != 1 {
		t.Fatalf("comms send to a name nobody uses exited %d, want 1 — a caller that branches on the exit code was told the message was delivered (stderr=%q)", code, stderr)
	}
	if !strings.Contains(stdout, "019fef4e") {
		t.Fatalf("comms send stopped printing the event id: %q", stdout)
	}
	if !strings.Contains(stderr, "bravoo") {
		t.Errorf("a send to a name nobody uses printed nothing about that name, so it reads exactly like a delivered message:\nstdout=%q\nstderr=%q", stdout, stderr)
	}
	if !strings.Contains(stderr, "nobody has received it") {
		t.Errorf("the warning does not say the message reached nobody, which is the fact the sender is missing:\n%s", stderr)
	}
}

// TestCommsSend_KnownRecipientsStaySilent is the control. A warning on every
// send would be noise, and noise is ignored, so it has to fire only for a name
// this project does not use.
func TestCommsSend_KnownRecipientsStaySilent(t *testing.T) {
	dir := commsSendProject(t)
	sock := commsServeSendingDaemon(t, dir)

	for _, to := range []string{"bravo", "captain", "operator"} {
		t.Run(to, func(t *testing.T) {
			_, stderr, code := commsSendTo(t, dir, sock, to)
			if code != 0 {
				t.Fatalf("comms send --to %s exited %d, want 0 (stderr=%q)", to, code, stderr)
			}
			if strings.Contains(stderr, "no agent named") {
				t.Errorf("comms send --to %s warns about a name this project uses:\n%s", to, stderr)
			}
		})
	}
}

// TestCommsSend_BroadcastNeverWarns pins that a broadcast keeps its old output.
// "*" is not a recipient name and no registry will ever hold it.
func TestCommsSend_BroadcastNeverWarns(t *testing.T) {
	dir := commsSendProject(t)
	sock := commsServeSendingDaemon(t, dir)

	var code int
	_, stderr := captureStd(t, func() {
		code = runCommsSendSubcommand([]string{
			"--broadcast", "--from", "alpha", "--topic", "status",
			"--project", dir, "--socket", sock, "epic complete",
		})
	})
	if code != 0 {
		t.Fatalf("comms send --broadcast exited %d, want 0 (stderr=%q)", code, stderr)
	}
	if strings.Contains(stderr, "no agent named") {
		t.Errorf("a broadcast warned about an unknown recipient:\n%s", stderr)
	}
}

// TestCommsRecipientKnown_SourcesAreIndependent checks each source on its own,
// so a change that drops one of the three is visible here rather than only in
// whichever end-to-end case happened to cover it.
func TestCommsRecipientKnown_SourcesAreIndependent(t *testing.T) {
	dir := commsSendProject(t)

	ts := time.Now().UTC().Format(time.RFC3339)
	eventsDir := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(eventsDir, 0o750); err != nil {
		t.Fatalf("mkdir events dir: %v", err)
	}
	line := commsPresenceLine(t, "01965b00-0000-7000-8000-000000000001", ts, "charlie", "online", "join")
	if err := os.WriteFile(filepath.Join(eventsDir, "events.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write events.jsonl: %v", err)
	}

	cases := []struct {
		name  string
		want  bool
		since string
	}{
		{"charlie", true, "the presence registry"},
		{"bravo", true, "the crew registry"},
		{"captain", true, "the agent manifests"},
		{"operator", true, "the always-addressable list"},
		{"bravoo", false, "nothing"},
		{"nosuchlane", false, "nothing"},
	}
	for _, tc := range cases {
		if got := commsRecipientKnown(dir, tc.name); got != tc.want {
			t.Errorf("commsRecipientKnown(%q) = %v, want %v — %s should decide it", tc.name, got, tc.want, tc.since)
		}
	}
}

// TestCommsSend_UnknownRecipientExitsLikeWake is the sibling-agreement check
// (hk-zj9nw). `harmonik wake --agent <name>` and `harmonik comms send --to
// <name>` are asked the same question — a name this project has no session for
// — and they must not answer it differently. wake refuses with 1
// (checkWakeTarget); send now reports 1 after recording the message.
func TestCommsSend_UnknownRecipientExitsLikeWake(t *testing.T) {
	dir := commsSendProject(t)
	sock := commsServeSendingDaemon(t, dir)

	const nobody = "nosuchlane"

	wakeCode := checkWakeTarget(dir, nobody)
	if wakeCode == 0 {
		t.Fatalf("checkWakeTarget(%q) = 0: the precedent this test compares against is gone", nobody)
	}

	stdout, stderr, sendCode := commsSendTo(t, dir, sock, nobody)
	if sendCode != wakeCode {
		t.Errorf("comms send --to %s exited %d but wake --agent %s exits %d: two surfaces disagree about whether reaching nobody succeeded\nstdout=%q\nstderr=%q",
			nobody, sendCode, nobody, wakeCode, stdout, stderr)
	}
	if !strings.Contains(stdout, "019fef4e") {
		t.Errorf("comms send stopped recording the message when it started reporting failure: stdout=%q", stdout)
	}
}
