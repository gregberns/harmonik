package core

// MetricLabels is the typed alias for the optional `labels` field of the
// metric event (event-model.md §8.8.1).
//
// MetricLabels is a key-value map providing dimensional labels for a metric
// (e.g., subsystem name, run scope). Both keys and values are opaque strings;
// the set of valid keys is open per the §8.9(g) escape-hatch exception.
//
// Spec ref: event-model.md §8.8.1.
// Bead ref: hk-hqwn.72.
type MetricLabels map[string]string
