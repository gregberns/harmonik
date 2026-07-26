---
schema_version: 1
crew_name: hotel
queue: hotel-q
epic_id: hk-keeper-delivery-agent-input-6nz2j
captain_name: captain
model: opus
goal: "Drive Track A — the keeper-restart-delivery kerf work (leader-session keeper timing + delivery redesign, from xray's research). 10 beads T1-T10. Start the 3 parallel-ready P1 roots (T1/T2/T5), then work the dependent tier. Honor the load-bearing guardrails. INLINE, independent-review each, report."
---

# Mission: hotel — Track A (keeper-restart-delivery)

You are crew **hotel** (NATO naming; we skipped 'golf' — an abandoned `golf-q` queue still exists). You own the
**keeper-restart-delivery kerf work** (Track A) and report to **captain**. This is the leader-session keeper
timing + delivery redesign from xray's research. Landing Track A is what **unfences** the Codex
keeper-retirement slice — so it matters beyond its own beads.

## The work — 10 beads (label `codename:keeper-restart-delivery`)
`kerf next --only=bead` attaches all 10; `br list --label codename:keeper-restart-delivery` lists them.
**START NOW, in PARALLEL (3 ready P1 roots, no cross-dep):**
- **T1** `hk-keeper-delivery-agent-input-6nz2j` — keeper as comms producer + presence-reachability read.
- **T2** `hk-keeper-delivery-config-surface-e1mdc` — config surface: warn_messages leader-defer + crew keys → WatcherConfig.
- **T5** `hk-keeper-delivery-restartnow-nonce-kz4w6` — restart-now `--nonce` flag + carry-for-audit provenance.
Then the dependent tier: T3 (templated defer slots), T4 (mtime-gated per-tick re-read), T6 (nonce provenance),
T7 (K1 delivery decision: presence pre-check + comms-send vs terminal fallback, SK-INV-006), T8 (K5 in-cycle
operator-attached TOCTOU re-check).

## LOAD-BEARING GUARDRAILS (from the research — do NOT violate)
1. **ZERO keeper threshold changes** — the warn/act thresholds in STATUS.md are FIXED. This is delivery/timing, not tuning.
2. **Do NOT naively drop the hk-89g retry-Enter reliability loop** — preserve its reliability guarantee; redesign around it, don't delete it.
3. **Honor `keeper-verdict-design`** (the design doc) — read it before touching delivery decisions.
4. **The 2 test beads are ACCEPTANCE CRITERIA, not optional:**
   - `hk-keeper-delivery-scenario-tests-qji8g` (P2, scenario: session-twin integration tier — 5 failures to turn green).
   - `hk-keeper-delivery-exploratory-w09ua` (P2, explore: operator CLI surface & on-the-fly config).
   Track A is not "done" until both are green.
5. **Independent review gate on every commit + a pre-deploy ISOLATED E2E** per standing rules.

## SEQUENCING FENCE (load-bearing)
yankee is doing Codex Phase-2, whose keeper-retirement slice (hk-f8wtm) is **design-only behind this Track A**.
**Do NOT let your keeper-code edits and yankee's collide** — you own the keeper delivery/timing code; yankee
stays out of keeper code until Track A lands. If you find yourself needing to touch codexdriver/harness code,
stop and tell captain (that's yankee's lane).

## How you work (INLINE — daemon worktree-dispatch is off; do NOT `queue submit`)
`br show <id>` → root-cause → fix in the main tree → **independent review** (spawn a reviewer sub-agent; captain
gates) → commit **explicit paths only** (NEVER `git add -A`/`.`, bare commit, reset, amend). **Keep the shared
tree BUILD-GREEN at every pause.** After each commit **verify `git rev-parse HEAD` == your new SHA** (concurrent-commit
race, hk-jejte) — if not, your changes are still staged, retry. Reference the bead id in the commit subject.
Do NOT set in_progress or close — captain owns terminal transitions.

## On boot
0. `harmonik agent brief`.
1. `harmonik comms join --name hotel` + confirm identity = hotel.
2. Arm `harmonik comms recv --agent hotel --follow --json` **via the Monitor tool**.
3. `br update hk-keeper-delivery-agent-input-6nz2j --assignee hotel` (mirror first root; re-affirm on each adopt).
4. Read the kerf work context + `keeper-verdict-design` before editing. Post a boot status to captain (`--topic status`).

## Progress feed
`comms --topic status` to captain on each bead-done + a ≤10-min timer while working + boot/drain bookends.

## Keeper restart
Re-read this file, re-join comms as `hotel`, re-arm the recv Monitor, re-affirm `--assignee`. Resume the current bead.
