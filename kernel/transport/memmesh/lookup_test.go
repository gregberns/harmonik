package memmesh_test

import (
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/transport/memmesh"
)

// lkT0 is the fixed instant the cross-node LOOKUP tests pass in place of a clock
// read, so TTL stays deterministic. It mirrors the transport package's own
// injected-clock test shape.
var lkT0 = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// Two nodes each claim one name; a LookupGet returns BOTH claimants and the mesh
// picks neither. The merge reads each node's own map directly — the whole of the
// cross-node LOOKUP behaviour in this slice.
func TestLookupReturnsBothClaimantsOnANameClash(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")

	// One declaration wins mesh-wide; both nodes see the LOOKUP channel.
	declare(t, a, "reg.leader", kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP)

	if _, err := a.LookupPut("reg.leader", "epoch-7", []byte("addr-a"), 0, lkT0); err != nil {
		t.Fatalf("node-a LookupPut: %v", err)
	}
	if _, err := b.LookupPut("reg.leader", "epoch-7", []byte("addr-b"), 0, lkT0); err != nil {
		t.Fatalf("node-b LookupPut: %v", err)
	}

	// A read from either node merges both claims.
	entries, err := a.LookupGet("reg.leader", "epoch-7", lkT0)
	if err != nil {
		t.Fatalf("node-a LookupGet: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("a name clash returned %d entries, want 2 (both claimants)", len(entries))
	}

	byWriter := map[string][]byte{}
	for _, e := range entries {
		if e.GetKey() != "epoch-7" {
			t.Errorf("entry key = %q, want epoch-7", e.GetKey())
		}
		byWriter[e.GetWriterNode()] = e.GetValue()
	}
	if got := byWriter["node-a"]; string(got) != "addr-a" {
		t.Errorf("node-a claim = %q, want addr-a", got)
	}
	if got := byWriter["node-b"]; string(got) != "addr-b" {
		t.Errorf("node-b claim = %q, want addr-b", got)
	}
}

// A put is local to its node; a single claim surfaces once across the mesh, and
// an unclaimed key is zero entries and no error from any node.
func TestLookupGetMergesToOneForASingleClaim(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")
	declare(t, a, "reg.leader", kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP)

	if _, err := a.LookupPut("reg.leader", "epoch-1", []byte("addr-a"), 0, lkT0); err != nil {
		t.Fatalf("node-a LookupPut: %v", err)
	}

	// node-b, which wrote nothing, still sees node-a's single claim via the merge.
	entries, err := b.LookupGet("reg.leader", "epoch-1", lkT0)
	if err != nil {
		t.Fatalf("node-b LookupGet: %v", err)
	}
	if len(entries) != 1 || entries[0].GetWriterNode() != "node-a" {
		t.Fatalf("single-claim merge = %d entries, want 1 from node-a", len(entries))
	}

	// An unclaimed key is zero entries, not an error.
	empty, err := b.LookupGet("reg.leader", "never", lkT0)
	if err != nil {
		t.Fatalf("LookupGet on an unclaimed key errored: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("unclaimed key returned %d entries, want 0", len(empty))
	}
}

// LookupList merges matching claims from every node, and honors TTL on the local
// clock per node.
func TestLookupListMergesAcrossNodesAndHonorsTTL(t *testing.T) {
	m := memmesh.New()
	a := addNode(t, m, "node-a")
	b := addNode(t, m, "node-b")
	declare(t, a, "reg.leader", kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP)

	if _, err := a.LookupPut("reg.leader", "svc.a", []byte("1"), 0, lkT0); err != nil {
		t.Fatalf("node-a put: %v", err)
	}
	if _, err := b.LookupPut("reg.leader", "svc.b", []byte("2"), 5*time.Second, lkT0); err != nil {
		t.Fatalf("node-b put: %v", err)
	}

	// Before any expiry: both claims list.
	got, err := a.LookupList("reg.leader", "svc.", lkT0)
	if err != nil {
		t.Fatalf("LookupList: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("merged listing = %d entries, want 2", len(got))
	}

	// Past node-b's TTL: only node-a's non-expiring claim remains.
	got, err = a.LookupList("reg.leader", "svc.", lkT0.Add(10*time.Second))
	if err != nil {
		t.Fatalf("LookupList past ttl: %v", err)
	}
	if len(got) != 1 || got[0].GetWriterNode() != "node-a" {
		t.Fatalf("post-TTL listing = %d entries, want 1 from node-a", len(got))
	}
}
