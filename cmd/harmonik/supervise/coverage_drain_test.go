package supervisecmd

// coverage_drain_test.go — behaviour tests for pure-logic surfaces of the
// supervise verbs that do NOT require a live daemon or a spawned process:
//   - ps.go:      RunPs / printPsResult formatting (read-only)
//   - status.go:  buildStatusWithProbe metadata population; supervisorProjectHash
//   - pause.go:   opVerb, isSocketAbsentOrRefused, sendOperatorOp socket paths
//   - config.go:  WriteSentinel round-trip
//   - assetskew:  notifyCaptainSkew body selection; execCommsSend test-binary guard
//
// Chunk I coverage drain (hk-z646k neighbourhood).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- ps.go ------------------------------------------------------------------

// TestPrintPsResult_RendersAllSections verifies the human-readable ps rendering
// emits the project, hash, and every process/tmux signature line.
func TestPrintPsResult_RendersAllSections(t *testing.T) {
	t.Parallel()
	result := PsResult{
		SchemaVersion: 1,
		ProjectDir:    "/some/proj",
		ProjectHash:   "deadbeef",
		ProcessSignatures: []ProcessSignature{
			{Name: "daemon", Pattern: "harmonik --project /some/proj", Command: "pgrep -af 'x'"},
		},
		TmuxSessions: []TmuxSessionTarget{
			{Name: "flywheel", Session: "harmonik-deadbeef-flywheel", Command: "tmux has-session -t 'y'"},
		},
	}
	var out bytes.Buffer
	if err := printPsResult(&out, result); err != nil {
		t.Fatalf("printPsResult: %v", err)
	}
	s := out.String()
	for _, want := range []string{
		"project:      /some/proj",
		"project_hash: deadbeef",
		"process_signatures:",
		"harmonik --project /some/proj",
		"pgrep -af 'x'",
		"tmux_sessions:",
		"harmonik-deadbeef-flywheel",
		"tmux has-session -t 'y'",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q; got:\n%s", want, s)
		}
	}
}

// TestRunPs_Help verifies --help prints usage and exits 0 without resolving a
// project dir.
func TestRunPs_Help(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunPs([]string{"--help"}, &out, &errB); code != 0 {
		t.Fatalf("RunPs --help exit = %d; want 0", code)
	}
	if !strings.Contains(out.String(), "harmonik supervise ps") {
		t.Errorf("usage not printed; got:\n%s", out.String())
	}
}

// TestRunPs_JSONFields verifies --project + --json emits a schema-versioned
// PsResult whose canonical fields are populated for a real directory.
func TestRunPs_JSONFields(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out, errB bytes.Buffer
	code := RunPs([]string{"--project", dir, "--json"}, &out, &errB)
	if code != 0 {
		t.Fatalf("RunPs --json exit = %d; want 0 (stderr: %s)", code, errB.String())
	}
	var got PsResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal ps json: %v (out: %s)", err, out.String())
	}
	if got.SchemaVersion != 1 {
		t.Errorf("schema_version = %d; want 1", got.SchemaVersion)
	}
	if len(got.ProcessSignatures) == 0 || len(got.TmuxSessions) == 0 {
		t.Errorf("expected populated signatures; got %+v", got)
	}
	if got.ProjectHash == "" {
		t.Error("expected non-empty project hash")
	}
}

// TestRunPs_Human verifies the default (non-JSON) path renders through to
// printPsResult for a real dir.
func TestRunPs_Human(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out, errB bytes.Buffer
	if code := RunPs([]string{"--project=" + dir}, &out, &errB); code != 0 {
		t.Fatalf("RunPs exit = %d; want 0 (stderr: %s)", code, errB.String())
	}
	if !strings.Contains(out.String(), "process_signatures:") {
		t.Errorf("expected human output; got:\n%s", out.String())
	}
}

// TestCanonicalProjectDir_Missing verifies the EvalSymlinks error branch: a
// non-existent path yields an error, not a silent empty string.
func TestCanonicalProjectDir_Missing(t *testing.T) {
	t.Parallel()
	_, err := canonicalProjectDir(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error resolving a non-existent path")
	}
}

// --- status.go --------------------------------------------------------------

