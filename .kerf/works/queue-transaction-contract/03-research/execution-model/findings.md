# Execution-model research findings

## Authorized scope

CQ-02 may align only the named-queue consumption surfaces listed in its
approved card: the two queue glossary entries, EM-015f, EM-062 through EM-065,
the queue bullet in §6.5, the steady-state queue-selection block in §7.4, the
queue-model dependencies in §9.3, and queue dispatch conformance in §10.1.
Run/workflow/checkpoint/failure and event-schema semantics remain unchanged.

## Finding 1 — execution still describes a project-wide singleton

The queue model already defines one live queue per normalized name, immutable
QueueStore fleet snapshots, per-queue workers, and QM-067 name-ordered
round-robin. Execution-model still says “the active queue,” scans only one
queue for duplicates, and treats any occupied name as blocking every submit.
Those statements would make the named-queue queue-model impossible to consume.

QM-061 also retained one stale sentence claiming QM-027 establishes a global
singleton. Its safeguard is per normalized name; submission still serializes
through QueueStore.

## Finding 2 — duplicate prevention must scan the complete fleet

Both daemon eager refill (EM-063) and orchestrator pre-submit (EM-064) must
scan every named queue in one immutable QueueStore generation. Checking only
the selected or proposed target permits one bead to be live under two names.
The all-queue scan precedes git/Beads/event supplemental checks and precedes
per-name submit or append.

## Finding 3 — selection has two independent capacity gates

An eligible dispatch candidate is an active queue with an active group and an
eligible item whose queue-local in-flight count is below `workers`. The daemon
also needs free global `--max-concurrent` capacity. A paused, completed, full,
or ineligible sibling contributes no candidate but cannot block an eligible
queue. QM-067 then selects among candidates and advances its persistent
round-robin cursor on every selection.

If the QueueStore named set exists but has no eligible candidate, the daemon
idles. `br ready` is considered only when that complete named set is empty.

## Finding 4 — eager refill needs a deterministic named target

EM-062 must compute from the same complete snapshot used by its all-queue
duplicate pre-screen. At Phase 1 the bounded choice is the first eligible
active stream queue in normalized-name order with positive deficit after both
global and per-queue capacity. This is deterministic and does not redefine
QM-067 dispatch fairness.

## Finding 5 — receipt durability wording remains valid

EM-015f retains the previously reviewed normal-path producer obligation:
authoritative per-name group/queue state commits first, final success receipt
durability is queue-model authority, event append failure does not roll state
back, and restart does not synthesize `queue_group_completed`.

## Finding 6 — fallback owners and dependency citations must match the loop

EM-066 and EM-067 still name the removed singleton `queue IS None` branch
after §7.4 moved fallback ownership to a complete QueueStore snapshot. Their
only required correction is the literal branch reference
`fleet.named_queues IS EMPTY`; default, sealing, and operator-pause semantics
do not change.

The §10.2 pause fixture also inverts the opt-in: fallback is enabled only with
`--auto-pull` set. An adjacent fixture must prove that a non-empty but wholly
ineligible named fleet suppresses `br ready` even with that opt-in. Finally,
§9.3 must cite lifecycle under queue-model §8 and the QM-062 capacity rule
under §9 alongside QM-060/QM-066/QM-067.

## Proof boundary

The machine proof compares the full execution draft against baseline and
allows only the exact regions above, four literal fallback/test replacements,
frontmatter `version`/`last-updated`, and one revision row containing
EM-015f, EM-062, EM-065, QueueStore, QM-067, `queue_group_completed`, EM-066,
EM-067, `--auto-pull`, and `nonempty-ineligible`.

## Decision

Correct QM-061’s per-name statement and align only the authorized execution
consumers with complete-fleet, per-name, two-gate, QM-067 semantics. Correct
the four exact fallback-owner/test tokens and §9.3 citations without widening
their clauses. Make no event-schema or non-queue execution change.
