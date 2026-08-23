//go:build e2e_real_claude

package daemon_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

func rcsmFixtureCheckPreconditions(t *testing.T) {
	t.Helper()

	requiredBinaries := []string{"claude", "tmux", "git", "br", "ntm"}
	for _, bin := range requiredBinaries {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("e2e_real_claude: %q not found on PATH; skipping: %v", bin, err)
		}
	}

	if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" {
		t.Skip("e2e_real_claude: neither ANTHROPIC_API_KEY nor CLAUDE_CODE_OAUTH_TOKEN is set; skipping (no API credentials)")
	}
}

func rcsmFixtureBuildHarmonik(t *testing.T) string {
	t.Helper()

	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("e2e_real_claude: 'go' not found in PATH; cannot build harmonik: %v", err)
		return ""
	}

	outDir := t.TempDir()
	binPath := filepath.Join(outDir, "harmonik")

	cwd, cwdErr := os.Getwd()
	if cwdErr != nil {
		t.Fatalf("rcsmFixtureBuildHarmonik: getwd: %v", cwdErr)
	}

	//nolint:gosec // G204: goTool is from exec.LookPath; args are literals
	goModCmd := exec.CommandContext(t.Context(), goTool, "env", "GOMOD")
	goModCmd.Dir = cwd
	goModOut, goModErr := goModCmd.Output()
	if goModErr != nil {
		t.Skipf("e2e_real_claude: go env GOMOD failed: %v; skipping", goModErr)
		return ""
	}
	moduleRoot := filepath.Dir(strings.TrimSpace(string(goModOut)))

	//nolint:gosec // G204: goTool is from exec.LookPath; pkgPath is a literal constant
	buildCmd := exec.CommandContext(t.Context(), goTool, "build", "-o", binPath, "github.com/gregberns/harmonik/cmd/harmonik")
	buildCmd.Dir = moduleRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		t.Fatalf("rcsmFixtureBuildHarmonik: build failed: %v\n%s", buildErr, out)
	}
	return binPath
}

func rcsmFixtureProject(t *testing.T) (smokeDir, beadID string) {
	t.Helper()

	smokeDir = t.TempDir()

	gitRun := func(args ...string) {
		t.Helper()
		//nolint:gosec // G204: git args are test-internal literals; not user input
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = smokeDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=smoke-e2e",
			"GIT_AUTHOR_EMAIL=smoke@harmonik.local",
			"GIT_COMMITTER_NAME=smoke-e2e",
			"GIT_COMMITTER_EMAIL=smoke@harmonik.local",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rcsmFixtureProject: git %v: %v\n%s", args, err, out)
		}
	}

	gitRun("init", "-b", "main")
	gitRun("config", "user.email", "smoke@harmonik.local")
	gitRun("config", "user.name", "smoke-e2e")

	markerPath := filepath.Join(smokeDir, "marker.txt")
	if err := os.WriteFile(markerPath, []byte(""), 0o644); err != nil {
		t.Fatalf("rcsmFixtureProject: write marker.txt: %v", err)
	}
	gitRun("add", "marker.txt")
	gitRun("commit", "-m", "initial")

	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Skipf("e2e_real_claude: br not found on PATH: %v", err)
	}

	//nolint:gosec // G204: brPath is from exec.LookPath; args are literals
	initCmd := exec.CommandContext(t.Context(), brPath, "init", "--prefix", "smoke")
	initCmd.Dir = smokeDir
	if out, initErr := initCmd.CombinedOutput(); initErr != nil {
		t.Fatalf("rcsmFixtureProject: br init: %v\n%s", initErr, out)
	}

	const beadBody = `Append the line ` + "`SMOKE-OK`" + ` to marker.txt in this worktree and commit with message ` + "`add SMOKE-OK`" + `. Use the Edit tool and a single git commit.`

	//nolint:gosec // G204: brPath is from exec.LookPath; args are literals
	createCmd := exec.CommandContext(t.Context(),
		brPath,
		"create",
		"--title", "Add SMOKE-OK marker line",
		"--body", beadBody,
		"--type", "task",
		"--priority", "1",
		"--format", "id",
	)
	createCmd.Dir = smokeDir
	createOut, createErr := createCmd.CombinedOutput()
	if createErr != nil {
		t.Fatalf("rcsmFixtureProject: br create: %v\n%s", createErr, createOut)
	}
	beadID = strings.TrimSpace(string(createOut))
	if beadID == "" {
		t.Fatal("rcsmFixtureProject: br create returned empty ID")
	}

	return smokeDir, beadID
}

type rcsmEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func rcsmFixtureTailEvents(ctx context.Context, t *testing.T, jsonlPath string) []rcsmEvent {
	t.Helper()

	var (
		mu     sync.Mutex
		events []rcsmEvent
		done   = make(chan struct{})
	)

	go func() {
		defer close(done)

		fileCtx, fileCancel := context.WithTimeout(ctx, 30*time.Second)
		defer fileCancel()
		for {
			if _, err := os.Stat(jsonlPath); err == nil {
				break
			}
			select {
			case <-fileCtx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}

		var offset int64

		for {
			// Open, seek to last offset, and scan new lines.
			//nolint:gosec // G304: jsonlPath is constructed from t.TempDir(); not user input
			f, openErr := os.Open(jsonlPath)
			if openErr != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(200 * time.Millisecond):
				}
				continue
			}

			fi, statErr := f.Stat()
			if statErr != nil || fi.Size() <= offset {
				_ = f.Close()
				select {
				case <-ctx.Done():
					return
				case <-time.After(200 * time.Millisecond):
				}
				continue
			}

			if _, seekErr := f.Seek(offset, io.SeekStart); seekErr != nil {
				_ = f.Close()
				return
			}

			scanner := bufio.NewScanner(f)
			completed := false
			for scanner.Scan() {
				line := strings.TrimSpace(scanner.Text())
				if line == "" {
					continue
				}
				var ev rcsmEvent
				if jsonErr := json.Unmarshal([]byte(line), &ev); jsonErr != nil {
					continue
				}
				mu.Lock()
				events = append(events, ev)
				isRunCompleted := ev.Type == "run_completed"
				mu.Unlock()

				if isRunCompleted {
					completed = true
				}
			}
			if scanErr := scanner.Err(); scanErr != nil {
				_ = f.Close()
				return
			}

			newOffset, tellErr := f.Seek(0, io.SeekCurrent)
			if tellErr == nil {
				offset = newOffset
			}
			_ = f.Close()

			if completed {
				return
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}

	mu.Lock()
	defer mu.Unlock()
	result := make([]rcsmEvent, len(events))
	copy(result, events)
	return result
}

func rcsmFixtureTmuxSession(t *testing.T, suffix string) (sessionName string, cleanup func()) {
	t.Helper()

	sessionName = fmt.Sprintf("harmonik-e2e-%s", suffix)

	//nolint:gosec // G204: tmux args constructed from test-internal literals
	newCmd := exec.CommandContext(t.Context(), "tmux", "new-session", "-d", "-s", sessionName)
	if out, err := newCmd.CombinedOutput(); err != nil {
		t.Skipf("e2e_real_claude: tmux new-session failed (no tmux server?): %v\n%s", err, out)
		return "", func() {}
	}

	cleanup = func() {
		//nolint:gosec // G204: sessionName is test-internal
		killCmd := exec.Command("tmux", "kill-session", "-t", sessionName) //nolint:noctx // cleanup runs after test context has expired
		_ = killCmd.Run()
	}

	return sessionName, cleanup
}

func rcsmFixtureLaunchHarmonik(
	t *testing.T,
	harmonikBin, sessionName, smokeDir string,
) (stop func()) {
	t.Helper()

	// Launch harmonik inside the tmux session as a new window.
	// We use `tmux new-window` so that $TMUX is set inside the child process,
	// satisfying the PL-028b guard.
	//nolint:gosec // G204: harmonikBin from build step; args from test-internal literals
	launchCmd := exec.CommandContext(t.Context(),
		"tmux", "new-window",
		"-t", sessionName,
		"-d", // detach (don't switch to it)
		"-n", "hk-e2e-daemon",
		"--",
		harmonikBin, "--project", smokeDir, "--max-concurrent", "1",
	)
	launchCmd.Env = os.Environ()

	if out, err := launchCmd.CombinedOutput(); err != nil {
		t.Fatalf("rcsmFixtureLaunchHarmonik: tmux new-window: %v\n%s", err, out)
	}

	stop = func() {
		// Send SIGTERM to all processes in the tmux window.
		//nolint:gosec // G204: sessionName is test-internal
		killCmd := exec.Command("tmux", "send-keys", "-t", sessionName+":hk-e2e-daemon", "q", "") //nolint:noctx // stop runs after test context
		_ = killCmd.Run()
		time.Sleep(500 * time.Millisecond)
		//nolint:gosec // G204: sessionName is test-internal
		killWindowCmd := exec.Command("tmux", "kill-window", "-t", sessionName+":hk-e2e-daemon") //nolint:noctx // stop func
		_ = killWindowCmd.Run()
	}
	return stop
}

func rcsmAssertRunCompleted(t *testing.T, events []rcsmEvent) {
	t.Helper()
	for _, ev := range events {
		if ev.Type != "run_completed" {
			continue
		}
		var payload struct {
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Errorf("rcsmAssertRunCompleted: unmarshal run_completed payload: %v", err)
			return
		}
		if !payload.Success {
			t.Errorf("run_completed event has success=false; expected success=true")
		}
		return
	}
	t.Errorf("run_completed event not found in collected events")
}

func rcsmAssertEventSequence(t *testing.T, events []rcsmEvent, wantSeq []string) {
	t.Helper()
	gotTypes := make([]string, len(events))
	for i, ev := range events {
		gotTypes[i] = ev.Type
	}

	wi := 0
	for _, got := range gotTypes {
		if wi >= len(wantSeq) {
			break
		}
		if got == wantSeq[wi] {
			wi++
		}
	}
	if wi < len(wantSeq) {
		t.Errorf("event sequence mismatch:\n  want (subsequence): %v\n  got (all):          %v\n  matched %d of %d",
			wantSeq, gotTypes, wi, len(wantSeq))
	}
}

func rcsmAssertMarkerOK(t *testing.T, workspacePath string) {
	t.Helper()
	markerPath := filepath.Join(workspacePath, "marker.txt")
	//nolint:gosec // G304: workspacePath extracted from events.jsonl payload (daemon-written); not user input
	contents, err := os.ReadFile(markerPath)
	if err != nil {
		t.Errorf("rcsmAssertMarkerOK: read marker.txt: %v", err)
		return
	}
	if !strings.Contains(string(contents), "SMOKE-OK") {
		t.Errorf("marker.txt does not contain SMOKE-OK; got:\n%s", string(contents))
	}
}

func rcsmAssertNewCommit(t *testing.T, workspacePath string) {
	t.Helper()
	//nolint:gosec // G204: workspacePath extracted from events.jsonl payload; not user input
	cmd := exec.Command("git", "-C", workspacePath, "log", "--oneline") //nolint:noctx // assertion helper; test context has expired
	out, err := cmd.Output()
	if err != nil {
		t.Errorf("rcsmAssertNewCommit: git log: %v", err)
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		t.Errorf("rcsmAssertNewCommit: expected ≥2 commits; got %d:\n%s", len(lines), string(out))
	}
}

func rcsmAssertBeadClosed(t *testing.T, smokeDir, beadID string) {
	t.Helper()
	brPath, err := exec.LookPath("br")
	if err != nil {
		t.Errorf("rcsmAssertBeadClosed: br not found: %v", err)
		return
	}
	//nolint:gosec // G204: brPath from LookPath; beadID from br create output; not user input
	cmd := exec.Command(brPath, "show", beadID, "--format", "json") //nolint:noctx // assertion helper
	cmd.Dir = smokeDir
	out, err := cmd.Output()
	if err != nil {
		t.Errorf("rcsmAssertBeadClosed: br show %s: %v", beadID, err)
		return
	}
	var items []struct {
		Status      string `json:"status"`
		CloseReason string `json:"close_reason"`
	}
	if jsonErr := json.Unmarshal(out, &items); jsonErr != nil || len(items) == 0 {
		t.Errorf("rcsmAssertBeadClosed: parse br show output: %v\n%s", jsonErr, out)
		return
	}
	if items[0].Status != "closed" {
		t.Errorf("bead %s status = %q; want closed", beadID, items[0].Status)
	}
	if items[0].CloseReason != "done" {
		t.Errorf("bead %s close_reason = %q; want done", beadID, items[0].CloseReason)
	}
}

func rcsmAssertSettingsJSON(t *testing.T, workspacePath string) {
	t.Helper()
	settingsPath := filepath.Join(workspacePath, ".claude", "settings.json")
	//nolint:gosec // G304: workspacePath extracted from events.jsonl payload; not user input
	contents, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Errorf("rcsmAssertSettingsJSON: read .claude/settings.json: %v", err)
		return
	}
	if !strings.Contains(string(contents), "hook-relay") {
		t.Errorf(".claude/settings.json exists but contains no hook-relay entries;\ncontents: %s", string(contents))
	}
}

