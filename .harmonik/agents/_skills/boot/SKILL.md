Run `harmonik agent brief` — that IS your complete boot context; no other skill needed to orient.

## Boot action

```bash
harmonik agent brief
```

`$HARMONIK_AGENT` is set by the launcher in every agent process — no `--agent` flag needed.
To boot for a specific agent: `harmonik agent brief --agent <name>`.

## What brief emits (SPEC §4 order)

1. **Identity (soul)** — who you are; re-pinned from `soul.md` on every boot (never from stale handoff).
2. **Wake reason** — `fresh` | `keeper-restart` | `trigger:<id>`.
3. **Operating instructions** — your loop and skills (short-desc + pointer; pull full bodies on demand).
4. **Active triggers** — what fires you and how.
5. **Handoff** — last session's state (episodic only; no identity re-statement).

On keeper `/clear`: re-run this skill so identity re-pins from `soul.md` (provenance rule I1).
