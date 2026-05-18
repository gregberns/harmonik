# Kilroy upstream-lookup — answers to harmonik's open D-rows (2026-05-18)

## Terminal nodes in Kilroy

**Single exit type. No "approved vs needs-attention" differentiation.**

Per attractor-spec §7.2 lint rule `terminal_node`: "Every graph must have at least one exit node (shape=Msquare or id matching `exit`/`end`)." The ExitHandler is a no-op returning `Outcome(status=SUCCESS)` (§4.4). All exit nodes behave identically. No `terminal_kind`, no exit-class enum, no "needs-attention" terminal.

**Implication for D12:** Kilroy does NOT solve this. Harmonik's review-loop need for distinct-purpose terminals is genuinely harmonik invention.

## Schema versioning in Kilroy

Nearly absent. Attractor-spec has no `schema_version` in the DOT grammar (§2.2–2.7). Kilroy-metaspec §4.3 declares `version: 1` on the run-config YAML only. No formal N-1 compatibility rule. Template-provenance graphs carry a `provenance_version` for linting (§7.2) but it is not a portability contract.

**Implication for D9/D10:** Kilroy provides no useful precedent. Harmonik invention.

## context_updates typing in Kilroy

Free-form dict with prefix conventions, no registration. Attractor-spec §5.1: "thread-safe key-value store shared across all stages." Convention-only prefixes (`context.*`, `graph.*`, `internal.*`, `parallel.*`, `stack.*`, `human.gate.*`). Keys match `[A-Za-z_][A-Za-z0-9_]*`.

## Edge-condition LHS in Kilroy (directly relevant to D4/D5)

attractor-spec §10.2 grammar:
```
ConditionExpr ::= Clause ( '&&' Clause )*
Clause        ::= Key Operator Literal
Key           ::= 'outcome' | 'preferred_label' | 'context.' Path
Operator      ::= '=' | '!='
```

AND-only, equals/not-equals only, no OR, no `>`/`<`, no regex. `outcome` and `preferred_label` are special LHS; everything else must prefix with `context.`. `failure_class` is NOT a legal LHS in Kilroy.

## Gates / parallel / sub-workflows

Kilroy makes all three shape-based node types with handlers (§2.8): `hexagon`=`wait.human` gate, `component`=`parallel`, `tripleoctagon`=`parallel.fan_in`, `house`=`stack.manager_loop` sub-workflow. D3's "control-point as node type" framing aligns.

## Open D-rows — upstream coverage

| Row | Question | Upstream-defined? | Citation |
|---|---|---|---|
| D5 | Edge-condition dialect | YES | attractor-spec §10.2 grammar — adopt verbatim |
| D8 | context_updates typing | Partial | Kilroy §5.1 free-form-with-prefix; harmonik stricter (registered-key list) — invention |
| D9 | Unknown-attribute policy | Partial | Kilroy permissive §4.12; harmonik stricter — invention |
| D10 | schema_version placement | NO | Kilroy has none in DOT — harmonik invention |
| D11 | Repo layout for .dot files | NO | Kilroy silent — harmonik invention |
| D12 | Terminal-node differentiation | NO | Kilroy single-exit — harmonik invention (multi-exit with distinct IDs) |
| D15 | Mechanism-tag Gate schema drift | NO | Kilroy has none — harmonik-specific |
| D17 | Tool-node contract | Partial | Kilroy `tool` handler uniform; exit-code mapping is harmonik's call |
| D19 | Normative dispatch table | YES | Kilroy §2.8 ships one — adopt |
| D6/D7 | Verdict surfacing / gate-decision payload | NO | Kilroy uses `preferred_label` + `notes` — harmonik may add gate-decision kind |
| D13/D14/D16/D18/D20 | Various | Mostly NO | Already known harmonik-specific |

## Net change to remaining design work

**Closed-by-upstream (adopt verbatim):**
- D5 — edge-condition dialect
- D19 — normative dispatch table
- D4 partial — LHS whitelist for `{outcome, preferred_label, context.*}` is upstream-canonical; harmonik narrows context to registered keys

**Genuinely harmonik invention:**
- D12 terminal-node differentiation (multi-exit semantics)
- D10/D11 schema-version placement + repo layout
- D8 registered context-key typing (stricter than Kilroy)
- D9 unknown-attribute policy (stricter than Kilroy)
- D1 `failure_class` as edge LHS (extension beyond Kilroy)
- D6/D7/D13/D14/D15/D16/D17/D18 — Kilroy silent or weaker

## Source paths

- `/Users/gb/github/harmonik/.kerf/recon/kilroy-findings.md`
- `/Users/gb/github/harmonik/.kerf/recon/attractor-findings.md`
- `/Users/gb/.kerf/projects/gregberns-harmonik/phase-3-dot/03-research/SUMMARY.md`
- Upstream: `danshapiro/kilroy` `docs/strongdm/attractor/attractor-spec.md` §§2.2–2.8, §4.4, §5.1, §7.2, §10.2, §10.6
