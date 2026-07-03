# Research: Worktree Agent Write Surface (C2)

**Pass 3 — 2026-05-30**

## Research questions

1. What `br` write operations currently occur inside worktrees? Are there existing constraints in specs/beads-integration.md?
2. Does BI-010/BI-011/BI-025e already permit `br create` from agents?
3. What idempotency mechanisms does `br create` provide?

---

## Existing spec constraints

From `specs/beads-integration.md`:

- **BI-010:** Daemon owns all terminal transitions (open→closed, in_progress→closed, etc.). Agents must NOT call `br close` or `br update --status=closed` from inside worktrees.
- **BI-011:** No intra-run writes to the bead ledger except for the `claim` write (daemon calls `br update --status=in_progress` on the agent's behalf before spawning it). Agents may NOT call `br create` under the current spec.
- **BI-025e:** Concurrent `br` invocations across processes are permitted (SQLite WAL).

**Current state:** `br create` from inside a worktree agent is **not permitted** under BI-011. This is the gap to close.

---

## Idempotency for br create

`br create` accepts a `--title` argument. There is no native idempotency key. The only safe idempotency check is:

```bash
br list --label=codename:hk-<parent-id> | grep <expected-title>
```

If the result is non-empty, skip the create. This is the agent's responsibility — `br` itself does not deduplicate by title or label.

---

## Required spec amendment

BI-011 must be amended to add `br create` as a permitted intra-run write category (alongside the existing `claim` write), subject to:

1. Child beads MUST include `codename:hk-<parent-id>` label (orphan-sweep anchor).
2. Agent MUST check for existing child beads before creating (idempotency).
3. Terminal transitions remain daemon-only (BI-010 unchanged).

This is already captured in C2 component requirements (BI-010e, BI-011 amendment). No new findings beyond confirming the BI-011 gap exists and the idempotency approach.
