# C3 — Claim-time per-bead profile resolver — Change Spec

**Component:** C3 (detailed — the crux). At claim time, read the bead's
`profile:<name>` label, resolve it to a tuple against the resolved pi harness, and
thread it into the run context — inheriting the hk-pkugu harness-aware discipline so
no claude default leaks.

## Requirements (from 03-components.md C3)

1. A bead carrying the profile-selecting label resolves to that profile's full tuple
   in `RunCtx`; a bead with no such label leaves all five fields empty (⇒
   harness-global fallback, C4).
2. The resolver runs **only after** `resolveHarnessAgentTypeQuiet` and keys off the
   resolved pi harness type — it MUST NOT produce a tuple for a claude-resolved bead,
   and MUST NOT inherit a claude tier-3 default (hk-pkugu discipline; constraint §6).
3. The tuple is resolved atomically per bead: provider+base_url+api arrive together
   (no partial split that would cross wire formats).
4. `label VALUE` (profile name / model) is never value-validated at this hop
   (opacity; matches `modelpreference.go:220-224`) — existence checked against C2's
   validated map.
5. The five fields added to `claudeRunCtx` are copied into the `RunCtx` literal at
   `harnessregistry.go:240-264` (both edits required, per analysis §5).

## Research summary (from 04-research/C3)

- **Template — the `model:` label path.** `labelPrefixModel = "model:"`
  (`modelpreference.go:166`). `resolveModelField` (`:203-256`) collects all labels
  with the prefix (`:212-218`): exactly one → `strings.TrimPrefix` accepted as-is,
  shape validation deferred (opacity, `:220-224`); >1 → `emitBeadLabelConflict`,
  treat tier-1 absent, fall through (`:225-232`); 0 → absent, no event (`:233`). The
  **collect→count (==1 accept / >1 conflict / 0 absent)** pattern is the exact
  template for a `profile:<name>` collector. The profile resolver is SIMPLER: no
  tier-2/2.5/3 cascade — a bead either names a profile (look it up in C2's validated
  map) or it doesn't (empty tuple → C4 `h.*` fallback).
- **hk-pkugu ordering (load-bearing).** `resolveHarnessAgentTypeQuiet(bead, "", "",
  deps.defaultHarness)` runs FIRST at `workloop.go:3077-3082`, producing
  `resolvedAgentType`, which is passed to `ResolveModelPreference` (`:3083-3090`) so
  the model tier-3 default matches the real harness. The doc-comment at `:3066-3076`
  records the leak: a hardcoded claude-code agent-type sealed `sonnet` into
  `rc.model` for pi runs. **C3 MUST run its profile resolver AFTER line 3082 and key
  off `resolvedAgentType`** — resolve a pi profile tuple only when the resolved
  harness is the pi family; for a claude/codex-resolved bead the tuple stays empty.
- **Carry + projection (both edits).** `claudeRunCtx` struct at
  `claudelaunchspec.go:50` (`type claudeRunCtx struct` at `:21`); `resolvedModel`
  lands at `claudeRunCtx.model` (`workloop.go:4052`) beside `effort` (`:4053`). The
  ONLY projection onto the public `RunCtx` literal for the pi path is
  `harnessregistry.go:240-264` (`Model: rc.model` at `:258`, `Effort: rc.effort` at
  `:259`). Missing either edit = the tuple never reaches `LaunchSpec`.
- **DOT per-node path (hk-lfrub).** A per-node DOT `model=` attribute is applied via
  `nodeModelForHarness(resolvedModel, node.Model, effectiveHarness)`
  (`dot_cascade.go:1274-1281`), which pins `node.Model` ONLY for the claude-code
  family and leaves pi/codex at the run-level `resolvedModel`. The provider tuple is
  NOT threaded through the DOT per-node path — it is set once at claim time
  (`workloop.go:4052`) and rides `claudeRunCtx` unchanged through the cascade. C3 must
  verify the tuple survives `driveDotWorkflow` node re-launches unclobbered (the DOT
  cascade operates on `model=`/`effort=`, not provider).

## Approach

### 1. Label constant + resolver function (`internal/daemon/modelpreference.go` or a new sibling file)

Add a label prefix constant beside `labelPrefixModel` (`modelpreference.go:166`):

```go
// labelPrefixProfile is the label prefix for per-bead Pi provider-profile
// selection (pi-provider-switch). E.g. `profile:ornith-dgx`.
const labelPrefixProfile = "profile:"
```

Add a resolver mirroring `resolveModelField`'s collect→count pattern. It takes the
resolved harness agent-type and the validated profile map, and returns a
`PiProfileConfig` (zero value = no override):

