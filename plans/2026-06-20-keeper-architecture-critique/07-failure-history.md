# 07 — Keeper Failure History (Failure-Mode Historian)

**Lens:** Catalog the keeper's *actually observed* failures from durable evidence, then
classify each as architectural / complexity / timing / plain-bug / spec-gap. Recurrence is
the strongest signal of architectural vs. surface trouble.

**Evidence base:**
- `git log` — **160** keeper-touching commits; **94** touch `internal/keeper` or the keeper
  CLI; **41** are `fix(keeper)`/`revert` commits (≈44% of code-touching commits are fixes).
- `.harmonik/events/events.jsonl` — live `session_keeper_*` traces (the ACT-loop, the
  `restart_now_blocked` reasons, the suspended `^Z` pane).
- `.kerf/works/keeper-redesign/01-problem-space.md` — the team's OWN postmortem: a
  documented **~69% fix-of-fix regression rate** and a 33-agent adversarial deep-dive.
- `plans/2026-06-20-keeper-investigation-recovery/README.md` — the recent overnight bug-hunt.
- `docs/retro/2026-06-10/A6-session-keeper-enable.md` — the enable-time risk register.
- Open beads: hk-gffc, hk-5da7, hk-7myt, hk-34ac, hk-nlio, hk-gfpd.

---

## The headline number (team's own measurement)

The keeper-redesign problem space states it outright: the keeper has a **~69% fix-of-fix
regression history — roughly two of every three fixes to the identity/liveness state machine
re-broke a previously-fixed case.** That is not my inference; it is the operator/team's
recorded finding, and it is corroborated by the commit ledger (41 fix commits, repeatedly
re-touching the same ~5 files). Two failure modes dominate by raw event volume:

- **~2,852** `no_gauge:stale` events — gauge dead while the pane is demonstrably alive.
- **~3,956** `operator-attached` false-suppressions — captain parked warn-only.
- By contrast the inject step itself fails **~0.5%** of the time.

---

## Failure catalog (grouped by class)

