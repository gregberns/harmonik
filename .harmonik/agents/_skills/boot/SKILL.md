Run `harmonik agent brief` — it gives you your identity, your operating contract and your last state. Start there, then pull whatever it points at.

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

The brief is a starting point, not a ceiling. Its Skills and Docs sections list paths
rather than full text — read the ones your work needs. If you need a fleet-level contract
the brief does not carry, the project's `CLAUDE.md` load map says which one your role reads.

On keeper `/clear`: re-run this skill so identity re-pins from `soul.md` (provenance rule I1).