// TestBuildStatusWithProbe_PopulatesMetadata verifies the sentinel/config/
// loop-status file surfaces flow into the StatusResult (the top branches of
// buildStatusWithProbe that the keeper-loop tests do not touch).
func TestBuildStatusWithProbe_PopulatesMetadata(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteSentinel(dir); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if err := WriteConfigAtomic(dir, Config{
		SchemaVersion:    1,
		RestartPolicy:    "on-failure",
		RestartMax:       5,
		StartedAt:        "2026-01-01T00:00:00Z",
		DaemonInstanceID: "daemon-xyz",
	}); err != nil {
		t.Fatalf("WriteConfigAtomic: %v", err)
	}
	if err := WriteLoopStatusAtomic(dir, LoopStatusRecord{
		SchemaVersion: 1,
		Status:        CognitionLoopStatusBudgetPaused,
		PauseReason:   "budget-paused",
	}); err != nil {
		t.Fatalf("WriteLoopStatusAtomic: %v", err)
	}

	// No pidfile, keeper probe false → stopped, but metadata must still populate.
	res := buildStatusWithProbe(dir, func(string) bool { return false })
	if !res.SentinelOK {
		t.Error("expected SentinelOK=true")
	}
	if res.RestartPolicy != "on-failure" || res.RestartMax != 5 {
		t.Errorf("restart metadata not populated: %+v", res)
	}
	if res.StartedAt != "2026-01-01T00:00:00Z" || res.DaemonID != "daemon-xyz" {
		t.Errorf("config metadata not populated: %+v", res)
	}
	if res.LoopStatus != "budget-paused" || res.PauseReason != "budget-paused" {
		t.Errorf("loop-status not populated: %+v", res)
	}
	if res.Status != "stopped" {
		t.Errorf("expected stopped with no pidfile + false probe; got %q", res.Status)
	}
}

// TestSupervisorProjectHash_StableAndHex verifies the project hash is a stable
// 12-hex-char digest and matches FlywheelSessionName's embedded hash.
func TestSupervisorProjectHash_StableAndHex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h1 := supervisorProjectHash(dir)
	h2 := supervisorProjectHash(dir)
	if h1 != h2 {
		t.Errorf("hash not stable: %q != %q", h1, h2)
	}
	if len(h1) != 12 {
		t.Errorf("hash len = %d; want 12 (6 bytes hex)", len(h1))
	}
	if !strings.Contains(FlywheelSessionName(dir), h1) {
		t.Errorf("FlywheelSessionName %q should embed hash %q", FlywheelSessionName(dir), h1)
	}
}

// --- pause.go ---------------------------------------------------------------

// TestOpVerb verifies the op→verb mapping incl. the passthrough default.
func TestOpVerb(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"operator-pause":  "pause",
		"operator-resume": "resume",
		"something-else":  "something-else",
	}
	for op, want := range cases {
		if got := opVerb(op); got != want {
			t.Errorf("opVerb(%q) = %q; want %q", op, got, want)
		}
	}
}

// TestIsSocketAbsentOrRefused verifies the daemon-down classifier over nil,
// matching, and non-matching errors.
func TestIsSocketAbsentOrRefused(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("dial unix /x: connect: no such file or directory"), true},
		{errors.New("dial unix /x: connect: connection refused"), true},
		{errors.New("no such file"), true},
		{errors.New("i/o timeout"), false},
	}
	for _, c := range cases {
		if got := isSocketAbsentOrRefused(c.err); got != c.want {
			t.Errorf("isSocketAbsentOrRefused(%v) = %v; want %v", c.err, got, c.want)
		}
	}
}

