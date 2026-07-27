# Durability Round-3 response

The final focused durability review's `R3-1` finding is accepted.

Queue-model QM-061 now states that submission serializes through the
daemon/project QueueStore while QM-027 occupancy is per normalized name, not a
project-wide singleton.

The execution-model draft now consumes the named QueueStore consistently in
only the newly authorized regions:

- the glossary defines active queues and groups per named queue;
- EM-015f terminalizes only the named queue/group identified by the Run;
- EM-062 selects a deterministic eligible active stream refill target from one
  complete immutable named-set snapshot;
- EM-063 and EM-064 pre-screen duplicates across every named queue;
- EM-065 submits to a free target name or appends/rejects within an occupied
  target name;
- §7.4 permits `br ready` only when the named set is empty, filters eligible
  active queues without sibling blocking, applies both `--max-concurrent` and
  selected-queue `workers`, and advances the QM-067 cursor on every selection;
- §9.3 and §10.1 now describe QueueStore identity/lifecycle ownership and the
  same consumption rules.

Research, design, changelog, task ownership, and CQ-02 evidence were updated
to match. The full-file semantic checker allows only those exact regions,
frontmatter version/date, and one qualifying history row; all Run, workflow,
checkpoint, failure, event-payload, event-log, and non-queue execution
semantics remain byte-preserved.

No normative `specs/` file, production code, task index, Beads ledger, commit,
or reviewer artifact was changed. Ready for focused durability and
composition re-review after the complete validation set passes.
