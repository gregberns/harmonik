# 04 — Change Design: `specs/beads-integration.md`

**Work:** process-group-provenance · **Pass:** 4 (change design) · **Crew:** kilo · **Date:** 2026-07-22
**Inputs:** `02-components.md` §1.3; `specs/beads-integration.md` @ `ae470edc`;
`internal/brcli/adapter.go`, `internal/lifecycle/orphansweepbr.go`;
`process-lifecycle-design.md` (this pass).

This file was **not** in the problem space's affected-areas list. Pass 2 added it, and correctly:
`BI-014a` is the normative source of hk-c6dt2's behavior, so a fix confined to
`process-lifecycle.md` would leave the requirement that *mandates* the hazard standing.

---

## 1. Current state

### BI-014a `:352` — orphan `br` subprocess sweep on daemon startup

> "On daemon startup, the orphan sweep of [process-lifecycle.md §4.2 PL-006] MUST enumerate
> processes **whose binary path matches the pinned `br` location** and whose parent PID is 1
> (re-parented to init). For each match, the adapter MUST send SIGTERM and wait up to 5s, then
> SIGKILL, mirroring the BI-025c termination discipline. Orphan `br` subprocesses surviving the
> sweep are a Cat 0 prerequisite failure (SQLite WAL contention) per [reconciliation/spec.md
> §8.1]."
>
> "Cross-spec coordination request to PL: extend PL-006 orphan-sweep enumeration to include `br`
> subprocesses… Tracked as new OQ-BI-010."

Three defects, in increasing severity:

1. **Direct spec-vs-spec contradiction, both in force.** `PL-007:475` says the sweep "MUST NOT
   match on binary path alone and MUST NOT kill a process lacking a valid project-scoped
   marker." BI-014a *mandates* binary-path matching. Two live MUSTs in opposition.
2. **The implementation is weaker than the weaker of the two.** `orphansweepbr.go:64-79` matches
   `comm == "br" && PPID == 1` — a process **basename**, not even the pinned path. It reaps
   other projects' and other developers' `br` processes. This is hk-c6dt2, and it is a live
   production hazard: `br` is the operator's own day-to-day CLI.
3. **No project scope exists to apply.** `PL-006:343` claims `br` children "bear the same
   PL-006a provenance marker (env var + PGID)." Verified false (D1): `adapter.go:146` is a bare
   `exec.CommandContext`; the only field set afterward is `cmd.Dir` (`:152`). A grep for
   `cmd.Env` and `SysProcAttr` across `internal/brcli` returns **zero** non-test hits. The child
   inherits the daemon's environment — and the daemon never sets the marker on itself (zero
   `os.Setenv` hits in `internal/daemon` and `cmd/harmonik`).

**Sharpened by this pass's Finding 1** (`process-lifecycle-design.md` §0.2, as corrected
2026-07-22): the marker is written by the daemon **handler** path onto handler subprocesses
(`workloop.go:1102-1104`) and by nothing else — in particular the daemon does **not** set it on
itself. A `br` child inherits the daemon's environment, so there is no marker for it to pick up
even in principle. The `br` sweep has no project-scoped signal available to it today — which is
precisely why it fell back to a basename. (The earlier phrasing of Finding 1, "carried by zero
live processes anywhere on the box," was a steady-state snapshot overstated into a code fact and
has been corrected upstream; the conclusion for `br` is unchanged and rests on the narrower
claim, which is the one that was always doing the work here.)

---

## 2. Target state

### 2.1 BI-014a enumeration — match the marker, never the path

Replace the enumeration clause. The sweep MUST identify candidate orphan `br` subprocesses by
the **PL-006e provenance marker**, evaluated under **PL-006f matcher discipline** (which carries
the darwin argv-strip rule and the universal fail-closed rule). Specifically:

1. **Marker match is the only admissible identification.** Binary path, pinned path, and `comm`
   are demoted to **narrowing filters applied after** a marker match — never the match itself.
   This is A6 with teeth, and applying it here is what makes the current implementation
   non-conformant, which is the point of the change.
2. **`PPID == 1` is retained** as a narrowing filter for this sweep — `br` is daemon-forked, so
   unlike the handler sweep (D6) the filter is not structurally blind here. It is a filter, not
   an identifier.
3. **Fail closed, stated locally.** An unmarked `br` process is **not ours** and MUST NOT be
   touched — even at the cost of leaving a genuine orphan behind. The trade is stated in-text
   because it is a real trade, and it is not close: SQLite WAL contention is recoverable and
   already classified (Cat 0 prerequisite failure, with an operator-visible path); killing a
   developer's or a peer project's `br` is silent, immediate, and unrecoverable.
4. **The SIGTERM → 5s → SIGKILL discipline is unchanged**, as is the Cat 0 classification of
   survivors. Only *who is a candidate* changes.

### 2.2 New: the BI-side spawn obligation (A5(a))

`internal/brcli` is BI's subsystem, not PL's, so the **write** obligation belongs in this file
rather than being asserted about BI from `process-lifecycle.md`:

> The `br` adapter MUST set the PL-006e provenance marker on every `br` subprocess it spawns.

Two points pass 5 must not soften:

