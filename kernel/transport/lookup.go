package transport

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// lookup is the kernel-held LOOKUP state for one Transport: a box-local map, per
// channel, of key -> this node's claim on that key. It carries the opaque value
// bytes of a claim and never reads them.
//
// This node is the SOLE writer of every key it holds — a LookupPut writes this
// node's claim, nothing else. Two nodes claiming one name are two local maps,
// each with its own single entry; the all-claimants read the contract promises
// is assembled by the in-process mesh double reading each node's map directly
// (kernel/transport/memmesh), not by any replication here. The REPLICATED
// LOOKUP protocol — gossip, read-repair, boot_id, the disk-wipe experiment — is
// a later slice and no part of this package.
//
// A revision is this writer's own monotonic counter, bumped on every LookupPut
// across all keys, so this node's puts carry a strict order. It is comparable
// only within one writer, exactly as the contract's LookupEntry.revision says.
//
// TTL is honored on the LOCAL clock, and the clock is injected: every method
// takes the instant as an argument rather than reading a wall clock, the same
// discipline kernel/roster holds (the internal/runloop ports precedent). Expiry
// is lazy — an entry past its deadline is dropped the next time a read or a
// write for its channel walks it.
//
// All of this state is guarded by the one mu, the same single-lock shape the
// PUBSUB, POINT_TO_POINT, and REQUEST_REPLY paths keep; t.mu guards only the
// channel registry this reads for the type check.
type lookup struct {
	mu sync.Mutex
	// revision is this node's per-writer monotonic counter. It is bumped on
	// every LookupPut, whatever the key, so the values this writer stamps are
	// strictly increasing across all of its puts.
	revision uint64
	// byChannel holds each LOOKUP channel's key -> entry map. A channel entry
	// appears on the first LookupPut against it and stays for the Transport's
	// life.
	byChannel map[string]map[string]*lookupEntry
}

// lookupEntry is this node's one claim on a key: the opaque value, the revision
// this writer stamped it with, when it was written, and when it expires. A zero
// expiresAt means no expiry.
type lookupEntry struct {
	value     []byte
	revision  uint64
	updatedAt time.Time
	expiresAt time.Time
}

func newLookup() *lookup {
	return &lookup{byChannel: make(map[string]map[string]*lookupEntry)}
}

// LookupPut writes THIS node's claim on key in a LOOKUP channel and returns the
// monotonic revision it stamped. This node is the sole writer of the key; a
// later put from this node overwrites its own prior claim and carries a strictly
// greater revision. A ttl of zero never expires; otherwise the claim expires at
// now+ttl on the local clock. The value is opaque and never read. The channel
// must be declared and of type LOOKUP (ErrChannelTypeNotImplemented otherwise),
// matching how Stamp/Request refuse the types they do not carry.
func (t *Transport) LookupPut(channel, key string, value []byte, ttl time.Duration, now time.Time) (uint64, error) {
	if err := t.requireLookup(channel); err != nil {
		return 0, err
	}
	return t.lk.put(channel, key, value, ttl, now), nil
}

// LookupGet returns every claim THIS node holds on key — zero entries or, since
// this node is its keys' sole writer, one. An unclaimed or expired key is zero
// entries and no error, never a failure. The cross-node all-claimants read (two
// nodes, one key, two entries) is the mesh double merging each node's LookupGet;
// see kernel/transport/memmesh. The channel must be a declared LOOKUP channel.
func (t *Transport) LookupGet(channel, key string, now time.Time) ([]*kernelv1.LookupEntry, error) {
	if err := t.requireLookup(channel); err != nil {
		return nil, err
	}
	return t.lk.get(channel, key, t.node, now), nil
}

// LookupList returns this node's claims whose key begins with keyPrefix, each as
// a LookupEntry, expired claims pruned. An empty prefix lists every live claim
// on the channel. Entries come back ordered by key so a caller sees a stable
// list. The channel must be a declared LOOKUP channel.
func (t *Transport) LookupList(channel, keyPrefix string, now time.Time) ([]*kernelv1.LookupEntry, error) {
	if err := t.requireLookup(channel); err != nil {
		return nil, err
	}
	return t.lk.list(channel, keyPrefix, t.node, now), nil
}

// requireLookup confirms channel is declared and is a LOOKUP channel, the only
// type LookupPut/Get/List carry. A non-LOOKUP type is ErrChannelTypeNotImplemented,
// matching how requireRequestReply refuses a non-REQUEST_REPLY channel.
func (t *Transport) requireLookup(channel string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	typ, declared := t.channels[channel]
	if !declared {
		return fmt.Errorf("%w: %q", ErrChannelNotDeclared, channel)
	}
	if typ != kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP {
		return ErrChannelTypeNotImplemented
	}
	return nil
}

func (lk *lookup) put(channel, key string, value []byte, ttl time.Duration, now time.Time) uint64 {
	lk.mu.Lock()
	defer lk.mu.Unlock()

	byKey := lk.byChannel[channel]
	if byKey == nil {
		byKey = make(map[string]*lookupEntry)
		lk.byChannel[channel] = byKey
	}

	lk.revision++
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = now.Add(ttl)
	}
	byKey[key] = &lookupEntry{
		value:     value,
		revision:  lk.revision,
		updatedAt: now,
		expiresAt: expiresAt,
	}
	return lk.revision
}

func (lk *lookup) get(channel, key, writer string, now time.Time) []*kernelv1.LookupEntry {
	lk.mu.Lock()
	defer lk.mu.Unlock()

	byKey := lk.byChannel[channel]
	e, ok := byKey[key]
	if !ok {
		return nil
	}
	if expired(e, now) {
		delete(byKey, key)
		return nil
	}
	return []*kernelv1.LookupEntry{entryProto(key, writer, e)}
}

func (lk *lookup) list(channel, keyPrefix, writer string, now time.Time) []*kernelv1.LookupEntry {
	lk.mu.Lock()
	defer lk.mu.Unlock()

	byKey := lk.byChannel[channel]
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]*kernelv1.LookupEntry, 0, len(keys))
	for _, key := range keys {
		e := byKey[key]
		if expired(e, now) {
			delete(byKey, key)
			continue
		}
		if !strings.HasPrefix(key, keyPrefix) {
			continue
		}
		out = append(out, entryProto(key, writer, e))
	}
	return out
}

// expired reports whether e's TTL deadline has passed at now. A zero deadline
// never expires.
func expired(e *lookupEntry, now time.Time) bool {
	return !e.expiresAt.IsZero() && !now.Before(e.expiresAt)
}

// entryProto builds the wire LookupEntry for one held claim. writer is the
// node that holds it — this transport's own node name, since every key a
// transport holds was written by it.
func entryProto(key, writer string, e *lookupEntry) *kernelv1.LookupEntry {
	return &kernelv1.LookupEntry{
		Key:        key,
		Value:      e.value,
		WriterNode: writer,
		Revision:   e.revision,
		UpdatedAt:  timestamppb.New(e.updatedAt),
	}
}
