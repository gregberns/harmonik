package refactorstorage

// TASK (eval-refactor-storage) — behavior-preserving multi-file refactor.
//
// Right now three files (store.go, service.go, report.go) are coupled to the
// CONCRETE *memStore type. Refactor the package so callers depend on a small
// Store INTERFACE instead, WITHOUT changing any observable behavior:
//
//   1. Introduce an exported interface (suggested name: Store) with the methods
//      the callers actually use (Get, Set, Delete, Len).
//   2. Make Service and Report depend on that interface, not on *memStore.
//   3. Keep *memStore as the single concrete implementation, constructed in
//      exactly one place. No caller outside store.go may name the memStore type.
//
// The committed golden test (TestStore) pins the observable behavior and must
// keep passing. TestStoreDecoupled enforces the decoupling (it is skipped under
// -short so the scenario-gate does not see the intentional pre-refactor failure).
//
// Do NOT edit *_test.go — the test is the held-out contract.

// Store is the abstraction the callers should depend on. *memStore already
// satisfies it; the refactor is to make Service and Report use this interface
// instead of the concrete type.
type Store interface {
	Get(k string) (string, bool)
	Set(k, v string)
	Delete(k string)
	Len() int
}

// memStore is an in-memory key/value store backed by an inline map.
type memStore struct {
	m map[string]string
}

func newMemStore() *memStore {
	return &memStore{m: make(map[string]string)}
}

func (s *memStore) Get(k string) (string, bool) {
	v, ok := s.m[k]
	return v, ok
}

func (s *memStore) Set(k, v string) {
	s.m[k] = v
}

func (s *memStore) Delete(k string) {
	delete(s.m, k)
}

func (s *memStore) Len() int {
	return len(s.m)
}
