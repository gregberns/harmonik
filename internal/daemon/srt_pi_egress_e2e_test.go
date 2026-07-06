package daemon_test

// srt_pi_egress_e2e_test.go — ISOLATED end-to-end proof of the Pi srt egress
// bug and its fix (hk-ybuts / hk-u69my). Inaugural instance of the PRE-DEPLOY
// END-TO-END TEST GATE (orchestrator-rules): a changed daemon launch behavior is
// proven by an e2e test that reproduces the REAL launch path in isolation — no
// live daemon is constructed or touched.
//
// # The bug
//
// The daemon launches the Pi harness (SessionIDCaptured) via the exec path with
// an srt sandbox wrap. With the DGX vLLM model server confirmed LIVE, a
// daemon-launched Pi run failed in ~4s ("implement exited without advancing
// HEAD") and the model server logged ZERO inbound requests: the srt-wrapped Pi
// process never reached the model. An identical OUT-OF-DAEMON Pi launch (same
// argv/env/models.json, NO srt wrap) succeeded end-to-end.
//
// # Root cause (proven by this test)
//
// GenerateSandboxProfile hardcoded network.allowLocalBinding=false. The model
// server is at http://192.168.1.86:8551 — a private-LAN address that srt's
// default no_proxy set (127.0.0.1, 10/8, 172.16/12, 192.168/16, …) routes as a
// DIRECT connection, bypassing srt's MITM proxy. macOS Seatbelt then denies that
// raw socket ("Operation not permitted") unless network.allowLocalBinding is
// true. srt's allowedDomains path only covers PROXIED public HTTPS (the
// openrouter.ai spike), so it does nothing for a direct-connect LAN/loopback
// endpoint. There was no config path to enable local binding, so a sandboxed Pi
// could never reach a locally-hosted model.
//
// # What this harness does (real-srt-spawn mode)
//
//  1. Stands up a STUB HTTP model server on loopback (httptest.Server) that
//     records whether it received a request and returns a minimal
//     OpenAI-completions response. Loopback is a faithful stand-in for the LAN
//     vLLM: both are no_proxy direct-connect addresses gated by allowLocalBinding
//     (verified out-of-band: srt blocks BOTH loopback and 192.168.x direct
//     sockets identically unless allowLocalBinding is set).
//  2. Builds the srt spawn decision via the REAL gate (ExportedSandboxSpawnForRun)
//     and wraps a trivial `curl <stub>` command via the REAL exec-path wrapper
//     (ExportedSandboxWrapExecArgv) — the exact functions the workloop uses for a
//     SessionIDCaptured (pi) run.
//  3. Spawns the wrapped argv and asserts the stub recorded the request.
//
// RED (old code): AllowLocalBinding is ignored (hardcoded false) → curl's socket
// is denied → stub NOT hit. GREEN (after fix): AllowLocalBinding is honored →
// stub hit.
//
// If srt or curl is unavailable the test SKIPS the real spawn and instead asserts
// on the generated profile JSON (allowLocalBinding must be true) — documented at
// the skip site.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
)

// egressStubModelServer starts a loopback HTTP server standing in for the local
// OpenAI-compatible model endpoint. It records whether any request arrived and
// returns a minimal completions body.
func egressStubModelServer(t *testing.T) (url string, wasHit func() bool) {
	t.Helper()
	var hit atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-stub","object":"text_completion","choices":[{"text":"PONG"}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, hit.Load
}

// egressSandboxInput builds a SandboxProfileInput exactly as the workloop would,
// toggling AllowLocalBinding. All REQUIRED fields are satisfied so
// GenerateSandboxProfile succeeds.
func egressSandboxInput(t *testing.T, allowLocalBinding bool) daemon.SandboxProfileInput {
	t.Helper()
	tmp := t.TempDir()
	return daemon.SandboxProfileInput{
		WorktreePath:      tmp,
		GitDir:            tmp + "/.git",
		RunID:             "egress-e2e-run",
		DaemonSockPath:    tmp + "/daemon.sock",
		AllowLocalBinding: allowLocalBinding,
	}
}

