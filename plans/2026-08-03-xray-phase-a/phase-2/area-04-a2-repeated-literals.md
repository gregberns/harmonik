# Area 4 — A2: a repeated literal with no constant, and a detector that is 36% wrong

**Class:** A2. **Phase:** A. **Detector:** `heuristic`. **Lock unit:** *not* the file — see §1.
**653 values across 4,895 sites.** Data: `data/area-04-a2-by-value.json` (one row per decision),
`data/area-04-a2-repeated-literals.json` (one row per site).

**Read §1 before scheduling anything from this area.** It is the only area in this directory that is unsafe
to fan out as generated.

## 1. Why this is blocked

An A2 finding is reported **once per value across all its sites** — deliberately, because the fix is one
decision (name the concept, put it in the package that owns it), not N conflicting tasks. But the `Finding`
schema has a single `file` field, so the record keeps only the first site's file.

**550 of 653 A2 findings (84%) name more than one file in their evidence, and one names 68.** So 226 A2
operations each declare one lock and will in fact edit up to sixty-eight files, and the scheduler will
happily run two of them concurrently on a file both are about to rewrite. (Excluding the struct-tag false
positives of §2 it is 330 of 420 — the hazard is not an artefact of the noise.)

Two ways out, and neither is optional:

- **Fix the schema** — add `files []string` to `Finding`, populate it from the site list. One-line change in
  the organism repo (area 8, item 1). This is the right fix and it makes 226 operations safe.
- **Or serialise** — run exactly one A2 operation at a time, no fan-out. Correct, and it throws away the
  whole reason to generate a plan.

Until one of those happens, treat this file as a research input, not a work order.

## 2. The detector is 36% wrong, and this was found while writing this document

Rolling the 653 values up by shape:

| shape | values | sites | verdict |
|---|---:|---:|---|
| struct tags — `json:"run_id"`, `json:"type"`, … | **233** | **1,913** | **not fixable. Go struct tags cannot reference a constant.** |
| CLI flag strings — `--project=`, `--help`, `--json` | 67 | 728 | usually deliberate; a flag name is already a name |
| everything else | **353** | **2,254** | the real worklist |

A struct tag is a string literal in the AST and the detector counts it as one. It is not hoistable: `json:`
tags must be compile-time literals in the tag position, so there is no fix to perform. **`json:"run_id"` at
151 sites across 68 files is the single largest A2 finding in harmonik and it is a false positive.**

Consequences to carry forward:

- Every published A2 count in this project — including `05_findings-to-operations`' 645, and the
  22,244-minute total in the README — is **inflated by roughly a third**.
- The fix belongs in `refactor/cmd/detect`: skip literals in `*ast.BasicLit` positions that are struct tags
  (area 8, item 4). Cheap, exact, and it removes 1,913 sites of noise from every future run.
- Until it lands, any triage document for A2 needs `deny` rules covering the tag values, and any agent
  handed A2 work should be told to reject them on sight rather than reason about them.

## 3. The real worklist

Top of the remainder — these are wire-protocol and filesystem vocabulary with no name anywhere:

```
  72 sites /  9 files  "session_id"
  65 sites / 11 files  "run_id"
  58 sites / 25 files  "tmux"
  56 sites / 27 files  "path"
  44 sites / 14 files  "bead_id"
  43 sites / 16 files  "reason"
  41 sites /  1 files  "internal_error"
  30 sites / 17 files  "type"
  29 sites / 17 files  "unix"
  28 sites / 13 files  ".json"
  23 sites / 13 files  "status"
  23 sites / 12 files  "worktree"
  22 sites / 16 files  "rev-parse"
  21 sites /  4 files  "node_id"
  20 sites /  1 files  "WG-024"
  17 sites / 14 files  "events.jsonl"
  14 sites /  4 files  "agent_type"
  14 sites /  4 files  "refs/heads/"
```

**Three distinct sub-classes hide in that list, and they want different fixes:**

- **Map keys for the event envelope** — `session_id`, `run_id`, `bead_id`, `node_id`, `agent_type`,
  `event_type`. These are the *same vocabulary* the struct tags encode, reached through
  `map[string]any` instead of through a struct. The honest fix is usually not "hoist a constant" but
  "stop using an untyped map here" — which is A3, and much larger. Hoisting the key to a constant is the
  cheap version and it is still worth it: it makes the two spellings of the same concept greppable.
- **Filesystem and git vocabulary** — `.json`, `events.jsonl`, `refs/heads/`, `rev-parse`, `worktree`,
  `tmux`. These have an owner (whichever package wraps git / tmux / the event log) and hoisting them there
  is unambiguous and useful: it puts the vocabulary at the boundary that owns it.
- **Single-file repeats** — `internal_error` (41 sites, 1 file), `WG-024` (20 sites, 1 file). These are the
  cheapest wins in the entire directory: one file, one lock, no cross-package question, no scheduling
  hazard, and §1's blocker does not apply because the file set is a singleton. **Start here.**

## 4. The trap that makes A2 worse than A1

A1 asks "does a constant already hold this value?" — a fact. A2 asks "should a constant hold this value,
and *where should it live*?" — a design decision. Getting the home package wrong creates a dependency that
did not exist, which is strictly worse than the literal.

The rule: **the constant goes in the package that owns the concept, not the package that uses it most.**
`run_id` belongs wherever `Run` is defined. If no package obviously owns it, that is a finding in itself —
report it and do not invent a `constants` package. A grab-bag of strings is a grab-bag package
(catalog D2), and harmonik already has one at cohesion 0.25.

## 5. Gate

Unlike A1, **A2 is not a no-op after constant folding** if the hoist moves the value to a different
package — the compiled output can legitimately differ. So the byte-identical trick does not apply, and the
gate is a real test run over the blast radius:

```sh
go test -count=1 ./<declaring pkg>/... ./<every pkg named in the finding's site list>/...
```

Confirm green before. Exit metric: one declaration holds the value, every site points at it, and
`grep -c '"<value>"'` returns 1.