- **This is the clause that makes §2.1 non-vacuous.** A marker-only matcher against processes
  that carry no marker reaps *nothing* — which is safe, but leaves hk-c6dt2's WAL-contention
  half unfixed forever. The obligation and the matcher MUST land together. Splitting them yields
  either a sweep that cannot work (matcher first) or a widened blast radius with no guard
  (marker first). `process-lifecycle-design.md` §8 risk 2 states the general form of this rule;
  this is its sharpest instance, because BI is the one subsystem where both halves sit in the
  same small file.
- **Explicit inheritance note.** `br` children need the marker set on the `exec.Cmd`, not merely
  inherited, because the daemon does not carry the marker in its own environment (§1.3). Stating
  this prevents the natural but wrong implementation — "the daemon has it, children inherit it" —
  which is exactly the assumption `PL-006:343` already encodes and that measurement refutes.

### 2.3 Retire OQ-BI-010, and correct the cross-spec request

The "cross-spec coordination request to PL" at `:352` asks PL-006 to extend its enumeration to
include `br` subprocesses. Under the design this is **already satisfied and mis-stated**: PL-006e
gives every harmonik-spawned process, `br` included, one marker; PL-006f gives every reaper one
matching discipline. There is nothing left for PL to "extend."

- **Retire OQ-BI-010** with a dated resolution note pointing at PL-006e/PL-006f.
- Replace the coordination-request paragraph with a plain cross-reference.
- **`br` enters the PL-006g spawn-site register** as a **must-become-conformant** row
  (`process-lifecycle-design.md` §3), so the obligation in §2.2 is checkable from the register
  rather than discoverable only by reading this file.

---

## 3. Rationale

**Why this file has to change at all.** Every other change in this work could land and hk-c6dt2
would remain specified-as-hazardous: BI-014a is the requirement the implementation obeys. Fixing
`PL-007` alone would leave two contradictory MUSTs and let the weaker one keep winning, which is
the status quo that produced the defect.

**Why the marker, and not a `br`-specific scheme.** Pass 2's §2 warns the work is at risk of
adding a fifth provenance scheme alongside the env marker, the tmux name prefix, PL-006d's
sentinels, and HC-044a's `.lock`. This design ends with **fewer**: HC-044a's `.lock` is retired
(`handler-contract-design.md` §4), and `br` joins the single PL-006e marker instead of getting
its own. A `br`-specific scheme would be the accretion this work exists to stop.

**Why fail-closed here specifically.** `br` is not an internal implementation detail — it is the
operator's own CLI, run by hand, constantly, in this repo and others. The asymmetry between the
two error directions is larger for `br` than for any other process class in the work, which is
why the trade is written into the requirement rather than left to the general PL-006f rule.

**Research grounding.** No pass-3 area was assigned to BI directly; the applicable findings are
transitive and are cited where used: registry-b1 on scheme proliferation and GC, substrate-path
Finding 1 (empty marker population) and the argv-strip forgery close, both re-measured this pass.

---

## 4. Requirements traceability

| Pass-2 item | Target state | Where |
|---|---|---|
| BI-014a contradicts PL-007 `:475` | Enumeration rewritten to marker-match; path/`comm` demoted to post-match filters | §2.1 |
| D1 — `br` children carry no marker | BI-side spawn obligation added; PL-006 `:343`'s false sentence deleted on the PL side | §2.2 |
| A5(a) spawn obligation | Landed here, in BI's own subsystem | §2.2 |
| A5(b) sweep matches the marker | §2.1(1) | §2.1 |
| A5(c) fail-closed for unmarked `br` | §2.1(3), with the trade stated | §2.1 |
| A6 argv/path never provenance | §2.1(1) — this file is where the prohibition first bites | §2.1 |
| D2 — spawn-site register | `br` enters PL-006g as must-become-conformant | §2.3 |
| OQ-BI-010 | Retired with a dated resolution note | §2.3 |
| Goal 4 — reapers match the marker | Satisfied for the `br` reaper | §2.1 |

**Cross-area consistency.** Consistent with `process-lifecycle-design.md` (PL-006e/f/g, fail-closed,
argv prohibition) and `handler-contract-design.md` (scheme count reduced, not increased). No
target state in this file depends on a decision left open in either.

---

## 5. Risks into pass 5

1. **Ordering within the change is load-bearing.** §2.2 (write) must ship with or before §2.1
   (match). Marker-first alone widens the candidate population before the matcher tightens;
   matcher-first alone silently disables the `br` sweep. Same-file scope makes this easy to get
   right and easy to overlook.
2. **A silently-disabled sweep looks like success.** After §2.1 and before §2.2 takes effect
   across all `br` spawn paths, the sweep reaps nothing and emits nothing. Pass 5 should require
   an observable signal — candidates-considered vs candidates-matched — so "zero kills" is
   distinguishable from "not running." This is the same trap that let the handler sweep record
   `subprocesses_killed = 0` across all 366 sweeps without anyone noticing it had never worked.
3. **Marker on `br` widens what a *future* buggy matcher could reach.** Accepted, and mitigated
   by PL-006f's conjuncts landing in the same change — but worth naming, since `br` runs as the
   operator constantly and is the most consequential process class to get wrong.