func rcsmWorkspacePath(events []rcsmEvent) string {
	for _, ev := range events {
		if ev.Type != "run_started" {
			continue
		}
		var payload struct {
			WorkspacePath string `json:"workspace_path"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err == nil && payload.WorkspacePath != "" {
			return payload.WorkspacePath
		}
	}
	return ""
}

// TestE2ERealClaudeSingleMode is the real-Claude single-mode happy-path E2E
// smoke test.  It codifies the GREEN smoke procedure from
// docs/dogfood-smoke-procedure-bridge.md:
//
//  1. Skip-guard: checks all required binaries and credentials.
//  2. Builds the harmonik binary from source.
//  3. Creates a scratch project: git repo, marker.txt, br init, single P1 bead.
//  4. Launches harmonik inside a detached tmux session.
//  5. Tails events.jsonl, waits for run_completed or 180s timeout.
//  6. Asserts event sequence, post-conditions (marker.txt, git commit,
//     bead closed, .claude/settings.json).
//
// Build tag: e2e_real_claude — excluded from default go test ./...
// Timeout budget: 180s for the tail watcher (set -timeout 300s on the test binary).
//
// Bead: hk-36cip (real-Claude single-mode E2E smoke).
func TestE2ERealClaudeSingleMode(t *testing.T) {
	rcsmFixtureCheckPreconditions(t)

	harmonikBin := rcsmFixtureBuildHarmonik(t)
	smokeDir, beadID := rcsmFixtureProject(t)
	t.Logf("e2e_real_claude: smokeDir=%s beadID=%s harmonikBin=%s", smokeDir, beadID, harmonikBin)

	sessionName, tmuxCleanup := rcsmFixtureTmuxSession(t, beadID)
	defer tmuxCleanup()

	jsonlPath := filepath.Join(smokeDir, ".harmonik", "events", "events.jsonl")

	watchCtx, watchCancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer watchCancel()

	eventsCh := make(chan []rcsmEvent, 1)
	go func() {
		eventsCh <- rcsmFixtureTailEvents(watchCtx, t, jsonlPath)
	}()

	rawStop := rcsmFixtureLaunchHarmonik(t, harmonikBin, sessionName, smokeDir)
	var stopOnce sync.Once
	stopDaemon := func() { stopOnce.Do(rawStop) }
	defer stopDaemon()

	var events []rcsmEvent
	scenariotest.MustCompleteWithin(t, jsonlPath, "", tmux.OSAdapter{}, 210*time.Second, func() {
		events = <-eventsCh
	})

	stopDaemon()

	t.Logf("e2e_real_claude: collected %d events", len(events))
	for _, ev := range events {
		t.Logf("  event: %s", ev.Type)
	}

	wantSeq := []string{
		"daemon_started",
		"daemon_orphan_sweep_completed",
		"run_started",
		"agent_started",
		"agent_ready",
		"outcome_emitted",
		"run_completed",
	}
	rcsmAssertEventSequence(t, events, wantSeq)
	rcsmAssertRunCompleted(t, events)

	workspacePath := rcsmWorkspacePath(events)
	if workspacePath == "" {
		t.Fatalf("workspace_path not observed in run_started event — cannot verify post-conditions")
	}
	rcsmAssertMarkerOK(t, workspacePath)
	rcsmAssertNewCommit(t, workspacePath)
	rcsmAssertSettingsJSON(t, workspacePath)

	rcsmAssertBeadClosed(t, smokeDir, beadID)
}
