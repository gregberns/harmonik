package daemon_test

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
	"github.com/gregberns/harmonik/internal/projectconfig"
)

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

func egressPiSandboxConfig(allowLocalBinding bool) projectconfig.SandboxConfig {
	return projectconfig.SandboxConfig{
		Backend:   "srt",
		Harnesses: []string{"pi"},
		Network: projectconfig.SandboxNetworkConfig{
			AllowLocalBinding: allowLocalBinding,
		},
	}
}

func runWrappedCurl(t *testing.T, allowLocalBinding bool, url string) (ranClean bool, combined string) {
	t.Helper()

	spawn := daemon.ExportedSandboxSpawnForRun(
		egressPiSandboxConfig(allowLocalBinding),
		core.AgentType("pi"),
		egressSandboxInput(t, allowLocalBinding),
	)
	if spawn == nil {
		t.Fatalf("gate returned nil spawn — pi run under backend=srt should be wrapped")
	}
	spawn.SrtBinary = "srt"

	bin, args, _, err := daemon.ExportedSandboxWrapExecArgv(
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

func nonLoopbackStubModelServer(t *testing.T, bindAddr string) (url string, wasHit func() bool) {
	t.Helper()
	var hit atomic.Bool
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", bindAddr+":0")
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
