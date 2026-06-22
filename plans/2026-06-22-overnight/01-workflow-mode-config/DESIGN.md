# DESIGN — Config-owned default workflow so `queue submit` runs triple-review, not single-reviewer

Date: 2026-06-22
Area: workflow-mode resolution / `queue submit` / daemon project config
Status: BUILDABLE-NOW (one-line client fix is the core; config key already exists)

---

## 1. The problem, in plain terms

When a captain submits work with `harmonik queue submit --beads hk-a,hk-b`, every bead runs
the **two-node implementer→single-reviewer loop** (`review-loop`) instead of the intended
**triple-Sonnet-review DOT** (implementer + 3 distinct Sonnet reviewers consolidated).

To get the DOT today you must hand-author a batch-JSON file with `workflow_mode:"dot"` +
`workflow_ref`, which captains keep forgetting — so the fleet runs for hours on single-review.

The surprising part: **the daemon already defaults to the right thing.** The project's
`workflow.dot` at repo root is *byte-identical* to `.harmonik/workflows/sonnet-triple-review.dot`
(verified: `diff workflow.dot .harmonik/workflows/sonnet-triple-review.dot` → no output), and
the daemon's tier-3 default is `dot`. The single-reviewer default is injected **client-side, by
the `queue submit` CLI**, and it *overrides* the daemon default. That one override is the whole bug.

---

## 2. Current mechanism (code-grounded)

### 2.1 The daemon ALREADY resolves to `dot` → `workflow.dot` → triple-review

Resolution lives in `internal/daemon/moderesolve.go:53` `resolveWorkflowMode`, a four-tier walk
(`internal/daemon/moderesolve.go:9-12`):

- **Tier 1** — per-bead `workflow:<mode>` label (`moderesolve.go:59-89`).
- **Tier 2** — per-project config. The comment calls it a "reserved no-op for MVH" (`moderesolve.go:92-95`) — but see §2.4: a *daemon-level* project config tier already exists, it just isn't this function's tier-2.
- **Tier 3** — daemon default `deps.workflowModeDefault` (`moderesolve.go:97-100`).
- **Tier 4** — hard fallback **`dot`**, never `single` (`moderesolve.go:102-106`, `EM-012a-FLOOR`, hk-30vlb).

The daemon-level default (tier 3) is `cfg.WorkflowModeDefault`, set at daemon boot. The boot flag
`--workflow-mode` **defaults to `dot`** (`cmd/harmonik/main.go:793` —
`flag.StringVar(&workflowModeFlag, "workflow-mode", string(core.WorkflowModeDot), …)`), and the
config block can override it (see §2.4). So a running daemon's tier-3 default is `dot` unless the
operator says otherwise.

When mode resolves to `dot` with **no** explicit `workflow_ref`, the daemon loads
`<projectDir>/workflow.dot` (`internal/daemon/workloop.go:3010-3011`, and the DOT case at
`workloop.go:3224-3247`), falling back to the embedded `standard-bead.dot` only if that file is
absent (`workloop.go:3002-3018`, `3237-3247`). Since `workflow.dot` == the triple-review graph,
a bare `dot` submit already runs triple-review. **The plumbing is correct end to end.**

### 2.2 ROOT CAUSE — `queue submit` stamps `review-loop` as a per-item (tier-0) override

`internal/queue/cli/submit.go:48`:

```go
workflowMode := "review-loop" // default: review-loop per hk-g0ckv / hk-rssrg / hk-tldws
```

This value is stamped onto **every** minted item via `beadsToQueueDoc(beadIDs, queueName, workflowMode)`
(`submit.go:83`; the item gets `WorkflowMode: workflowMode` at
`internal/queue/cli/helpers.go:211`). The item-level `workflow_mode` is **tier-0** — it is read in
the daemon claim path at `internal/daemon/workloop.go:2593-2599`:

