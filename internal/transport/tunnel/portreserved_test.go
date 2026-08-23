package tunnel

import "testing"

// TestPortReserved_TracksTheSetAllocateAndReleaseWrite asserts the reader
// follows one port through its whole life: unknown before, held after the
// allocation, and free again after the release.
//
// The middle claim is the one that matters. A reader that always returned false
// would pass a test that only checked the end state, and would then report every
// leaked port in every other package as correctly given back.
func TestPortReserved_TracksTheSetAllocateAndReleaseWrite(t *testing.T) {
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
	if PortReserved(0) {
		t.Error("PortReserved(0) = true, but the allocator never hands out port 0")
	}
}
