# Research — Beads integration

## Questions

1. What may select a canary item?
2. What validates it before dispatch?
3. Which record is authority when evidence disagrees?
4. How are incomplete terminal writes recovered?

## Findings

- `BI-013` makes the submitted queue the daemon input. `br ready` is a planning
  input only. `BI-013d` requires its priority sort.
- `BI-013b` requires `br show <bead_id> --format json` for every submitted
  item. It must not mutate Beads. `BI-013c` requires a second open-state check
  immediately before claim.
- `BI-021` through `BI-023` make Beads the authority for bead content and
  coarse state. Git owns completion. JSONL is evidence only.
- `BI-029` through `BI-032` require a durable intent file before every terminal
  Beads write. Restart retains ambiguous intent files for reconciliation.

## Patterns to keep

- Read the source ledger through `br`, not its SQLite files.
- Join run evidence by bead ID through checkpoint trailers, event payloads, and
  session logs, as `BI-017` through `BI-020` require.
- Use one scratch daemon. `BI-025e` does not support multi-daemon access to one
  Beads store.

## Risks and decisions

- The readiness snapshot must inspect `.harmonik/beads-intents/` and
  `.harmonik/events/events.jsonl` as possible unresolved evidence.
- A scratch event result cannot close or reopen fleet work.
- A commit message alone is not completion proof. `BI-022` records this false
  close risk.