// shortSocket returns a unix-socket path short enough to stay under the ~104
// char sun_path limit (t.TempDir() names are too long on macOS).
func shortSocket(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "hks")
	if err != nil {
		t.Fatalf("mkdirtemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return filepath.Join(d, "d.sock")
}

// TestSendOperatorOp_SocketAbsent verifies dialing a non-existent socket path
// returns exit 17 (daemon down) and writes the daemon-not-running message.
func TestSendOperatorOp_SocketAbsent(t *testing.T) {
	t.Parallel()
	var errB bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sock := shortSocket(t)
	code := sendOperatorOp(ctx, sock, "operator-pause", &errB)
	if code != 17 {
		t.Fatalf("code = %d; want 17 for absent socket", code)
	}
	if !strings.Contains(errB.String(), "daemon not running") {
		t.Errorf("expected daemon-not-running message; got %q", errB.String())
	}
}

// TestSendOperatorOp_Acked stands up a one-shot unix socket that replies
// {"ok":true} and verifies sendOperatorOp returns 0 after the full request/
// response round-trip (marshal, write, half-close, decode).
func TestSendOperatorOp_Acked(t *testing.T) {
	t.Parallel()
	sock := shortSocket(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Read the request op to confirm the client actually sent it.
		var req struct {
			Op string `json:"op"`
		}
		_ = json.NewDecoder(conn).Decode(&req)
		_, _ = conn.Write([]byte(`{"ok":true}`))
	}()

	var errB bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code := sendOperatorOp(ctx, sock, "operator-resume", &errB)
	if code != 0 {
		t.Fatalf("code = %d; want 0 (stderr: %s)", code, errB.String())
	}
}

// TestSendOperatorOp_DaemonError verifies an {"ok":false,"error":...} response
// maps to exit 1 with the daemon-error message surfaced.
func TestSendOperatorOp_DaemonError(t *testing.T) {
	t.Parallel()
	sock := shortSocket(t)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var req struct {
			Op string `json:"op"`
		}
		_ = json.NewDecoder(conn).Decode(&req)
		_, _ = conn.Write([]byte(`{"ok":false,"error":"already draining"}`))
	}()

	var errB bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code := sendOperatorOp(ctx, sock, "operator-pause", &errB)
	if code != 1 {
		t.Fatalf("code = %d; want 1 on daemon error", code)
	}
	if !strings.Contains(errB.String(), "already draining") {
		t.Errorf("expected daemon-error text; got %q", errB.String())
	}
}

// --- config.go --------------------------------------------------------------

// TestWriteSentinel_RoundTrip verifies WriteSentinel creates the cognition dir
// and writes the exclusion marker content, and RemoveSentinel clears it (and is
// idempotent on a second call).
func TestWriteSentinel_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteSentinel(dir); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	data, err := os.ReadFile(SentinelPath(dir))
	if err != nil {
		t.Fatalf("read sentinel: %v", err)
	}
	if string(data) != "schema_version=1\n" {
		t.Errorf("sentinel content = %q; want %q", string(data), "schema_version=1\n")
	}
	if err := RemoveSentinel(dir); err != nil {
		t.Fatalf("RemoveSentinel: %v", err)
	}
	if _, err := os.Stat(SentinelPath(dir)); !os.IsNotExist(err) {
		t.Errorf("expected sentinel removed; stat err = %v", err)
	}
	// Idempotent: removing an absent sentinel is not an error.
	if err := RemoveSentinel(dir); err != nil {
		t.Errorf("RemoveSentinel (absent) should be nil; got %v", err)
	}
}

// --- assetskew.go -----------------------------------------------------------

// TestNotifyCaptainSkew_BodySelection verifies the never-synced vs behind-binary
// body is chosen by the verdict, delegating to the injected CommsSendNotifier.
func TestNotifyCaptainSkew_BodySelection(t *testing.T) {
	saved := CommsSendNotifier
	defer func() { CommsSendNotifier = saved }()

	var bodies []string
	CommsSendNotifier = func(_, body string) error {
		bodies = append(bodies, body)
		return nil
	}

	notifyCaptainSkew("/p", AssetSkewVerdict{Skewed: true, NeverSynced: true, ChangedCount: 4}, nil, nil)
	notifyCaptainSkew("/p", AssetSkewVerdict{Skewed: true, ChangedCount: 6, ConflictCount: 2}, nil, nil)

	if len(bodies) != 2 {
		t.Fatalf("expected 2 notify bodies; got %d", len(bodies))
	}
	if !strings.Contains(bodies[0], "never synced") || !strings.Contains(bodies[0], "4") {
		t.Errorf("never-synced body wrong: %q", bodies[0])
	}
	if !strings.Contains(bodies[1], "behind the running binary") ||
		!strings.Contains(bodies[1], "6") || !strings.Contains(bodies[1], "2 conflicts") {
		t.Errorf("behind-binary body wrong: %q", bodies[1])
	}
}