```go
workflowMode := resolveWorkflowMode(ctx, beadRecord, deps.workflowModeDefault, deps.bus)
if itemWorkflowMode != "" {                       // tier-0 per-item override
    if candidate := core.WorkflowMode(itemWorkflowMode); candidate.Valid() {
        workflowMode = candidate                  // ← review-loop wins here
    }
}
```

So the CLI's hardcoded `"review-loop"` **beats** the daemon's `dot` default on every bare
`--beads` submit. That is the recurring single-review default. It is **not** a daemon decision and
**not** a deliberate fallback — it is a stale CLI default left over from when `review-loop` *was*
the system default (the comments name hk-g0ckv / hk-rssrg, the very beads that made review-loop the
default before hk-30vlb flipped the system default to `dot`).

`beadsToQueueDoc`'s own docstring already documents the escape hatch
(`internal/queue/cli/helpers.go:184-185`):
> `workflowMode` is stamped onto each minted item … **Pass "" to omit it (daemon default applies).**

The `omitempty` JSON tag on the item's `WorkflowMode` (`helpers.go:195`, `211`) means an empty
string is dropped from the wire entirely, so the daemon sees no tier-0 value and falls to tier-3 `dot`.

### 2.3 The batch-JSON override path (the one that DOES get the DOT)

