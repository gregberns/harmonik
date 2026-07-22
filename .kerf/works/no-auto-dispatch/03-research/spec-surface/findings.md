# Research — C4 Spec surface (execution-model.md)

> **Provenance.** The planning agent independently GREPPED and READ `specs/execution-model.md` on
> branch `phase1-session-restart-substrate` (2026-07-21). Line/section references below were read
> directly, not bead-sourced. This is the one component the bead tree does NOT enumerate — surfaced
> here because harmonik is spec-first.

## The fallback is still NORMATIVE in the spec

Deleting the code without amending these leaves a spec-vs-code contradiction — the exact class
EM-066/EM-067 were originally introduced (v0.8.1, kerf `pilot`) to RESOLVE:

- **§4.11 EM-066 — "No-auto-pull (queue-only) daemon topology"** (line ~836). Per the v0.8.2
  `hk-8vy18` amendment, "queue-only is now the default for ALL daemon topologies; `--auto-pull` is
  the opt-in to enable the historical `br ready` fallback; `--no-auto-pull` is retained as a no-op
  back-compat alias." So the spec today makes the fallback a LEGAL OPT-IN, not a removed path.
- **§4.11 EM-067** (line ~848) — "Operator-pause binding and defense-in-depth gate on the
  `br ready` fallback path." Binds the fallback's pause behavior to the single pause-truth
  (`operator_pause_status`, ON-056/ON-057). Entirely about the fallback path; moot once it is gone.
- **§7.4 Run main loop pseudocode** (lines ~1465-1488): the `queue IS None` branch is a two-way
  fork — `IF no_auto_pull(): idle_wait_for_queue_submission; CONTINUE` ELSE `br ready` fallback
  with a defense-in-depth operator-pause re-assert. Post-removal this collapses to a single
  unconditional `idle_wait` arm (mirrors the C1 code collapse exactly).
- **§10.1 Core MVH conformance** (~line 1763): "Queue-only is the default for all topologies
  (EM-066); a bare boot with no submitted queue MUST dispatch zero runs. When `--auto-pull` is set …
  the `br ready` fallback is a conforming opt-in … The `--no-auto-pull` flag is accepted as a no-op
  back-compat alias." — makes the fallback conformant; must change to "queue-only is the ONLY
  topology."
- **§10.2 test obligations** (~line 1784): the EM-066/EM-067 obligations include a
  "Historical-topology test: boot WITH `--auto-pull` … verify the `br ready` fallback dispatches
  ready[0]" — this obligation is deleted; the quiet-daemon zero-runs test is KEPT and becomes the
  sole boot obligation.

## Amendment history context (from §12 revision log, read directly)

- v0.8.1 (2026-05-31, kerf `pilot`): ADDED EM-066/EM-067 to reconcile "spec said MUST NOT fall back
  to br ready; the live binary still did."
- v0.8.2 (2026-05-31, `hk-8vy18`): FLIPPED the default OFF — queue-only default, `--auto-pull`
  opt-in.

The through-line: the spec has been walking auto-pull DOWN (default-on -> default-off -> now:
remove). This work is the terminal step.

## Open decision D1 (needs a human — spec-shape)

Two conforming ways to amend, operator's call:
1. **RETIRE** EM-066/EM-067 outright (queue-only becomes an unconditional invariant folded into
   §7.4/§10.1; EM-066/EM-067 IDs marked retired in §12 with a revision-log entry). Cleanest.
2. **KEEP** EM-066 as the statement "queue-only is the only topology" (rewrite its body, drop the
   opt-in), RETIRE only EM-067 (pure fallback-gate). Preserves the EM-066 ID as the anchor.

Either way the §7.4 pseudocode fork collapses and the §10.1/§10.2 opt-in + historical-test language
is removed. The choice is bookkeeping (retire IDs vs repurpose EM-066), not behavior.
