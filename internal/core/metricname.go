package core

// MetricName is the typed alias for the `metric_name` field of the metric
// event (event-model.md §8.8.1).
//
// MetricName is an opaque string identifier for a metric. The set is open;
// any subsystem may emit metric events with any non-empty MetricName per the
// §8.9(g) escape-hatch exception (no sibling-spec emission citation required).
//
// Spec ref: event-model.md §8.8.1.
// Bead ref: hk-hqwn.72.
type MetricName string
