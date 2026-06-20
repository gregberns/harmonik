# 09 — Adversarial Verifier: Sequencing & Completeness

**Role:** challenge the EXEC_SUMMARY wave-B ordering and find what's MISSING. I may
overrule the plan. All claims verified against code on 2026-06-20, not memory.

---

## Verdict up front

The plan's GROUPING is right; the ORDER inside wave B is **partly wrong** and it has
**one false dependency** and **one critical omission (C2 gauge-stale)** that I am
adding to wave B. Corrected order below.

---

## 1. Sequencing — W1 dominates, but W3 must move UP, and W4-after-W2 is defensible

**The plan's W2-after-W4 logic is sound, not backwards.** Report 03 §3 (caveat 3)
and §"verdict" explicitly recommend shipping crew watchers **warn-only, no
`--respawn-cmd`** *precisely because* the auto cycle (W2) is still open-loop —
arming 5 more open-loop ACT cyclers "multiplies exposure to a botched clear." So
W4 does NOT need W2 first: W4 ships in a mode (warn-only) that never invokes the
open-loop path. Doing W2 first to "restart-enable crew watchers immediately" is the
WRONG instinct — it would arm 5 destructive open-loop cyclers the moment they spawn.
Crew restart-enable is a deliberate later phase (after both W2 *and* a respawn-cmd
fork-bomb guard, given the 1500-session smoke history). **Keep W2 after W4.**

**But W1 alone is a half-fix without W3.** A `while…sleep…done` supervisor (W1)
converts "crashed-and-dead" into "crash-loops-restarting" — which is better, but a
keeper that is *up but blind* (the stale/foreign-gauge state, 165 live `no_gauge`
events) still emits nothing, and a supervisor that itself dies/wedges is again
silent. W3 (the `<agent>.keeperalive` heartbeat + `ops-monitor` `keeper-dead:`
alarm) is the ONLY finding that makes a dead/wedged watcher LOUD. Per
risk-reduction-per-step, **W3 is co-equal with W1, not a follow-on** — ship them in
the SAME change so the supervisor and the alarm-when-it-fails land together. The
plan already bundles W1+W3+W4 in step 1; I'm hardening that to "W1+W3 are the
indivisible pair; W4 rides with them."

**Optimal risk-reduction order (unchanged groups, hardened):**
`W1+W3 (indivisible)` → `W4 warn-only` → `W2+W6` → `+C2 (new, see §3)` →
`W5+W7` → `D1`.

---

## 2. Hidden dependency / collision — ONE plan claim is FALSE

**FALSE dependency: hk-uldg does NOT touch `captain-launch.sh`.** The EXEC_SUMMARY
attack premise ("hk-uldg ALSO touches captain-launch.sh to wire await-ack") is
unsupported. Verified: `grep -rln await-ack .claude/skills scripts` → **empty**;
hk-uldg's bead is the *agent-side ACK timer* (arm a timer, watch the pane for
`[KEEPER ACK <nonce>]` after the agent fires restart-now/ping) — a CLI/skill-side
mechanism, not an edit to the launch script. So **W1 and hk-uldg do NOT collide on
captain-launch.sh.** The W1/W4 collision surface is real but is the *launch-spawn
pattern* (captain-launch.sh:116-117 `tmux new-session … harmonik keeper …` vs the
new crewstart.go step 6b) across **two different files** — they share a SHAPE, not
a file. Safe to do in one agent to avoid hand-copying the supervisor wrapper twice;
no merge race.

**W2+W6 vs hk-zole/hk-0ouc:** W2+W6 edit `cycle.go` (884/913/923) and
`watcher.go` (hoist the 765 block above the 662/711 continues). hk-zole adds
*tests* on impure paste/respawn paths; hk-0ouc adds *test teardown*. Neither edits
the `runCycle` body or the watcher loop structure — they touch `*_test.go` +
respawn spawn. **Low collision risk, but real ordering coupling:** if W2 changes
`runCycle`'s tail (inject `AckLine`+`AwaitAck`), hk-zole's impure-path tests will
need to assert the new ACK step. **Safe merge order: land hk-zole/hk-0ouc FIRST**
(they're nearly done, P2, test-only), then W2+W6 — so W2 extends a green
impure-path baseline rather than racing it. hk-uldg (agent-side ACK) should also
land before W2 so the auto-cycle wiring reuses the same `AwaitAck` seam hk-uldg
exercises, not a parallel one.

**Safe overall merge order:** `hk-nbft` (W5/W7 depend on it) → `hk-0ouc`,
`hk-zole`, `hk-uldg` → **W1+W3+W4** → **W2+W6** → **C2** → **W5+W7** → **D1**.

---

## 3. Completeness — C2 (gauge-stale) is the top MISSING item; add to wave B

Re-reading 06/07 against the table: **W1–W7 contain NO fix for C2, the gauge that
dies on a live pane (165 `no_gauge:stale` events in the recent window — report 07
ground-truth + P4).** This is not cosmetic. Per report 07 §P4, C2 makes the
watchdog's *own input lie*: a fresh-but-wrong gauge (recently written,
under-reporting — round-1 saw the captain at 313k while the gauge showed 20-27%)
passes the freshness check, so the agent is **never flagged for restart**. That is a
silent **false-negative restart** — the exact failure the whole keeper exists to
prevent — and it degrades BOTH the keeper (stale-continue at watcher.go:662) AND the
crew watchdog from the same root. W6 only hoists the precompact block *past* the
stale continue; it does NOT make the gauge fresh. **C2 is higher-priority than W7
(a CLI ergonomics polish) and arguably than W5.** I am adding it to wave B between
W2+W6 and W5+W7. (Diagnosis-first: it needs an investigator to find WHY the
statusline gauge stops writing on a live pane before a fix — file `codename:keeper`,
type=bug, p1.)

