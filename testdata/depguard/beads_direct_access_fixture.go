//go:build ignore

// Package depguard holds lint-verification fixtures for depguard rules in
// .golangci.yml. Files in this package carry `//go:build ignore` and are
// excluded from all normal builds and `go test` runs.
//
// To verify the beads-direct-access-ban rule fires:
//
//	golangci-lint run --enable-only depguard ./testdata/depguard/
//
// Expected output: depguard violation on the database/sql import below,
// citing the BI-002 rule message.
//
// Spec ref: specs/beads-integration.md §4.2 BI-002.
package depguard

import (
	// This import MUST be flagged by the beads-direct-access-ban depguard rule
	// in .golangci.yml. The rule denies database/sql outside internal/brcli.
	//
	// The blank import is intentional: this file is a rule-verification fixture,
	// not production code. The `ignore` build tag prevents compilation.
	_ "database/sql"
)
