# Kerf Beta Feedback

This project is the **first beta-tester** of new-kerf — the `kerf next` / `triage` / `pin` / `work edit` / `init` surface. Friction observed during agent use of these commands should be logged to [`docs/kerf-beta-feedback.md`](docs/kerf-beta-feedback.md).

## Convention

Append entries to `docs/kerf-beta-feedback.md` as you encounter issues. Each entry should include:

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
