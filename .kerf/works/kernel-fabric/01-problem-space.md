# 01 — Problem Space: kernel-fabric (P1)

**Work:** kernel-fabric · **Date:** 2026-07-22 · **Crew:** kilo
**Source material:** `plans/2026-07-21-p1-kernel-fabric/_plan.md` (reviewed, must-fixes applied),
`plans/2026-07-21-platform-architecture/{PROBLEM,DECISIONS,REVIEW,INPUTS}.md`,
`research/A1–A5`.

## Why this pass is a consolidation rather than a conversation

The jig opens pass 1 with "ask 2–3 focused questions." **That would be the wrong move here, and
saying why is part of the record.** This problem space has already been through a full
operator-led alignment: a problem statement written for alignment and agreed, six crux decisions
locked (C1–C6), an independent chief-architect review that returned a verdict per plan, and
three rounds of operator clarifications. Re-opening it as a fresh interview would either
re-litigate locked decisions or produce answers that contradict them.

So this pass consolidates what is settled, states the boundary, and isolates the **one** question
that genuinely still needs the operator. Where a normal pass-1 would end with a list of open
questions, this one ends with a list of *closed* ones and the evidence that closed them.

## What must be true about the system after this change

Harmonik has **no generic way for a component on one machine, container, or process to find and
talk to another** except by hand-building a bespoke pipe. The scrapped ssh-per-node model is the
proof, and it is worth stating as evidence rather than as an anecdote: every one of its wedges
was a missing fabric feature reinvented badly.

| ssh-model wedge | The missing fabric feature |
|---|---|
| Hand-allocated reverse-tunnel ports | No addressing |
| One hardcoded worker | No registration |
| Hook relay smuggled over ssh | No generic event channel |
| Every hack landed inside `internal/daemon` | No plugin boundary |

After this work, the following are true:

1. **A component addresses another by name, not by a hand-built pipe.** Named, typed channels
   move opaque bytes between plugins on the same or different machines.
2. **A new application is a plugin, not a daemon edit.** Applications (execution dispatch, comms)
   are in-proc Go libraries against a stable kernel interface. The kernel is the only
   network-aware layer; plugins reach other machines *through* it.
3. **The kernel cannot learn the domain.** It has never heard of a bead, a DOT node, a queue, a
   run, a session, a repo, an agent, or an SSH key. `Payload` is `[]byte` — the kernel *cannot*
   parse a payload even if a future contributor wants it to.
4. **Plugins get state from a standard seam** rather than each inventing its own storage.
5. **Growth pressure on the kernel is a signal, not a request to satisfy.** Two consecutive
   plugins needing a new kernel verb means the boundary is wrong.

## The problem this solves, stated at the level that matters

Not "we lack a message bus." The actual failure is **architectural gravity**: with no fabric and
no plugin boundary, every cross-machine or cross-process need has exactly one place to land —
`internal/daemon` — and so the daemon accumulated 641 files and every subsequent need got harder
to place. The ssh model was not badly built by careless people; it was the only shape available.
P1 exists to make a second shape available, so that P3's distributed execution does not become
the next ssh model.

**This is explicitly not over-architecting, and that framing is itself a first-class goal.** The
operator has raised distributed-architecture needs repeatedly and had them dismissed as too
complicated. The evidence above is what makes the need concrete rather than speculative. The
guard against the opposite failure — a plan whose premise is "objections were the pattern to
break" has no brake — is the guardrail package (see the open question).

## Scope

### In scope

- The kernel interface: transport (pubsub, point-to-point, request-reply, lookup), addressing
  (lookup + roster), plugin lifecycle, and the reserved resource seam with **State only**.
- `internal/kernel/` as a new package with a dependency-allowlist test.
- Two implementations behind one interface: an **in-memory kernel** (default test double) and a
  **networked kernel** whose transport product choice is deliberately deferred.
- The kernel↔kernel boundary: channel bytes on a route, LOOKUP entries, liveness probes. Nothing
  else crosses a machine boundary.

### Explicitly out of scope

- **The artifact plane.** Git stays it (C1). The fabric carries control, events, and addresses —
  never code, diffs, or repos.
- **Work dispatch.** The queue, scheduler, and leases are an *application* on top (C3), not a
  kernel property. The kernel has no queue.
- **Delivery guarantees.** The kernel is at-most-once: never queues, never retries, never
  redelivers. Durability is built *by plugins* from journal + `message_id` + interest + liveness.
- **The distributed-systems hard part.** Leases, redelivery, orphan recovery, and the C5 liveness
  controls are the dispatch plugin's machinery — P3's design, not P1's. P1's obligation is to
  expose exactly the four tools that make them buildable, and it does.
- **Resources beyond State.** The seam is designed to admit Blob/Secrets later; building them now
  is forbidden until a plugin actually needs one.
- **LOOKUP's replicated implementation and any consumer of it.** The interface ships; the
  cross-node implementation lands when dynamic worker join is scheduled.
- **P2 extraction and P3 execution.** Separate threads with separate clocks.

## Constraints