| # | Signature (durable evidence) | Root cause (if known) | Fix(es) attempted | Recurred? | Class |
|---|---|---|---|---|---|
| **C1** | **Identity mis-bind / `.managed` drift / foreign_session.** `--session-id` goes DEAD after 1st `/clear`; stale `.managed`; UUIDv7-clobbers-UUIDv4; same-agent `.ctx` clobber. | Heuristic identity state machine *infers* which session to bind (latch / auto-clear / flap-cooldown / suppress / UUID-version + uppercase guards). Each edge case spawned a new branch that interacted with the others. | a8e97568 (pin identity across cycle), 5ff54d0a (key `.ctx` by sid), bf8c929b (self-heal stale v7), 0eaf022e (skip-retain v7), df727a7f (foreign auto-recovery + rebind CLI), edb35c7a (flap cooldown + atomic write + uppercase guard), 056f60e3 (latchSuppressed self-recovery), cfaa8483 (atomic write), ddd24a73 (clear-on-timeout), 1575546d / 12853cc6 / a16188e0 (re-resolve `.managed` from `.sid`), 6894b4de / 05035b60 (`.sid` single-writer), **93f7000e (DELETE all the heuristics)** | **YES — ~10+ fixes, each re-broke a prior case.** This IS the ~69% loop. Only stopped when 93f7000e *deleted* the inference layer. | **architectural** |
| **C2** | **Gauge never-live / `no_gauge:stale` on a live pane.** ~2,852 events; warn AND act both die silently. Confirmed live 2026-06-18 (`session_keeper_warn pct:20/25/27` then watchdog "captain ctx high (313k)" — gauge under-reporting). | `.ctx` written only by the statusLine hook; goes stale on idle and returns NA right after `/clear`; nothing re-derives occupancy from the live transcript. | 02832adb (keeper-side heartbeat so `.ctx` never goes stale), 3125b68c (gauge-independent live-pane recovery), 74154ef1 (gate on abs tokens not pct). Redesign G4/G6 still OPEN (hk-gffc, hk-gfpd F47). | **YES — multiple, still open.** | **architectural** |
| **C3** | **operator-attached false-suppress.** ~3,956 captain suppressions; `restart_now_blocked reason:operator_attached` seen live 2026-06-20. Captain permanently parked warn-only under iOS/remote-control workflow. | `OperatorAttached` = raw `tmux list-clients` attached-count; cannot tell an idle/remote/monitor client from an actively-typing human. | 51f8a391 (introduced the guard), 178fa2e7 (gate on keystroke recency not bare attach). Redesign G5 (activity-recency) still OPEN. | **YES — fix introduced the very problem; follow-up partial; still open.** | **architectural** (the design *chose* a signal that can't carry the meaning) |
| **C4** | **ACT-when-idle path LOOPS.** Live in events 2026-06-18/20: `cycle_id …-000001 → -000006`, each `handoff_started → clear_unconfirmed → cycle_complete/aborted(handoff_timeout)`, re-firing with a new cycle number; handoff file truncated to 0 lines between cycles. | ACT injects `/session-handoff`, times out before `/clear` confirms, re-fires with a fresh nonce; freshness gate + truncation interact. | 89852bb3 (unmerged) rips out the marker→poll→nonce/journal/freshness state machine and acts directly+synchronously. ddd24a73 earlier added an "anti-loop escape hatch." | **YES — anti-loop hatch added, loop still observed live months later.** | **architectural** (state machine with no convergent terminal) |
| **C5** | **restart-now silent no-op / false-success.** Writes "marker written", exit 0, cycle never advances; also exits 0 when NO keeper is running (no liveness check). | (a) `--project` defaults to `os.Getwd()`; captain's CWD differs → marker lands in wrong dir. (b) marker only consumed on a fresh/non-stale/non-foreign gauge tick (couples to C1/C2). (c) no ACK/liveness handshake. | f5bcae4f (decouple from CrispIdle, F46), 3d0fe353 (forced-clear bypass CrispIdle), 2ba7845e (boot-grace v2 + cross-SID retry), 89852bb3 (unmerged: direct synchronous path + ACK handshake). Bugs A/B still un-beaded. | **YES — several CrispIdle/grace fixes; still unreliable; the canonical operator pain.** | **architectural** (indirection through marker+poll on a possibly-dead gauge) + spec-gap (no liveness contract) |
| **C6** | **restart_now_blocked: handoff_stale.** Seen live ~6× on 2026-06-18 — captain writes handoff THEN fires, strict `mtime >= requestedAt` rejects it. | Strict-mtime freshness gate vs. write-then-fire ordering. | C (89852bb3) replaces with a 10-min freshness window. | First observation; fixed in unmerged branch. | **timing** |
| **C7** | **pct flags inert on 1M window / band display confusion.** `--warn-pct/--act-pct` feed only a legacy fallback; abs caps 200k/215k always win on a 1M window. Operator repeatedly sees "27% warn" and reads it as broken. | Tokens-vs-Pct split-brain: two threshold representations, only one live. | 873a8358 (F45 warn-below-warn_pct), 74154ef1 (gate on abs), 8337dbea (abs gate when Window==0), 0a7351c9 (`--*-abs-tokens` flags), 1dd39e3f (de-inert pct), 9222f7d8 (fix inert band TEXT), cffcbe1d/e42a4a43 (single-source defaults), 3882a5a5 (300k reset). | **YES — ~8 fixes across the threshold representation.** | **complexity** (two parallel threshold systems) — note: the *band values* are deliberately pinned (operator HARD-NO on retune); the bug is the dual representation, not the values. |
| **C8** | **Flag-ordering / leading-dash footgun.** `restart-now captain --project X` exits 2; positional-vs-`--agent` parse divergence across subcommands. | `resolveKeeperAgent` accepted `--project` only *before* the positional agent. Each subcommand parsed args differently. | 8c89acc9/be3de639 (enable+doctor parity), fdae70fc (subcmd parity), 0cbb3a25/c27b030f/2b804287 (reject leading-dash exit 2), 84c0e3cb (add `--agent` flag). Bug A still un-beaded. | **YES — fixed subcommand-by-subcommand, recurred per-command.** | **plain-bug** (repeated because of duplicated hand-rolled arg parsing) |
| **C9** | **Smoke/soak harness fork-bombs host.** 1500+ leaked `*-flywheel` tmux sessions, load→42, ~13 sessions/sec. Live incident 2026-06-20 06:08–06:18. | `restart-now` smoke/soak harness self-replicated, no cleanup on `no_tmux_target`/wedge path. | Contained (pkill + rm binaries); hk-7myt filed. Committed test is NOT the culprit. | One-off incident (but a *latent* second-order risk of the restart-now complexity — the thing being soak-tested is the fragile path). | **plain-bug** (test harness) |
| **C10** | **Suspended (`^Z`) keeper process.** Pane `hk-…-keeper` showed `^Z` SUSPENDED 2026-06-20; keeper silently stopped watching. | Unconfirmed — under investigation; a stopped keeper can't gauge or cycle, with no self-revive (daemon supervisor does NOT auto-revive keepers, by design — see hk-34ac). | hk-34ac (sid-independent hard-ceiling backstop + blind-keeper alarm) — OPEN. | First captured; the *absence* of a backstop is the recurring theme. | **architectural** (no liveness/failsafe layer; single point of silent failure) |
| **C11** | **Crew gauge not wired on live deploy.** `keeper doctor` shows drift; crews never get a readable gauge; statusLine/Stop/PreCompact stanzas unwired (A6 retro). | Phase-2 enablement is operator-manual, multi-step, and easy to half-wire; `.managed` present but hooks absent → silently inert. | A6 enable runbook; `keeper enable`/`doctor`; hk-gfpd F47 (captain never holds a readable gauge) — OPEN. | **YES — recurs every deploy** because enablement isn't atomic. | **complexity / spec-gap** (no single atomic "make it live" operation; testability gap) |
| **C12** | **Warn-text churn.** Repeated reword/trim of injected warn text; stale doc comments; `/quit` accidentally in warn text. | Warn text hard-coded in multiple places, drifts from the real constants. | e5d3ac40, d9035d79, 158f167b, 9ac3a5cb, 07ce9063, 9222f7d8, 8d2ab9fa. | **YES — ~7 small fixes.** | **plain-bug** (cosmetic, but a recurrence symptom of duplicated state) |

---

## Recurrence tally

Counting **12 distinct failure classes**:

| Bucket | Classes | Count | Share |
|---|---|---|---|
| **Recurring AND architectural** | C1, C2, C3, C4, C5, C10 | **6** | **50%** |
| **Recurring, complexity-driven** | C7, C11, C12 | 3 | 25% |
| **Recurring, plain-bug (duplicated code)** | C8 | 1 | 8% |
| **One-off / timing** | C6 (timing), C9 (one-off harness) | 2 | 17% |

- **9 of 12 classes (75%) demonstrably recurred** despite one or more landed fixes.
- **6 of 12 (50%) are architectural** — each absorbed multiple fixes and several remain
  OPEN in the redesign epic (hk-gffc) after months of patching.
- Only **~2 of 12 (≈17%) are genuinely one-off / surface** (the fork-bomb harness and the
  freshness-mtime timing bug, the latter already fixed cleanly in one shot).

The single most damning datum is the team's own **~69% fix-of-fix rate** on the
identity/liveness machine (class C1), and the fact that the loop only *stopped* when commit
93f7000e **deleted** the heuristic layer rather than patching it again — the redesign's whole
thesis (replace inference with authoritative launch-passed identity, net-LOC-down) is an
admission that the recurrence was structural, not incidental.

---

## Pattern across the architectural classes

Every architectural class (C1–C5, C10) shares one shape: the keeper **infers a fact it
should be told, then acts indirectly through a polled side-channel that can independently
die.**

- C1: infers identity from gauge/transcript instead of being handed it.
- C2: infers occupancy from a hook-written gauge that goes stale on a live pane.
- C3: infers "operator busy" from a client-attach count that can't carry that meaning.
- C5: acts via a marker file consumed only on a healthy gauge tick (couples to C1+C2).
- C4: a multi-step gated cycle with no convergent terminal → loops on partial failure.
- C10: no liveness backstop, so any of the above fails *silently* with no alarm.

The redesign goals map 1:1 onto these (G1 authoritative identity, G4/G6 live gauge +
gauge-independent recovery, G5 activity-recency, hk-34ac blind-keeper backstop). That the
fixes converge on **deleting inference + adding a liveness handshake** confirms the failures
are architectural, not a string of unrelated bugs.

---

## Verdict

**Recurrence tally: 9/12 classes (75%) recurred despite fixes; 6/12 (50%) are architectural;
team's own measured fix-of-fix rate is ~69%.** The keeper's repeated failures are
**predominantly architectural — a heuristic-inference identity/liveness layer driving an
indirect, silently-failable side-channel** — not a run of incidental bugs; the surgical
deletion that finally broke the loop (93f7000e) proves the structure, not the code, was the
problem.
