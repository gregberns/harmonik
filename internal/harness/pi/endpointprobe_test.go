package pi

// endpointprobe_test.go — exercises ProbeBaseURL (hk-p06sq) through the real
// dial entry point: a listener that accepts, and a port nothing listens on.

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// TestProbeBaseURL_Reachable dials a real listener and expects no error.
func TestProbeBaseURL_Reachable(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	baseURL := "http://" + ln.Addr().String() + "/v1"
	if err := ProbeBaseURL(context.Background(), baseURL, time.Second); err != nil {
		t.Errorf("ProbeBaseURL(%q) = %v; want nil (listener is up)", baseURL, err)
	}
}

// TestProbeBaseURL_Unreachable names a port nothing listens on (grab one from
// a listener, then close it before dialing) and asserts the error names both
// the base_url and the host:port that failed — the "one true sentence" the
// bead asks for, not a bare dial error.
func TestProbeBaseURL_Unreachable(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()
	if closeErr := ln.Close(); closeErr != nil {
		t.Fatalf("close probe listener: %v", closeErr)
	}

	baseURL := "http://" + addr + "/v1"
	err = ProbeBaseURL(context.Background(), baseURL, time.Second)
	if err == nil {
		t.Fatalf("ProbeBaseURL(%q) = nil; want an error (nothing listens on %s)", baseURL, addr)
	}
	if !strings.Contains(err.Error(), addr) {
		t.Errorf("ProbeBaseURL error %q does not name the unreachable host:port %q", err.Error(), addr)
	}
	if !strings.Contains(err.Error(), baseURL) {
		t.Errorf("ProbeBaseURL error %q does not name the configured base_url %q", err.Error(), baseURL)
	}
}

// TestProbeBaseURL_InvalidURL exercises the parse-failure path.
func TestProbeBaseURL_InvalidURL(t *testing.T) {
	if err := ProbeBaseURL(context.Background(), "://not-a-url", time.Second); err == nil {
		t.Errorf("ProbeBaseURL(invalid URL) = nil; want an error")
	}
}

// TestProbeBaseURL_NoHost exercises a URL that parses but carries no host.
func TestProbeBaseURL_NoHost(t *testing.T) {
	if err := ProbeBaseURL(context.Background(), "/v1", time.Second); err == nil {
		t.Errorf("ProbeBaseURL(no host) = nil; want an error")
	}
}