- **C1–C6 are locked.** Reopening one requires strong new evidence, not a preference.
- **No hacks in either P1 or P3** (C2's governing rule). P3 is P1's test bed; when P3 discovers a
  need, P1 adds it to the contract *only if* it is domain-blind.
- **Nothing new for the execution path is born in `internal/daemon`.**
- **`[]byte` payloads, never a typed or `Any` value.** substrate-v2's one hard line, kept verbatim.
- **One dispatch plugin with a `role: primary|worker` switch**, not separate primary and worker
  plugins — two namespaces could not share `dispatch.*` without weakening namespace ownership,
  which is the boundary that makes cross-plugin storage access unrepresentable rather than merely
  forbidden.
- **In-proc plugins forfeit live-reload.** Accepted consequence of C6; swapping a plugin means
  restarting the daemon.

## Success criteria — what the specs should describe when this is done

1. A spec defines the kernel's plugin-facing interface: transport, lookup, roster, resources,
   info, and the plugin lifecycle contract, with namespace scoping stated as a property of the
   handle rather than a rule about arguments.
2. A spec states the kernel↔kernel boundary as an exhaustive list of what crosses a machine.
3. A spec states the at-most-once guarantee and names the four durability tools plugins build on.
4. A spec states the manifest and channel-declaration rules, including that a conflicting type
   for a channel name is rejected at registration.
5. A spec states the payload ceiling as a mechanism enforcing C1, with its value and its reason.
6. A spec states the boundary tests and what firing one obliges — investigate the boundary, not
   satisfy the request.
7. The LOOKUP deferral is stated as a contract with a named trigger, not as an omission.

## What is already decided — closed, with what closed it

Recorded so a later pass does not reopen them.

| Question | Answer | Closed by |
|---|---|---|
| Does the fabric carry artifacts? | No — control plane only; git is the artifact plane | C1 |
| Kernel now or ad-hoc pipe migrated later? | Kernel now, minimal slice, built in parallel with P3 | C2 |
| Where does "central" live? | Transport leaderless; dispatch central *as an application* | C3 |
| Plugin boundary: separate binary or in-proc? | In-proc Go interface; kernel↔kernel is the only wire | C6 |
| Streaming shape | Go channels + cancel func | P1 §7 |
| Namespace isolation | Namespace-scoped handle at `Start`; no namespace argument anywhere | P1 §7 |
| Package home | `internal/kernel/`, dep-allowlisted, outside `internal/daemon` | P1 §7 |
| Cross-machine transport product | Deferred until the first cross-machine P3 run is scheduled | Review |
| Static-primary first cut acceptable? | Yes — locked verbatim; the question was deleted, not answered | C3 |
| Live-reload loss | Accepted consequence of C6 | Review |
| Payload ceiling | Hard low ceiling, order 256 KB — converts C1 from discipline to mechanism | Review |
| Dispatch channel ownership | One plugin, `role: primary\|worker`, one namespace | Review must-fix 6 |
| LOOKUP deferral vs. P3 marking it REQUIRED | Static-config reading wins; LOOKUP is post-v1 with a named trigger | Review must-fix 1 |

## The one open question — needs the operator

**Ratify the kill-criteria / boundary-test guardrail as shared, pre-agreed ground rules.**

`DECISIONS.md` lists this as "proposed — to ratify," so it is explicitly reserved for the
operator. The package: dependency-allowlist test, vocabulary/boundary test (no domain noun in
`internal/kernel`), the `[]byte`-payload rule, a kernel-size tripwire, and "two plugins needing
new kernel verbs = stop."

**Why it needs ratification rather than adoption:** its whole value is that it is *pre-agreed*.
A guardrail one side adopts unilaterally settles nothing — the next scope dispute is still won by
whoever says "over-engineering" or "dismissal" first. Ratified in advance, the dispute is settled
by a test both sides already accepted.

**One caveat on how it is expressed.** The operator's standing direction is that agents work
better from **principles (directions to travel) than rules (laws)** — rules get obeyed literally
and produce goofy behaviour. `DECISIONS.md` OQ-1 accordingly reframes this as principles folded
into the platform's existing set, *not* a separate "kill-criteria" construct. This pass adopts
that framing: the mechanical CI checks exist as **signals that prompt thought, not laws that stop
it**, and the four platform principles (kernel carries signals not meaning; a healthy kernel
stops growing; the fabric moves references not cargo; code lives in the component that owns it)
are the durable form. Ratification is of the package as principle-backed checks.

**Recommendation: ratify.** One yes also covers the equivalent question in P2 and P3 — the review
deduped three raw questions into this one.

## Affected spec areas — first cut for pass 2

No existing spec covers a kernel or plugin fabric; `internal/kernel` does not exist (verified).
This is genuinely greenfield, which makes the decompose pass mostly about *what to create*:

- **New:** a kernel/fabric spec — the plugin-facing interface, manifest and channel rules,
  namespace scoping, at-most-once semantics, the kernel↔kernel boundary, the payload ceiling.
- **New:** a plugin-contract spec — lifecycle, health, registration-time validation, and the
  resource seam with State as its only current member.
- **Touched, to be confirmed in pass 2:** `architecture.md` gains the layer; `process-lifecycle.md`
  may need the kernel's place in daemon startup; `operator-nfr.md` may need the boundary tests as
  checkable properties.

Pass 2 confirms this list against the corpus rather than inheriting it.
