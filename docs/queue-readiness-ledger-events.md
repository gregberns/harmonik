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

Capture one snapshot before you select the canary item. Today the capture is a
helper package with no command in front of it, so a gate owner reaches it from
Go and not from a shell. The `harmonik queue readiness` verb and the
`make queue-dogfood-readiness` target that drive it are task T9 of the kerf work
`queue-dogfood-readiness`.

The snapshot records:

- the capture time;
- every command that produced part of the evidence, and the file that holds its
  retained output;
- the candidate set — the selected item first, then every candidate you set
  aside;
- the reason you set each other candidate aside;
- the live ledger data for every candidate;
- every event-log file the evidence came from;
- the terminal-intent inspection.

The selected item must be open. It must be repeat-safe, and you must say why. It
must suit one local stream run: one item, concurrency one, no remote worker.

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
time. A caller supplies bead identifiers and reasons. It cannot supply a status.

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

## 7. References

- `specs/beads-integration.md` §4.5b BI-013e, §4.5a BI-013b, §4.7 BI-021 to
  BI-023.
- `specs/event-model.md` §8.10.
- `.kerf/works/queue-dogfood-readiness/08-stale-graph-triage.md` — the worked
  example the stale-finding fields are shaped after.
