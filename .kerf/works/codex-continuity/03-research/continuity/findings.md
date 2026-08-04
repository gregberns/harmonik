# Continuity research findings

The continuity keeper must be generic in its domain and specific only at source
and delivery edges. It cannot inspect assistant prose. The current stopped POC
proved that a direct tmux watcher can inject input, but it is not a safe or
reusable architecture.

An interactive Codex target needs a typed source binding and an attached tmux
delivery adapter. A structured Codex target uses `InputPort` with its existing
run and sequence correlation. Both fail closed after ambiguous delivery.

Iteration uses a work lease, status epoch count and time limits, a declaration
deadline, source freshness, and an absolute review deadline. It has no total
continuation cap. Open decisions remain in the existing decision projection.

These findings are implemented by the single continuity specification.
