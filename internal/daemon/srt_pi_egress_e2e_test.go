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
// # The TRUE srt local-egress finding (Option A rationale)
//
// srt does NOT treat loopback and a REMOTE private-LAN host identically.
// Empirically proven this session:
//
//   - LOCAL interfaces (loopback 127.0.0.1, and this host's own non-loopback
//     interface IPs): with network.allowLocalBinding=true the direct socket OPENS.
//     curl reaches a locally-hosted endpoint. (Proven by
//     TestPiEgress_LocalInterfaceIsPermitted below.)
//   - REMOTE host on the LAN (the DGX box at 192.168.1.86:8551 — a DIFFERENT
//     machine): the direct socket stays BLOCKED under srt regardless of
//     allowLocalBinding. A raw socket to a remote host is denied.
//
// The earlier comment here claimed srt "blocks BOTH loopback and 192.168.x direct
// sockets identically unless allowLocalBinding is set." That premise was FALSE on
// two counts: allowLocalBinding opens loopback, AND it opens this host's own
// non-loopback interfaces; the true discriminator is REMOTE-HOST vs LOCAL, not
// loopback vs non-loopback. Because the old test only exercised a loopback stub,
// it went green while the REAL path to the remote DGX model server was still
// blocked. (Note: the remote-host block is not reproducible with an in-process
// stub, since a stub can only bind local interfaces — see
// TestPiEgress_LocalInterfaceIsPermitted's comment.)
//
// The operator chose OPTION A: keep the srt sandbox and reach the DGX model over a
// LOOPBACK SSH TUNNEL — config base_url is now http://127.0.0.1:8551/v1. This
// works precisely because loopback opens under allowLocalBinding while the LAN
// address does not. So the loopback-reaches-stub assertion below faithfully
// mirrors the REAL Option-A path: config -> 127.0.0.1 tunnel -> DGX.
//
// # What this harness does (real-srt-spawn mode)
//
//  1. Stands up a STUB HTTP model server on loopback (httptest.Server) that
//     records whether it received a request and returns a minimal
//     OpenAI-completions response. Loopback faithfully mirrors the real Option-A
//     path (config -> 127.0.0.1 tunnel -> DGX): both are a loopback direct-connect
//     socket gated by allowLocalBinding.
//  2. Builds the srt spawn decision via the REAL gate (ExportedSandboxSpawnForRun)
//     and wraps a trivial `curl <stub>` command via the REAL exec-path wrapper
//     (ExportedSandboxWrapExecArgv) — the exact functions the workloop uses for a
//     SessionIDCaptured (pi) run.
//  3. Spawns the wrapped argv and asserts the stub recorded the request.
//
// A companion test (TestPiEgress_LocalInterfaceIsPermitted) binds a stub to the
// host's real non-loopback IPv4 and shows the srt-wrapped curl CAN reach it with
// allowLocalBinding=true — pinning the true discriminator (remote-host vs local),
// since the actual DGX remote-host block cannot be reproduced by an in-process
// stub (a stub can only bind local interfaces).
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
	"net"
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
//
// The loopback stub is not merely a convenient stand-in: it mirrors the REAL
// Option-A path (config -> 127.0.0.1 SSH tunnel -> DGX), where reachability
// depends on allowLocalBinding opening the loopback socket. (A LAN address would
// NOT open — see TestPiEgress_NonLoopbackDirectIsBlocked.)
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

// firstNonLoopbackIPv4 returns the host's first non-loopback IPv4 address, or ""
// if none is found. This is the property that makes a stub bound to it a faithful
// reproduction of the REAL DGX target (a private-LAN direct-connect address),
// rather than a convenient loopback stand-in.
func firstNonLoopbackIPv4() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		return ip4.String()
	}
	return ""
}

// nonLoopbackStubModelServer binds an httptest server to a specific (non-loopback)
// address rather than the default loopback. It records whether it was hit, exactly
// like egressStubModelServer.
func nonLoopbackStubModelServer(t *testing.T, bindAddr string) (url string, wasHit func() bool) {
	t.Helper()
	var hit atomic.Bool
	ln, err := net.Listen("tcp", bindAddr+":0")
	if err != nil {
		t.Skipf("cannot bind stub to non-loopback address %q: %v", bindAddr, err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"cmpl-stub","object":"text_completion","choices":[{"text":"PONG"}]}`))
	}))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL, hit.Load
}

// TestPiEgress_LocalInterfaceIsPermitted pins down the TRUE srt discriminator,
// which the original loopback-only stub hid and which is subtler than the task's
// first framing ("non-loopback == blocked").
//
// EMPIRICAL FINDING (this test, run on this host): allowLocalBinding=true opens
// NOT ONLY loopback but ANY LOCAL INTERFACE address, including a non-loopback
// private-LAN interface IP owned by this machine (e.g. 192.168.10.1). The srt
// block observed against the DGX model server (192.168.1.86:8551) is therefore
// NOT a "non-loopback" property — it is a REMOTE-HOST property: DGX is a different
// machine, and a raw socket to a remote LAN host is denied.
//
// This distinction matters and is UNREPRODUCIBLE with an in-process stub: an
// httptest server can only bind a LOCAL interface, which allowLocalBinding permits.
// So the faithful, locally-checkable assertion is the POSITIVE one below (a local
// non-loopback interface IS reachable), which proves the block is remote-specific.
// It does not contradict Option A: DGX is remote, so the 127.0.0.1 tunnel is still
// required — the tunnel turns the remote host into a local-loopback endpoint that
// allowLocalBinding permits.
func TestPiEgress_LocalInterfaceIsPermitted(t *testing.T) {
	if _, err := exec.LookPath("srt"); err != nil {
		t.Skip("srt binary not available; real-spawn egress repro requires srt")
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not available; real-spawn egress repro requires curl")
	}
	lanIP := firstNonLoopbackIPv4()
	if lanIP == "" {
		t.Skip("no non-loopback IPv4 interface found; cannot exercise the local-interface path")
	}

	url, wasHit := nonLoopbackStubModelServer(t, lanIP)

	// allowLocalBinding=true — the same toggle that opens loopback. It must ALSO
	// open this LOCAL non-loopback interface, proving "non-loopback" is not the
	// discriminator (remote-vs-local is).
	ranClean, out := runWrappedCurl(t, true /*allowLocalBinding*/, url)
	if !wasHit() {
		t.Fatalf("sandboxed process did NOT reach a LOCAL non-loopback stub at %s "+
			"(curl clean=%v, output=%q). With allowLocalBinding=true srt should permit "+
			"any local interface, not just loopback. If this now fails, srt's local-binding "+
			"semantics have tightened to loopback-only — re-verify the Option-A tunnel "+
			"assumptions (config -> 127.0.0.1 tunnel -> DGX).", url, ranClean, out)
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
