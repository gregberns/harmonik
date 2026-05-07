// Package core holds shared types that cross subsystem boundaries.
// internal/core imports nothing from internal/* subsystems.
package core

// NodeID is a workflow-unique node identifier (execution-model.md §6.1; namespaced under sub-workflow expansion per §4.8.EM-034a).
type NodeID string
