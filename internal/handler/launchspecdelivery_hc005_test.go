package handler_test

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

func keb6oFixtureHandlerSpec(t *testing.T) *handlercontract.LaunchSpec {
	t.Helper()
	runID := core.RunID(uuid.MustParse("0196f000-0000-7000-8000-000000aaaaaa"))
	wfID, err := core.NewWorkflowID("launchspec-delivery-graph")
	if err != nil {
		t.Fatalf("core.NewWorkflowID: %v", err)
	}
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

	expectedJSON, err := handlercontract.MarshalLaunchSpec(hs)
	if err != nil {
		t.Fatalf("MarshalLaunchSpec: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "sh", "-c", "cat")
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

	stdoutBytes, readErr := io.ReadAll(sess.Stdout())
	if readErr != nil {
		t.Fatalf("ReadAll stdout: %v", readErr)
	}
	if err := sess.Wait(t.Context()); err != nil {
		t.Errorf("Session.Wait: %v", err)
	}

	received := strings.TrimSpace(string(stdoutBytes))
	if received == "" {
		t.Fatal("child produced no stdout; CloseStdin may have been called before write")
	}

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
	if waitErr := sess.Wait(t.Context()); waitErr != nil {
		t.Fatalf("sess.Wait: child exited abnormally: %v", waitErr)
	}

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

	tmpDir := t.TempDir()
	lineCapture := tmpDir + "/line.txt"
	childScript := `read line; printf '%s' "$line" > "` + lineCapture + `"; printf '{"type":"agent_ready"}\n'`

	spec := handler.LaunchSpec{
		Binary:      "sh",
		Args:        []string{"-c", childScript},
		Env:         []string{},
		WorkDir:     tmpDir,
		Role:        "test",
		HandlerSpec: nil, // no delivery — legacy path
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if err := sess.SendInput(t.Context(), "manual-line"); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	if err := sess.CloseStdin(); err != nil {
		t.Fatalf("CloseStdin: %v", err)
	}

	select {
	case <-watcher.Done():
	case <-t.Context().Done():
		t.Fatal("watcher.Done() did not close before test context cancelled")
	}
	if waitErr := sess.Wait(t.Context()); waitErr != nil {
		t.Fatalf("sess.Wait: child exited abnormally: %v", waitErr)
	}

	// Read the captured line from the temp file.
	//nolint:gosec // G304: path is test-generated; not user-controlled
	captured, readErr := os.ReadFile(lineCapture)
	if readErr != nil {
		t.Fatalf("ReadFile(line capture): %v — SendInput may not have reached the child", readErr)
	}

	if !strings.Contains(string(captured), "manual-line") {
		t.Errorf("expected 'manual-line' captured from stdin when HandlerSpec=nil; got: %q", captured)
	}
}

// TestHandler_Launch_NilHandlerSpec_StdinDevNull_ClosesStdinImmediately
// verifies the hk-y20d2 fix: on the direct exec.CommandContext path, when
// HandlerSpec is nil and StdinDevNull is true, Launch closes stdin right
// away so an argv-driven ProcessExit harness (pi, codex) sees startup EOF on
// fd0 instead of blocking forever on an unfed pipe.
func TestHandler_Launch_NilHandlerSpec_StdinDevNull_ClosesStdinImmediately(t *testing.T) {
	t.Parallel()

	pub := &handlercontract.CollectingEmitter{}
	dl := handlercontract.NoopWatcherDeadLetter{}
	h := handler.NewHandler(pub, dl, handlercontract.NewAdapterRegistry())

	tmpDir := t.TempDir()
	childScript := `cat > /dev/null; printf '{"type":"agent_ready"}\n'`

	spec := handler.LaunchSpec{
		Binary:       "sh",
		Args:         []string{"-c", childScript},
		Env:          []string{},
		WorkDir:      tmpDir,
		Role:         "test",
		HandlerSpec:  nil,
		StdinDevNull: true,
	}

	sess, watcher, err := h.Launch(t.Context(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	select {
	case <-watcher.Done():
	case <-t.Context().Done():
		t.Fatal("watcher.Done() did not close before test context cancelled — stdin was not closed (hk-y20d2 regression)")
	}
	if waitErr := sess.Wait(t.Context()); waitErr != nil {
		t.Fatalf("sess.Wait: child exited abnormally: %v", waitErr)
	}
}