```go
// resolvePiProfile resolves the per-bead Pi provider profile from a `profile:<name>`
// label. Returns the zero PiProfileConfig (all-empty) when: agentType is not
// core.AgentTypePi; no profile: label is present; or (conflict) more than one is present.
// Existence is checked against the C2-validated profiles map; an unknown reference
// is fail-loud. Profile NAME is never value-validated (opacity).
//
// hk-pkugu discipline: callers MUST pass resolvedAgentType from
// resolveHarnessAgentTypeQuiet so a claude/codex-resolved bead yields the zero tuple.
func resolvePiProfile(
    ctx context.Context,
    beadLabels []string,
    agentType core.AgentType,
    piCfg PiHarnessConfig,
    bus handlercontract.EventEmitter,
    beadID string,
) (PiProfileConfig, error)
```

Logic:

1. **Harness gate (hk-pkugu).** If `agentType != core.AgentTypePi`, return the zero
   `PiProfileConfig` immediately (no lookup, no error). The predicate is the single
   concrete `core.AgentTypePi` constant (used at `harnessregistry.go:64`,
   `workloop.go:4094/4837`, `dot_cascade.go`, and the hk-pkugu tests) — there is NO
   "family" of pi types; do not write an over-broad predicate. A `profile:` label on a
   claude/codex-resolved bead resolves to the empty tuple — quiet, non-fatal
   (matches the quiet handling of tier-1 mismatches elsewhere). Optionally emit an
   observability event, but do NOT fail. This is the load-bearing no-leak guard.