A positional `<queue-file>` is read verbatim (`submit.go:89-111`), normalized
(`normalizeQueueDocGroups`, `submit.go:103`), and sent as-is. So a JSON item carrying
`"workflow_mode":"dot"` + `"workflow_ref":"…"` lands as tier-0 dot — correct. The **known quirk**:
the JSON top-level `"name"` (queue) field IS honored in the file path *only if* the operator put it
in the file; but a `--queue <name>` flag is injected into the file doc at `submit.go:107-111`
(`queueDoc["name"] = nameBytes`). The handoff's reported "JSON queue field ignored → work lands in
`main`" is the inverse case: if the operator relies on the *flag* but the *file* already has a
`name`, the flag overwrites it; if neither sets it, it defaults to `main`. This is orthogonal to
the workflow-mode bug and is noted here only so the fix doesn't accidentally regress queue routing
(it doesn't — we touch only the `--beads` minting default).

### 2.4 Existing config for a default workflow — IT ALREADY EXISTS (server-side)

`.harmonik/config.yaml` supports a `daemon:` block (`internal/daemon/projectconfig.go:40-44`,
`216-224`, `560-600`) with a **`workflow_mode`** key:

```yaml
schema_version: 1
daemon:
  workflow_mode: dot          # review-loop or dot; single FORBIDDEN (PL-004a floor)
  max_concurrent: 4
```

- Parsed/validated by `parseDaemonBlock` (`projectconfig.go:756-791`): rejects unknown values
  (`*ErrMalformedConfigYAML`), rejects `single` (`*ErrWorkflowModeFloorViolation`,
  `projectconfig.go:772-774`).
- Applied at boot with **flag > config > built-in** precedence
  (`cmd/harmonik/main.go:947-948`): `if !explicitFlags["workflow-mode"] && projCfg.Daemon.WorkflowMode != "" { workflowModeFlag = … }`.
- Fed into `daemon.Config.WorkflowModeDefault` (`main.go:1124`), which `daemon.Start` validates
  fail-closed (`internal/daemon/daemon.go:682-686`) and caches as the tier-3 default.

**So a project can ALREADY set the daemon-wide default workflow today** by adding
`daemon: { workflow_mode: dot }` to `.harmonik/config.yaml`. The repo's current
`.harmonik/config.yaml` has **no `daemon:` block at all** — so it relies on the flag default
(`dot`), which is then *masked on every submit* by §2.2. Adding the block does not fix the bug by
itself, because the CLI tier-0 override still wins. **Both halves must change.**

### 2.5 Why "review-loop default" exists at all

Historical accident, not design. The CLI default predates hk-30vlb (which flipped the *system*
default to `dot`). The `queue submit` default was never updated to match. Comments at `submit.go:48`
and the regression tests `cmd/harmonik/workflowmode_default_hkrssrg_test.go` (which still assert
`review-loop` via a *local* FlagSet, line 42, NOT the real `main.go` flag) are stale fossils of the
pre-`dot` era. The real `main.go:793` daemon flag is already `dot`; only the CLI submit path and
those stale tests still say `review-loop`.

---

## 3. Proposed design

### 3.1 Principle

There are two defaults in play. Make the daemon the single source of truth and stop the CLI from
silently overriding it.

1. **`queue submit --beads` stops stamping a hardcoded mode.** When the operator gives no explicit
   `--workflow`/`--workflow-mode`, mint items with **empty** `workflow_mode` so the daemon's
   resolved default applies. This is the load-bearing one-line change.
2. **The daemon default is config-owned** (already true via `daemon.workflow_mode`), with a shipped
   built-in fallback of **`dot`** (already true at `main.go:793` and tier-4 `moderesolve.go:106`).
3. **A project sets it once** by writing `daemon: { workflow_mode: dot }` to
   `.harmonik/config.yaml`; that persists across all captains and daemon restarts (loaded at boot,
   `main.go:916`/`947`).

This honors the operator MANDATE: there is no *silently invented* value masking a missing config —
the daemon *fails closed* if `WorkflowModeDefault` is empty/invalid
(`daemon.go:682-686`), and `dot` is the explicit, documented, audited system default (hk-30vlb),
not a convenience fallback. Single-mode remains reachable ONLY via an explicit per-bead
`workflow:single` label, audited by `review_bypassed` (`moderesolve.go:74-80`).

### 3.2 Resolution order (after the fix)

Per item, the daemon resolves (highest wins):

1. **Per-item `workflow_mode`** (tier-0; from JSON file, or from a new explicit CLI flag — §3.4).
2. **Per-bead `workflow:<mode>` label** (tier-1; `moderesolve.go:59`).
3. **(reserved per-project tier-2 in `resolveWorkflowMode` — still a no-op; unchanged.)**
4. **Daemon default** = `daemon.workflow_mode` config → else `--workflow-mode` flag → else built-in
   (tier-3; `main.go:947`, `daemon.go`).
5. **Built-in hard fallback `dot`** (tier-4; `moderesolve.go:106`).

The bug fix removes the *spurious* tier-0 value that the CLI was injecting unconditionally, so a
bare submit now falls through to tier-3/4 = `dot` = triple-review.

`workflow_ref` resolution is unchanged (`resolveWorkflowRef`, `moderesolve.go:145`): per-item ref →
per-bead `dot:<name>` label → `<projectDir>/workflow.dot` → embedded `standard-bead.dot`. With
mode=dot and no ref, the project's `workflow.dot` (= triple-review) is used.

### 3.3 Config key — location and shape (NO new key needed; reuse existing)

Use the **already-shipped** key:

```yaml
# .harmonik/config.yaml
schema_version: 1
daemon:
  workflow_mode: dot     # review-loop | dot  (single forbidden, PL-004a floor)
```

No new struct, parser, or precedence wiring is required — `rawDaemonConfig.WorkflowMode`
(`projectconfig.go:219`), `DaemonConfig.WorkflowMode` (`projectconfig.go:571`), `parseDaemonBlock`
(`projectconfig.go:756`), and the `main.go:947` apply-step already exist and are tested.

> Decision point (minor, agent-owned): we deliberately do **not** add a separate
> `daemon.default_workflow_ref` key. The `dot` mode already resolves the ref via the file convention
> (`workflow.dot`) + per-bead `dot:<name>` label, which is the existing, tested mechanism. Adding a
> config ref key would create a second, redundant source of truth for "which .dot file". If a future
> need arises to point the daemon default at a *non-default* .dot without renaming `workflow.dot`,
> that's a clean follow-up; it is not needed to fix this bug.

### 3.4 Secondary: `queue submit --workflow` flag for ad-hoc override

`queue submit` already parses `--workflow-mode <mode>` (`submit.go:63-68`). Keep it; it becomes the
explicit per-submit tier-0 override (e.g. `--workflow-mode review-loop` to force the cheap loop for
a throwaway bead). The only change is its **default**: from `"review-loop"` to `""` (= "inherit
daemon default"). No new flag name is required; optionally alias `--workflow` → `--workflow-mode`
for ergonomics (low priority, additive).

---

## 4. Exact change list (BUILDABLE-NOW)

### C1 — Core fix (load-bearing): stop the CLI stamping a default

`internal/queue/cli/submit.go:48`
```go
-	workflowMode := "review-loop" // default: review-loop per hk-g0ckv / hk-rssrg / hk-tldws
+	workflowMode := "" // empty = inherit the daemon-resolved default (dot → workflow.dot triple-review);
+	                    // an explicit --workflow-mode still overrides. (supersedes hk-g0ckv/hk-rssrg CLI default)
```
- `beadsToQueueDoc` already omits an empty mode from the wire (`helpers.go:195` `omitempty`, docstring `helpers.go:184-185`). No other code change needed for the core fix.

### C2 — Update the docstring banner

`internal/queue/cli/submit.go:32-33` — change "(default: review-loop)" to
"(default: empty = inherit daemon default, normally dot/triple-review)".

### C3 — Set the project default explicitly (persistence across captains/restarts)

Add to `/Users/gb/github/harmonik/.harmonik/config.yaml`:
```yaml
daemon:
  workflow_mode: dot
```
This makes the intent durable and visible even if someone later changes the `main.go` flag default.
(Belt-and-suspenders with the flag default; both already resolve to `dot`.)

### C4 — Fix the stale regression tests

`cmd/harmonik/workflowmode_default_hkrssrg_test.go` asserts `review-loop` via a *local* FlagSet
(line 42) — it does not reflect `main.go:793` (which is `dot`) and contradicts the new CLI default.
Re-point these to assert the post-fix contract:
- daemon `--workflow-mode` default is `dot` (matches `main.go:793`);
- `queue submit --beads` with no flag mints items with **no** `workflow_mode` (empty/omitted);
- `queue submit --beads --workflow-mode review-loop` still stamps `review-loop`.
Add a focused test in `internal/queue/cli/` (e.g. `submit_default_workflow_test.go`) asserting
`beadsToQueueDoc(ids, "", "")` produces items with no `workflow_mode` field on the wire, and that
the parsed default in `RunQueueSubmit` is empty. Keep the hk-rssrg fossils only as renamed
"historical" assertions if any still hold; delete the ones that assert the wrong default.

### C5 — `harmonik run` parity (optional, recommended)

`cmd/harmonik/run.go:330-364` independently maps `--workflow-mode builtin` → review-loop/single via
the `--review-loop` boolean (`run.go:331-337`). This is the `harmonik run` (inline) path, not
`queue submit`, but it shares the captain-facing confusion. Recommend: when `--workflow-mode` is
unset AND `--review-loop`/`--no-review-loop` are both unset, leave `itemWorkflowMode = ""` so `run`
also inherits the daemon default. Gate behind the same review to avoid surprising scripted callers
that rely on `--review-loop`. (Secondary — the operator's reported pain is `queue submit`.)

### C6 — Optional one-shot migration command (pattern exists)

Model on `cmd/harmonik/migrate_rc_prefix_cmd.go` (in-place config.yaml patch preserving the rest of
the file, `migrate_rc_prefix_cmd.go:71`, `177`). A `harmonik migrate-workflow-default` could ensure
`daemon.workflow_mode: dot` is present in existing projects. **Lower priority** than C1/C3 — for
THIS repo, C3 (hand-edit) suffices; the migration command matters only for the multi-project
fleet-portability story. If built, register it in the ON-INV allowlist exactly as
migrate-rc-prefix was (commit 54e6936b precedent).

---

## 5. Risks / edge-cases

- **Scripted callers depending on the old `review-loop` default.** Anyone who ran
  `queue submit --beads …` *expecting* single-review now gets triple-review (more agents, slower,
  more tokens per bead). Mitigation: that's the *intended* correctness fix; `--workflow-mode
  review-loop` restores the old behavior explicitly. Document in the change.
- **A project with NO `workflow.dot` at root.** mode=dot with no ref falls back to embedded
  `standard-bead.dot` (`workloop.go:3237`), which is single-implementer→single-review-style, NOT
  triple. For THIS repo `workflow.dot` exists and == triple-review, so we're fine; for portability,
  C6/init should ensure projects that want triple-review ship a `workflow.dot`. Flag in review.
- **`single` floor.** Confirm no path lets the empty default resolve to `single`: tier-4 is `dot`
  (`moderesolve.go:106`), config rejects `single` (`projectconfig.go:772`), daemon `Start` rejects
  empty (`daemon.go:682`). Safe.
- **Crew submits.** Crews submit via the same `queue submit` surface (harmonik-dispatch skill /
  `internal/queue/cli`), so C1 fixes crew submits too — no separate change.
- **Embedded-asset re-sync.** No `.claude/skills/*` or `captain-tools/*` edits here, so the
  `TestSkillAssetsEmbedInSync` / `TestCaptainLaunchShEmbedInSync` traps don't fire. (Noted from
  memory: embedded-asset re-sync gotcha.)
- **`omitempty` correctness.** Verify the wire actually drops the field when empty so the daemon
  sees tier-0 absent (it will — `helpers.go:195` tag) — covered by the C4 wire-shape test.

---

## 6. Test plan

1. **Unit (queue/cli):** `beadsToQueueDoc(ids, "", "")` → marshaled JSON has no `workflow_mode`
   key on any item. `beadsToQueueDoc(ids, "", "review-loop")` → each item has
   `"workflow_mode":"review-loop"`.
2. **Unit (queue/cli):** `RunQueueSubmit` with no `--workflow-mode` mints empty-mode items;
   with `--workflow-mode review-loop` stamps review-loop. (Parse-level, no daemon.)
3. **Daemon resolution (existing harness):** with `deps.workflowModeDefault = dot` and an item whose
   `WorkflowMode == ""`, `resolveWorkflowMode` + the tier-0 guard
   (`workloop.go:2593-2599`) yields `dot`. (Extend an existing moderesolve test.)
4. **Config:** `parseDaemonBlock` with `workflow_mode: dot` → `DaemonConfig.WorkflowMode == dot`;
   `main.go` apply-step sets `WorkflowModeDefault = dot` when the flag is not explicit. (Existing
   coverage; assert it still passes after C4.)
5. **Regression rewrite:** update `workflowmode_default_hkrssrg_test.go` to the new contract (C4).
6. **End-to-end smoke (manual / scenario):** `harmonik queue submit --beads <id>` on a project whose
   `workflow.dot` == triple-review → run uses the DOT walker (assert `run_started` payload
   `workflow_mode == dot`, or the 3-reviewer cascade events fire), NOT `review_loop_*` events.
7. **Full `go test ./cmd/harmonik/ ./internal/queue/... ./internal/daemon/...`** to catch the stale
   fossils and any item-shape assertions.

---

## 7. Verdict

**BUILDABLE-NOW.**

The fix is overwhelmingly a **one-line client change** (C1: `submit.go:48` `"review-loop"` → `""`)
plus a **one-line project-config line** (C3: `daemon.workflow_mode: dot` in `.harmonik/config.yaml`),
with the remainder being docstring/test hygiene (C2/C4) and optional parity/migration polish
(C5/C6). The entire four-tier resolution chain, the `daemon.workflow_mode` config key, the
flag>config>fallback wiring, and the `dot → workflow.dot → triple-review` plumbing **already exist
and are tested** — nothing new needs to be designed or invented. The shipped built-in default is
and remains **`dot` (triple-review)**, which is the correct, audited system default.

An implementer bead can be written straight from §4 (C1+C3+C2+C4 as the must-do set; C5+C6 as
follow-ups).
