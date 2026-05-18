# Kerf Beta Feedback

This project is the **first beta-tester** of new-kerf — the `kerf next` / `triage` / `pin` / `work edit` / `init` surface. Friction observed during agent use of these commands is logged under [`docs/kerf-feedback/`](docs/kerf-feedback/).

## Convention

New issues go into a **fresh dated file** under `docs/kerf-feedback/YYYY-MM-DD.md` (one file per session or batch). Do **NOT** append to old files — completed feedback files are periodically handed off to the kerf project, and a clean cut-off makes that hand-off unambiguous.

- Start a new file at the top of any session that surfaces new friction: `docs/kerf-feedback/$(date +%Y-%m-%d).md`.
- If today's file already exists, append to it; otherwise create it.
- Old files stay in place as a permanent record of what was reported when.

Each entry should include:

- **Severity tag** (one of): `BLOCKER`, `MAJOR`, `MINOR`, `NIT`
- **Command** that surfaced the friction (e.g. `kerf next`, `kerf init`, `kerf triage --ack`)
- **Observed** vs **expected** behavior
- **Repro** if non-obvious

## Severity tags

- `BLOCKER` — kerf cannot be used for its intended purpose; workaround required
- `MAJOR` — kerf returns wrong / misleading output; user can recover with effort
- `MINOR` — friction or papercut; correct output but awkward UX
- `NIT` — cosmetic / wording / formatting

When in doubt, log it. The feedback log is the channel back to the kerf author.

## Hand-off history

- `docs/kerf-feedback/2026-05-15.md` — first batch (504 lines), handed off to kerf project 2026-05-18.

## Harmonik-side spillover

Some entries in past feedback files surfaced **harmonik-side** issues (e.g. over-broad `.gitignore`, bead label conventions). Those have been filed as beads — search `br list --label kerf-feedback-spillover` to find them.
