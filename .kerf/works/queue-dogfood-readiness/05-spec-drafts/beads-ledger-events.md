# Queue readiness ledger and event record

## 1. Purpose

This record defines the read-first evidence required before first-canary
selection. Beads and git remain authoritative. Scratch results are evidence
only.

## 2. Readiness snapshot

Before selecting a canary, the gate owner MUST retain one readiness snapshot.
The snapshot MUST record capture time, commands, output paths, candidate set,
selected item, exclusion reasons, selected-item data, event-log path, and
terminal-intent inspection. It MUST name each stale graph finding, the checked
source path, and its disposition.

The selected item MUST be open, repeat-safe, and suitable for one local stream
run. Planning and selection MUST NOT close, create, or otherwise change fleet
ledger state.

## 3. Event evidence

The snapshot MUST identify the event files used as evidence. It MUST record
that JSONL is observational. It MUST NOT use a scratch event result to change
the fleet ledger. After the controlled run, scratch evidence may be attached to
the assessor report only.

## 4. Remaining findings

During implementation triage, a confirmed current condition MUST become a
new scoped open record with current source evidence. A stale condition may be
closed only after the operator authorizes the ledger change.

## 5. References

- `specs/beads-integration.md` BI-013 through BI-013c and BI-021 through BI-023.
- `specs/event-model.md` §8.10.
