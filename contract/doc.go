// Package contract holds the harmonik wire contract: the proto types that cross
// the boundary between the substrate and a plugin.
//
// This module is a pure leaf. It imports nothing from this project. The real
// proto and its generated Go types land in a later change; this placeholder
// only gives the module a compile target, so the workspace and the boundary
// checks have something to build against from the first commit.
package contract
