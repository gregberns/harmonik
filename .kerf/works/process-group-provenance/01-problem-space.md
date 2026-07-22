# 01 — Problem Space

**Work:** process-group-provenance
**Type:** spec (amends `specs/process-lifecycle.md` §4.2 and `specs/handler-contract.md` §4.10)
**Beads:** hk-n93gq (P1), hk-o7x4w (P1), hk-g1qby (P1), hk-c6dt2 (P1), hk-5z4ww (P2)
**Supersedes:** kerf work `child-pgid` (shelved). Its artifacts are inherited verbatim under
`00-inherited-child-pgid/` and are an INPUT to this work, not a completed pass. Four of its
factual claims are corrected below.

---

## What is changing and why

The system has never answered one question, and five beads are filed against five different
symptoms of that single missing answer:

> **Given an arbitrary process on this box, how does the daemon prove that process is its own?**

Today there is no durable, project-scoped, forgery-resistant provenance marker. Every cleanup
path that needs one either (a) reads a marker that does not exist on the platform we run on,
(b) guesses from the command line and would kill live infrastructure, or (c) is written,
tested, and deliberately left unwired because nobody can make it safe.

The five symptoms:

| Bead | Symptom | Underlying gap |
|---|---|---|
| hk-n93gq | Run children join the **daemon's** process group (`handler.go:309`), so no group-directed kill can reach a grandchild | the PGID is being used as a provenance value, which prevents it being a kill handle |
| hk-o7x4w | Handler orphan sweep reads `/proc/<pid>/environ` (`provenance.go:187-195`), which is **always** `ErrNotExist` on darwin → the sweep is an unconditional no-op on our platform | no darwin-readable marker exists |
| hk-g1qby | Daemon never calls `syscall.Setsid()`; `SetsidDaemon()` has **zero** production callers vs `specs/process-lifecycle.md:372` "MUST" | group leadership is an accident of launch path, so the group is not a trustworthy namespace |
| hk-c6dt2 | `SweepOrphanBr` matches `comm=="br" && PPID==1` with **no project scoping** → reaps other projects' and developers' br processes | br children carry no marker at all |
| hk-5z4ww | `ReapPriorAgentFollowWatchers` is complete, tested, and has zero callers — it identifies by command line, which cannot distinguish a leaked watcher from a live crew's **mandatory** one | no per-session provenance signal |

These are one design. Fixing any one in isolation has already been demonstrated to produce a
destructive or vacuous result — see "Prior refusals" below.

---

## What is actually implemented today (verified against source, not spec)

Provenance signals with a real consumer at HEAD:

| Consumer | Marker it reads | Works on darwin? |
|---|---|---|
| `orphansweep.go:257,262` — `OSHandlerProcessLister.ListOrphanHandlerPIDs` | `HARMONIK_PROJECT_HASH` env via `ReadProcessEnviron` | **NO** — `provenance.go:195` always `ErrNotExist`; loop `continue`s at `:259` |
| `orphansweep.go:135` — `SweepOrphanTmuxSessions` | tmux session-name prefix `harmonik-<hash>-` (`provenance.go:106`) | **YES** — portable |
| `tmux/orphansession.go:52`, `bootreconcile.go:250` — `adoptDeadRunSessions` | same name prefix | **YES** |
| `orphansweepbr.go:64-79` — `SweepOrphanBr` | `comm=="br"` + `PPID==1`, **no project scope** | yes, and that is the defect (hk-c6dt2) |

**PGID is read by nothing at HEAD.** `PriorDaemonOrphanPGID` exists only in the uncommitted,
review-BLOCKED patch at `/Users/gb/github/harmonik-wt/kilo-preserved/hk-o7x4w-BLOCKED-do-not-merge.patch`.

Spawn sites and their markers:

- `handler.go:309` — run children: `SpawnChildSysProcAttr(RecordedPGID())` ⇒ join daemon group.
- `handler.go:273` — **returns early to `launchViaSubstrate` BEFORE `:309`.** tmux-hosted runs
  never receive that `SysProcAttr` at all. Any fix applied at `:309` covers only the direct-exec
  branch. *(Missed by `child-pgid` entirely — see correction C4.)*
- `brcli/adapter.go:146` — br children: `exec.CommandContext` with **neither** `SysProcAttr`
  **nor** `cmd.Env` (only `cmd.Dir`). No marker whatsoever.
- `scheduletick.go:255`, `dot_cascade.go:2108`, `dot_cascade.go:2269` — already spawn with bare
  `SysProcAttr{Setpgid:true}` (⇒ `Pgid:0`, own group), and `dot_cascade` already does
  `syscall.Kill(-cmd.Process.Pid, SIGKILL)` (bead hk-me8ru).