// egressPiSandboxConfig is the config the live deployment uses:
// backend=srt, harnesses:[pi], NO network block beyond the local-binding toggle.
func egressPiSandboxConfig(allowLocalBinding bool) daemon.SandboxConfig {
	return daemon.SandboxConfig{
		Backend:   "srt",
		Harnesses: []string{"pi"},
		Network: daemon.SandboxNetworkConfig{
			AllowLocalBinding: allowLocalBinding,
		},
	}
}

// runWrappedCurl spawns the srt-wrapped `curl <url>` and reports whether it
// exited 0. It reproduces the workloop's exec path: gate → exec-wrap → spawn.
func runWrappedCurl(t *testing.T, allowLocalBinding bool, url string) (ranClean bool, combined string) {
	t.Helper()

	// REAL gate: for a pi run under backend=srt + harnesses:[pi] this returns a
	// non-nil SrtSpawnConfig carrying the profile input. AgentType "pi".
	spawn := daemon.ExportedSandboxSpawnForRun(
		egressPiSandboxConfig(allowLocalBinding),
		core.AgentType("pi"),
		egressSandboxInput(t, allowLocalBinding),
	)
	if spawn == nil {
		t.Fatalf("gate returned nil spawn — pi run under backend=srt should be wrapped")
	}
	spawn.SrtBinary = "srt"

	// REAL exec-path wrap: srt --settings <profile> curl <long-flags> <url>.
	//
	// curl's LONG flags are deliberate: srt uses commander, whose parser would
	// otherwise consume curl's short -s/-o/-w as srt's own -s/--settings etc.
	// (interleaved-option collision). Pi's real argv (pi --mode json --provider
	// … --model … <seed>) is all long flags, so it has no such collision — the
	// long-flag curl faithfully mirrors that argv shape through the SAME wrap.
	bin, args, err := daemon.ExportedSandboxWrapExecArgv(
		spawn, "curl",
		[]string{"--silent", "--max-time", "6", "--output", "/dev/null", "--write-out", "%{http_code}", url},
	)
	if err != nil {
		t.Fatalf("exec-wrap failed: %v", err)
	}
	if bin != "srt" {
		t.Fatalf("expected srt-wrapped binary, got %q", bin)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	out, runErr := cmd.CombinedOutput()
	return runErr == nil, string(out)
}

// TestPiEgress_LocalBindingReachesStub is the RED→GREEN proof. On old code
// (AllowLocalBinding ignored) the sandboxed curl cannot reach the loopback stub
// and the stub is never hit → FAIL. After the fix, the stub is hit → PASS.
func TestPiEgress_LocalBindingReachesStub(t *testing.T) {
	if _, err := exec.LookPath("srt"); err != nil {
		t.Skip("srt binary not available; real-spawn egress repro requires srt")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available; real-spawn egress repro requires curl")
	}

	url, wasHit := egressStubModelServer(t)

	ranClean, out := runWrappedCurl(t, true /*allowLocalBinding*/, url)
	if !wasHit() {
		t.Fatalf("sandboxed process did NOT reach the stub model server "+
			"(curl clean=%v, output=%q). With AllowLocalBinding=true the srt profile "+
			"must permit the direct loopback socket. RED before the GenerateSandboxProfile "+
			"fix (allowLocalBinding hardcoded false); GREEN after.", ranClean, out)
	}
}

// TestPiEgress_ProfileHonorsLocalBinding is the pure-profile assertion (no spawn):
// the generated srt JSON must reflect AllowLocalBinding. This is the fallback
// mode's assertion and also guards the wiring directly.
func TestPiEgress_ProfileHonorsLocalBinding(t *testing.T) {
	for _, want := range []bool{false, true} {
		profileBytes, err := daemon.GenerateSandboxProfile(egressSandboxInput(t, want))
		if err != nil {
			t.Fatalf("GenerateSandboxProfile: %v", err)
		}
		var parsed struct {
			Network struct {
				AllowLocalBinding bool `json:"allowLocalBinding"`
			} `json:"network"`
		}
		if err := json.Unmarshal(profileBytes, &parsed); err != nil {
			t.Fatalf("unmarshal profile: %v", err)
		}
		if parsed.Network.AllowLocalBinding != want {
			t.Fatalf("profile network.allowLocalBinding=%v, want %v — "+
				"AllowLocalBinding is not wired into GenerateSandboxProfile "+
				"(hardcoded false = the egress bug). Profile: %s",
				parsed.Network.AllowLocalBinding, want, profileBytes)
		}
	}
}
