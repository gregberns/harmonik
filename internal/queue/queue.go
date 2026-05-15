package queue

// Queue is the daemon-owned execution-plan envelope submitted by an external
// orchestrator. It is defined normatively in specs/queue-model.md §3 (Queue
// envelope record).
//
// TODO(hk-9s6yr): implement the full Queue, Group, and Item records and
// constructors when the RECORD-types bead is dispatched.
type Queue struct{}
