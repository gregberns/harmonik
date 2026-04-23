# Corpus Index

> Aggregated index of the extracted planning-dialog corpus. Per-project catalogs + the 10 dialog-only extracts produced in sub-phase 1C.

Last updated: 2026-04-23

## Per-project catalogs (from sub-phase 1A)

- [harmonik](harmonik/_catalog.md) — 6 sessions cataloged (exhaustive)
- [kerf](kerf/_catalog.md) — 52 sessions cataloged
- [machine-setup](machine-setup/_catalog.md) — 4 sessions cataloged (exhaustive)
- [secure-dev](secure-dev/_catalog.md) — 133 sessions cataloged (sampled, 8 ntm-worktree dirs excluded)

Note: sub-agent emblematic flags in these catalogs were re-evaluated against the `n_human_text_turns` signal; several initial flags were false positives (controller sessions, single-directive dispatches). See `../references/session-type-discriminator.md` for the refined classifier.

## Dialog extracts (sub-phase 1C)

Produced by `../scripts/extract_dialog.py` applying the human-text-turn filter.

### Primary planning-dialog corpus

| Session | ht | Project | Size | Notes |
|---|---|---|---|---|
| [79a42399](secure-dev/79a42399-0ca2-4a57-bf19-f80307706dba.md) | 38 | secure-dev | 100K | "project coming along... no idea how it works" — richest back-and-forth |
| [38415843](kerf/38415843-98c8-4265-8872-bea0eb6b0ed6.md) | 31 | kerf | 45K | spec-only-project framing — origin of spec-first workflow |
| [c6d1bd16](secure-dev/c6d1bd16-4262-4ad5-b848-d4baee9605fb.md) | 25 | secure-dev | 96K | exploratory testing design (from 12MB raw) |
| [3bf5774c](harmonik/3bf5774c-b8c7-495a-87d5-57a51223da80.md) | 21 | harmonik | 244K | catch-up then design dialog |
| [f588ff0c](harmonik/f588ff0c-699f-460c-a9d8-d0909cb8937d.md) | 20 | harmonik | 86K | figure-out-next-direction session |
| [2a50e0fc](machine-setup/2a50e0fc-f23d-48c8-92fd-ef3b28087420.md) | 19 | machine-setup | 46K | beads + orchestrate — borderline (orchestrator pattern with real dialog) |
| [d1704aa0](secure-dev/d1704aa0-6003-4c17-99ed-d48f69937e5b.md) | 17 | secure-dev | 31K | partially-implemented + study-specs — borderline |

### Borderline / variant-study corpus

| Session | ht | Project | Size | Notes |
|---|---|---|---|---|
| [13493c8d](harmonik/13493c8d-9ec9-43dd-ad72-f7badc36c8fa.md) | 5 | harmonik | 20K | Context-dump variant — harmonik founding vision (5294 / 1903 / 3441 char messages) |
| [729dad16](kerf/729dad16-8b08-4e64-a0a1-c412c23b7fec.md) | 14 | kerf | 26K | Session-recovery handoff variant |
| [00eb9fc9](harmonik/00eb9fc9-1dc2-4ccb-9bf8-a4502955f334.md) | 4 | harmonik | 28K | Recent harmonik, short but substantive |

### Compression ratios (raw JSONL → dialog-only markdown)

Average: 4.5% (range 0.7%–14%). Most of raw JSONL is tool_result content; dialog is the small fraction. Confirms that filter correctly discards non-dialog payload.

## References produced in this phase

- [`../references/session-type-discriminator.md`](../references/session-type-discriminator.md) — The classifier filter and `n_human_text_turns` signal.
- [`../references/tried-protocols.md`](../references/tried-protocols.md) — Taxonomy of 5 interaction variants discovered in the user's actual practice.
- [`../references/perplexity-initial-research.md`](../references/perplexity-initial-research.md) — Starting-point brainstorm (pre-research).