2. **Collect** all labels with `labelPrefixProfile` (mirror `:212-218`).
3. **Count:**
   - `== 1`: `name := strings.TrimPrefix(profileLabels[0], labelPrefixProfile)`.
     Look up `piCfg.Profiles[name]`. **Existence check (fail-loud):** if the name is
     absent from the validated map, return an error naming the unknown profile and
     the bead — this is the C2→C3 contract (C2 validates the config map; C3 fails
     loud on an unknown reference at claim time, requirement 5 of C2 / Q4 of C3).
     Otherwise return the found `PiProfileConfig`. NAME value never re-validated
     (opacity; C2 already shape-checked the profile's fields).
   - `> 1`: conflict — `emitBeadLabelConflict` (reuse the existing helper), treat as
     absent, return the zero tuple (mirror `:225-232`).
   - `== 0`: absent, return the zero tuple, no event (mirror `:233`).

### 2. `model:` vs `profile:` precedence (LOCKED DECISION — encode exactly)

**Decision: option (b) — `model:` overrides ONLY the profile's model field.** When a
bead carries BOTH a `profile:<name>` label AND a `model:<alias>` label:

- The wire-format triple `{provider, base_url, api}` PLUS credentials
  (`api_key_env`, `api_key_file`) come **atomically from the profile** and are NEVER
  split.
- The `model:` label overrides ONLY the resolved model string. `model` is orthogonal
  to the coupled triple (it is not part of `{provider, base_url, api}`), so overriding
  it alone cannot produce a wrong-wire-format launch. This matches how `rc.Model`
  already overrides `h.model` per-field at C4 (`piharness.go:126-129`).

Concretely, after both `resolvePiProfile` and `ResolveModelPreference` have run at
claim time:

- `claudeRunCtx.provider   = profile.Provider`
- `claudeRunCtx.apiKeyEnv  = profile.APIKeyEnv`
- `claudeRunCtx.apiKeyFile = profile.APIKeyFile`
- `claudeRunCtx.baseURL    = profile.BaseURL`
- `claudeRunCtx.api        = profile.API`
- `claudeRunCtx.model`: if a `model:` label resolved a non-empty tier-1 value, use
  it; ELSE use `profile.Model`. (i.e. `resolvedModel` from
  `ResolveModelPreference` already reflects the tier-1 `model:` label when present;
  when the profile is present and no `model:` label exists, seat `profile.Model` into
  `claudeRunCtx.model`.)

Precedence for `claudeRunCtx.model` when a profile is present, in order:
1. tier-1 `model:<alias>` label (if exactly one present) — overrides.
2. `profile.Model` — the profile's own model.
3. (no profile) existing tier-2/2.5/3/4 `ResolveModelPreference` walk — unchanged.

When NO profile is present, the `model` resolution is byte-identical to today (C6).

### 3. Claim-time wiring (`internal/daemon/workloop.go:3082–3091, 4052`)

After `resolvedAgentType` is computed (`:3082`) and `ResolveModelPreference` returns
(`:3090`), call:

```go
resolvedProfile, profErr := resolvePiProfile(
    ctx, beadRecord.Labels, resolvedAgentType,
    deps.projectCfg.Harnesses.Pi, deps.bus, string(beadID),
)
if profErr != nil {
    // fail-loud: unknown profile reference. Route through the SAME claim-time
    // refuse-before-launch seam that CrossRepoUnsafeError and StartFromRefError
    // already use in this function — stderr log + brAdapter.ReopenBead(reason) +
    // early return:
    reopenTID, _ := deps.tidGen.Next()
    fmt.Fprintf(os.Stderr, "daemon: workloop: bead %s refused: %v (reopening)\n", beadID, profErr)
    _ = deps.brAdapter.ReopenBead(ctx, deps.intentLogDir, deps.brTimeoutCfg,
        runID, reopenTID, beadID, profErr.Error())
    return
}
```

**Concrete seam (fail-loud mechanism — the implementer MUST cite it).** The
unknown-profile `profErr` plugs into the identical claim-time refuse-before-launch
path used elsewhere in this same function: the `CrossRepoUnsafeError` block at
`workloop.go:~3110` and the `resolveParentCommit`/`StartFromRefError` block at
`~:3140`. Terminal effect on the bead: it is **reopened** via
`brAdapter.ReopenBead(... profErr.Error())` and the workloop returns BEFORE any
launch-spec is built — the bead is NOT left `in_progress`, NOT launched on an
empty/wrong tuple. This makes the "undefined profile → fail loud, does NOT launch"
locked requirement mechanically concrete rather than a comment placeholder.

Model coalescing (encode the LOCKED precedence via the `hasSingleModelLabel`
approach): if `resolvedProfile` is non-zero (`resolvedProfile != (PiProfileConfig{})`)
AND the tier-1 `model:` label was absent, set
`resolvedModel = resolvedProfile.Model` before it is seated into `claudeRunCtx`.
Detect "tier-1 model label absent" via a small helper
`hasSingleModelLabel(beadRecord.Labels) bool` (mirrors `resolveModelField`'s
exactly-one test; false for both 0 and >1 `model:` labels ⇒ coalesce to
`profile.Model`). Do NOT have `resolvePiProfile` return a coalesce flag — it inspects
`profile:` labels, not `model:` labels, so a model-coalesce flag would couple it to a
concern it does not own. The single `hasSingleModelLabel` helper in the caller is the
chosen mechanism; the resolver's responsibility stays scoped to profile resolution.

At `workloop.go:4052-4053`, where `model`/`effort` are seated into the
`claudeRunCtx`, ALSO seat the five tuple fields from `resolvedProfile`:

```go
model:      resolvedModel,   // now possibly profile.Model (see precedence above)
effort:     resolvedEffort,
provider:   resolvedProfile.Provider,
apiKeyEnv:  resolvedProfile.APIKeyEnv,
apiKeyFile: resolvedProfile.APIKeyFile,
baseURL:    resolvedProfile.BaseURL,
api:        resolvedProfile.API,
```

### 4. `claudeRunCtx` struct (`internal/daemon/claudelaunchspec.go:21-50+`)

Add five fields beside `model`/`effort`:

```go
provider   string
apiKeyEnv  string
apiKeyFile string
baseURL    string
api        string
```

Doc: "per-bead Pi provider tuple; empty ⇒ harness-global default (C4 fallback)."

### 5. Projection onto `RunCtx` (`internal/daemon/harnessregistry.go:240-264`)

Add the five fields to the `handlercontract.RunCtx{...}` literal (beside
`Model: rc.model` at `:258`):

```go
Provider:   rc.provider,
APIKeyEnv:  rc.apiKeyEnv,
APIKeyFile: rc.apiKeyFile,
BaseURL:    rc.baseURL,
API:        rc.api,
```

### 6. DOT-path survival (verify, likely no code change)

The DOT cascade (`dot_cascade.go:1274-1281`) re-resolves `node.Model`/`effort` per
node but does NOT touch the provider tuple; the tuple rides `claudeRunCtx` unchanged
through `driveDotWorkflow` node re-launches. **No DOT code change is required, but
the C5 no-leak scenario MUST include a DOT-path variant** (locked decision C3-Q5) to
prove the tuple survives the cascade unclobbered. If the C5 DOT variant reveals the
tuple being reset, add a guard mirroring `nodeModelForHarness` — but the analysis
predicts no change is needed.

## Files & changes

| File | Change |
|------|--------|
| `internal/daemon/modelpreference.go` (or new `pi_profile_resolve.go`) | Add `labelPrefixProfile` const + `resolvePiProfile(...)` collect→count resolver with the harness gate and fail-loud unknown-reference; optional `hasSingleModelLabel` helper. |
| `internal/daemon/workloop.go` | After `:3090`, call `resolvePiProfile`; coalesce `resolvedModel` with `profile.Model` per the precedence decision; seat the five tuple fields into `claudeRunCtx` at `:4052`. Route `profErr` through the `brAdapter.ReopenBead` refuse-before-launch seam (as CrossRepoUnsafeError/StartFromRefError do). |
| `internal/daemon/claudelaunchspec.go` | Add `provider/apiKeyEnv/apiKeyFile/baseURL/api` fields to `claudeRunCtx`. |
| `internal/daemon/harnessregistry.go` | Copy the five fields into the `RunCtx` literal (`:240-264`). |

## Acceptance criteria

1. A pi-resolved bead with `profile:ornith-dgx` (defined in config) yields a
   `RunCtx` carrying that profile's full tuple (provider, base_url, api,
   api_key_env, api_key_file, model), verified end-to-end into `LaunchSpec`.
