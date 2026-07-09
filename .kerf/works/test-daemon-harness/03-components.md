# Decompose — test-daemon-harness

The capability decomposes into five components. Three are LANDED and enter this
plan only as the substrate the remaining work builds on; two carry the genuine
remaining design decisions.

## Component map

| # | Component | State | Owns |
|---|---|---|---|
| C1 | **Isolation & lifecycle core** (`init/build/up/status/down/cycle`) | LANDED | Standing up + tearing down a fully-isolated second daemon with the four fleet-safety guards. |
| C2 | **Batch runner** (`batch`) | LANDED | Submit a named bead batch to the scratch queue, fold the event stream into a structured pass/fail artifact; the shared `oneline` fail-signature normalizer. |
| C3 | **Feedback engine** (`feedback`) | LANDED | Turn FAIL items into deduped OPEN beads on the fleet DB; provenance-key idempotency. |
| C4 | **Verification harness** (`scratch-daemon-smoke.sh`) | LANDED (hermetic) / GAP (live + CI) | Prove the loop end-to-end; protect the normalizer corpus from rot. |
| C5 | **Durability & standing-capability wiring** | GAP | Make it a *durable* on-demand capability: CI regression gate, `make` discoverability, disk hygiene, a live-run demonstration of record. |

## Design decisions (the passes that carry real choices)

### D1 — C4: how to protect the normalizer from silent rot
**Chosen: a CI-gated hermetic run of the DEFAULT smoke phases (A/A2/B/C/D), plus a
golden-corpus regression test for `oneline`.**
- Rationale: the default smoke is already fast, hermetic, secret-free, and exit-code
  clean — it is CI-ready as-is. The one thing it lacks is a *stable golden corpus*:
  today the synthetic streams are inline. Extracting a `testdata/` corpus of
  (raw-summary → expected-signature) pairs makes new false-merge/false-split
  regressions a one-line diff rather than a buried assertion.
- Rejected: a Go port of `oneline`. The redaction logic lives in the shipped bash+jq
  path; porting it to Go would create a second source of truth that can drift from
  the one the harness actually runs. Test the real path, don't reimplement it.

### D2 — C5: what "durable standing capability" concretely requires
**Chosen: three small, independent additions, each self-contained and fleet-safe.**
1. **`make scratch-smoke`** target → `scripts/scratch-daemon-smoke.sh` (default
   hermetic phases). Mirrors the existing `make smoke-scratch` idiom so the
   capability is discoverable from `make help`.
2. **A CI gate** (or a documented pre-merge hook) that runs the hermetic smoke on
   any change to `scripts/scratch-daemon*.sh`. Path-scoped so it costs nothing on
   unrelated PRs.
3. **A `gc`/`reset` subcommand** on `scratch-daemon.sh` — prune the scratch clone's
   accumulated worktrees + `.harmonik/batch-*.json` artifacts + reset the scratch
   beads DB to a clean baseline, WITHOUT re-cloning. Must inherit `guard_path` +
   `assert_not_supervised`. Rationale: a durable loop run repeatedly will hit the
   Disk<10GiB merge-failure wall; a cheap reset keeps the inner loop fast.
- Rejected for this plan (kept as non-goals per problem-space): multi-machine / N-worker
  scheduling, auto-revive/supervision of the scratch daemon.

### D3 — C4: the live end-to-end demonstration
**Chosen: a recorded, reproducible live run behind an explicit opt-in flag, NOT a
default-CI live run.** A live batch needs a real claude binary + fleet slots-worth
of compute and is non-deterministic; forcing it into every CI run would be slow and
flaky. Instead: keep it gated (`--full`/`SMOKE_SCENARIO_RUN=1`), but produce ONE
recorded green run (log captured, referenced from the runbook) so the "demonstrated
end-to-end live" success criterion has evidence, and document the exact invocation.
- Rationale: matches the project's existing pattern (the remote-substrate e2e is
  already an opt-in scenario test); satisfies the success criterion without adding a
  flaky default gate.

## Dependency order
C1→C2→C3→C4 are already satisfied (all landed). The remaining work is:
- **C4-gap** (golden corpus + CI gate) and **C5.1** (`make` target) are independent
  and can dispatch in parallel.
- **C5.3** (`gc`/`reset`) is independent of both.
- **D3** (recorded live run) depends only on C1–C3 being present (they are) — it is a
  verification/evidence task, not new code, so it can run any time.
