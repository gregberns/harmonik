// Package operatornfr contains the authoritative exit-code taxonomy for the
// Operator NFR spec (specs/operator-nfr.md §8), together with test fixtures
// and static-check tests.
//
// Production API:
//
//   - [ExitCodeEntry] — one row of the §8 taxonomy table.
//   - [ExitCodes] — the complete registry of all 24 codes (0–23).
//   - [LookupExitCode] — lookup by numeric code.
//
// Test helpers in *_test.go files use the exitCodeFixture and
// obligationsFixture prefixes (per the implementer protocol); production test
// helpers in exitcode_test.go use the exitCodeRegistry prefix.
//
// Governed by specs/operator-nfr.md; if any value or constraint appears to
// conflict with the spec, the spec is authoritative.
package operatornfr
