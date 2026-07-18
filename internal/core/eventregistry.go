package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sync"
)

// EventPayload is the marker interface every event type's payload struct
// satisfies. It is intentionally empty; payload types declare themselves as
// EventPayload by satisfying the interface.
//
// Spec ref: event-model.md §6.3 EV-032 — "Event types in Go MUST be represented
// as a tagged-union: a top-level Event envelope struct carrying common fields
// plus Payload json.RawMessage; a per-type constructor registry
// map[EventType]func() EventPayload decodes Payload keyed by Event.type."
//
// EV-026 (event-model.md §4.6 EV-026) — internal-event carve-out: internal
// events (within a subsystem's own Go package, never dispatched to the bus) MUST
// NOT cross the bus and do not require §8 registration. Internal payload types
// MAY implement EventPayload for local type-safety, but they MUST NOT be passed
// to RegisterEventType. Only §8-declared event types are registered in this
// package's global registry.
type EventPayload interface{}

// ErrUnknownEventType is returned by DecodePayload when Event.Type has no
// registered constructor.
var ErrUnknownEventType = errors.New("core: unknown event type")

// ErrDuplicateEventType is returned by RegisterEventType when the same type
// name is registered more than once.
var ErrDuplicateEventType = errors.New("core: event type already registered")

// ErrSchemaVersionMismatch is returned by ValidateEnvelopeSchemaVersion when
// the envelope's schema_version does not match the per-type version declared in
// the registry per EV-028 (event-model.md §4.7).
var ErrSchemaVersionMismatch = errors.New("core: envelope schema_version does not match registered per-type schema version")

// typeEntry holds the constructor and per-type schema version for one
// registered event type. Per-type versions evolve independently per EV-028 /
// §6.4; bumping one type's version requires an EV-027 foundation amendment.
type typeEntry struct {
	constructor   func() EventPayload
	schemaVersion int // declared schema version for this type's payload; >= 1
}

// eventRegistry holds the per-type entry map guarded by a mutex.
//
// Registration is startup-time per EV-034: once the registry is sealed
// (SealEventRegistry, called at the same lifecycle point as bus.Seal per
// EV-009), all further registration attempts return ErrRegistrySealed. The
// mutex also prevents a data-race during init-order weirdness where multiple
// init() calls arrive concurrently (e.g., test binaries with parallel
// package-level inits).
//
// TODO(hk-hqwn.41/EV-034a): source_subsystem identifier registration is a
// separate concern; see EV-034a. Not implemented here.
//
// secretPrefixRe is the EV-036 / HC-031 common-prefix regex. Any struct field
// whose exported name matches this regex on a registered payload type MUST cause
// ScanRegisteredPayloadsForSecretFields to return ErrSecretPrefixField.
//
// Spec ref: event-model.md §4.10 EV-036; handler-contract.md §4.7 HC-031.
var secretPrefixRe = regexp.MustCompile(`(?i)(secret|token|password|api[_-]?key|auth)`)

type eventRegistry struct {
	mu      sync.Mutex
	entries map[string]typeEntry // TODO(hk-hqwn.59.82): hoist key from string to EventType when the enum lands.
	sealed  bool                 // EV-034: once true, registration is forbidden.
}

// ErrRegistrySealed is returned by RegisterEventType / RegisterEventTypeAtVersion
// when the event-type registry has already been sealed (SealEventRegistry).
//
// Per EV-034 (event-model.md §4.9), payload-type registration is startup-time:
// registration after the registry is sealed (at the same lifecycle point as
// bus.Seal, EV-009) MUST be a startup-time error rather than silently mutating
// the dispatch table after dispatch has begun.
var ErrRegistrySealed = errors.New("core: event-type registry is sealed; registration is startup-time only (EV-034)")

// SealEventRegistry seals the global event-type registry. After this call,
// RegisterEventType and RegisterEventTypeAtVersion return ErrRegistrySealed;
// read paths (DecodePayload, LookupTypeSchemaVersion, …) are unaffected.
//
// Per EV-034 the registry MUST be sealed at the same lifecycle point as the bus
// (bus.Seal, EV-009) — i.e. after all init()/RegisterEventType startup calls and
// the EV-036 secret-field scan, immediately before the daemon begins dispatch.
// The daemon wires this in daemon.go alongside bus.Seal().
//
// Sealing is idempotent: calling it more than once is a no-op. Thread-safe.
func SealEventRegistry() {
	r := globalEventRegistry
	r.mu.Lock()
	r.sealed = true
	r.mu.Unlock()
}

// EventRegistrySealed reports whether the global event-type registry has been
// sealed. Exposed primarily for tests and startup diagnostics. Thread-safe.
func EventRegistrySealed() bool {
	r := globalEventRegistry
	r.mu.Lock()
	sealed := r.sealed
	r.mu.Unlock()
	return sealed
}

var globalEventRegistry = &eventRegistry{
	entries: make(map[string]typeEntry),
}

