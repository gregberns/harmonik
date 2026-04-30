# HC Load Findings — 2026-04-29

`load-version: 0.1` — captures the load-time findings from the HC pilot v0.1.0 load run via
`scripts/load-pilot.py` against `docs/decompose-to-tasks/hc-pilot-data.yaml` on 2026-04-29.
Companion to `hc-pilot.md` (the pilot draft). Analog to `bi-smoke-load-findings.md` (BI's
post-load report) and the EM v0.2.1 / EV v0.1.2 errata patterns.

## Load summary

- **Beads:** 81 created total (1 epic + 80 children). 53 had been loaded in the prior killed
  mid-run (2026-04-28); 27 newly loaded by this session's resume.
- **Edges accepted:** 223 (174 intra-HC + 11 cross-AR + 12 cross-EM + 26 cross-EV).
- **Edges forward-deferred (logged only, NOT loaded):** 7 (`forward:pl-001`, `forward:pl-003b`,
  `forward:pl-005`, `forward:pl-006`, `forward:pl-009b`, `forward:cp-budget-retry-policy`,
  `forward:rc-snapshot-token`, `forward:rc-startup-sweep`, `forward:wm-session-log`,
  plus a few ON cites — exact list per the `cross_specs:` resolution log).
- **Edges rejected by Beads cycle detector:** 6.
- **DB state after load:** `br dep cycles` clean. 81 hc beads = 1 epic + 80 children, perfect
  sync between `/tmp/hc-mnem-map.csv` and DB after orphan cleanup (see F-load-HC-1).

## Findings

### F-load-HC-1 — orphan duplicate from prior killed mid-load

- **What:** `hk-8i31.54` was found in DB but absent from `/tmp/hc-mnem-map.csv`. Inspection
  showed it was a duplicate of `hc-046` (the §2.3 coalesce bead for HC-046+HC-047). The
  prior mid-run on 2026-04-28 created the bead in DB but the process was killed before the
  map-write happened. Today's resume run created a fresh bead (`hk-8i31.55`) for `hc-046`,
  and that fresh bead carries all the real edge linkage. The orphan `hk-8i31.54` had only
  the parent-child edge.
- **Resolution:** `br delete hk-8i31.54 --force --reason "orphan duplicate ..."`. Tombstone
  created; audit trail preserved.
- **Implication for tooling:** `scripts/load-pilot.py` should write the map-row BEFORE
  calling `br create` would be ideal but creates the inverse problem (map row without DB
  bead). The tradeoff is not solvable without a transaction; the right discipline is
  "if killed mid-load, run with `--dry-run` first to surface stray IDs and reconcile by hand."
  Document in the loader's docstring as a known limitation.

### F-load-HC-2 to F-load-HC-7 — 6 cycle-closer edge rejections

Beads's cycle detector rejected the following 6 edges (out of 229 attempted, of which 7
were forward-deferred and not attempted, leaving 229 attempted - 7 = 222... actually the
math: 236 total edges - 7 forward-deferred = 229 attempted, 223 accepted, 6 rejected).
These are pilot-data findings (analog to BI smoke-load F1-F11, EM erratum 8 rejections,
EV erratum 15 rejections) — the loader correctly surfaced them.

| # | Citing | Prereq | Rejection reason |
|---|---|---|---|
| F-load-HC-2 | `hc-026a` | `hc-008a` | Cycle: `hk-8i31.32 → hk-8i31.9` |
| F-load-HC-3 | `hc-044` | `hc-007` | Cycle: `hk-8i31.51 → hk-8i31.7` |
| F-load-HC-4 | `hc-error.taxonomy` | `hc-008a` | Cycle: `hk-8i31.76 → hk-8i31.9` |
| F-load-HC-5 | `hc-error.taxonomy` | `hc-024a` | Cycle: `hk-8i31.76 → hk-8i31.29` |
| F-load-HC-6 | `hc-error.taxonomy` | `hc-026` | Cycle: `hk-8i31.76 → hk-8i31.31` |
| F-load-HC-7 | `hc-error.taxonomy` | `hc-048a` | Cycle: `hk-8i31.76 → hk-8i31.57` |

