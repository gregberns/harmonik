# 04 — Change Design: `specs/handler-contract.md`

**Work:** process-group-provenance · **Pass:** 4 (change design) · **Crew:** kilo · **Date:** 2026-07-22
**Inputs:** `02-components.md` §1.2; `03-research/{registry-b1,pgid-b2,substrate-path}/findings.md`;
`specs/handler-contract.md` @ `ae470edc`; `process-lifecycle-design.md` (this pass).

---

## 1. The gate, resolved — and what it means for this file

`02-components.md` §1.2 makes every change in this file **conditional on OQ-PL-008**, with a
three-row branch map: HC-044 changes under **B1** and **B3**, and is **null under B2** (children
must stay in the daemon's group for a daemon-group matcher to have anything to match).

**OQ-PL-008 is resolved to B4-strict** (`process-lifecycle-design.md` §0.1): the provenance
marker is the inherited environment variable, read portably, with a normative argv-strip. The
consequence for this file is unambiguous:

> **PGID carries no provenance meaning under B4-strict.** It is therefore free to serve as a
> kill handle. HC-044 changes **exactly as pass 2 predicted for the B1/B3 branch.**

This is the branch pass 2 wanted resolved before any HC-044 edit, and the reason is worth
keeping in the record: had A3 landed first, the spec would have retained a darwin MUST
("match on the PGID") whose only surviving reading — *match processes whose PGID equals their
own PID* — is true of **every group leader on the box**, i.e. a machine-wide kill. The ordering
constraint was real and it held.

**One correction to pass 2's branch map.** It labels the B1/B3 change "PGID is freed from the
provenance role." Under B4-strict the sharper statement is that **PGID never had a working
provenance role to be freed from** — it was never set to a project-constant value by any spawn
site, so nothing is being given up. HC-044's group obligation is a pure addition, not a trade.

---

## 2. HC-044 — Subprocess is a child of the daemon

### Current state (`:806`)

> "The daemon MUST spawn every handler subprocess as a direct child process (per
> [process-lifecycle.md §4.5]). … On Linux, handler subprocesses SHOULD install
> `PR_SET_PDEATHSIG(SIGTERM)` at spawn time; macOS has no equivalent and subprocess survival
> across daemon death is a platform reality addressed by §4.10.HC-044a."

Two facts about the current text, both confirmed this pass:

