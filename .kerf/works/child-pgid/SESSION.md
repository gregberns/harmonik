# SESSION — child-pgid (SHELVED 2026-07-22)

## Why this work is shelved

**It was absorbed into the kerf work `process-group-provenance`.** Not abandoned, not rejected —
its scope turned out to be one half of a design whose other half it had declared out of scope.

Captain decision 2026-07-22: fold hk-o7x4w + hk-g1qby + hk-n93gq into one work, "they are one
design, not three bugs." hk-n93gq was this work's only bead, so folding it in means folding this
work in. Two additional beads (hk-5z4ww, hk-c6dt2) joined the same design afterward.

The decisive technical reason, beyond the captain's call: **this work's spec drafts and the other
beads' fix shapes write conflicting normative text into the same requirement IDs** — PL-006a(ii)
`:370`, PL-006a `:372`, PL-006a `:376`, PL-006 `:343`, PL-INV-005 `:1135`, OQ-PL-008 `:1363`.
Two works cannot both draft PL-006a. They would collide at finalize.

## Where the work went

Bench: `~/.kerf/projects/gregberns-harmonik/process-group-provenance/`

Every artifact from this work was copied verbatim to
`process-group-provenance/00-inherited-child-pgid/` and is treated there as an **input**, not a
completed pass. Nothing is lost. `process-group-provenance/01-problem-space.md` carries the
full reasoning.

Bead hk-n93gq was relabelled `codename:child-pgid` → `codename:process-group-provenance`.

## What holds up, and what does not

**Holds up — carry it forward.** The core proposal (run children spawn as own-group leaders,
`Pgid:0`; `Kill` signals `-child_pgid`) is correct and is more strongly supported than this work
realized: **the same design has already landed three times in this repo** —
`scheduletick.go:255`, `dot_cascade.go:2108`, `dot_cascade.go:2269`, with `dot_cascade` already
doing `syscall.Kill(-pid, SIGKILL)` (bead hk-me8ru). That precedent is cited nowhere here and is
the single best argument for the change.

**Does not hold up — four corrections (verified against source, not spec):**

- **C1.** `03-research/lifecycle/findings.md:74-77`, design §2, and
  `05-spec-drafts/process-lifecycle.md:25-27` claim br children "stay in the daemon group" via
  `SpawnSysProcAttr` and carry the `HARMONIK_PROJECT_HASH` env marker. **Both halves are false.**
  `internal/brcli/adapter.go:146` sets neither `SysProcAttr` nor `cmd.Env`; `SpawnSysProcAttr`
  (`provenance.go:59`) has zero production callers. Shipping that paragraph would write a false
  statement into the normative spec. Now filed as bead **hk-c6dt2**.
- **C2.** "The darwin sweep was already a structural no-op anyway" is overstated. The
  *handler-process* sweep is; the *sweep* is not — tmux name-prefix provenance
  (`orphansweep.go:135`, `tmux/orphansession.go:52`, `bootreconcile.go:250`) is live and portable
  on darwin.
- **C3.** The draft restates only the pidfile half of `PL-006a:372` and **silently deletes the
  `syscall.Setsid()` MUST** without declaring it retired. That sentence is the entire normative
  basis of bead hk-g1qby.
- **C4.** `internal/handler/handler.go:273` returns early to `launchViaSubstrate` **before** the
  `:309` spawn site. tmux-hosted runs never receive `SpawnChildSysProcAttr`, so the proposed kill
  fix covers only the direct-exec branch — half a fix. Not mentioned anywhere in this work.

## The decision this work got wrong

It rated OQ-PL-008 ("what is the darwin provenance marker?") a surfaced non-gate, on the grounds
that the PGID path "was never implemented." That framing is inaccurate: a 36 KB implementation
exists (`/Users/gb/github/harmonik-wt/kilo-preserved/hk-o7x4w-BLOCKED-do-not-merge.patch`),
blocked on **pid-recycling safety** (~37 pids/sec, ~45-min wrap; a live collision on group 91086
holding five keeper watchers), not on being unimplementable.

**OQ-PL-008 is the gate.** If children lead their own groups, hk-o7x4w's PGID matcher becomes
*vacuous by construction* on darwin — the exact bug it was filed to fix, re-created by design
instead of by platform. Resolving the darwin marker must precede the kill-path change or it is
silently foreclosed.

## If you resume this instead of `process-group-provenance`

Don't, unless `process-group-provenance` has been abandoned. If you must: apply C1–C4 first, and
resolve OQ-PL-008 before advancing past spec-draft.

## Reading order

1. `~/.kerf/projects/gregberns-harmonik/process-group-provenance/01-problem-space.md` — start here.
2. This file.
3. `01-problem-space.md` (this work) → `04-design/child-pgid-design.md` → `05-spec-drafts/`.
4. `br show hk-n93gq`, then hk-o7x4w (last three comments), hk-g1qby, hk-c6dt2, hk-5z4ww.
