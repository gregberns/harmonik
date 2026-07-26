# Pass-4 component naming — a deliberate divergence from `kerf square`

**Crew:** kilo · **Date:** 2026-07-22

`kerf square` reports three "missing" pass-4 files:

```
Missing: 04-design/pgid-b2-design.md
Missing: 04-design/registry-b1-design.md
Missing: 04-design/substrate-path-design.md
```

**These files are not missing; they should not exist.** `kerf` expands the jig's `{component}`
placeholder from the subdirectory names under `03-research/`, and pass 3's components were named
after **research questions**. Pass 4's instruction names a different unit:

> "**One design file per affected spec area is the default.** … Save per area to
> `04-design/{component}-design.md`."

Pass 4 and pass 5 are per **spec area**; pass 3 was per **research question**. There is no
"registry-b1 spec area" to design, and pass 5's output (`05-spec-drafts/{component}.md`) must be
per spec file or it cannot produce spec text at all. The files written follow the normative pass
instruction; the square check is comparing against a placeholder inherited from the previous
pass's shape.

## Mapping — research question → design file

| `03-research/` (question) | Feeds | `04-design/` (spec area) |
|---|---|---|
| `registry-b1/` — is a durable per-pid registry viable? | OD-1 (B1 rejected), OD-7, scheme-count | `process-lifecycle-design.md`, `handler-contract-design.md` |
| `pgid-b2/` — does PGID + start-time close recycling? | OD-1 (B2 rejected), A8/A7 withdrawal | `process-lifecycle-design.md` |
| `substrate-path/` — is the darwin marker the main event? | OD-1 (B4 adopted), OD-2, OD-5, the non-coverage row | all three |

## Files that DO constitute pass 4

| File | Spec area |
|---|---|
| `process-lifecycle-design.md` | `specs/process-lifecycle.md` (incl. new §4.2a PL-006e/f/g) |
| `handler-contract-design.md` | `specs/handler-contract.md` |
| `beads-integration-design.md` | `specs/beads-integration.md` |

Pass 5 should carry the **spec-area** naming forward (`05-spec-drafts/process-lifecycle.md`
etc.), and `kerf square`'s pass-4/5 file expectations will continue to read from the pass-3
directory names until the placeholder is re-derived. Worth a kerf-side follow-up: `{component}`
means two different things in passes 3 and 4/5 of the same jig, and `square` silently assumes
the earlier one.

---

## Confirmed at pass 5 (2026-07-22, kilo)

Pass 5 carried the spec-area naming forward as this note recommended: `05-spec-drafts/` contains
`process-lifecycle.md`, `handler-contract.md`, and `beads-integration.md`, each a complete updated
file named to match its target in `specs/`. `kerf square` now reports three further phantom files
(`05-spec-drafts/pgid-b2.md`, `registry-b1.md`, `substrate-path.md`) for the same reason.

**The jig's own text settles it.** From `kerf jig show spec`:

> "In this jig, the `{component}` placeholder expands to affected spec areas (typically spec
> filenames without the `.md` extension). In the Spec Draft pass, `{component}` expands to target
> spec filenames — each draft in `05-spec-drafts/` maps 1:1 to a file in the system `specs/`
> directory."

So the drafts are named exactly as the jig requires, and `square` is expanding the placeholder from
the pass-3 subdirectory names instead of from the affected spec areas. This is a defect in the check,
not a divergence in the work, and it will mis-report any spec work whose research components are not
named after spec files.
