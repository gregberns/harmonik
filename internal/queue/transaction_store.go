package queue

import "context"

// QueueSnapshot is an immutable detached queue value plus its volatile
// process-local generation. Generation is never persisted or recovery evidence.
//
// The live registry owns generation allocation and validation. Queue operations
// use this value through TransactionStore so they do not import the registry
// package.
type QueueSnapshot struct {
	Name       string
	Queue      *Queue
	Generation uint64
}

// TransactionRequest describes one clone-mutate-persist-install operation.
// The live registry owns execution of this request.
type TransactionRequest struct {
	Snapshot       QueueSnapshot
	ProjectDir     string
	OperationKind  OperationKind
	WakeRequired   bool
	ArchiveHandoff *ArchiveHandoffPlan
	Mutate         func(*Queue) error

	// Precondition, when non-nil, runs under the live registry write lock after
	// snapshot validation and before Mutate. It receives copies of all other
	// loaded queues. A non-nil error rejects the operation before namespace I/O.
	Precondition func(others map[string]*Queue) error
}

// TransactionResult combines durable namespace truth with a fresh snapshot.
// Snapshot is populated only when committed state was installed.
type TransactionResult struct {
	NamespaceResult
	Snapshot   QueueSnapshot
	CleanupErr error
}

// TransactionStore is the consumer-owned transaction port for a live queue
// registry. Implementations validate the snapshot, persist a detached
// candidate, and install it only after a durable commit.
type TransactionStore interface {
	Snapshot(name string) QueueSnapshot
	Transact(ctx context.Context, req TransactionRequest) TransactionResult
}