- `supervise/daemon_watchdog.go:397` — `SysProcAttr{Setsid:true}`; this is the **only** reason any
  daemon leads its own session, and it applies to supervised launches only.

Dead PL-006a helpers with zero production callers: `SetsidDaemon` (`provenance.go:79`),
`SpawnSysProcAttr` (`provenance.go:59`).

---

## Corrections to the inherited `child-pgid` work

Its **kill-path fix is correct and well-precedented** (three in-repo sites already do exactly it,
hk-me8ru) and should survive into this work. Its **justification** does not, on four counts:

- **C1 — br provenance claim is false.** `03-research/lifecycle/findings.md:74-77`, design §2 and
  spec-draft `process-lifecycle.md:25-27` assert br children "stay in the daemon group" via
  `SpawnSysProcAttr` and "carry the same `HARMONIK_PROJECT_HASH` env marker." Both halves are
  false (`brcli/adapter.go:146`; `SpawnSysProcAttr` has zero callers). Shipping that paragraph
  would write a **false normative statement** into the spec. It is now bead hk-c6dt2.
- **C2 — "darwin sweep was already inert anyway" is overstated.** The *handler-process* sweep is
  inert on darwin; the *sweep* is not — tmux name-prefix provenance is live and portable there.
  Correct statement: the direct-exec handler-process sweep is a darwin no-op; the tmux-hosted
  path has working provenance.
- **C3 — it silently drops a MUST.** Its draft restates only the pidfile-recording half of
  `PL-006a:372` and deletes the `syscall.Setsid()` MUST **without declaring it retired**. That
  sentence is the entire normative basis of hk-g1qby.
- **C4 — the tmux bypass is unmentioned.** `handler.go:273` routes substrate-hosted runs around
  the spawn site being changed, so the proposed kill fix is half a fix.

Also understated in its own favor: the `Pgid:0` + group-kill design it proposes has **already
landed three times** in this repo (hk-me8ru). That is the strongest evidence for it and is cited
nowhere. It also proves `PL-INV-005` ("every spawn site MUST set the marker (env var + PGID)",
`specs/process-lifecycle.md:1135`) is **already descriptively false in four places**.

---

## The hard constraint: PGID is not a stable identity

The PGID cannot be used as a bare provenance marker, and this is the reason hk-o7x4w's
implementation is BLOCKED rather than merged:

- Measured on this box: **~37 pids/sec, ~45-minute wrap.** After any real downtime a recorded
  PGID identifies nothing — or worse, identifies something else.
- **Live collision observed:** group 91086 had a dead leader while holding five live keeper
  watchers, including the working session's own. A sweep keyed on a recorded PGID would have
  reaped them. That is bead hk-220lv's failure mode, produced by the fix meant to cure it.
- Therefore any PGID-based match **MUST** be bounded by a start-time conjunct — via `ps lstart`
  or a pidfile-recorded start time, **NOT** file mtime. That touches the **PL-002b pidfile
  schema**, which is why this is a spec call rather than a code fix. *(Operator/captain
  direction, 2026-07-22.)*

---

## Prior refusals — the failure modes this design must not re-create

1. **Command-line matching reaps live infrastructure.** hk-o7x4w's originally-prescribed fix
   ("match `HARMONIK_PROJECT` on the command line") was refused. Live capture: `harmonik keeper
   --agent captain` (40843), `harmonik comms recv --agent mike --follow` (8610), peer-project
   daemons under `/private/tmp/hk-acdxb` (13115) are all `PPID==1` with harmonik strings on argv.
   Every crew is **required** by its operating contract to keep `comms recv --follow` armed while
   idle, so a live mandatory watcher is externally indistinguishable from a leaked one.
   `specs/process-lifecycle.md:475` forbids it: MUST NOT match on binary path alone.
2. **hk-5z4ww is the same trap** wearing "it's already written and tested."
3. **hk-c6dt2 is the same trap already in production**, matching on binary name alone.

**Design test:** any proposed marker must be one a process can *prove* about itself and that a
sweep cannot forge or collide into — evaluated explicitly against (a) pid/pgid recycling,
(b) a live sibling of the same identity, (c) a peer project on the same box, (d) darwin.

---

## Motivating real-world evidence: a live double-dispatch, caused by this exact gap

**2026-07-22, crew kilo, twice in one session.** This is not a hypothetical, and it is the
strongest case for the marker: the absence of a provenance signal caused a real coordination
failure *in the fleet's own operation*, not merely in its cleanup paths.

