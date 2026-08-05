# Queue readiness ledger and event record

**Operational record.** Normative source: `specs/beads-integration.md` §4.5b
BI-013e. Implementation: `internal/queue/readiness`.

## 1. What this record is for

A person picks the first canary item for a queue dogfood pass. An assessor judges
that choice later, in a different session, with none of the context. This record
is what the assessor reads.

Beads and git stay authoritative. A scratch run produces evidence and nothing
more. It does not change fleet state.

## 2. The readiness snapshot

Capture one snapshot before you select the canary items. Two commands drive it:

    harmonik queue readiness capture   # reads the live ledger, writes the record
    harmonik queue readiness validate  # judges the record, the plan and the host

`make queue-dogfood-readiness` runs both in order and measures the host between
them. It requires four inputs and guesses none of them: the scratch clone, the
evidence directory, the canary items with a reason each, and the concurrency.

The snapshot records:

- the capture time;
- the run shape — how many items, how many at the same time, and whether the run
  stays on this machine;
- every command that produced part of the evidence, and the file that holds its
  retained output;
- the candidate set — the selected items first, then every candidate you set
  aside;
- the reason you set each other candidate aside;
- the live ledger data for every candidate;
- every event-log file the evidence came from;
- the terminal-intent inspection.

Every selected item must be open, and each must carry its own reason for being
safe to run more than once. One sentence covering three items is two of them
taken on trust.

## 2a. The run shape

The record holds a run to one rule and measures the rest. A remote run is out of
scope for the first pass, so the run must be LOCAL. Everything else about the
shape is recorded rather than refused.

**The one-item rule is withdrawn.** An earlier reading of BI-013e required one
item at concurrency one. The operator overruled it: a queue that can carry only
one item at a time proves nothing worth proving, and the assessor's job is to
sign off on several items running at once.

What replaced it is arithmetic. The stated item count must equal the number of
items the record names. Without that check, widening the run shape gives a
record that claims three items and names one, and the assessor reading it six
weeks later cannot tell which number is true. The concurrency must be stated;
no particular value is required.

The run shape is one field of the record and not a copy inside each selected
item, so two items in one record cannot disagree about how many items there are.

Selection is planning. It closes no bead, creates no bead, and changes no fleet
ledger state.

## 3. Why the record cannot change the ledger

`internal/queue/readiness` reads the ledger through one port, `BeadReader`. That
port has one method, `ShowBead`. It has no method that can write. No ordinary
call through it can close or create a bead.

Read the size of that promise exactly. The compiler stops the easy mistake, not
a determined one: Go lets code recover a wider interface from a narrower one
with a type assertion, and that still compiles. Two tests cover the two halves.
`TestBeadReaderExposesNoWriteMethod` goes red if the port grows a method that is
not a read. `TestCapture_MakesNoLedgerCallThatCouldChangeABead` goes red if a
capture widens the port it was given and writes through it — the fake ledger it
runs against accepts writes, so the test can see one.

Every candidate status in the snapshot comes from a live `br show` at capture
time, one read per candidate. A caller supplies bead identifiers and reasons. It
cannot supply a status.

## 4. Event evidence

The snapshot names every event-log file it read. The log rotates, so a capture
that spans a rotation names more than one file. It also carries a fixed note that says
the JSONL log is observational: it is evidence about what happened, and it never
drives a write back to the ledger. `specs/beads-integration.md` §4.7 BI-023 is
the rule. The constructor writes the note, so a capture cannot soften it.

Attach scratch event evidence to the assessor report. Attach it nowhere else.

## 5. Terminal-intent inspection

The snapshot records the directory it inspected for pending terminal writes, and
every entry it found there. The directory is `.harmonik/beads-intents`.

An empty result with a named directory is a finding. It says the inspection ran
and found nothing pending. A snapshot with no directory says nothing at all, and
the record refuses it.

## 6. Stale findings and current findings

The snapshot holds two separate lists. It does not tell the two apart by a field
on one shared list.

A **stale finding** is an old condition that current source already fixed. It
names the finding, the source path that was checked, the commit that fixed it,
the focused test that pins the new behaviour, and the disposition. All five are
required. A disposition without a commit and a test is a judgement, not
evidence.

A **current finding** is a condition that current source still has. It becomes a
new scoped open record. It names that record, the current source evidence for
it, and the source path where it was seen.

Only the operator authorizes a ledger change that closes a stale finding. The
snapshot records the evidence for one. It does not make one.

## 7. Schema versions

The snapshot is at version 2 and the validation record is at version 2. Version
2 of the snapshot replaced the single selection object with a list and lifted
the run shape to the top of the record. Both are renames, so a reader of the
older version cannot read the newer one, and the decoder refuses a version it
cannot read rather than reporting an empty selection.

These numbers are not the assessor handoff schema version.
`specs/assessor-handoff-schema.md` is a separate artifact with its own number.
Do not reconcile the two. That spec briefly carried a second, conflicting
version number, which was retired on 2026-08-05. It now states one version.

## 8. References

- `specs/beads-integration.md` §4.5b BI-013e, §4.5a BI-013b, §4.7 BI-021 to
  BI-023.
- `specs/event-model.md` §8.10.
- `.kerf/works/queue-dogfood-readiness/08-stale-graph-triage.md` — the worked
  example the stale-finding fields are shaped after.
