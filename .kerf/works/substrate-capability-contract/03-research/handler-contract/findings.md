# Research — Handler Contract

## Questions

1. Should the 16 daemon-private capabilities extend `handler.Substrate`?
2. Does the public handler contract already define a suitable input and
   observation boundary?
3. Does Step 14 require a handler-contract amendment?

## Findings

`internal/handler/substrate.go` defines `Substrate` as one operation:
`SpawnWindow`. `SubstrateSession` exposes process lifecycle only: `Kill`,
`Wait`, `Outcome`, `PID`, and `Stdout`. This is a narrow consumer-owned port.

`specs/handler-contract.md` defines public `Handler`, `Session`, and the
separately asserted `InputPort`. It does not define a public substrate
capability interface. HC-056 names the tmux ready-timeout start behavior.
HC-054 and HC-069 define stable observation and input seams. HC-069 rejects
the old type-asserted paste-side mechanism as the input contract.

The 16 private capabilities are daemon operations. They cover tmux adapter and
session access, pane behavior, spawn caps, boot readiness, cleanup, and crew
or run session control. Putting them on the base port would require each future
substrate to imitate tmux details or return unsupported errors for normal work.

## Pattern to Follow

Keep `Substrate` as process hosting and `SubstrateSession` as lifecycle. Keep
input on `InputPort` and observation on `Session.Attach`. Keep operational
capabilities private to their daemon consumers.

## Disposition

No handler-contract amendment is required. A later non-normative clarification
may state that substrate host-control capabilities remain composition-root
details. It must not widen `Handler`, `Session`, `InputPort`, or `Substrate`.
