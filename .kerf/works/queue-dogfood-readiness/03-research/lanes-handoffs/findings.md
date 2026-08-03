# Research — Lanes and live handoffs

## Questions

1. Which lane owns each readiness change?
2. What order protects the shared release boundary?
3. Which state changes need operator authority?
4. What does the first controlled batch permit?

## Findings

- `LANES.md` “Queue dogfood priority” keeps the fleet daemon stopped. It
  defines one repeat-safe local stream item at concurrency one and excludes
  remote workers, Pi, cross-repository targets, and waves.
- Alpha owns DOT shutdown drain and daemon-side release-path tests. Bravo owns
  queue, queue wiring, CLI, non-daemon transaction proofs, ratchets, and the
  scratch and core-loop scripts.
- The lanes must agree the release contract before changing a shared boundary.
  The lane document assigns the queue and command surface to Bravo but keeps
  daemon release behavior with Alpha.
- Registry reset, mission retirement, and new mission creation require a
  read-first inspection and operator authority.
- `HANDOFF-alpha.md` records the same stopped-daemon boundary and the current
  split. It also preserves the operator-owned dirty lane plan and unrelated
  Kerf work.

## Patterns to keep

- Keep shared release behavior as a contract-first change.
- Use a scratch daemon for the proof. Do not submit normal queue work or use
  the assessor as a worker.
- Record task ownership and release order in the lane plan and live handoff
  before implementation starts.

## Risks and decisions

- The current first-canary policy is a readiness constraint. It must not become
  a universal queue-model rejection rule.
- The lane document names old, dirty lane branches as operator decisions. This
  work must not rebase, adopt, or remove them.
