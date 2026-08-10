# Area 1 — `core.Event.Type` is `string`, and the enum it was waiting for landed

**Class:** A3 (stringly-typed domain value). **Phase:** A. **Lock unit:** whole-repo, serial.
**Status:** verified by hand at `d5a12348f`, 2026-08-09. **Not detected by any tool** — A3 is a catalog
entry with no implementation, which is why this sat for four waves without appearing in a findings file.

## The situation

`internal/core/event.go:47`:

```go
	// Type identifies the event type; MUST be one of the §8 rows (event-model.md §8).
	// The EventType enum is declared in a separate bead (hk-hqwn.59); this field
	// uses string until that enum lands (non-breaking hoist).
	// Required (non-empty).
	Type string `json:"type"`
```

**The enum landed.** `internal/core/eventtype.go` declares **181 `EventType` constants**, and the registry,
the compat table and the daemon all use them. The comment describes a world that stopped existing.

The field did not follow. So every typed API in harmonik converts at its boundary:

```sh
grep -rn 'string(.*EventType' --include='*.go' .   # 263
grep -rn 'core\.EventType(' --include='*.go' .     #  95
```

**358 conversions exist because one field is the wrong type.** That is the shape worth naming: the root
cause is not idle, it is *billing itself in instalments*, and each new typed API adds an instalment.

## Why it blocks decomposition

This is the highest-value entry in the whole catalog for exactly one reason: **it is the only change here
that makes the compiler enforce a vocabulary across a package boundary it currently cannot see.**

- Today, any `string` reaches `Event.Type`. The compiler cannot tell you where the event vocabulary is
  consumed, so you cannot find the boundary the vocabulary implies — which is the entire premise of phase A.
- The A1 findings in area 3 are downstream of this. A bare `"run_started"` in a caller is only *possible*
  because the field it flows into accepts any string. Type the field and a large fraction of area 3 becomes
  a build error rather than a detector heuristic.
- 358 casts is 358 places where the graph records a `TYPEREF` to `string` instead of to `core.EventType`.
  The symbol graph is measuring conversion noise as structure.

## What to do

Change `Type string` to `Type EventType`, delete the stale comment, then follow the compiler.

The work is mechanical but **not parallelisable** — one type change, repo-wide blast radius, and the
intermediate states do not build. It is one agent, serially, or it is a mess. Estimate it by the cast count
(358 sites, most of which *delete* a conversion rather than add one), not by the file count.

Three sub-decisions an agent must make and should surface rather than guess:

1. **JSON round-trip.** `EventType` is a defined string type, so `encoding/json` handles it unchanged. Confirm
   with the wire-format tests rather than by inspection — harmonik has schema-version compatibility rules
   (`SchemaVersion`, N-1 readable, EV-029) that a type change must not perturb.
2. **The empty value.** `Type` is documented "Required (non-empty)". Decide whether `EventType("")` stays the
   zero value or whether validation moves into a constructor. Do not silently change which one rejects.
3. **The unregistered seven.** Area 2 is a strict prerequisite in spirit though not in code: typing the field
   is the moment those seven become findable, and doing area 2 first means area 1's compiler sweep confirms
   the fix rather than discovering the hole.

## Gate

Blast radius is the repo, so the usual "edited package plus consumers" rule degenerates. Use instead:

- `go build ./...` — the primary signal; the change is complete when it passes with no remaining cast.
- `go vet ./...`
- `go test -count=1 ./internal/core/... ./internal/eventbus/... ./internal/daemon/...` — the packages that
  own the vocabulary, the bus that carries it, and the largest consumer. Confirm green **before** starting.
- **Exact post-condition:** the JSON encoding is unchanged. A defined string type marshals identically, so
  any recorded event fixture must round-trip byte-identically. If harmonik has golden event captures, that
  is the gate; if it does not, writing one is the cheaper half of this task.
- **Count check:** `grep -rn 'string(.*EventType\|core\.EventType(' --include='*.go' . | wc -l` should fall
  from 358 toward the small residue of genuine boundary conversions (wire decode, CLI input). Any cast that
  survives should be able to say why.

## Traps

- **Do not "fix" the comment and leave the type.** That converts a visible defect into an invisible one.
- **`EventType` is declared in `internal/core`, which has 48 dependents and no dependencies** (D = 0.98,
  `health/cmd/mq`). Typing the field does not add a dependency — every one of those packages already imports
  `core` to get `Event`. But it does mean a mistake here reaches everything, which is the argument for doing
  it as one reviewed change rather than in slices.
- **Expect the diff to be mostly deletions.** If it is mostly additions, the change is being made in the
  wrong direction — casts are being added to satisfy the new type instead of removed because they became
  redundant.
