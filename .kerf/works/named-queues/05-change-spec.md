# named-queues — Change Spec (normative delta map)

> Consolidated map of every `specs/queue-model.md` (QM-*) change this work makes.
> Per this spec-first project, **the spec text is written as part of each implementation
> bead**, not in a separate spec-draft commit. This file is the index of *which* rules
> change and *in which bead*, so a reviewer can check spec/code alignment per commit.
> Research (Pass 4) and integration (Pass 6) were compressed into the analyze + decompose
> + independent-review passes (see `02-analysis.md`, `03-components.md`, `07-tasks.md`);
> the design was verified against `main` @ 550d3a78 by the reviewer (verdict SOUND-WITH-FIXES).

## Amendments to existing rules

| Rule | Today | After | Bead |
|------|-------|-------|------|
| **QM-027** (single-active-queue) | At most one active queue per daemon | At most one active queue **per name** (N names allowed) | NQ-A1 |
| **QM-002 / §2.1** (queue envelope) | UUID `queue_id` only | + durable `name` field (routing key, distinct from per-submission `queue_id`) | NQ-A1 |
| **QM-002 / §2.2** (QueueStatus enum) | Spec omits `cancelled` (code has it) | Enum lists all 5 incl. `cancelled` | **NQ-R1** (prereq) |
| **QM-061** (multi-orchestrator safeguard) | Leans on "one queue" | Re-worded to "one submitter, N queues" | NQ-A1 |
| **QM-062 / §9.3** (concurrency) | Single global `--max-concurrent` gate | Two-level: `min(group_pending, per_queue_workers − queue_running, global_cap − global_running)` | NQ-B1 |
| **QM-003 / QM-053** (unlink) | Singleton unlink | Per-name unlink | NQ-A2 |
| **§2.9** (file layout) | `.harmonik/queue.json` | `.harmonik/queues/<name>.json` + one-time legacy `queue.json`→`main` migration | NQ-A2 |
| **§2.10** (wire records) | No name field | Additive-optional `name`/`queue` on Submit/Append requests (absent = `main`; no schema bump per ON-018) | NQ-D1 |
| **QM-054 / QM-055** (pause) | `paused-by-drain` is global | Per-queue; named pause drains only that queue; unnamed global pause still drains ALL (back-compat + EM-067 br-ready gate source) | NQ-C1 |
| **QM-056** (pause-reason enum) | — | Extend if a per-queue-operator reason value is needed | NQ-C1 |
| **§A.3** (deferred v0.2 surface) | per-queue `queue-resume` deferred | **Un-deferred** (deliberate G3 scope add) | NQ-C1 |
| **error-code block** `-32010..-32019` | One slot left (`-32019`) | `-32019 → queue_not_found`; **block now exhausted** (future reason needs a PL-003a block extension) | NQ-D1 |

## New rules

| New rule | Content | Bead |
|----------|---------|------|
| **Queue naming** | charset/length, reserved default `main`; name = durable routing key | NQ-A1 |
| **Registry enumeration** | startup scans `.harmonik/queues/`; single-writer-per-file invariant preserved | NQ-A2 |
| **Per-queue worker count** | `workers` field; default = today's `--max-concurrent`; MUST be ≤ global cap | NQ-B1 |
| **Cross-queue dispatch policy** | name-ordered round-robin with a daemon-state cursor advancing **every tick** (not reset to 0 — else first-named queue starves the rest); oversubscription (Σ caps > global) allowed + logged **once** as a warning at create/submit; explicitly NOT weighted fairness (that is N3 / extqueue v0.2) | NQ-B1 |
| **Routing default** | absent `--queue` ⇒ `main`; implicit-create on first submit to an unknown name with default workers | NQ-D1 |
| **Investigate-queue note** (informative) | `investigate` is an ordinary named queue with **no special semantics**; subscription billing comes from being a daemon-spawned claude (credfence), NOT from anything queue-model-specific; NO per-queue budget (N2) | NQ-E1 |

## Decisions locked (orchestrator, this session)

- **Budget scope = shared global** (open-decision 1, option A). Per-queue caps = deferred follow-up **NQ-X1** (P3). *User may still redirect to option B.*
- **Term = "named queue"**; "channel" is a documented synonym (no second abstraction — N4).
- **Queue creation = implicit-on-first-submit**, plus an optional explicit `create` for presetting workers.
- **Contention policy** = per-queue cap + global hard ceiling + name-ordered round-robin (confirmed starvation-free by the reviewer).
