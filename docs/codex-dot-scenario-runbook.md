# Runbook — the codex-first DOT scenario (`codex-dot:local`, hk-ity2u)

**What this scenario proves:** a bead labelled `harness:codex` runs its **implement** node
on Codex while **both** reviewer nodes stay on Claude, each reviewer actually returns a
verdict, a commit lands, and the bead closes. That combination is the safety boundary that
makes per-bead Codex labelling usable at all.

**What it is not:** a test of whether Codex can do the work. Capability is established
separately (the assessor's verdict). Running this to find out whether Codex is capable is
testing in production; this validates routing and lifecycle once capability is established.

**Why routing is asserted and not just the outcome.** When the boundary breaks, the reviewer
silently goes to Codex, emits no verdict, and the run reds in a way that reads as "Codex
cannot do the work" rather than "the routing was wrong" (hk-ofm89, hk-3eso9). The inverse is
worse: a run can close **green** with the reviewer on the wrong harness. A green outcome
with the wrong routing is **false evidence**, so the outcome alone is never the result.

---

## Preconditions

Check all four before running. Any one of them missing turns the cell into false evidence
rather than a failure — it will still produce a verdict, just not the one you think.

1. **The daemon binary carries the codex fixes.** The scenario asserts behavior that only
   exists in a rebuilt binary; a stale daemon reds for the wrong reason.
2. **Resolved workflow mode is `dot`.** `scripts/core-loop-matrix.sh` exports
   `SCRATCH_WORKFLOW_MODE=dot` by default, so the scratch daemon boots in dot mode. If you
   run the cell by hand against another daemon, confirm the mode — a `single` or
   `review-loop` resolve has **no node pins at all**, so the reviewer inherits the bead's
   `harness:codex` label and the boundary is defeated silently.
3. **The seed bead carries `harness:codex` and NO `workflow:<mode>` label.** The
   `workflow:` label is the per-bead boundary-defeat vector: it can re-resolve the run out
   of dot mode, which removes the node pins. `scenarios/core-loop-proof/seed-beads.json`
   documents this at the `codex-dot` seed; do not add the label to make a run "work".
4. **No `dot:<file>` label either.** The cell must exercise the *project-default*
   `workflow.dot` — which is `standard-bead.dot`, whose `review` and `qa` nodes pin
   `harness="claude-code"` + `model="claude-opus-4-8"`. That pin is the thing under test.

---

## Running it

The cell name is not `harness:substrate`, so it joins the matrix through `EXTRA_CELLS`
exactly like `pi-dot:local`:

```sh
EXTRA_CELLS='codex-dot:local|codex|local' scripts/core-loop-matrix.sh --assert
```

Offline, against the checked-in golden streams (no daemon, zero tokens):

```sh
bash scripts/core-loop-assert-test.sh          # the whole assertion library
```

One captured stream against the real cell spec:

```sh
SPEC="$(jq -c '.cells[] | select(.cell=="codex-dot:local")
               | .seed_bead="<real-bead-id>" | ._observed_lands_on="main"' \
        scenarios/core-loop-proof/cells.json)"
bash scripts/core-loop-assert-cell.sh <capture.ndjson> "$SPEC"
```

`CELL_VERDICT green` (exit 0) / `red` (1) / `pending` (2).

---

## Reading the result

The gap that matters is **gap7**. Its pass line names the routing it observed:

```
GAP  gap7  pass  implement=codex/tier1; review=claude-code/tier3 verdict=APPROVE
                 qa=claude-code/tier3 verdict=APPROVE; no EPERM; no model leak into codex
```

### The tier numbers are the assertion, not decoration

- **implement → tier 1.** Tier 1 is the per-bead label path. A Codex implementer at any
  other tier means the *label* is not what routed it, and the run proves nothing about
  labelling.
- **review / qa → tier 3.** Tier 3 is what `pinnedHarnessLaunchSpecBuilder` emits, so it
  proves the **DOT node pin** held. A Claude reviewer at **tier 4** is the *global default*
  agreeing by coincidence — the pin itself is untested and breaks the day the default moves.

Relaxing `reviewer_tier` to "any Claude" silently guts this scenario. If you are tempted to,
that is the bug, not the assertion.

### Why routing is recovered positionally

`harness_selected` carries only `{bead_id, agent_type, tier}` — there is **no node id on
it**. Per-node routing therefore cannot be read off the event, and no amount of filtering
will produce it. It is recovered by position: `dot_cascade.go` emits
`node_dispatch_requested` at the top of every node visit, before the node is handled, and
`harness_selected` is emitted inside the launch-spec builder closure at each node's launch —
so every `harness_selected` belongs to the nearest preceding `node_dispatch_requested`.
`reviewer_verdict` is attributed the same way, for the same reason. Events before the first
dispatch attribute to `<pre-cascade>` and are reported, never dropped.

Do not "simplify" gap7 to filter by node id. That field does not exist.

### Why gap1 is absent from this cell

`gap1` asserts on the **last** `harness_selected` for the bead. In a DOT cascade that is
`qa`'s claude-code/tier-3, so listing gap1 would red a perfectly-routed run. gap7 is
strictly stronger — harness *and* tier, per node — and it carries gap1's node-pin model
no-leak check as clause (f), so nothing is lost. Do not restore gap1 to this cell.

---

## Triage — what each red means

| gap7 message | What actually happened | First move |
|---|---|---|
| `no node_dispatch_requested in the stream` | Not a DOT run. The bead resolved to single/review-loop, so there were no node pins at any point. | Check precondition 2 and 3 — usually a stray `workflow:` label or a daemon not in dot mode. |
| `implement node routed to <x>/tier<n>` | The `harness:codex` label did not route the implementer. | Check the label is on the bead and the tier-1 path is live in this binary. |
| `reviewer node(s) … never launched` | The cascade never reached review/qa. | Look upstream: the commit gate or the implement node failed; gap3/gap4 usually red too. |
| `REVIEWER ROUTED TO codex` | **The boundary failed.** The bead label overrode the node pin. | This is the failure the scenario exists to catch (hk-ofm89). Do not re-run hoping; report it. |
| `reviewer node … routed to claude-code/tier4` | Right harness, wrong reason — the global default, not the pin. | The pin is not holding. Treat as a real red even though the run may be green. |
| `launched on claude-code but emitted NO reviewer_verdict` | Silent reviewer — the same false red by another route. | Check the reviewer pane/verdict file; the run may still close green. |
| `EPERM / 'operation not permitted'` | The Codex shell step hit a sandbox denial. Not a typed event, which is why it is caught by scanning the stream text. | Sandbox/isolation configuration, not routing. |
| `node-model pin LEAKED into the codex implement launch` | The reviewer nodes' `model=claude-opus-4-8` pin reached the Codex launch (hk-lfrub/hk-pkugu shape). | Routing can be perfect and the run green while this is broken; treat as a real red. |

**Every failure fixture in the self-test closes green with `run_completed success`.** That
is deliberate: it is what makes them a real test of the "must also assert the routing"
requirement, since the outcome alone cannot tell any of them from the pass.

---

## Files

| Path | What it holds |
|---|---|
| `scripts/core-loop-assert.jq` | gap7 — the assertion, with the attribution and tier reasoning inline. |
| `scripts/core-loop-assert-test.sh` | The offline self-test: one fixture per failure mode plus the green. |
| `scenarios/core-loop-proof/cells.json` | The `codex-dot:local` cell spec (`expect.dot_routing`). |
| `scenarios/core-loop-proof/seed-beads.json` | The `codex-dot` seed — and the note on the two labels that must stay absent. |
| `scenarios/core-loop-proof/testdata/codex-dot-*.ndjson` | The golden streams. |

Bead: hk-ity2u. Related: hk-ofm89 (the daemon-side mode guard), hk-3eso9 (the silent hang a
non-dot resolve produces).