// RegisterEventType registers a constructor for the given event type name at
// schema version 1 (the MVH baseline per EV-028 / OQ-EV-004).
//
// The constructor is called by DecodePayload to obtain a fresh zero-value
// target for JSON unmarshaling.
//
// EV-025 (event-model.md §4.6 EV-025) — each event type has exactly one owning
// spec for payload shape: event-model.md §6.3 is normative for the SHAPE; the
// emitting subsystem spec is normative for the WHEN (timing and preconditions).
// RegisterEventType is the in-Go enforcement of the one-constructor-per-type-name
// half of EV-025: calling it twice with the same typeName returns
// ErrDuplicateEventType, preventing any second shape-owner from silently
// overwriting the canonical constructor. The typeName MUST correspond to a
// §8-declared event type; types not in §8 MUST NOT be registered here (see
// EV-026 for internal-only events that are out of scope for this registry).
//
// To register a type at a schema version other than 1, use
// RegisterEventTypeAtVersion.
//
// Returns ErrDuplicateEventType if typeName has already been registered.
// Thread-safe.
func RegisterEventType(typeName string, constructor func() EventPayload) error {
	return RegisterEventTypeAtVersion(typeName, constructor, 1)
}

// RegisterEventTypeAtVersion registers a constructor for the given event type
// name at the specified schemaVersion. schemaVersion MUST be >= 1.
//
// Per EV-028 (event-model.md §4.7), the declared schemaVersion is the value
// that ValidateEnvelopeSchemaVersion checks against an incoming event's
// envelope schema_version field. Per-type versions MAY increment independently;
// each increment requires an EV-027 foundation amendment per §6.4.
//
// Returns ErrDuplicateEventType if typeName has already been registered.
// Thread-safe.
func RegisterEventTypeAtVersion(typeName string, constructor func() EventPayload, schemaVersion int) error {
	if typeName == "" {
		return fmt.Errorf("core: RegisterEventTypeAtVersion: typeName must not be empty")
	}
	if constructor == nil {
		return fmt.Errorf("core: RegisterEventTypeAtVersion: constructor must not be nil")
	}
	if schemaVersion < 1 {
		return fmt.Errorf("core: RegisterEventTypeAtVersion: schemaVersion must be >= 1, got %d", schemaVersion)
	}
	r := globalEventRegistry
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return fmt.Errorf("%w: %q", ErrRegistrySealed, typeName)
	}
	if _, exists := r.entries[typeName]; exists {
		return fmt.Errorf("%w: %q", ErrDuplicateEventType, typeName)
	}
	r.entries[typeName] = typeEntry{constructor: constructor, schemaVersion: schemaVersion}
	return nil
}

// LookupTypeSchemaVersion returns the per-type schema version registered for
// typeName. Returns (version, true) when found, or (0, false) when typeName
// has no registered entry.
//
// Per EV-028 (event-model.md §4.7), the envelope's schema_version MUST equal
// the per-type version returned here. Use ValidateEnvelopeSchemaVersion to
// enforce this invariant on an incoming Event.
//
// Thread-safe.
func LookupTypeSchemaVersion(typeName string) (int, bool) {
	r := globalEventRegistry
	r.mu.Lock()
	entry, ok := r.entries[typeName]
	r.mu.Unlock()
	if !ok {
		return 0, false
	}
	return entry.schemaVersion, true
}

// ValidateEnvelopeSchemaVersion checks that e.SchemaVersion matches the
// per-type schema version declared in the registry for e.Type per EV-028.
//
// Returns nil when the versions match, ErrUnknownEventType when e.Type is not
// registered, or a non-nil error wrapping ErrSchemaVersionMismatch when the
// envelope version differs from the registered per-type version.
//
// Spec ref: event-model.md §4.7 EV-028 — "schema_version is an integer on the
// envelope and MUST match the schema version of the payload for that type."
//
// Thread-safe.
func ValidateEnvelopeSchemaVersion(e Event) error {
	r := globalEventRegistry
	r.mu.Lock()
	entry, ok := r.entries[e.Type]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownEventType, e.Type)
	}
	if e.SchemaVersion != entry.schemaVersion {
		return fmt.Errorf("%w: type %q has registered version %d but envelope carries %d",
			ErrSchemaVersionMismatch, e.Type, entry.schemaVersion, e.SchemaVersion)
	}
	return nil
}

// AllPayloadSchemaVersions returns a snapshot of the per-type schema version
// table. Each key is a registered event type name; each value is its current
// declared schema version (≥ 1).
//
// Spec ref: event-model.md §4.8 EV-029 — "N (currently 71) independent
// compatibility contracts."
// Bead ref: hk-hqwn.38.
func AllPayloadSchemaVersions() map[string]int {
	r := globalEventRegistry
	r.mu.Lock()
	snapshot := make(map[string]int, len(r.entries))
	for k, v := range r.entries {
		snapshot[k] = v.schemaVersion
	}
	r.mu.Unlock()
	return snapshot
}

// CurrentPayloadSchemaVersion returns the declared schema version for the given
// event type. Returns (version, true) when the type is registered, (0, false)
// when it is not. Alias for LookupTypeSchemaVersion.
//
// Spec ref: event-model.md §4.8 EV-028; §4.8 EV-029.
// Bead ref: hk-hqwn.38.
func CurrentPayloadSchemaVersion(typeName string) (int, bool) {
	return LookupTypeSchemaVersion(typeName)
}

