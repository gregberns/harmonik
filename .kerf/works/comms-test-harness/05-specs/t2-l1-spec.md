# Change Spec — T2 (L1 in-process bus/hub)

Full assertions specified in design doc §2 "L1" and §3 scenarios 1, 2, 3. Each of the 4 T2 beads implements
one Go test using `eventbus.NewBusImpl*` + `daemon.NewSubscribeHub` with injected `Now`/`NewTimer` +
`net.Pipe()` fake client. Verification: `go test ./...` scoped to `internal/daemon` and `internal/eventbus`.
Edge cases: N1/N2 boundary (backlog + mid-backlog anchor + live emit, assert no gap/no dup); N3 fan-out
(2 consumers, broadcast+directed mix, simulated pre-advance crash + re-drain, assert at-least-once +
recipient-side dedupe); B1 pin (follower registered first, then one-shot recv for same agent, assert 0-drain
+ `comms log --since` still shows all); back-pressure (256-slot drop-oldest, assert `subscription_gap{dropped:N}`
emitted, durable log still has full history).
