# 05 — Spec draft: execution-model.md amendments (no-auto-dispatch)

> **DRAFT — pending operator ratification of D1** (retire-both vs repurpose-EM-066). This draft
> renders **branch A (retire both EM-066 + EM-067)** as the primary; branch B is noted inline.
> These are amendment fragments against `specs/execution-model.md` on branch
> `phase1-session-restart-substrate`. Copy to `specs/` on `kerf finalize` only AFTER D1 is settled.

## §3 Glossary — active-queue line (UNCHANGED, already correct)

> "active queue … The daemon dispatches exclusively from the active queue; absent an active queue,
> no dispatch occurs (§4.11, §7.4)."

No change: this line already states the terminal invariant. It becomes literally true (not "unless
--auto-pull") once the fallback is removed.

## §4.11 EM-066 / EM-067 — RETIRED (branch A)

Replace both requirement bodies with a retirement notice:

> **EM-066 / EM-067 — RETIRED (v0.9.x, no-auto-dispatch / hk-04q2j).** The `br ready` boot-time
> auto-pull fallback and its operator-pause binding are REMOVED from the daemon. Queue-only is no
> longer a *default* with a legal opt-in; it is the daemon's ONLY dispatch topology. The daemon
> dispatches EXCLUSIVELY from the active operator/agent-submitted queue (§3, §7.4); a bare boot with
> no submitted queue dispatches zero runs, spawns no agent subprocess, and claims no bead. There is
> no `--auto-pull` opt-in. These IDs are retired and NOT reused. Rationale: operator directive
> (2026-07-21, plans/2026-07-21-platform-architecture/DECISIONS.md) — the daemon is a dumb execution
> substrate; only agents decide what runs through it.

> **[Branch B alternative]** Keep EM-066, rewrite its body to the "queue-only is the ONLY topology;
> zero runs on bare boot; no fallback" statement above; retire only EM-067. Choose this if the
> cross-spec EM-066 anchor (cited by queue-model.md §8.5 QM-054, §10.1) should be preserved.

## §7.4 Run main loop — pseudocode collapse

The `queue IS None` branch collapses from a two-way fork to a single arm:

```
-- BEFORE (v0.8.2):
IF active_queue IS None:
    IF no_auto_pull():                 -- §4.11.EM-066 startup flag (default ON)
        idle_wait_for_queue_submission(); CONTINUE
    -- else: br-ready fallback (defense-in-depth pause re-assert per EM-067)
    ready = br_ready(); IF ready: dispatch(ready[0]); ...

-- AFTER (no-auto-dispatch):
IF active_queue IS None:
    idle_wait_for_queue_submission(); CONTINUE   -- queue-only: the daemon never self-starts work
```

Delete the `no_auto_pull()` conditional, the `br ready` fallback arm, and the EM-067
defense-in-depth pause re-assert comment.

## §10.1 Core MVH conformance — remove the opt-in

- Remove EM-066/EM-067 from the Core-MVH required requirement-set enumeration (branch A) OR update
  it to cite only the rewritten EM-066 (branch B).
- Replace: "Queue-only is the default for all topologies (per EM-066); … When `--auto-pull` is set
  … the `br ready` fallback is a conforming opt-in … The `--no-auto-pull` flag is accepted as a
  no-op back-compat alias."
- With: "Queue-only is the ONLY topology. Dispatch input MUST be the active queue per §7.4; a bare
  boot with no submitted queue MUST dispatch zero runs. There is no `br ready` fallback and no
  `--auto-pull` opt-in."

## §10.2 test obligations — drop the historical-topology test

- DELETE: "Historical-topology test: boot a daemon WITH `--auto-pull` … verify the `br ready`
  fallback dispatches ready[0]" and the "Sealing test" for the auto-pull config.
- KEEP + PROMOTE to sole boot obligation: "Quiet-daemon test: boot with no `--auto-pull` (now: boot,
  period) and submit no queue; verify over a bounded window that zero `run_started` events are
  emitted, no agent subprocess is spawned, and the daemon sits idle waiting for a queue submission."
- The operator-pause conformance for the *queue* path is unaffected (owned elsewhere); only the
  fallback-path pause tests go.

## Cross-spec ripple (must land in the same finalize)

- `queue-model.md §8.5 QM-054` informative note: drop "and the execution-model br-ready fallback
  gate (EM-067)" from the list of `operator_pause_status` co-consumers.
