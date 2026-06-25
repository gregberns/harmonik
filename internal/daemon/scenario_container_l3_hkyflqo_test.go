package daemon_test

// scenario_container_l3_hkyflqo_test.go — L3 of the remote-substrate test
// pyramid: Docker/Lima containers as local Linux "workers," exercising real
// FS/OS separation between host (macOS) and container (Linux).
//
// # Problem class
//
// L1 (hk-52xnr) injects a path-remap runner: no SSH, no separate OS, no real
// filesystem isolation — just a synthetic path translation. L2 (hk-8u2al) uses
// real SSH to localhost: real SSH transport but same OS/FS. L3 uses Docker
// containers: genuinely separate Linux OS and filesystem from the macOS host.
// This is what makes it "BUILD LAST" — it requires both the SSH transport
// quoting fixed in L2 AND Linux OS support added for L3.
//
// # New capability: Linux OS target (decision 4)
//
// Containers run Linux. The remote-worker path previously only had a darwin
// collector. This bead adds linuxCollectorScript + parser extensions (memfree=
// and swapused= keys) to workers.CollectReport, dispatched by Worker.OS=="linux".
//
// # Topology
//
//	host      = this test process (macOS)
//	container = Docker container (Linux, alpine:latest), accessed via docker exec
//
//	DockerExecRunner wraps `docker exec <id> <cmd> <args...>`: no shell
//	quoting needed (docker passes args as discrete argv, not through a shell).
//
// # Scenarios
//
//	A — FS axis: seed gate-verdict.json inside a container; confirm
//	    ExportedGateVerdictExistsVia + ExportedReadGateVerdictVia route through
//	    DockerExecRunner and find the file. A bare os.Stat on the host path
//	    must return false (genuine FS separation from host).
//
//	B — Linux collector: call workers.CollectReport with Worker{OS:"linux"} +
//	    DockerExecRunner; confirm the parsed WorkerReportPayload has sane values
//	    sourced from /proc/loadavg, /proc/meminfo, df — Linux-specific paths.
//
//	C — Multi-container disjoint FS: spin up N=2 containers; seed the SAME
//	    verdict-path in each with different decision values; DockerExecRunner per
//	    container returns container-specific content — proving FS is isolated
//	    between containers even when the path is identical.
//
// All tests skip when `docker info` is unavailable. Bead: hk-yflqo.
// Helper prefix: hkyflqo (bead hk-yflqo, per implementer-protocol §Helper-prefix).

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/workers"
)

// ─────────────────────────────────────────────────────────────────────────────
// Pre-flight helpers
// ─────────────────────────────────────────────────────────────────────────────

// hkyflqoDockerAvailable reports whether Docker is reachable within 15 s.
// Returns (true, "") on success, (false, reason) otherwise so the skip message
// is actionable (daemon not running, no permissions, etc.).
func hkyflqoDockerAvailable(ctx context.Context) (bool, string) {
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	//nolint:gosec // G204: test-controlled literal; not user input
	out, err := exec.CommandContext(cctx, "docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput()
	if err != nil {
		return false, strings.TrimSpace(string(out)) + " (" + err.Error() + ")"
	}
	return true, ""
}

// hkyflqoAlpineAvailable checks whether the alpine:latest image is pullable /
// already present. We do NOT pull on demand — callers that need the image skip
// when it is absent so the test suite stays hermetic.
func hkyflqoAlpineAvailable(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// `docker image inspect` exits 0 iff the image is already in the local cache.
	//nolint:gosec // G204: test-controlled literal
	err := exec.CommandContext(cctx, "docker", "image", "inspect", "alpine:latest").Run()
	if err == nil {
		return true
	}
	// Not cached: attempt a pull (requires network, may timeout in air-gapped CI).
	pullCtx, pullCancel := context.WithTimeout(ctx, 60*time.Second)
	defer pullCancel()
	//nolint:gosec // G204: test-controlled literal
	pullErr := exec.CommandContext(pullCtx, "docker", "pull", "--quiet", "alpine:latest").Run()
	return pullErr == nil
}

// ─────────────────────────────────────────────────────────────────────────────
// DockerExecRunner — CommandRunner backed by `docker exec`
// ─────────────────────────────────────────────────────────────────────────────

// hkyflqoDockerExecRunner is a CommandRunner that routes every command through
// `docker exec <ContainerID> <name> <args...>`. Unlike SSHRunner, docker exec
// delivers discrete argv directly to the container — no shell intermediary, no
// quoting needed. The container sees exactly the name and args supplied.
//
// This runner is the L3 analog of SSHRunner in L2: both tunnel through an
// external process, but docker exec crosses a real OS boundary (Linux kernel
// namespace) rather than a TCP socket.
type hkyflqoDockerExecRunner struct {
	ContainerID string
}

func (r hkyflqoDockerExecRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	dockerArgs := make([]string, 0, 2+len(args))
	dockerArgs = append(dockerArgs, "exec", r.ContainerID, name)
	dockerArgs = append(dockerArgs, args...)
	//nolint:gosec // G204: ContainerID is test-controlled; name/args from production seam
	return exec.CommandContext(ctx, "docker", dockerArgs...)
}

// Compile-time assertion: hkyflqoDockerExecRunner implements tmux.CommandRunner.
var _ tmux.CommandRunner = hkyflqoDockerExecRunner{}

// ─────────────────────────────────────────────────────────────────────────────
// Container lifecycle helpers
// ─────────────────────────────────────────────────────────────────────────────

// hkyflqoStartContainer launches a detached alpine:latest container that sleeps
// indefinitely. Returns the container ID (trimmed). Registers a t.Cleanup that
// stops and removes the container. If start fails, t.Fatalf is called.
func hkyflqoStartContainer(t *testing.T, ctx context.Context) string {
	t.Helper()
	//nolint:gosec // G204: test-controlled literals
	out, err := exec.CommandContext(ctx,
		"docker", "run", "--rm", "-d", "alpine:latest", "sleep", "3600").Output()
	if err != nil {
		t.Fatalf("hkyflqoStartContainer: docker run: %v", err)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer stopCancel()
		//nolint:gosec // G204: test-controlled container ID
		_ = exec.CommandContext(stopCtx, "docker", "stop", id).Run()
	})
	return id
}