- **Incident 1 — artifact/liveness conflation.** A handoff reported a prior implementer had
  "written nothing." The worktree was verified empty (clean tree, zero commits), and from that
  the crew inferred *no worker was running* and re-dispatched the bead. The prior worker was in
  fact alive; six minutes later it wrote into the same worktree. Two agents then worked one bead
  concurrently, each mutating shared production files during the other's test runs.
- **Incident 2 — grep/liveness conflation.** Asked to confirm the situation was safe, the crew
  grepped the process table for the worktree path and the prior session id, got no match, and
  reported the other worker dead and the risk closed. The worker was still live and wrote again
  twenty minutes later. The assurance was unsound when given.

**Both are the same error as the bugs in this work:** an *artifact* (an empty directory, a
matching command-line string) was read as proof about a *process*. That is exactly what
`ReapPriorAgentFollowWatchers` (hk-5z4ww) and `SweepOrphanBr` (hk-c6dt2) do when they identify a
process by its command line or binary name — and exactly why hk-o7x4w's original fix shape was
refused. The failure generalizes past process reaping: with no way to ask "is a live process
holding this work, and is it mine?", *any* consumer — a sweep, a crew, an orchestrator — is
reduced to guessing from artifacts.

**What this adds as a requirement:** the marker must support a *liveness-and-ownership query*
("is there a live process that owns X, and is it ours?"), not only a post-crash reap decision.
A design that answers only "is this dead pid reapable?" leaves the double-dispatch failure
unaddressed. Weigh B1/B2/B3 against this too.

*(Recorded at the captain's direction, 2026-07-22: "a live double-dispatch caused by
artifact/grep liveness-inference is the strongest case for the provenance marker."
Standing operational rule adopted alongside it: check for a live PROCESS before any
re-dispatch — never an empty worktree, never a grep.)*

---

## Goal — what should be true afterwards

1. There is **one named provenance mechanism**, stated normatively, that works on darwin and
   Linux, survives daemon restart, and is safe against pid/pgid recycling.
2. The kill path reaches grandchildren on both platforms and on **both** spawn branches
   (direct-exec *and* substrate/tmux-hosted).
3. Every spawn site either sets the marker or is explicitly and normatively exempted — so
   PL-INV-005 becomes descriptively true.
4. Every reaper (handler sweep, br sweep, watcher reaper) identifies targets by that marker,
   never by binary path or command line.
5. Group leadership is a property of the daemon, not of its launcher.

## Non-goals

- The `waitWithSocketGrace` zero-grace-ctx bug (sibling in-lane fix).
- General keeper restart timing (hk-pvrfx, hk-4tjyj) — different subsystem area.
- Reworking `Pdeathsig` beyond keeping the darwin/linux spawn split intact.

## Constraints / must-not-change

- Preserve `PL-INV-005` intent (sweep reaps only marked processes) and the "exactly one
  `cmd.Wait()`" reap discipline in `session.go`.
- Keep the darwin/linux spawn-variant split (`Pdeathsig` is Linux-only, must not appear in the
  darwin build).
- Fail **closed**: an unresolvable marker must mean "do not reap," never "reap anyway."

## Open questions (OQ-PL-008 resolution is the gate)

- **OQ-PL-008 is not a surfaced non-gate — it is the decision that determines whether hk-o7x4w
  has any fix shape left at all.** If children lead their own groups, hk-o7x4w's PGID matcher
  becomes *vacuous by construction* on darwin. Resolving the darwin marker (marker-file? pidfile
  registry? something else) must come **before** the kill-path change, or it is silently
  foreclosed.
- What does hk-g1qby buy after this? Its *stated* rationale (PGID is the only usable darwin
  signal) does not survive. Three narrower justifications do, and one is valuable and unwritten:
  a setsid'd daemon gives br children a **project-scoped group** they presently lack — a
  candidate fix for hk-c6dt2. hk-g1qby should be re-argued on that basis or downgraded to
  spec-conformance.
- Does the marker cover *grandchildren*, or only direct children? The orphan leak is a
  grandchild problem.

## Spec areas affected

- `specs/process-lifecycle.md` — §4.2 PL-006a(ii) `:370`, PL-006a setsid MUST `:372`, PL-006a
  platform clause `:376`, PL-006 cleanup bullet `:343`, matching prohibition `:475`, PL-INV-005
  sensor `:1135`, OQ-PL-008 default `:1363`, PL-002b pidfile schema (start-time field).
- `specs/handler-contract.md` — §4.10 HC-044 (+ HC-044a, HC-018).
- Blast radius (code): `internal/handler` (both spawn branches + Kill), `internal/lifecycle`
  (spawn variants, provenance, all three sweeps), `internal/brcli` (adapter spawn),
  `internal/daemon` (bootreconcile wiring, pidfile).
