# AMENDMENT to `specs/harness-contract.md` (HN) — codex local + host-sandbox posture

> Pass 5 spec draft. ADDITIVE amendment (per HN-023). Inserts a new subsection **§4.10 Codex
> host posture** after §4.9 (HN-023/HN-024). New IDs **HN-025**, **HN-026** (next free after
> HN-024; confirm against `specs/_registry.yaml` at finalize). House idiom matches HN-022.
> Normative text only; rationale lives in `04-design/`.

---

### 4.10 Codex host posture

#### HN-025 — Codex MUST be launchable on the local host

Codex MUST be launchable on the local daemon host via the local runner. Harmonik MUST NOT require
an enabled remote/ssh isolation-boundary worker to be bound as a precondition for launching codex,
and MUST NOT fail a codex launch closed for the sole reason that no such worker is bound. When the
`codexdriver` substrate is selected and no remote worker is configured, codex MUST run on the local
runner — the same host posture as claude.

This supersedes the prior agent-originated fail-closed isolation-boundary rule (removed: the
`CodexRequireIsolationBoundary` guard and the refused-argv0 diagnostic). Any remaining ssh
worker-routing seam is inert and deprecated; its removal is a separate (platform-architecture)
concern and MUST NOT reintroduce a launch precondition.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### HN-026 — Codex runs unsandboxed on the host (`danger-full-access`)

Codex's native sandbox MUST be off: codex MUST run `danger-full-access` on the host, uniform with
the unsandboxed claude posture and with the Pi precedent (PI-015, "the harness runs unsandboxed").
Specifically:

- **S1.** The codex-exec argv MUST set `sandbox_mode = "danger-full-access"` on BOTH the initial
  and the resume launch branches. It MUST NOT carry `workspace-write`, and MUST NOT carry a
  per-harness `writable_roots` carveout (the `.git` carveout is inert under danger-full-access and
  MUST be removed).
- **S2.** The sandbox mode MUST be applied via the `-c sandbox_mode=…` config override, NOT the
  `--sandbox` flag, because `codex exec resume` rejects `--sandbox` (and `--add-dir` / `-C`) with
  an argument-parse error.

Under this posture codex's own `exec_command` shell steps (tests, `git status`, multi-step git) and
its commit write to a linked worktree's `.git` common dir are permitted; the prior EPERM/Seatbelt
denials were artifacts of the `workspace-write` writable-root restriction that no longer applies.
Landing the code diff does not depend on codex self-committing: the daemon's `ensureCodexRefsTrailer`
fallback remains the backstop committer and idempotently no-ops when HEAD already carries the
`Refs:<bead>` trailer.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

> INFORMATIVE: HN-026 records the CURRENT per-harness posture. Security is not dropped — it is
> re-homed. A single uniform harmonik-level command-execution sandbox for all harnesses (see the
> operator-NFR command-execution sandbox invariant ON-024) is a SEPARATE, PARALLEL workstream that
> supersedes this per-harness `danger-full-access` posture when it lands. "Native sandbox off" MUST
> NOT be read as "security dropped."