// TestNotifyCaptainSkew_SendErrorSwallowed verifies a notifier failure is
// best-effort: it does not panic and returns cleanly.
func TestNotifyCaptainSkew_SendErrorSwallowed(t *testing.T) {
	saved := CommsSendNotifier
	defer func() { CommsSendNotifier = saved }()
	CommsSendNotifier = func(_, _ string) error { return errors.New("no captain") }
	// Must not panic; log is nil so the error path is exercised silently.
	notifyCaptainSkew("/p", AssetSkewVerdict{Skewed: true, ChangedCount: 1}, nil, nil)
}

// TestExecCommsSend_RefusesTestBinary verifies the fork-bomb guard: the real
// execCommsSend, run from the test binary (name ends in .test), refuses to
// re-exec itself rather than re-running the whole suite.
func TestExecCommsSend_RefusesTestBinary(t *testing.T) {
	t.Parallel()
	err := execCommsSend(t.TempDir(), "hello")
	if err == nil || !strings.Contains(err.Error(), "refusing comms send from test binary") {
		t.Fatalf("expected test-binary refusal; got %v", err)
	}
}

// --- pause/resume arg parsing -----------------------------------------------

// TestRunPause_Help verifies --help prints usage and exits 0 (no socket dial).
func TestRunPause_Help(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunPause([]string{"--help"}, &out, &errB); code != 0 {
		t.Fatalf("RunPause --help exit = %d; want 0", code)
	}
	if !strings.Contains(out.String(), "harmonik supervise pause") {
		t.Errorf("usage not printed; got:\n%s", out.String())
	}
}

// TestRunPause_UnknownArg verifies an unrecognised flag exits 1 with a message.
func TestRunPause_UnknownArg(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunPause([]string{"--bogus"}, &out, &errB); code != 1 {
		t.Fatalf("RunPause --bogus exit = %d; want 1", code)
	}
	if !strings.Contains(errB.String(), "unknown argument") {
		t.Errorf("expected unknown-argument message; got %q", errB.String())
	}
}

// TestRunResume_Help verifies --help prints usage and exits 0 (no socket dial).
func TestRunResume_Help(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunResume([]string{"--help"}, &out, &errB); code != 0 {
		t.Fatalf("RunResume --help exit = %d; want 0", code)
	}
	if !strings.Contains(out.String(), "harmonik supervise resume") {
		t.Errorf("usage not printed; got:\n%s", out.String())
	}
}

// TestRunResume_UnknownArg verifies an unrecognised flag exits 1 with a message.
func TestRunResume_UnknownArg(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunResume([]string{"--bogus"}, &out, &errB); code != 1 {
		t.Fatalf("RunResume --bogus exit = %d; want 1", code)
	}
	if !strings.Contains(errB.String(), "unknown argument") {
		t.Errorf("expected unknown-argument message; got %q", errB.String())
	}
}

// --- status.go RunStatus ----------------------------------------------------

// TestRunStatus_Help verifies --help prints usage and exits 0.
func TestRunStatus_Help(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunStatus([]string{"--help"}, &out, &errB); code != 0 {
		t.Fatalf("RunStatus --help exit = %d; want 0", code)
	}
	if !strings.Contains(out.String(), "harmonik supervise status") {
		t.Errorf("usage not printed; got:\n%s", out.String())
	}
}

// TestRunStatus_JSON verifies the JSON path over a fresh project emits a
// schema-versioned StatusResult with a well-formed status field. The exact
// value is left unasserted: buildStatus's keeper-loop probe (pgrep/tmux) is
// environment-dependent, so a live keeper loop on the host box may legitimately
// report "running" for a fresh temp dir.
func TestRunStatus_JSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out, errB bytes.Buffer
	if code := RunStatus([]string{"--project", dir, "--json"}, &out, &errB); code != 0 {
		t.Fatalf("RunStatus --json exit = %d; want 0 (stderr: %s)", code, errB.String())
	}
	var got StatusResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal status json: %v (out: %s)", err, out.String())
	}
	if got.SchemaVersion != 1 {
		t.Errorf("schema_version = %d; want 1", got.SchemaVersion)
	}
	switch got.Status {
	case "running", "stopped", "unknown":
	default:
		t.Errorf("status = %q; want a known status value", got.Status)
	}
}

