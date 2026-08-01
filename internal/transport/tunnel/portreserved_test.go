package tunnel

// portreserved_test.go — PortReserved answers about the set AllocatePort and
// ReleasePort actually use.
//
// The reader exists for tests in OTHER packages: internal/daemon drives a whole
// remote run and asks whether the run gave its tunnel port back. Every one of
// those tests reads its verdict through this function, so a reader wired to the
// wrong map, or one that simply answered false, would turn all of them green
// while defending nothing. That failure is invisible from internal/daemon,
// because from there a leaked port and a reader that cannot see reservations
// look the same. It is visible from here, where the set is in reach.
//
// So this file pins the reader against the two operations that own the set,
// including the case a plain "false after release" assertion would miss: the
// reader must say TRUE while the reservation stands.

import "testing"

// TestPortReserved_TracksTheSetAllocateAndReleaseWrite asserts the reader
// follows one port through its whole life: unknown before, held after the
// allocation, and free again after the release.
//
// The middle claim is the one that matters. A reader that always returned false
// would pass a test that only checked the end state, and would then report every
// leaked port in every other package as correctly given back.
func TestPortReserved_TracksTheSetAllocateAndReleaseWrite(t *testing.T) {
	// Not parallel: reads and writes the package-global reservedTunnelPorts set.
	port, err := AllocatePort()
	if err != nil {
		t.Fatalf("AllocatePort: %v", err)
	}
	t.Cleanup(func() { ReleasePort(port) })

	if !PortReserved(port) {
		t.Fatalf("PortReserved(%d) = false immediately after AllocatePort returned it.\n"+
			"The reader is not looking at the set the allocator writes. Every test in another "+
			"package that reads a give-back through this function would report success for a "+
			"run that leaked its port.", port)
	}
	if !reservedTunnelPorts[port] {
		t.Fatalf("the allocator did not reserve port %d, so this test measured the reader against "+
			"an empty set", port)
	}

	ReleasePort(port)
	if PortReserved(port) {
		t.Errorf("PortReserved(%d) = true after ReleasePort", port)
	}
}

// TestPortReserved_SaysNothingAboutAPortNobodyTook asserts the reader does not
// report a port that was never allocated as held.
//
// A reader that answered true for everything would fail every give-back test in
// every other package, which is the loud direction. It is checked anyway because
// the port a caller asks about is often one it chose rather than one the
// allocator handed it.
func TestPortReserved_SaysNothingAboutAPortNobodyTook(t *testing.T) {
	// Not parallel: reads the package-global reservedTunnelPorts set.
	//
	// Port 0 is never handed out — allocatePort reads a bound listener's address,
	// which the kernel has already resolved to a real port — so no concurrent
	// reservation can make this flap.
	if PortReserved(0) {
		t.Error("PortReserved(0) = true, but the allocator never hands out port 0")
	}
}
