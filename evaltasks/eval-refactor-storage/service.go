package refactorstorage

// Service is the observable public API of the package.
type Service struct {
	store Store
}

// NewService builds a Service backed by the in-memory store.
func NewService() *Service {
	return &Service{store: newMemStore()}
}

// Put stores a value under key k.
func (svc *Service) Put(k, v string) {
	svc.store.Set(k, v)
}

// Fetch returns the value for key k and whether it was present.
func (svc *Service) Fetch(k string) (string, bool) {
	return svc.store.Get(k)
}

// Remove deletes key k if present (no-op otherwise).
func (svc *Service) Remove(k string) {
	svc.store.Delete(k)
}

// Count returns the number of stored entries.
func (svc *Service) Count() int {
	return svc.store.Len()
}

// Summary returns a one-line description of the store contents.
func (svc *Service) Summary() string {
	return Report(svc.store)
}
