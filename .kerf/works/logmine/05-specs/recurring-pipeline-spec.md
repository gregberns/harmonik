# logmine — Change-Spec: from manual playbook → recurring pipeline

**Work:** logmine (epic hk-mhmaw) · **Pass 5 (change-spec)** · author crew **liet** · 2026-06-11.
**Input:** `pipeline.md` (the manual re-run playbook), `04-research/findings.md` (iter-1),
`04-research/findings-iter2.md` (iter-2). The method has now run **twice** end-to-end
(2026-06-09, 2026-06-11) with the same shape and improving yield — it is stable enough to automate.

## Goal (Done means…)

The logmine harvest fires on a **trigger** (not a human remembering to run it), executes the validated
fan-out, and lands a dated findings doc + `codename:logmine` beads + a captain digest **with zero human
initiation**. A human is involved only to act on the surfaced *decisions*, never to start the run.

## What iter-2 validated (lock these into the spec)

1. **6-slice fan-out beats 8.** Worktree transcripts auto-clean (only ~6 alive at any time), so the
   dedicated "sub-agent transcripts" slice is low-yield — fold it into the failures slice. The validated
   split: (1) failures/wedges, (2) reconciliation/queue-lifecycle, (3) review-subsystem,
   (4) daemon-log, (5) comms-bus, (6) process/workflow/docs+CI.
2. **The FIXED-vs-RECURRING delta is the payoff of recurrence.** Iter-2's highest-value output was
   confirming **12 prior findings FIXED** — a one-shot can't produce that. Every slice MUST read the
   prior `findings*.md` register first and classify each prior Fxx as FIXED-confirm / RECURRING / still-open.
   Carry the F-numbering forward (iter-2 = F23+).
3. **Dedup-against-open-beads is mandatory, not optional.** Iter-2 found most of the failure cluster was
   already filed (hk-hdbls/hk-g8hv2/hk-hypbi/hk-7evda/hk-sah87/hk-8oy). Pre-screen `br list --status=open`
   first; **enrich the existing bead with a comment** instead of filing a duplicate.
4. **The "52%-failure-rate is mostly FALSE" insight** only emerges by cross-checking `Refs:` commits on
   main per failed run. Keep the false-fail triangulation step (`git log --all --grep "Refs: <bead>"`).

## CLI gotchas (cost real time both runs — encode them)

- `br create --labels <a,b>` (PLURAL) but `br list --label <x>` (SINGULAR). Inconsistent; both required.
- `br list --json` silently caps at `--limit 50` (default); pass `--limit 0` for the full set, or use plain output.
- `br comments add <id> --author liet --message "…"` to enrich; never `br close` (daemon owns terminal transitions).
- Method rule (load-bearing, F14): **never hand-grep events.jsonl by run_id** — false negatives. Use `jq`
  filtered on `.timestamp_wall` prefix.
- `.kerf/*` and `/.harmonik/` are gitignored → bench writes don't dirty the tree (safe while the daemon runs).

## The change: a TRIGGER — RESOLVED (operator decision, 2026-06-11)

The only thing standing between the playbook and a recurring pipeline is automated initiation. The operator
has decided: **run it DAILY, billed to the SUBSCRIPTION (not the metered API — avoid the credit-burn class),
structured "like a crew member" — a persistent/scheduled `claude` session, NOT a headless API routine.**

That resolves the mechanism to a **crew-style daily trigger** — neither a cloud `/schedule` routine nor a
headless `claude -p` (both rejected by "not a headless API routine"). It reuses the existing **Captain &
Crew** spawn mechanism (interactive `claude --remote-control` in a detached tmux, **subscription-billed** —
the same way crew liet runs today), fired once per day. The full wiring is in `06-integration.md`.

- **Cadence: DAILY** (operator override of my event-volume recommendation — accepted; daily keeps a steady
  cadence and the FIXED-vs-RECURRING delta is still valuable on quiet days). Window = since the prior run's
  high-water `event_id`, recorded in each findings-doc footer.
- **Billing: subscription**, via the crew's interactive `claude --remote-control` session (never `claude -p`
  / metered API — the 2026-05-30 credit-burn class).
- **Mechanism: crew-style** — a daily scheduler ensures crew `liet` runs the logmine mission (spawn-fresh or
  re-task), executes the validated 6-slice harvest, files/routes beads, digests to captain, then idles/exits.

*Deferred follow-up (file as a bead):* a **harmonik-native scheduled-job primitive** so the daily trigger
isn't cron/launchd-dependent — cleaner long-term, but net-new daemon code; not a v1 blocker.

## Next kerf pass

On a trigger decision: advance to **integration** (Pass 6) — wire the chosen trigger, fold `pipeline.md`'s
boot+harvest+report steps into the routine's prompt, and file the automation as a bead. The harvest
*method* is frozen; only the *initiation* remains to build.
