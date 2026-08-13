package main

// comms_send_unknown_recipient_rtqmu_test.go — a directed `comms send` to a
// name nobody uses must not look like a delivered message.
//
// The reproduction was one line of output and no signal at all:
//
//	harmonik comms send --to nosuchlane --from alpha --no-wake --topic status "epic complete"
//	rc=0
//	019fef4e-9fbd-7ed0-9184-646377e53d07
//
// This is the channel a captain mails epics on. A directed send to a mistyped
// crew name was a silent black hole: the captain saw success, the crew never
// heard, and the only symptom was an epic that never completed — which reads as
// a stalled crew, not a misaddressed message. The failure was indistinguishable
// from the thing everybody was already hunting.
//
// THE SEND IS STILL ACCEPTED. comms-recv scans from the start of the event log
// when an agent has no stored cursor, so a message sent to a crew that boots an
// hour later is delivered in full on its first recv. Refusing an unknown name
// would break that. The repair is to stop the send from LOOKING delivered.
//
// The first repair changed the stderr text and left the exit code at 0, which
// only reached a human who was reading stderr. A script or an agent branching
// on the exit code was still told the epic was delivered. The exit code is now
// 1, matching `harmonik wake --agent <name>` for the same question — a name
// that matches nothing (hk-zj9nw). Accepting the send and reporting success are
// separate acts: the message stays durable and a later recv still gets it.
//
// Bead ref: hk-rtqmu, hk-zj9nw.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
)

// commsServeSendingDaemon accepts comms-send requests and answers each one with
// a fixed event id, the way a healthy daemon does for any recipient at all.
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

// commsSendTo runs one directed send through the fake daemon.
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

// commsSendProject builds a project holding one crew record and one agent
// manifest folder, which are two of the three ways a name becomes legitimate.
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

	// bravo: a crew registry record. captain: an agent manifest folder.
	// operator: a person who never registers on the bus.
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

	// The presence registry: a name that only ever emitted a beat.
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
	// The send is still accepted: the event id is on stdout and the message is
	// durable. Only the answer to the caller changed.
	if !strings.Contains(stdout, "019fef4e") {
		t.Errorf("comms send stopped recording the message when it started reporting failure: stdout=%q", stdout)
	}
}
