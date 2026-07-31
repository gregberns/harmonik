package daemon_test

import "github.com/google/uuid"

// newTestQueueID returns a fresh canonical UUIDv7 for a fixture queue's
// QueueID field.
//
// queue-model.md §2.1 says queue_id is a daemon-minted UUIDv7 and is never
// client-supplied, and the durable-write path enforces that: a replace intent
// carrying a non-UUIDv7 queue_id is rejected before any I/O. Fixtures that used
// readable strings such as "boiwe-wl-queue" therefore described a queue the
// daemon can never write, and every test built on one stopped exercising the
// dispatch path the moment that path started using the real durable write.
//
// Use this rather than a literal. Nothing should depend on the value.
func newTestQueueID() string {
	return uuid.Must(uuid.NewV7()).String()
}
