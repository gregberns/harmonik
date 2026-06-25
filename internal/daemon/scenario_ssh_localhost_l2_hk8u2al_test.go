package daemon_test

// scenario_ssh_localhost_l2_hk8u2al_test.go — L2 of the remote-substrate test
// pyramid: real SSH to localhost with an isolated tmux socket + separate worker
// checkout, catching SSHRunner argv / #{pane_id} quoting bugs that the L1
// stub-runner harness (scenario_fs_separation_hk52xnr_test.go) cannot reach.
//
// # Problem class
//
// L1 injects a local path-remap CommandRunner: no SSH hop, so quoting bugs in
// SSHRunner.Command() are invisible. L2 uses a REAL `ssh localhost` transport
// so the argv SSHRunner builds must survive the OpenSSH → remote-login-shell
// chain.
//
// The hk-fxy9 / hk-538l bug: SSHRunner shipped tmux argv UNQUOTED; OpenSSH
// space-joined the operands and the worker's login shell re-parsed them — a
// token like `-F #{pane_id}` made the remote shell treat `#{pane_id}` as a `#`
// COMMENT, truncating `tmux new-window` so the agent window was never created
// (→ agent_ready_timeout). The fix single-quotes every token.
//
// # Topology
//
//	boxA    = this test process (absolute t.TempDir paths, same physical machine)
//	worker  = same machine, accessed via SSHRunner{Host:"localhost"}
//	           – separate checkout: hk8u2alWorkerDir() (distinct temp dir)
//	           – sandboxed HOME:    fresh t.TempDir() passed as env HOME=
//	           – isolated socket:   tmux -L hk8u2al-test (no shared server state)
//
// # Scenarios
//
//	A — FS axis: drives ExportedGateVerdictExistsVia + ExportedReadGateVerdictVia
//	    with SSHRunner → `ssh localhost -- 'test' '-s' '<path>'` /
//	    `ssh localhost -- 'cat' '<path>'`; confirms path-quoting works over real SSH.
//
//	B — Tmux quoting axis: drives `tmux -L <socket> new-window -P -F #{pane_id}`
//	    via SSHRunner with a sandboxed HOME; asserts the pane ID is non-empty and
//	    starts with `%` — proving `#{pane_id}` arrived as one literal word, not
//	    truncated by the remote shell's `#`-comment rule.
//
// No build tags: both tests run in the normal `go test` pass; each skips when
// `ssh localhost true` is unavailable. Bead: hk-8u2al.
// Helper prefix: hk8u2al (per implementer-protocol §Helper-prefix discipline).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ─────────────────────────────────────────────────────────────────────────────
// Pre-flight helpers
// ─────────────────────────────────────────────────────────────────────────────

// hk8u2alSSHAvailable reports whether `ssh localhost true` succeeds within 15 s.
// Returns (true, "") on success, (false, reason) on failure so the skip message
// is actionable (no sshd, no key, host-key prompt, etc.).
func hk8u2alSSHAvailable(ctx context.Context) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	//nolint:gosec // G204: args are test-controlled literals
	cmd := exec.CommandContext(cctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"localhost", "true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
	}
	return true, ""
}

