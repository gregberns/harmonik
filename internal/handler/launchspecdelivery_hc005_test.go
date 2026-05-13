package handler_test

// launchspecdelivery_hc005_test.go — unit test for HC-005 LaunchSpec JSON
// delivery to subprocess stdin via Handler.Launch (bead hk-keb6o).
//
// Helper prefix: keb6oFixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-keb6o).
//
// Strategy: launch a sh -c child via NewSession directly (bypassing the
// watcher) that reads its entire stdin and echoes the raw bytes to stdout.
// The test verifies that the bytes round-trip to an equal LaunchSpec.
//
// A second test verifies that when HandlerSpec=nil the legacy path leaves
// stdin open for manual SendInput calls.

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// keb6oFixtureHandlerSpec returns a valid handlercontract.LaunchSpec with all
// required fields populated. This is the JSON payload the daemon delivers to
// the subprocess stdin per HC-005.
func keb6oFixtureHandlerSpec(t *testing.T) *handlercontract.LaunchSpec {
	t.Helper()
	runID := core.RunID(uuid.MustParse("0196f000-0000-7000-8000-000000aaaaaa"))
	wfID := core.WorkflowID(uuid.MustParse("0196f000-0000-7000-8000-000000bbbbbb"))
	beadID := "hk-smoke-test"
	return &handlercontract.LaunchSpec{
		RunID:               runID,
		WorkflowID:          wfID,
		NodeID:              core.NodeID("impl-node-keb6o"),
		AgentType:           core.AgentType("claude-code"),
		WorkspacePath:       t.TempDir(),
		RequiredSkills:      []string{"beads-cli"},
		SkillSearchPaths:    []string{"/usr/local/share/harmonik/skills"},
		Timeout:             3600,
		ProvisioningTimeout: 60,
		Budget:              core.BudgetRef("default"),
		FreedomProfileRef:   "standard",
		BeadID:              &beadID,
		SchemaVersion:       handlercontract.LaunchSpecSchemaVersion,
	}
}

// TestSession_CloseStdin_SendInputThenClose verifies the Session.CloseStdin
// primitive: SendInput writes a line to the subprocess stdin, CloseStdin
// closes the write end, and the subprocess (cat) echoes the bytes to stdout.
// This is the low-level mechanism that Handler.Launch's delivery goroutine uses.
func TestSession_CloseStdin_SendInputThenClose(t *testing.T) {
	t.Parallel()

	hs := keb6oFixtureHandlerSpec(t)

	// Encode the expected JSON independently.
	expectedJSON, err := handlercontract.MarshalLaunchSpec(hs)
	if err != nil {
		t.Fatalf("MarshalLaunchSpec: %v", err)
	}

	// Use NewSession with 'cat' to verify SendInput + CloseStdin round-trip.
	cmd := exec.CommandContext(t.Context(), "sh", "-c", "cat") //nolint:gosec // G204: test-only constant
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	sess, err := handler.NewSession(t.Context(), cmd)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	if err := sess.SendInput(t.Context(), string(expectedJSON)); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if err := sess.CloseStdin(); err != nil {
		t.Fatalf("CloseStdin: %v", err)
	}

	// Read everything the child echoed to stdout.
	stdoutBytes, readErr := io.ReadAll(sess.Stdout())
	if readErr != nil {
		t.Fatalf("ReadAll stdout: %v", readErr)
	}
	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}

	// The child (cat) echoes stdin to stdout verbatim. SendInput appends '\n'.
	received := strings.TrimSpace(string(stdoutBytes))
	if received == "" {
		t.Fatal("child produced no stdout; CloseStdin may have been called before write")
	}

	// Round-trip: parse the received bytes and compare key fields.
	var got handlercontract.LaunchSpec
	if err := json.Unmarshal([]byte(received), &got); err != nil {
		t.Fatalf("received stdin is not valid LaunchSpec JSON: %v\nraw: %q", err, received)
	}

	if received != string(expectedJSON) {
		t.Errorf("received JSON differs from expected:\nwant: %s\ngot:  %s", expectedJSON, received)
	}
}