**Other 06/07 omissions, ranked vs W7:**
- **`.ctx.tmp.*` write-race residue** (2 zero-byte leftovers): LOW. Latent
  identity-flap precursor; fold into the W1/W3 deploy change as a cleanup sweep.
  Below W7.
- **Duplicated agent-name derivation across 4 hooks** (inconsistent fallback
  chains) + **path-traversal guard missing from stop-hook/statusline** (report 06
  §4): MEDIUM. The path-guard gap is a real (if low-likelihood) security
  inconsistency; bundle BOTH into the eventual "collapse hooks to a single tested
  `harmonik keeper hook <event>` Go subcommand" — that is its own bead, NOT a wave-B
  item, but the path-guard one-liner (add `*/*|*..*` to stop+statusline) is cheap
  enough to ride with W5. Roughly W7-tier.
- **`ack_timeout` dead-letter** (07 P2): already captured as W3's second half
  (ops-monitor consumes the event) — confirm W3's bead explicitly adds the
  `session_keeper_ack_timeout` consumer, not just the `keeperalive` file. If W3 only
  ships the heartbeat, the dead-letter persists.

---

## 4. The D1 defer — correct, BUT a minimal identity fix must ride with W4

Deferring the full identity collapse (D1) is correct: report 02 proves the
gauge-latch/adopt arms are **sole-path on the unhappy paths** (no `.sid`), so
deleting them now kills crew/mobile monitoring. **However, the defer has a live
hazard for W4 that the plan misses.** Report 02 §1 + §3 Step A: crews have no
*guaranteed* `.sid` write (the SessionStart hook isn't proven wired for crews), so a
crew watcher armed by W4 will bind via the **gauge-fed latch** (watcher.go:713) —
the race-prone path. Report 03 §"caveat 2" claims crews DO have lowercase `.sid`
today, but that is happy-path observation, not a guarantee; on any crew where the
hook didn't fire, the W4 watcher latches whatever `cf.SessionID` the gauge supplied,
and a foreign/stale gauge → **crew watcher rebinds to the wrong sid → wrong-pane
inject** (the exact identity-flap D1 exists to kill). This is the adversarial answer
to the attack's question: **yes, leaving the 4 `.managed` writers + gauge-rederive
active actively undermines W4.**

**Minimal identity fix that SHOULD ship with W4 (NOT the full D1 collapse):**
guarantee `.sid` for every crew at spawn — in `crewstart.go HandleCrewStart`, seed
`<crew>.sid` from the minted/persisted registry `session_id` (which already exists,
report 03) at the same step that writes `.managed`. This is report 02's "Step A"
(the hard prerequisite) done for crews only — ~5 lines, no watcher.go/cycle.go
churn — and it removes the gauge-latch-binds-wrong-sid hazard for the new crew
watchers without touching the deferred collapse. **Make this a sub-item of the W4
bead, not a separate later task.**

---

## Cited anchors
- `scripts/captain-tools/captain-launch.sh:116-117` (bare one-shot keeper spawn, no
  while-loop) — confirms W1; `grep -rln await-ack .claude/skills scripts` empty —
  refutes the hk-uldg/captain-launch collision premise.
- `internal/keeper/watcher.go:662,711` (stale/foreign continue) before `:765`
  (precompact block) — confirms W6 hoist.
- `internal/keeper/cycle.go:884,913,923` (open-loop clear/resume + unconditional
  emitCycleComplete) — confirms W2.
- `internal/keeper/gates.go:11-25` (raw `filepath.Join`, no `Abs`) — confirms W5.
- report 07 ground-truth: 165 `no_gauge:stale` — C2 still live, absent from W1–W7.
- report 02 §1/§3 Step A + report 03 caveat 2 — the crew-`.sid` guarantee gap behind
  the W4/D1 interaction.
