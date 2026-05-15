// Package queue holds the daemon-owned execution-plan data model for harmonik.
//
// It is defined normatively in [specs/queue-model.md].
//
// The Queue, Group, and Item RECORD types are owned by hk-9s6yr (not yet
// shipped). This package currently contains only the package declaration and a
// stub type: the only non-test Go file is this doc.go and queue.go. Helpers,
// types, and placeholder classifiers that are used exclusively by tests are
// declared in *_test.go files in this package.
//
// See [specs/queue-model.md] §1 for purpose and scope.
package queue