// DecodePayload looks up the constructor for e.Type, instantiates a fresh
// payload value, and JSON-unmarshals e.Payload into it.
//
// Returns:
//   - (payload, nil) on success.
//   - (nil, ErrUnknownEventType) when e.Type has no registered constructor.
//   - (nil, <unmarshal error>) when JSON decoding fails (e.g., json.SyntaxError).
//
// Callers should prefer DispatchObservational or DispatchSynchronous
// (eventdispatch.go) which implement the EV-033 skip-vs-fail policy.
func (e Event) DecodePayload() (EventPayload, error) {
	r := globalEventRegistry
	r.mu.Lock()
	entry, ok := r.entries[e.Type]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownEventType, e.Type)
	}
	payload := entry.constructor()
	if err := json.Unmarshal(e.Payload, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// DecodePayloadStrict decodes e.Payload exactly like DecodePayload but rejects
// unknown payload fields (json.Decoder.DisallowUnknownFields), so an additive
// field a NEWER writer introduced surfaces as a decode error instead of being
// silently ignored. DecodePayload uses json.Unmarshal with no
// DisallowUnknownFields and therefore cannot see additive writer drift.
//
// Returns:
//   - (payload, nil) on success.
//   - (nil, ErrUnknownEventType) when e.Type has no registered constructor.
//   - (nil, <decode error>) when JSON decoding fails, INCLUDING an unknown field.
//
// The addition is purely additive: DecodePayload's tolerant semantics are
// unchanged and remain the default for historical replay. Strict mode is for
// replaying the harness's OWN freshly-recorded corpus, where an unknown field
// means a writer drifted (EV-049, event-model.md §4.7).
func (e Event) DecodePayloadStrict() (EventPayload, error) {
	r := globalEventRegistry
	r.mu.Lock()
	entry, ok := r.entries[e.Type]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownEventType, e.Type)
	}
	payload := entry.constructor()
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// ErrSecretPrefixField is the typed configuration error returned by
// ScanRegisteredPayloadsForSecretFields when a registered payload struct
// has an exported field whose name matches the secret-prefix rule.
//
// Spec ref: event-model.md §4.10 EV-036 — "any registered payload type
// whose struct field names match the secret-prefix rule MUST cause startup
// to fail with a typed configuration error."
var ErrSecretPrefixField = errors.New("core: registered payload type has secret-prefix field name")

// scanConstructors is the core implementation of the EV-036 structural check.
// It scans every constructor in ctors, instantiates each payload via reflection,
// and returns the first ErrSecretPrefixField violation found, or nil when clean.
//
// Non-struct payloads (e.g., map types) are skipped. Unexported fields are
// skipped (they cannot be set via JSON unmarshaling and carry no JSON key name).
//
// Separated from ScanRegisteredPayloadsForSecretFields so that tests can supply
// a local constructor map without touching the global registry.
func scanConstructors(ctors map[string]func() EventPayload) error {
	for typeName, ctor := range ctors {
		instance := ctor()
		if instance == nil {
			continue
		}
		rt := reflect.TypeOf(instance)
		// Dereference pointer to get the underlying struct type.
		for rt.Kind() == reflect.Ptr {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct {
			continue
		}
		for i := range rt.NumField() {
			field := rt.Field(i)
			if !field.IsExported() {
				continue
			}
			if secretPrefixRe.MatchString(field.Name) {
				return fmt.Errorf("%w: event type %q has field %q matching secret-prefix rule",
					ErrSecretPrefixField, typeName, field.Name)
			}
		}
	}
	return nil
}

// ScanRegisteredPayloadsForSecretFields scans every constructor registered in
// the global event-type registry and inspects the concrete struct type it
// produces via reflection. Any exported struct field whose name matches the
// EV-036 secret-prefix rule (`(?i)(secret|token|password|api[_-]?key|auth)`)
// causes the function to return a non-nil error wrapping ErrSecretPrefixField.
//
// The intent is daemon-startup use: call ScanRegisteredPayloadsForSecretFields
// after all RegisterEventType calls complete and before the bus is sealed. A
// non-nil return means a payload author accidentally named a field after a
// secret-class concept; the daemon MUST refuse to start (EV-036).
//
// Non-struct payloads (e.g., map types) are skipped; the check is structural
// and only applies to exported fields on struct types.
//
// Returns nil when all registered payload types are clean.
// Returns an error wrapping ErrSecretPrefixField on the first violation found.
//
// Spec ref: event-model.md §4.10 EV-036.
// Bead ref: hk-hqwn.52.
func ScanRegisteredPayloadsForSecretFields() error {
	r := globalEventRegistry
	r.mu.Lock()
	snapshot := make(map[string]func() EventPayload, len(r.entries))
	for k, v := range r.entries {
		snapshot[k] = v.constructor
	}
	r.mu.Unlock()
	return scanConstructors(snapshot)
}