1. **HC-044 says nothing whatsoever about process groups.** Not "joins the daemon's group," not
   "leads its own." The concept is absent. Yet `internal/handler/session.go:408-410` attributes
   the joined-group design to it: *"The handler is spawned with `SysProcAttr{Setpgid: true,
   Pgid: <daemon_pgid>}` (`lifecycle.SpawnChildSysProcAttr` per HC-044 / PL-006a)."* **The code
   mis-cites its own authority** — only `PL-006a:372` imposes that rule.
2. **"direct child process" is false for the majority path.** `PL-021b:732` *mandates*
   `tmux new-window`, whose child's parent is the tmux server. Measured in research: ~94% of
   production runs (3,426 of 3,629 `harness_selected` events) take the substrate branch. So the
   MUST is violated by the normal case, not an edge case (D5).

### Target state

**(a) Parentage — restate per regime, do not assert one parent.**

Replace "MUST spawn every handler subprocess as a direct child process" with a regime-scoped
statement matching `process-lifecycle-design.md` §6:

- **daemon-forked (direct-exec)**: the daemon is the initial parent; post-crash re-parenting to
  init is the only legal deviation.
- **substrate-hosted (tmux)**: the **tmux server** is the initial parent, by the mandate of
  PL-021b. This is conformant, not a deviation.

This resolves the D5 contradiction on the handler-contract side. PL-INV-005's mirror clause is
fixed in lockstep (`process-lifecycle-design.md` §4) so the two files cannot drift apart again —
they are currently in direct contradiction with each other *and* with the code.

**(b) Process group — a NEW obligation, presented as an addition.**

HC-044 gains, for the **daemon-forked regime only**:

1. Each handler subprocess MUST be spawned as the **leader of its own process group**
   (`SysProcAttr{Setpgid: true, Pgid: 0}`).
2. `session.Kill` MUST be **group-directed** (`kill(-pgid, …)`), so termination reaches
   descendants that remain in the group.
3. The child's PGID is a **kill handle** and carries **no provenance meaning**. A reaper MUST
   NOT infer ownership from it (the prohibition itself lives in PL-006f).

**Presentation matters here.** Pass 2 flagged that the inherited draft rewrites HC-044 as though
correcting a false statement. It is not: HC-044 imposed no group rule, so this is an *addition
of a new obligation*. Pass 5 must present it that way — the same defect class as correction C3
(a silent re-framing) that this work has already committed to not repeating.

**In-repo precedent to cite, which the inherited design cites nowhere:** the own-group +
group-kill shape has already landed three times — `dot_cascade.go:2108-2113`, `:2269-2277`, and
`scheduletick.go:255`, all `Setpgid:true, Pgid:0`, with the cascade cases following up with
`syscall.Kill(-cmd.Process.Pid, SIGKILL)` (bead hk-me8ru). This is the strongest available
argument that the shape is right, and it also means the change is a *convergence* on existing
practice rather than a novel design.

**(c) The marker obligation, cross-referenced.**

HC-044 cross-references PL-006e: every handler subprocess, on **both** regimes, MUST carry the
provenance marker. On the substrate regime the marker reaches the child via `tmux new-window -e
KEY=VALUE` (`handler.go:437`) — already the case in code, and the one half of the substrate path
that is conformant today. HC-044 does not restate the marker's definition; it names the source.

**(d) The honest limit, stated here as well as in PL.**

The group obligation reaches descendants **that remain in the group**. A descendant that calls
`setsid` leaves it and is not reached by `kill(-pgid)`. Per research §4 this is not a
hypothetical: Claude Code `setsid`s every subprocess it spawns, and those are the processes that
actually leak. HC-044 MUST NOT be written so that a reader concludes "grandchildren are now
handled."

**But state the limit at its true width, not wider** (corrected 2026-07-22 after design review).
What HC-044's group obligation does not reach, the PL-006 orphan sweep does reach *once the
process is orphaned to init* — that sweep is marker-based and origin-agnostic, so a `setsid`
origin does not exclude it. The genuinely uncovered case is the **still-parented** `setsid`
descendant, i.e. the window while its root is alive. Cross-reference the corrected coverage
table in `process-lifecycle-design.md` §6, which now carries a covered row and an uncovered row
rather than one blanket row.

---

## 3. HC-018 — Cancellation bound now applies to a tree

### Current state (`:483`)

> "A `ctx` cancellation MUST cause in-flight Go-side operations to return with
> `context.Canceled` (wrapped as `ErrCanceled` per §4.5) within 500ms, and MUST cause subprocess
> cleanup to complete within 5s. Exceeding the subprocess cleanup bound triggers escalation to
> hard termination per §4.6."

`PL-006:343` anchors the sweep's SIGTERM→SIGKILL interval to this 5s bound. Under §2(b),
"the subprocess" becomes "the subprocess **and its process group**."

### Target state

1. The 5s cleanup bound is **per-group, not per-process**. It bounds the whole group-directed
   termination, from first SIGTERM to the group being clear.
2. **The escalation clock does not restart per descendant.** State it explicitly.
3. State the failure mode this prevents, so the clause is not "simplified" later: with a
   per-process reading, a process tree of depth *n* takes *5n* seconds to clean up while the
   spec still claims 5s — and PL-006's sweep, which anchors its own escalation interval to this
   bound, would escalate to SIGKILL while a conformant cleanup was still legally in progress.
4. Unchanged: the 500ms Go-side bound, and the §4.6 hard-termination escalation.

Small edit; real failure mode behind it.

---

## 4. HC-044a — Launch fail-fast on orphan-held workspace (OD-7)

### Current state (`:812`)

The daemon MUST verify `workspace_path` is not held by a prior-generation handler. Detection is
a per-run pidfile at `.harmonik/worktrees/<run_id>/.lock`, written atomically at spawn and
removed on clean termination, plus a `kill(pid, 0)` liveness probe; stale pidfiles "(PID not
live, **or PID recycled to a non-handler process identifiable by argv check**)" MAY be
reclaimed.

Pass 2 flagged this as a **fourth** independent provenance scheme and asked whether the registry
subsumes it. Two facts settle it:

1. **Research finding: the `.lock` was never implemented.** So "retire it" costs nothing; there
   is no migration and no behavior to preserve. The count of provenance schemes goes **down**,
   not up.
2. **New this pass: HC-044a mandates an `argv check`** as its pid-recycling discriminator. That
   is precisely what A6 / PL-006f(2) forbid — argv used as identity. HC-044a is therefore not
   merely redundant with the new mechanism; left as written it would be **non-conformant with
   the spec this work is landing**, an in-file contradiction created the moment PL-006f lands.

### Target state — retire the mechanism, **keep the obligation**

The distinction is the whole design here, and it is easy to get wrong in either direction:

- **RETIRED:** the `.lock` pidfile as a detection mechanism, its `kill(pid,0)` probe, and the
  `argv check` recycling discriminator. All three are replaced.
- **RETAINED, unchanged in force:** the fail-fast *obligation* itself — a `Launch` onto a
  workspace held by a live prior-generation handler MUST return `ErrStructural` with sub-reason
  `workspace_held_by_orphan` and emit `agent_failed` carrying the offending PID. The daemon MUST
  NOT silently reclaim the workspace. That rationale is untouched by this work and is the
  strongest in the file: two concurrent subprocesses writing one worktree is, in the spec's own
  words, "the one scenario in this spec that can silently corrupt committed artifacts."
- **REPLACED BY:** ownership determined per PL-006e/PL-006f — the inherited marker plus the
  `HARMONIK_SESSION_GEN` generation nonce. "Prior generation" becomes decidable by the nonce
  rather than inferred from a recorded pid's liveness, which also removes HC-044a's own
  pid-recycling weakness at the root instead of patching it with an argv check.
- **Fail-closed, restated locally:** if ownership cannot be determined (marker unreadable per
  PL-006e(4)), the workspace MUST be treated as **held** — `Launch` fails fast. Note this is the
  *opposite* polarity to the reaper's fail-closed rule ("unreadable ⇒ do not kill"), and
  deliberately so: both resolve toward **not destroying state**. Stating both polarities
  together, with that shared principle named, prevents a future reader from "harmonising" them
  into one rule and inverting one of them.

**Retire explicitly, with a dated note**, per this work's standing rule that retirements are
written and not slipped in.

---

## 5. Requirements traceability

| Pass-2 item | Target state | Where |
|---|---|---|
| HC-044 branch map (B1/B3 shape) | Gate resolved to B4-strict ⇒ B1/B3 shape applies; group obligation added | §1, §2(b) |
| HC-044 group-silence (pass-2 finding 6) | Presented as an **addition**, not a correction; code mis-citation noted for pass 5 | §2(b) |
| D5 parentage contradiction | Parentage restated per regime | §2(a) |
| A3 PGID as kill handle | Own-group spawn + group-directed `session.Kill`; no provenance meaning | §2(b) |
| A9 OQ-PL-011 re-framing | Kill-reach limit stated at HC-044 too; owned by PL | §2(d) |
| HC-018 tree bound | Per-group bound; clock does not restart per descendant | §3 |
| OD-7 HC-044a subsumption | **Resolved:** retire mechanism, keep obligation, replace with marker+nonce | §4 |
| A6 argv prohibition | HC-044a's `argv check` retired as a conformance consequence | §4 |
| Goal 2 grandchild reach | Partial; non-coverage cross-referenced, not implied away | §2(d) |

**Cross-area consistency.** No target state here contradicts `process-lifecycle-design.md` or
`beads-integration-design.md`. The three shared dependencies resolve identically in all three
files: OQ-PL-008 → B4-strict; PGID → kill handle only; argv → never provenance, permitted only
as a post-match narrowing filter.

---

## 6. Risks into pass 5

1. **Presentation risk (highest).** HC-044's group rule is an addition. If pass 5 drafts it as a
   correction, the spec silently acquires a new MUST — the exact defect (C3) this work committed
   to not repeating, in the exact file where it was first caught.
2. **`session.go:408-410` mis-cites HC-044 today.** Once HC-044 genuinely carries a group rule
   the citation becomes accidentally correct. Pass 5 should note that the citation was wrong
   when written, so the history does not read as though the code was conformant all along.
3. **HC-044a's polarity inversion** (§4) is genuinely counter-intuitive: unreadable ⇒ *do not
   kill* for the reaper, unreadable ⇒ *treat as held* for launch. Both must appear with the
   shared "never destroy state" rationale attached, or a future editor will unify them.
