# Research — T4 (spec/doc reconciliation)

Both design questions are already scoped by the mission and design doc as operator-in-the-loop, NOT
self-resolvable:
1. B1 (recv-drains-0 under armed `--follow`): is the shared-cursor semantics acceptable long-term, or should
   `recv --agent` get an independent cursor from `--follow`? Tradeoff: independent cursors would duplicate
   delivery (both would "consume" the same messages), changing at-least-once semantics fleet-wide — high
   blast radius, needs operator sign-off.
2. B2 (idle `--follow` doesn't refresh presence): should idle `--follow` emit a periodic refresh beat?
   Tradeoff: adds a timer/heartbeat write path to a currently read-only follow loop — small code change but
   changes presence semantics that other tooling (ops-monitor, watch) may depend on for staleness detection.
Both surfaced to captain for operator relay per the mission; this kerf work's T4 deliverable is the pinning
spec + doc note, not a resolution.