// TestHandler_Launch_HandlerSpecDeliveredViaLaunch verifies that when
// LaunchSpec.HandlerSpec is non-nil, Handler.Launch's internal goroutine
// writes the JSON to stdin. The child saves stdin to a temp file and emits
// a fixed agent_ready so the watcher exits cleanly. After watcher.Done(), we
// read the temp file and compare to the expected JSON.
func TestHandler_Launch_HandlerSpecDeliveredViaLaunch(t *testing.T) {
	t.Parallel()

	hs := keb6oFixtureHandlerSpec(t)

	expectedJSON, err := handlercontract.MarshalLaunchSpec(hs)
	if err != nil {
		t.Fatalf("MarshalLaunchSpec: %v", err)
	}

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl, handlercontract.NewAdapterRegistry())

	// Capture stdin to a temp file, emit a fixed agent_ready so the watcher
	// exits cleanly, then exit.
	tmpDir := t.TempDir()
	stdinCapture := tmpDir + "/stdin.json"
	childScript := `cat > "` + stdinCapture + `"; printf '{"type":"agent_ready"}\n'`

	spec := handler.LaunchSpec{
		Binary:      "sh",
		Args:        []string{"-c", childScript},
		Env:         []string{},
		WorkDir:     tmpDir,
		Role:        "test",
		HandlerSpec: hs,
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	select {
	case <-watcher.Done():
	case <-t.Context().Done():
		t.Fatal("watcher.Done() did not close before test context cancelled")
	}
	_ = sess.Wait(t.Context())

	// Read the captured stdin from the temp file.
	//nolint:gosec // G304: path is test-generated; not user-controlled
	captured, readErr := os.ReadFile(stdinCapture)
	if readErr != nil {
		t.Fatalf("ReadFile(stdin capture): %v — LaunchSpec may not have been delivered", readErr)
	}

	received := strings.TrimSpace(string(captured))
	if received == "" {
		t.Fatal("stdin capture is empty; LaunchSpec was not delivered")
	}

	var got handlercontract.LaunchSpec
	if err := json.Unmarshal([]byte(received), &got); err != nil {
		t.Fatalf("captured stdin is not valid LaunchSpec JSON: %v\nraw: %q", err, received)
	}

	if received != string(expectedJSON) {
		t.Errorf("captured stdin differs from expected:\nwant: %s\ngot:  %s", expectedJSON, received)
	}
}

// TestHandler_Launch_NilHandlerSpec_StdinNotClosed verifies that when
// LaunchSpec.HandlerSpec is nil, Launch does NOT close stdin — the subprocess
// can still receive input via SendInput.
func TestHandler_Launch_NilHandlerSpec_StdinNotClosed(t *testing.T) {
	t.Parallel()

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl, handlercontract.NewAdapterRegistry())

	// Child: read one line from stdin, echo it to stdout as NDJSON, then exit.
	spec := handler.LaunchSpec{
		Binary:      "sh",
		Args:        []string{"-c", `read line; printf '{"type":"agent_ready","got":"%s"}\n' "$line"`},
		Env:         []string{},
		WorkDir:     t.TempDir(),
		Role:        "test",
		HandlerSpec: nil, // no delivery — legacy path
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// Send a line manually via SendInput; child should echo it.
	if err := sess.SendInput(t.Context(), "manual-line"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if err := sess.CloseStdin(); err != nil {
		t.Fatalf("CloseStdin: %v", err)
	}

	stdoutBytes, readErr := io.ReadAll(sess.Stdout())
	if readErr != nil {
		t.Fatalf("ReadAll stdout: %v", readErr)
	}

	select {
	case <-watcher.Done():
	case <-t.Context().Done():
		t.Fatal("watcher.Done() did not close before test context cancelled")
	}
	_ = sess.Wait(t.Context())

	if !strings.Contains(string(stdoutBytes), "manual-line") {
		t.Errorf("expected 'manual-line' in stdout when HandlerSpec=nil; got: %s", stdoutBytes)
	}
}