2. A pi-resolved bead with NO `profile:` label yields all five `RunCtx` tuple fields
   empty (⇒ C4 `h.*` fallback).
3. A **claude-resolved** bead with a `profile:` label yields the zero tuple (harness
   gate) — no pi tuple, no claude tier-3 leak into pi fields (hk-pkugu). Verified by
   C5 scenario 2.
4. A bead with BOTH `profile:X` and `model:Y`: the resolved `RunCtx` carries X's
   `{provider, base_url, api, api_key_env, api_key_file}` AND model `Y` (model:
   overrides only the model field; the triple stays atomic from the profile).
5. A bead referencing an undefined profile name fails loud at claim time via the
   `brAdapter.ReopenBead` refuse-before-launch seam (does NOT launch with an
   empty/wrong tuple). An end-to-end test (C5) asserts the workloop builds NO
   LaunchSpec for the unknown-profile bead — not merely that `resolvePiProfile`
   returns an error.
6. `> 1` `profile:` labels → `bead_label_conflict` emitted, treated as absent (zero
   tuple).
7. The tuple survives the DOT per-node cascade unclobbered (C5 DOT variant).

## Verification

- `go test ./internal/daemon/ -run 'ResolvePiProfile|PiProfile'` — resolver unit
  tests pass.
- The C5 e2e corpus (separate spec) exercises the end-to-end threading.
- `go build ./...`.

## Tests to add (`internal/daemon`)

- `TestResolvePiProfile_LabeledBead_ResolvesTuple` — pi harness + `profile:ornith`
  label + config map → returns the ornith `PiProfileConfig`.
- `TestResolvePiProfile_UnlabeledBead_ZeroTuple` — pi harness, no label → zero tuple.
- `TestResolvePiProfile_ClaudeHarness_ZeroTuple` — claude agent-type + `profile:`
  label → zero tuple, no error (harness gate; hk-pkugu).
- `TestResolvePiProfile_UnknownProfile_FailLoud` — pi harness + `profile:nope` not in
  map → error.
- `TestResolvePiProfile_MultipleLabels_Conflict` — two `profile:` labels →
  `bead_label_conflict` emitted, zero tuple.
- `TestResolvePiProfile_ModelLabelOverridesProfileModelOnly` — `profile:ornith` +
  `model:custom` → tuple has ornith provider/base_url/api/creds AND model=`custom`;
  the triple is NOT split.

Use the existing `emitBeadLabelConflict` test harness / bus-capture pattern from the
`modelpreference` tests. End-to-end projection (claudeRunCtx → RunCtx → LaunchSpec)
is covered by C5 scenario 1 via `ExportedRoutedLaunchSpecBuilder`.

## Error handling / edge cases

- **Unknown profile reference:** fail-loud claim-time error (requirement 5). Name the
  profile and bead in the error; route it through the `brAdapter.ReopenBead(...)`
  refuse-before-launch seam (the same one `CrossRepoUnsafeError` /
  `StartFromRefError` use at `workloop.go:~3110/~3140`): stderr log + ReopenBead with
  `profErr.Error()` as the reason + early `return`, so the bead is reopened and NEVER
  reaches launch-spec construction on an empty/wrong tuple.
- **`profile:` on a non-pi harness:** zero tuple, quiet, non-fatal (harness gate).
- **Multiple `profile:` labels:** conflict event, treated as absent.
- **`profile:` + `model:` together:** model overrides only the model field; triple
  stays atomic (locked precedence decision).
- **Value opacity:** profile NAME and model VALUE never value-validated at this hop
  (existence-only against C2's map).

## Migration / backwards compatibility

When no `profile:` label is present, `resolvePiProfile` returns the zero tuple and
the five `claudeRunCtx`/`RunCtx` fields stay empty — `model` resolution is
byte-identical to today (C6). The hk-pkugu ordering is preserved: the profile
resolver runs strictly after `resolveHarnessAgentTypeQuiet`.