// hkyflqoSeedFile writes content to containerPath inside the container.
// It creates parent directories as needed.
func hkyflqoSeedFile(t *testing.T, ctx context.Context, containerID, containerPath, content string) {
	t.Helper()
	dir := containerPath[:strings.LastIndex(containerPath, "/")]
	script := "mkdir -p " + dir + " && printf '%s' '" + strings.ReplaceAll(content, "'", `'\''`) + "' > " + containerPath
	//nolint:gosec // G204: test-controlled args
	out, err := exec.CommandContext(ctx, "docker", "exec", containerID, "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("hkyflqoSeedFile: seed %q: %v: %s", containerPath, err, out)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario A — FS axis (AC-A)
// ─────────────────────────────────────────────────────────────────────────────

// TestL3_ContainerFS_GateVerdictAxis_hkyflqo seeds a gate-verdict.json inside a
// Docker container (Linux, alpine:latest) and drives ExportedGateVerdictExistsVia
// + ExportedReadGateVerdictVia through DockerExecRunner, proving:
//
//  1. FS separation: the verdict file does NOT exist at the same path on the host
//     (os.Stat returns an error → the runner truly crosses a filesystem boundary).
//  2. Runner routing: DockerExecRunner delivers `test -s <path>` and `cat <path>`
//     into the container where the file IS seeded → returns true / GateActionAllow.
//
// This is the L3 analog of TestL2_SSHLocalhost_GateVerdictFSAxis_hk8u2al but
// with a genuine Linux container instead of a sandboxed localhost SSH session.
func TestL3_ContainerFS_GateVerdictAxis_hkyflqo(t *testing.T) {
	t.Parallel()

	if ok, detail := hkyflqoDockerAvailable(t.Context()); !ok {
		t.Skipf("L3 FS-axis requires Docker; skipping. probe: %s", detail)
	}
	if !hkyflqoAlpineAvailable(t.Context()) {
		t.Skip("L3 FS-axis requires alpine:latest image; skipping (image absent and pull failed)")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	containerID := hkyflqoStartContainer(t, ctx)

	// Seed gate-verdict.json INSIDE the container only.
	const containerVerdictPath = "/tmp/hkyflqo-a/.harmonik/gate-verdict.json"
	const verdictJSON = `{"schema_version":1,"decision":"allow","reason":"hk-yflqo L3 container gate"}`
	hkyflqoSeedFile(t, ctx, containerID, containerVerdictPath, verdictJSON)

	// ── Precondition: file must NOT exist on the host (FS separation proof). ──
	if _, err := os.Stat(containerVerdictPath); err == nil {
		t.Fatal("L3 FS-axis: precondition failed: gate-verdict.json exists on host; " +
			"cannot prove FS separation (container path must not exist on host)")
	}

	runner := hkyflqoDockerExecRunner{ContainerID: containerID}

	// ── gateVerdictExistsVia via DockerExecRunner ─────────────────────────────
	if !daemon.ExportedGateVerdictExistsVia(ctx, runner, containerVerdictPath) {
		t.Fatal("L3 FS-axis GateVerdictExists: DockerExecRunner returned false; " +
			"expected docker exec to find the seeded file inside the container")
	}

	// ── readGateVerdictVia via DockerExecRunner ───────────────────────────────
	action, err := daemon.ExportedReadGateVerdictVia(ctx, runner, containerVerdictPath)
	if err != nil {
		t.Fatalf("L3 FS-axis ReadGateVerdict: unexpected error over docker exec: %v", err)
	}
	if action != core.GateActionAllow {
		t.Errorf("L3 FS-axis ReadGateVerdict: action=%q; want %q", action, core.GateActionAllow)
	}

	// ── Negative guard: nil runner falls back to host os.Stat → file absent. ──
	if daemon.ExportedGateVerdictExistsVia(ctx, nil, containerVerdictPath) {
		t.Error("L3 FS-axis NilRunner: returned true; seam is broken — " +
			"nil runner must not find a file that only exists inside the container")
	}

	t.Logf("L3 container FS-axis OK: gate-verdict.json in container %s read via DockerExecRunner (absent on host)", containerID[:12])
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario B — Linux collector (AC-B)
// ─────────────────────────────────────────────────────────────────────────────

// TestL3_Container_LinuxCollector_hkyflqo calls workers.CollectReport with
// Worker{OS:"linux"} + DockerExecRunner against an alpine:latest container and
// verifies that the parsed WorkerReportPayload has sane Linux-sourced values:
//
//   - Load1 >= 0 (from /proc/loadavg via awk)
//   - NCPU >= 1 (from /proc/cpuinfo via grep -c)
//   - MemTotalMB > 0 (from /proc/meminfo MemTotal)
//   - MemFreeMB > 0 (from /proc/meminfo MemAvailable — the new memfree= key)
//
// This test is the only one that exercises the linuxCollectorScript and the
// new memfree=/swapused= parse paths introduced by this bead.
func TestL3_Container_LinuxCollector_hkyflqo(t *testing.T) {
	t.Parallel()

	if ok, detail := hkyflqoDockerAvailable(t.Context()); !ok {
		t.Skipf("L3 Linux-collector requires Docker; skipping. probe: %s", detail)
	}
	if !hkyflqoAlpineAvailable(t.Context()) {
		t.Skip("L3 Linux-collector requires alpine:latest image; skipping")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()

	containerID := hkyflqoStartContainer(t, ctx)
	runner := hkyflqoDockerExecRunner{ContainerID: containerID}

	// Worker declared with OS:"linux" so CollectReport dispatches linuxCollectorScript.
	// RepoPath="/tmp" is always present in alpine (no git needed — the worktrees
	// line falls back to 0 when git is absent, which is acceptable for this test).
	w := workers.Worker{
		Name:     "hkyflqo-test-container",
		OS:       "linux",
		RepoPath: "/tmp",
		MaxSlots: 2,
	}

	p, err := workers.CollectReport(ctx, runner, w, nil, 0, nil)
	if err != nil {
		t.Fatalf("L3 LinuxCollector: CollectReport failed: %v", err)
	}

	if p.Load1 < 0 {
		t.Errorf("L3 LinuxCollector: Load1=%v; must be >= 0", p.Load1)
	}
	if p.NCPU < 1 {
		t.Errorf("L3 LinuxCollector: NCPU=%d; must be >= 1 (Linux /proc/cpuinfo)", p.NCPU)
	}
	if p.MemTotalMB <= 0 {
		t.Errorf("L3 LinuxCollector: MemTotalMB=%d; must be > 0 (Linux /proc/meminfo MemTotal)", p.MemTotalMB)
	}
	if p.MemFreeMB <= 0 {
		t.Errorf("L3 LinuxCollector: MemFreeMB=%d; must be > 0 (Linux /proc/meminfo MemAvailable via memfree= key)", p.MemFreeMB)
	}
	// WorkerName is stamped by CollectReport, not the parser.
	if p.WorkerName != w.Name {
		t.Errorf("L3 LinuxCollector: WorkerName=%q; want %q", p.WorkerName, w.Name)
	}
	if p.SampledAt == "" {
		t.Error("L3 LinuxCollector: SampledAt is empty; CollectReport must stamp it")
	}

	t.Logf("L3 Linux collector OK: container=%s Load1=%.2f NCPU=%d MemTotalMB=%d MemFreeMB=%d SwapUsedMB=%d DiskFreeMB=%d",
		containerID[:12], p.Load1, p.NCPU, p.MemTotalMB, p.MemFreeMB, p.SwapUsedMB, p.DiskFreeMB)
}

// ─────────────────────────────────────────────────────────────────────────────
// Scenario C — Multi-container disjoint FS (AC-C)
// ─────────────────────────────────────────────────────────────────────────────

// TestL3_MultiContainer_DisjointFS_hkyflqo spins up N=2 Docker containers
// ("remote workers") and seeds the SAME containerPath in each with DIFFERENT
// gate decisions (allow vs deny). It verifies that:
//
//  1. DockerExecRunnerA reading from container A returns GateActionAllow.
//  2. DockerExecRunnerB reading from container B returns GateActionDeny.
//  3. The filesystems are genuinely isolated: seeding in A does not affect B.
//
// This faithfully reproduces the "multi-remote + local-at-once" topology: N
// workers with disjoint FSes, a DockerExecRunner per worker routing to the
// correct isolated namespace. No cloud required.
func TestL3_MultiContainer_DisjointFS_hkyflqo(t *testing.T) {
	t.Parallel()

	if ok, detail := hkyflqoDockerAvailable(t.Context()); !ok {
		t.Skipf("L3 multi-container requires Docker; skipping. probe: %s", detail)
	}
	if !hkyflqoAlpineAvailable(t.Context()) {
		t.Skip("L3 multi-container requires alpine:latest image; skipping")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()

	// Start N=2 containers.
	containerA := hkyflqoStartContainer(t, ctx)
	containerB := hkyflqoStartContainer(t, ctx)

	// Same path inside each container — the FS isolation means they hold different
	// content.
	const sharedPath = "/tmp/hkyflqo-multi/.harmonik/gate-verdict.json"

	// Seed "allow" in container A, "deny" in container B.
	hkyflqoSeedFile(t, ctx, containerA, sharedPath,
		`{"schema_version":1,"decision":"allow","reason":"hk-yflqo container-A"}`)
	hkyflqoSeedFile(t, ctx, containerB, sharedPath,
		`{"schema_version":1,"decision":"deny","reason":"hk-yflqo container-B"}`)

	runnerA := hkyflqoDockerExecRunner{ContainerID: containerA}
	runnerB := hkyflqoDockerExecRunner{ContainerID: containerB}

	// ── Container A: DockerExecRunnerA reads "allow" ───────────────────────────
	if !daemon.ExportedGateVerdictExistsVia(ctx, runnerA, sharedPath) {
		t.Fatal("L3 MultiContainer: container A: GateVerdictExists returned false; seeded file not found")
	}
	actionA, errA := daemon.ExportedReadGateVerdictVia(ctx, runnerA, sharedPath)
	if errA != nil {
		t.Fatalf("L3 MultiContainer: container A: ReadGateVerdict error: %v", errA)
	}
	if actionA != core.GateActionAllow {
		t.Errorf("L3 MultiContainer: container A: action=%q; want %q", actionA, core.GateActionAllow)
	}

	// ── Container B: DockerExecRunnerB reads "deny" ────────────────────────────
	if !daemon.ExportedGateVerdictExistsVia(ctx, runnerB, sharedPath) {
		t.Fatal("L3 MultiContainer: container B: GateVerdictExists returned false; seeded file not found")
	}
	actionB, errB := daemon.ExportedReadGateVerdictVia(ctx, runnerB, sharedPath)
	if errB != nil {
		t.Fatalf("L3 MultiContainer: container B: ReadGateVerdict error: %v", errB)
	}
	if actionB != core.GateActionDeny {
		t.Errorf("L3 MultiContainer: container B: action=%q; want %q", actionB, core.GateActionDeny)
	}

	// ── Cross-runner isolation: RunnerA does NOT reach container B's content ───
	// Container A holds "allow"; if the filesystems were shared, RunnerA would
	// read "deny" from container B's seed. It must return "allow" (its own FS).
	actionA2, errA2 := daemon.ExportedReadGateVerdictVia(ctx, runnerA, sharedPath)
	if errA2 != nil {
		t.Fatalf("L3 MultiContainer isolation re-check: container A second read error: %v", errA2)
	}
	if actionA2 != core.GateActionAllow {
		t.Errorf("L3 MultiContainer isolation: container A re-read action=%q; want %q "+
			"(container B seed must not have bled into container A FS)", actionA2, core.GateActionAllow)
	}

	// ── Host-side FS separation guard ─────────────────────────────────────────
	// File must be absent on the macOS host at the container path.
	if _, err := os.Stat(sharedPath); err == nil {
		t.Error("L3 MultiContainer: gate-verdict.json exists at the container path on the host; " +
			"FS isolation broken (host and container share the same path?)")
	}

	t.Logf("L3 multi-container OK: A=%s→allow B=%s→deny; filesystems disjoint (same path, different content)",
		containerA[:12], containerB[:12])
}
