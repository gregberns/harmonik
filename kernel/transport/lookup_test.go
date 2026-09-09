package transport

import (
	"bytes"
	"errors"
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// lkT0 is a fixed instant the LOOKUP tests pass in place of a clock read, the
// same injected-time shape kernel/roster's tests use. TTL deadlines are computed
// against it, so expiry is deterministic and never races a real wall clock.
var lkT0 = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

// lkChannel is the one LOOKUP channel the transport-level tests use.
const lkChannel = "reg.services"

// declareLookup stands up a transport named "box-1" with lkChannel declared
// LOOKUP. The node name is fixed because every claim this transport holds is
// written by it, so the tests assert "box-1" as the writer throughout.
func declareLookup(t *testing.T) *Transport {
	t.Helper()
	tr := New("box-1")
	if err := tr.Declare(lkChannel, kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP); err != nil {
		t.Fatalf("Declare(%q): %v", lkChannel, err)
	}
	return tr
}

// A writer's revision is strictly monotonic across its puts, whatever the key.
func TestLookupRevisionIsMonotonicPerWriter(t *testing.T) {
	tr := declareLookup(t)

	var last uint64
	for i, key := range []string{"a", "a", "b", "a", "c"} {
		rev, err := tr.LookupPut("reg.services", key, []byte("v"), 0, lkT0)
		if err != nil {
			t.Fatalf("put %d (%q): %v", i, key, err)
		}
		if rev <= last {
			t.Fatalf("put %d (%q): revision %d not strictly greater than prior %d", i, key, rev, last)
		}
		last = rev
	}
}

// LookupGet returns this node's one claim, stamped with the node as writer.
func TestLookupGetReturnsTheClaimWithWriterNode(t *testing.T) {
	tr := declareLookup(t)

	rev, err := tr.LookupPut("reg.services", "svc.a", []byte("addr-1"), 0, lkT0)
	if err != nil {
		t.Fatalf("LookupPut: %v", err)
	}

	entries, err := tr.LookupGet("reg.services", "svc.a", lkT0)
	if err != nil {
		t.Fatalf("LookupGet: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.GetKey() != "svc.a" {
		t.Errorf("key = %q, want svc.a", e.GetKey())
	}
	if !bytes.Equal(e.GetValue(), []byte("addr-1")) {
		t.Errorf("value = %q, want addr-1", e.GetValue())
	}
	if e.GetWriterNode() != "box-1" {
		t.Errorf("writer_node = %q, want box-1", e.GetWriterNode())
	}
	if e.GetRevision() != rev {
		t.Errorf("revision = %d, want %d", e.GetRevision(), rev)
	}
	if !e.GetUpdatedAt().AsTime().Equal(lkT0) {
		t.Errorf("updated_at = %v, want %v", e.GetUpdatedAt().AsTime(), lkT0)
	}
}

// A later put from the same node overwrites its own prior claim and carries the
// greater revision.
func TestLookupPutOverwritesOwnClaim(t *testing.T) {
	tr := declareLookup(t)

	if _, err := tr.LookupPut("reg.services", "svc.a", []byte("old"), 0, lkT0); err != nil {
		t.Fatalf("first put: %v", err)
	}
	rev2, err := tr.LookupPut("reg.services", "svc.a", []byte("new"), 0, lkT0)
	if err != nil {
		t.Fatalf("second put: %v", err)
	}

	entries, err := tr.LookupGet("reg.services", "svc.a", lkT0)
	if err != nil {
		t.Fatalf("LookupGet: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 entry after overwrite, got %d", len(entries))
	}
	if !bytes.Equal(entries[0].GetValue(), []byte("new")) {
		t.Errorf("value = %q, want new", entries[0].GetValue())
	}
	if entries[0].GetRevision() != rev2 {
		t.Errorf("revision = %d, want %d", entries[0].GetRevision(), rev2)
	}
}

// LookupGet on a key no put ever claimed returns zero entries and no error.
func TestLookupGetEmptyKeyReturnsZeroEntriesNotError(t *testing.T) {
	tr := declareLookup(t)

	entries, err := tr.LookupGet("reg.services", "never.claimed", lkT0)
	if err != nil {
		t.Fatalf("LookupGet on an empty key errored: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("want 0 entries for an unclaimed key, got %d", len(entries))
	}
}

// A claim with a TTL is returned before its deadline and gone at or after it,
// measured on the injected clock.
func TestLookupTTLExpiryHonoredOnLocalClock(t *testing.T) {
	tr := declareLookup(t)

	if _, err := tr.LookupPut("reg.services", "svc.a", []byte("addr"), 10*time.Second, lkT0); err != nil {
		t.Fatalf("LookupPut: %v", err)
	}

	// One second in: still live.
	got, err := tr.LookupGet("reg.services", "svc.a", lkT0.Add(1*time.Second))
	if err != nil {
		t.Fatalf("LookupGet before expiry: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("before expiry: want 1 entry, got %d", len(got))
	}

	// Exactly at the deadline: expired (deadline reached is expired).
	got, err = tr.LookupGet("reg.services", "svc.a", lkT0.Add(10*time.Second))
	if err != nil {
		t.Fatalf("LookupGet at expiry: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("at the deadline: want 0 entries, got %d", len(got))
	}

	// Past the deadline: still gone.
	got, err = tr.LookupGet("reg.services", "svc.a", lkT0.Add(30*time.Second))
	if err != nil {
		t.Fatalf("LookupGet past expiry: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("past the deadline: want 0 entries, got %d", len(got))
	}
}

// A zero TTL never expires, however far the clock advances.
func TestLookupZeroTTLNeverExpires(t *testing.T) {
	tr := declareLookup(t)

	if _, err := tr.LookupPut("reg.services", "svc.a", []byte("addr"), 0, lkT0); err != nil {
		t.Fatalf("LookupPut: %v", err)
	}
	got, err := tr.LookupGet("reg.services", "svc.a", lkT0.Add(1000*time.Hour))
	if err != nil {
		t.Fatalf("LookupGet far future: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("a zero-TTL claim expired; want 1 entry, got %d", len(got))
	}
}

// LookupList returns live claims matching a prefix, ordered by key; an empty
// prefix lists them all, and an expired claim is pruned from the listing.
func TestLookupListFiltersByPrefixAndPrunesExpired(t *testing.T) {
	tr := declareLookup(t)

	if _, err := tr.LookupPut("reg.services", "svc.a", []byte("1"), 0, lkT0); err != nil {
		t.Fatalf("put svc.a: %v", err)
	}
	if _, err := tr.LookupPut("reg.services", "svc.b", []byte("2"), 0, lkT0); err != nil {
		t.Fatalf("put svc.b: %v", err)
	}
	if _, err := tr.LookupPut("reg.services", "other.c", []byte("3"), 5*time.Second, lkT0); err != nil {
		t.Fatalf("put other.c: %v", err)
	}

	// Prefix "svc." selects the two svc keys, ordered.
	got, err := tr.LookupList("reg.services", "svc.", lkT0)
	if err != nil {
		t.Fatalf("LookupList(svc.): %v", err)
	}
	if len(got) != 2 || got[0].GetKey() != "svc.a" || got[1].GetKey() != "svc.b" {
		t.Fatalf("prefix listing = %v, want [svc.a svc.b] in order", keysOf(got))
	}

	// Empty prefix, past other.c's TTL: lists the two live svc keys only.
	got, err = tr.LookupList("reg.services", "", lkT0.Add(10*time.Second))
	if err != nil {
		t.Fatalf("LookupList(all): %v", err)
	}
	if len(got) != 2 || got[0].GetKey() != "svc.a" || got[1].GetKey() != "svc.b" {
		t.Fatalf("all-live listing = %v, want [svc.a svc.b]; expired other.c should be pruned", keysOf(got))
	}
}

// A LOOKUP operation against an undeclared channel or a channel of another type
// is a typed refusal, matching how the other paths refuse the types they do not
// carry.
func TestLookupRefusesWrongChannelType(t *testing.T) {
	tr := New("box-1")
	if err := tr.Declare("topic.events", kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB); err != nil {
		t.Fatalf("Declare pubsub: %v", err)
	}

	if _, err := tr.LookupPut("topic.events", "k", []byte("v"), 0, lkT0); !errors.Is(err, ErrChannelTypeNotImplemented) {
		t.Errorf("LookupPut on a PUBSUB channel: err = %v, want ErrChannelTypeNotImplemented", err)
	}
	if _, err := tr.LookupGet("no.such.channel", "k", lkT0); !errors.Is(err, ErrChannelNotDeclared) {
		t.Errorf("LookupGet on an undeclared channel: err = %v, want ErrChannelNotDeclared", err)
	}
}

func keysOf(entries []*kernelv1.LookupEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.GetKey()
	}
	return out
}