// TestRunStatus_Human verifies the default human path renders a status line and
// surfaces loop_status when loop-status.json is present.
func TestRunStatus_Human(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := WriteSentinel(dir); err != nil {
		t.Fatalf("WriteSentinel: %v", err)
	}
	if err := WriteLoopStatusAtomic(dir, LoopStatusRecord{
		SchemaVersion: 1,
		Status:        CognitionLoopStatusCircuitTripped,
		PauseReason:   "circuit-tripped",
	}); err != nil {
		t.Fatalf("WriteLoopStatusAtomic: %v", err)
	}
	var out, errB bytes.Buffer
	if code := RunStatus([]string{"--project=" + dir}, &out, &errB); code != 0 {
		t.Fatalf("RunStatus exit = %d; want 0 (stderr: %s)", code, errB.String())
	}
	s := out.String()
	if !strings.Contains(s, "status:") {
		t.Errorf("expected status line; got:\n%s", s)
	}
	if !strings.Contains(s, "loop_status:   circuit-tripped") {
		t.Errorf("expected loop_status line; got:\n%s", s)
	}
	if !strings.Contains(s, "pause_reason:  circuit-tripped") {
		t.Errorf("expected pause_reason line; got:\n%s", s)
	}
}

// --- logs.go arg parsing ----------------------------------------------------

// TestRunLogs_Help verifies --help prints usage and exits 0 (no tmux call).
func TestRunLogs_Help(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunLogs([]string{"--help"}, &out, &errB); code != 0 {
		t.Fatalf("RunLogs --help exit = %d; want 0", code)
	}
	if !strings.Contains(out.String(), "harmonik supervise logs") {
		t.Errorf("usage not printed; got:\n%s", out.String())
	}
}

// TestRunLogs_InvalidLines verifies a non-numeric --lines value exits 1 in both
// the split (`--lines X`) and joined (`--lines=X`) spellings.
func TestRunLogs_InvalidLines(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--lines", "notanumber"},
		{"--lines=notanumber"},
	} {
		var out, errB bytes.Buffer
		if code := RunLogs(args, &out, &errB); code != 1 {
			t.Errorf("RunLogs %v exit = %d; want 1", args, code)
		}
		if !strings.Contains(errB.String(), "invalid --lines value") {
			t.Errorf("RunLogs %v: expected invalid-lines message; got %q", args, errB.String())
		}
	}
}

// --- stop.go arg parsing ----------------------------------------------------

// TestRunStop_Help verifies --help prints usage and exits 0.
func TestRunStop_Help(t *testing.T) {
	t.Parallel()
	var out, errB bytes.Buffer
	if code := RunStop([]string{"--help"}, &out, &errB); code != 0 {
		t.Fatalf("RunStop --help exit = %d; want 0", code)
	}
	if !strings.Contains(out.String(), "harmonik supervise stop") {
		t.Errorf("usage not printed; got:\n%s", out.String())
	}
}

// TestRunStop_NoPidfile verifies stop is idempotent: a project with no pidfile
// reports "not running" and exits 0 (PL-011 / hk-ky7ye idempotency).
func TestRunStop_NoPidfile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var out, errB bytes.Buffer
	if code := RunStop([]string{"--project", dir}, &out, &errB); code != 0 {
		t.Fatalf("RunStop no-pidfile exit = %d; want 0 (stderr: %s)", code, errB.String())
	}
	if !strings.Contains(out.String(), "supervisor not running") {
		t.Errorf("expected idempotent not-running message; got:\n%s", out.String())
	}
}

// --- start.go probeDaemonSocket ---------------------------------------------

// TestProbeDaemonSocket_Absent verifies probing an absent daemon socket returns
// the daemon-down exit code (17) and writes the start hint.
func TestProbeDaemonSocket_Absent(t *testing.T) {
	t.Parallel()
	var errB bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code := probeDaemonSocket(ctx, shortSocket(t), &errB)
	if code != ExitCodeDaemonDown {
		t.Fatalf("probeDaemonSocket exit = %d; want %d", code, ExitCodeDaemonDown)
	}
	if !strings.Contains(errB.String(), "daemon not running") {
		t.Errorf("expected daemon-not-running hint; got %q", errB.String())
	}
}
