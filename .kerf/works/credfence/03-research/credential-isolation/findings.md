# Research — `credential-isolation.md` (NEW spec) — Components C1, C2, C3

> Pass 3 (`research`) of the `credfence` spec work. Covers the new cross-cutting credential-holder spec: holder contract + deny-list (C1), scrub-at-spawn-boundary + regression test (C2), Pi-scoped injection + `supervise start` source (C3). Grounded in the 2026-05-30 incident assessment (`docs/flywheel/2026-05-30-lifecycle-feasibility-and-gaps.md` §"Genuinely lost" #2-#4) and the live code tree (anchors re-verified 2026-05-31). Planning artifact; does not modify `specs/`.

## Research questions

- **RQ1.** Where exactly does the leaked key reach a daemon-spawned `claude`, and is there a single env-assembly boundary where a scrub belongs (vs scattered `env -u` wrappers)?
- **RQ2.** Where does the key reach Pi, and is the `os.Environ()` passthrough the only path?
- **RQ3.** Is there an existing strip pattern the scrub should be symmetric with?
- **RQ4.** Is the tmux substrate handoff a second scrub site or an assertion point?
- **RQ5.** Are there existing spec patterns for a cross-cutting invariant spec owned by no single subsystem?
- **RQ6.** What is the right non-committed scoped source for `supervise start` to inject the key, given the non-goal "no new vault subsystem"?

## Findings

### F1 — The daemon->claude env has exactly ONE assembly boundary; the scrub belongs there (RQ1, RQ4)

The implementer/reviewer child env is built in a single function and threaded to the tmux substrate untouched:

- `internal/handler/claudehandler_chb006_024.go:200` `ClaudeEnvVars(cfg ClaudeEnvConfig) []string` — starts from `cfg.BaseEnv`, then **already strips `HARMONIK_SECRET_*`** (verified, lines ~205-213: `if strings.HasPrefix(key, "HARMONIK_SECRET_") { continue }`), appends CHB-006 required vars, returns the `KEY=VALUE` slice. This is the **single point** where child env is constructed. The deny-list scrub belongs here, symmetric with the existing `HARMONIK_SECRET_*` strip (answers RQ3 — the pattern already exists; the scrub is a one-branch extension).
- `internal/daemon/claudelaunchspec.go:336` sets `BaseEnv: rc.baseEnv`; `:338` `env := handler.ClaudeEnvVars(cfg)`; `:410` `Env: env` on the launch spec. So `ClaudeEnvVars`'s output IS the downstream env.
- `internal/daemon/tmuxsubstrate.go:179` `Env: in.Env` — the substrate carries the whole pre-built env slice as-is; there is **no per-var `-e` injection** here (the assessment's "additive `-e`" phrasing predates current code; live code passes a single assembled `Env` slice). **This is an ASSERTION point, not a second scrub site** (answers RQ4).

> NOTE — stale anchor corrected: the assessment cites `internal/daemon/osadapter.go:497` for the tmux `-e` leak. That file does NOT exist in the live tree. The current handoff is `internal/daemon/tmuxsubstrate.go:179` `Env: in.Env`, which problem-space (01) already records correctly. The *mechanism* (key reaches child via inherited env) is real; only the file:line moved.

`BaseEnv` provenance: `rc.baseEnv` <- daemon `Config.HandlerEnv` <- daemon process env, seeded from `os.Environ()` at the self-wrap exec `cmd/harmonik/run.go:404` (`runBeadSelfWrapExec(tmuxBin, selfWrapArgv, os.Environ())`). A key the operator exports reaches `BaseEnv` and, absent a scrub, passes through `ClaudeEnvVars` into the child. **Conclusion:** scrubbing at `ClaudeEnvVars` catches every daemon-spawned claude regardless of how the key entered `BaseEnv`.

### F2 — The Pi side leaks via THREE `os.Environ()` passthroughs, not two (RQ2)

Live tree has three distinct passthrough sites:

- `cmd/harmonik/supervise/shim.go:103` — `syscall.Exec(resolved, command, os.Environ())` (the `_shim` that execs the supervisee incl. Pi).
- `cmd/harmonik/supervise/attach.go:58` — `syscall.Exec(tmuxBin, argv, os.Environ())` (operator `attach`; carries the key into the attached tmux client env).
- `internal/supervise/supervisor.go:336` — `cmd.Env = append(os.Environ(), s.spec.Env...)` (the Supervisor that runs the supervisee under restart control). **This one matters most**: it explicitly *appends* `s.spec.Env` to the full parent env — so an explicit `spec.Env` does NOT replace the inherited environment; it ADDS to it.

**Implication for C3:** "Pi-scoped injection" cannot be satisfied by setting `spec.Env` alone, because `supervisor.go:336` unions it with `os.Environ()`. The contract must require building Pi's env from an explicit allow-list base, then adding the credential. All three sites must convert from `os.Environ()` passthrough to a scoped builder. This is stronger than the problem-space's two-site framing — **flag for design (C3)**.

### F3 — Deny-list set + glob semantics (RQ1, RQ3)

Deny-list is `{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}`. Two exact names; `CLAUDE_CODE_OAUTH*` is a prefix glob. The existing `HARMONIK_SECRET_*` strip is already a prefix match (`strings.HasPrefix`), so the scrub (exact-set union prefix-set) is a direct generalization of shipping code. **Pattern:** keep the deny-list as ONE named constant referenced by scrub (C2), Pi-builder (C3 inverse), and secret scan (C7) — per `02-components.md §7`. Spec table names it normatively; code has one definition.

### F4 — Cross-cutting-invariant spec pattern exists in the corpus (RQ5)

Making `credential-isolation.md` a NEW spec matches an established pattern: specs that own a single cross-subsystem invariant rather than a subsystem. Precedent: `claude-launchspec.md` owns the launch-spec contract spanning daemon+handler+substrate; `handler-pause.md` owns a policy spanning execution-model+queue-model+control-points+handler-contract, and its **Appendix A** enumerates amendments to four sibling specs. A thin `credential-isolation.md` stating the invariant and referencing the env-assembly site (`claude-launchspec.md`), the Pi process (`cognition-loop.md` CL-001/CL-100), and the substrate handoff is **idiomatic**. It should carry: holder rule, deny-list table, scrub invariant + named test, Pi-scoped injection rule, and an Appendix-A-style list of additive notes it asks siblings to carry.

### F5 — `supervise start` injection source: the sanctioned-`.env` answer is consistent with the corpus (RQ6)

OQ-5 leans gitignored `.env` read only by `supervise start`. Findings support it:

- No existing secrets subsystem to reuse (grep for keychain/vault/secret-store in `internal/`+`cmd/` returns only `HARMONIK_SECRET_*` env conventions). Inventing one violates problem-space §3.
- `operator-nfr.md §4.1 ON-004` already mandates a **config inventory** of "every operator-configurable knob" with precedence layers (runtime override / operator-policy file / workflow def / default per `control-points.md §4.7 CP-037`). A `supervise start` credential source slots in as one more knob with a precedence rule (explicit env export > gitignored `.env` > error). C3 gets a home that already exists.
- The fix turns the incident's vector into the *sanctioned* path by pairing it with the `.gitignore` rule (C7 / hk-pbs1u): operator muscle memory works, file is uncommittable. **Recommendation: gitignored `.env`, read ONLY by `supervise start`, never by the daemon, never unioned into a child env.**

## Patterns to follow

- One scrub boundary, symmetric with `HARMONIK_SECRET_*` (`claudehandler_chb006_024.go:200`); no scattered `env -u`.
- One deny-list constant, referenced by scrub + Pi-builder + scan.
- Substrate handoff = assertion, not scrub (`tmuxsubstrate.go:179`).
- NEW thin invariant spec with an Appendix-A amendment list, mirroring `handler-pause.md` / `claude-launchspec.md`.
- Mechanism-tagged (architecture.md §4.4): deny-list fixed set; scrub deterministic; no model judgment.

## Risks / conflicts

- **R1 (design-blocking, C3).** `supervisor.go:336` `cmd.Env = append(os.Environ(), s.spec.Env...)` means scoped injection requires replacing the *base*, not just adding `spec.Env`. Contract must say Pi's env is built from an allow-list, not an `os.Environ()` union, and must name all three sites (shim, attach, supervisor). The problem-space two-site framing undercounts.
- **R2 (boundary precision).** `attach.go:58` carries the key into the *operator's* attached tmux client. The operator already holds the key, so scrubbing there may be unnecessary/harmful to the operator shell. Flag: is attach in the scrub scope, or only the daemon->claude + Pi-injection paths?
- **R3 (confirmation, no conflict).** The existing `HARMONIK_SECRET_*` strip proves the boundary already filters env; the deny-list is additive/backward-compatible. No spec asserts claude SHOULD receive `ANTHROPIC_API_KEY`, so the scrub breaks no documented contract.
- **R4.** Scrub must not log the value (problem-space §4). Remove by key name; the regression test (hk-4g32m) asserts absence-by-key, never prints the value.

## Open questions carried to design

- OQ-2 (one spec or two) — research confirms NEW spec is idiomatic (F4); lean stands.
- OQ-5 (supervise-start source) — confirmed consistent + reuses ON-004 config-inventory home (F5); lean stands.
- NEW: attach-path scope (R2) — pin whether `attach.go:58` is inside the scrub contract.
- NEW: all-three-sites obligation (R1) — C3 must enumerate shim + attach + supervisor.
