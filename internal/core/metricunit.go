package core

// MetricUnit is the typed alias for the optional `unit` field of the metric
// event (event-model.md §8.8.1).
//
// MetricUnit is an opaque string describing the dimensional unit of a metric
// value (e.g., "ms", "bytes", "count"). The set is open.
//
// Spec ref: event-model.md §8.8.1.
// Bead ref: hk-hqwn.72.
type MetricUnit string
