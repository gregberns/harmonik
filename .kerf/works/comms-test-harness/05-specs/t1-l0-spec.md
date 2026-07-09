# Change Spec — T1 (L0 pure-projection)

Full assertions specified in `plans/2026-07-06-quality-system/12-comms-test-design.md` §2 "L0" and §3
scenarios 3 (predicate half), 4, 6 (L0 half). Each of the 4 T1 beads implements one Go test file using
JSONL fixtures + the named pure seam (`eventbus.ScanAfter`, `presence.ComputeRegistry`,
`daemon.NewCursorStore`, `commsWakePaneCandidates`/`resolveProjectPath`). Verification: `go test ./...`
scoped to the touched package; each test is deterministic (no real clock, no goroutines beyond the test
itself). Edge cases per scenario are enumerated in the design doc (directed/broadcast/topic/from-wildcard
for N1; t0/t0+119s/t0+121s/t0+90s-send for the presence matrix; symlinked-path hashing for wake-candidates).