**Pattern.** Five of six involve `hc-error.taxonomy` (the §8 sentinel-set umbrella bead) as
the citing side, and the rejected prerequisites are §4 reqs whose bodies cite specific
sentinels (e.g., HC-008a cites `ErrStructural`; HC-024a cites `ErrTransient`/`ErrStructural`;
HC-026 cites `ErrStructural`; HC-048a cites `ErrTransient`/`ErrSkillProvisioningFailed`).
Per §2.11(c) the taxonomy bead OWNS the sentinel set; the §4 reqs USE the sentinels. So
the edge direction in the YAML/pilot is wrong: the taxonomy bead should NOT block on the
reqs that use its sentinels — the reqs should block on the taxonomy. The pilot table
emitted `hc-error.taxonomy → hc-008a` (taxonomy blocks on use-site) which inverts the
ownership.

This matches the BI F-pilot-bug "wrong-direction impl→sensor edge" pattern: the row tables
listed predecessors of the citing bead, and where the cite is term-use of the taxonomy
sentinel set, the prerequisite should be `hc-error.taxonomy` from the use-site, not the
inverse.

**Resolution candidates** (pilot-lane patch):
- F-load-HC-4..F-load-HC-7: invert the 4 edges. The §4 reqs (hc-008a, hc-024a, hc-026,
  hc-048a) should block on `hc-error.taxonomy`, not the other way. The current pilot
  already lists `hc-error.taxonomy` as a predecessor of these reqs (verify against the row
  notes). Likely the cycle was emitted because the §8 row in the pilot's §5 table also
  lists these reqs as predecessors of the taxonomy — that's the direction inversion.
- F-load-HC-2 (`hc-026a → hc-008a`): heartbeat → post-outcome shutdown window. Body of
  HC-026a says "shutdown window suppresses heartbeat" — that's term-use of HC-008a. So
  hc-026a SHOULD block on hc-008a. But the inverse edge already exists (from hc-008a →
  hc-026a, per the pilot's hc-008a row predecessors list `hc-026`, `hc-008`, `hc-inv-006`,
  …). Need to re-walk: is the inverse edge actually emitted? If yes, this is bidirectional
  and one direction must be reclassified to supporting cite (per F-pilot-AR-10).
- F-load-HC-3 (`hc-044 → hc-007`): subprocess-as-child rule cites HC-007 socket bind.
  HC-007's body lists `hc-044` as a predecessor (the socket path is owned by §4.10.HC-044).
  This is a true bidirectional cite — must resolve by reclassifying one side per discipline
  §2.7 / F-pilot-AR-10.

These 6 rejections are HC pilot-lane bugs (direction-inversion / bidirectional-cite
candidates) and are deferred to the HC r1 review pass per `pilot-review-protocol.md`.
After r1 finds and patches, re-run `python3 scripts/load-pilot.py
docs/decompose-to-tasks/hc-pilot-data.yaml` (resume mode is automatic — beads already
loaded will be skipped; only the missing 6 edges will retry, plus any new edges r1 adds).

## What the load tooling validated

- `scripts/load-pilot.py` worked end-to-end:
  - resumed cleanly from the prior killed mid-run via `/tmp/hc-mnem-map.csv` ledger
  - created the 27 missing beads with correct labels (verified per-bead via `br show`
    sample)
  - wired all loadable edges including coalesce-edges (`hc-007`, `hc-046`)
  - logged forward-deferred edges (PL/CP/WM/RC/ON cites) without attempting to load them
  - surfaced 6 real cycle-closer edge bugs as actionable findings
  - left the DB cycle-clean
- `docs/decompose-to-tasks/hc-pilot-data.yaml` is now the source-of-truth for HC's
  loadable bead set; `/tmp/load-hc.py` (the prior 933-line hand-coded Python load script)
  is superseded.

## Action items

- [ ] Run HC r1 review (per `pilot-review-protocol.md` §6) covering the 6 cycle-rejection
      findings + the §5.3 EV-edge count tension flagged in HANDOFF.md.
- [ ] Apply r1 patches to `hc-pilot-data.yaml` (and `hc-pilot.md` for narrative consistency).
- [ ] Re-run `python3 scripts/load-pilot.py docs/decompose-to-tasks/hc-pilot-data.yaml`
      (resume mode); verify rejections drop to 0 and any new r1 edges land.
- [ ] Confirm `br dep cycles` clean across the HC + AR + EM + EV + BI union.