// hk8u2alSSHRunner returns the standard localhost SSHRunner for L2 tests:
// BatchMode=yes (no passphrase prompt), StrictHostKeyChecking=accept-new
// (first-connect auto-accept), ConnectTimeout=10.
func hk8u2alSSHRunner() tmux.SSHRunner {
	return tmux.SSHRunner{
		Host: "localhost",
		Opts: []string{
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "ConnectTimeout=10",
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures
// ─────────────────────────────────────────────────────────────────────────────

// hk8u2alWorkerDir creates a separate temp dir (the "worker checkout") seeded
// with a gate-verdict.json containing an allow decision. The dir is distinct
// from any box-A project dir, representing the FS-separation axis.
func hk8u2alWorkerDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755); err != nil {
		t.Fatalf("hk8u2alWorkerDir: mkdir .harmonik: %v", err)
	}
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(
		filepath.Join(dir, ".harmonik", "gate-verdict.json"),
		[]byte(`{"schema_version":1,"decision":"allow","reason":"hk-8u2al L2 ssh-localhost gate"}`),
		0o644,
	); err != nil {
		t.Fatalf("hk8u2alWorkerDir: write gate-verdict.json: %v", err)
	}
	return dir
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — FS axis
// ─────────────────────────────────────────────────────────────────────────────

// TestL2_SSHLocalhost_GateVerdictFSAxis_hk8u2al drives ExportedGateVerdictExistsVia
// and ExportedReadGateVerdictVia through a real SSHRunner{Host:"localhost"}.
//
// The "worker checkout" (hk8u2alWorkerDir) is a separate temp dir from box A's
// project, representing FS separation. gate-verdict.json is seeded ONLY there.
// SSHRunner routes reads as:
//
//	`ssh [opts] localhost -- 'test' '-s' '<path>'`   (exists check)
//	`ssh [opts] localhost -- 'cat' '<path>'`         (content read)
//
// Since boxA and "worker" are the same machine the paths resolve — this test
// exercises the transport + quoting layer, not path remapping.
func TestL2_SSHLocalhost_GateVerdictFSAxis_hk8u2al(t *testing.T) {
	t.Parallel()

	if ok, detail := hk8u2alSSHAvailable(t.Context()); !ok {
		t.Skipf("L2 FS-axis requires `ssh localhost true`; skipping. probe: %s", detail)
	}

	workerDir := hk8u2alWorkerDir(t)
	verdictPath := filepath.Join(workerDir, ".harmonik", "gate-verdict.json")
	runner := hk8u2alSSHRunner()

	// gateVerdictExistsVia: SSHRunner → ssh localhost test -s <path>
	if !daemon.ExportedGateVerdictExistsVia(t.Context(), runner, verdictPath) {
		t.Fatal("L2 GateVerdictExists: SSHRunner returned false; expected ssh localhost to find the seeded file")
	}

	// readGateVerdictVia: SSHRunner → ssh localhost cat <path> → JSON parse
	action, err := daemon.ExportedReadGateVerdictVia(t.Context(), runner, verdictPath)
	if err != nil {
		t.Fatalf("L2 ReadGateVerdict: unexpected error over ssh localhost: %v", err)
	}
	if action != core.GateActionAllow {
		t.Errorf("L2 ReadGateVerdict: action=%q; want %q", action, core.GateActionAllow)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — Tmux quoting axis
// ─────────────────────────────────────────────────────────────────────────────

// TestL2_SSHLocalhost_TmuxPaneID_hk8u2al drives `tmux new-window -P -F #{pane_id}`
// through SSHRunner{Host:"localhost"} with a sandboxed HOME and isolated socket,
// proving the #{pane_id} format token survives the ssh → remote-shell quoting
// chain intact.
//
// The hk-fxy9 bug pattern: UNQUOTED `#{pane_id}` is parsed as a `#` COMMENT by
// the remote shell — tmux receives a truncated command and output is empty.
// With the fix (single-quote every token): `'#{pane_id}'` reaches tmux as one
// literal word; tmux expands it to a `%N` pane ID.
//
// Isolation:
//   - sandboxed HOME: a fresh t.TempDir(), set via `env HOME=<dir>` prefix in
//     each remote command so tmux reads no ~/.tmux.conf from the real HOME.
//   - isolated socket: `-L hk8u2al-test` binds a fresh tmux server, preventing
//     interference with the user's running tmux sessions.
//
// The tmux binary is resolved via exec.LookPath and passed as an absolute path
// so the command works over SSH even when Homebrew/custom PATH is not in the
// remote login shell's environment.
func TestL2_SSHLocalhost_TmuxPaneID_hk8u2al(t *testing.T) {
	t.Parallel()

	if ok, detail := hk8u2alSSHAvailable(t.Context()); !ok {
		t.Skipf("L2 tmux-axis requires `ssh localhost true`; skipping. probe: %s", detail)
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("L2 tmux-axis requires tmux on PATH; skipping")
	}

	runner := hk8u2alSSHRunner()

	// sandboxHome is the isolated HOME for this test's tmux server: tmux reads
	// $HOME/.tmux.conf at server start; an empty temp dir means no config file.
	sandboxHome := t.TempDir()

	// Fixed socket name: unique within this test package, hermetic per test run.
	// tmux handles a stale socket (dead server) transparently — new-session
	// starts a fresh server even if the socket file exists from a prior run.
	const (
		socketName = "hk8u2al-test"
		sessName   = "hk8u2al-sess"
	)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// ── Start a detached session on the isolated socket with sandboxed HOME. ──
	// `env HOME=<sandbox>` is shell-quoted by SSHRunner just like any other
	// token, so the remote shell sees it as a literal env-var assignment to the
	// env(1) utility, which sets HOME before executing tmux. The absolute tmuxBin
	// path bypasses login-shell PATH differences (e.g. Homebrew not in sshd PATH).
	startCmd := runner.Command(ctx,
		"env", "HOME="+sandboxHome,
		tmuxBin, "-L", socketName, "new-session", "-d", "-s", sessName)
	if out, err := startCmd.CombinedOutput(); err != nil {
		t.Skipf("L2 tmux-axis: could not start tmux on isolated socket %q: %v: %s",
			socketName, err, out)
	}
	t.Cleanup(func() {
		killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer killCancel()
		_ = runner.Command(killCtx,
			"env", "HOME="+sandboxHome,
			tmuxBin, "-L", socketName, "kill-server").Run()
	})

	// ── new-window -P -F #{pane_id}: the production spawn shape. ─────────────
	// This is the exact argv the production OSAdapter.NewWindow call builds.
	// If SSHRunner shipped `#{pane_id}` UNQUOTED, the remote shell would treat
	// it as a `#` comment and truncate the command — output would be empty,
	// tmux would error, or paneID would not start with `%`.
	nwCmd := runner.Command(ctx,
		"env", "HOME="+sandboxHome,
		tmuxBin, "-L", socketName,
		"new-window", "-P", "-F", "#{pane_id}", "-d", "-t", sessName)
	out, err := nwCmd.Output()
	if err != nil {
		t.Fatalf("L2 tmux new-window via SSHRunner failed: %v\noutput=%q", err, out)
	}

	paneID := strings.TrimSpace(string(out))
	if paneID == "" {
		t.Fatal("L2 tmux quoting: #{pane_id} expansion returned empty — " +
			"SSHRunner may be shipping #{pane_id} unquoted (# treated as shell comment by remote shell)")
	}
	if !strings.HasPrefix(paneID, "%") {
		t.Errorf("L2 tmux quoting: #{pane_id} returned %q; expected tmux pane-id starting with %%", paneID)
	}

	t.Logf("L2 ssh-localhost OK: #{pane_id}→%q over ssh localhost (socket=%s, HOME=%s)",
		paneID, socketName, sandboxHome)
}
