# Mega Codex Review — Mechanism Research

> Research doc. Read-only investigation, 2026-07-16. Every command below marked **[VERIFIED]**
> was actually run against `codex-cli 0.142.5` at `/opt/homebrew/bin/codex` on this machine and
> produced the stated result. Claims marked **[UNVERIFIED]** are reasoned, not executed — flagged
> for the adversarial reviewers.

## TL;DR

Drive Codex as a reviewer with **`codex exec`** (non-interactive, one-shot, streams to exit),
**pinning a working model** (`-m gpt-5.5`), in **`--sandbox read-only`**, with a **JSON output
schema** so every finding comes back machine-parseable. One invocation per subsystem directory;
the giant packages (`internal/daemon`, `internal/core`) get sub-chunked. Codex reads the files
itself inside the read-only sandbox — you do **not** stuff source into the prompt. Findings land as
one JSON object per subsystem via `--output-last-message`; a merge script dedupes them against the
Claude lane's findings (same schema).

---

## 1. Recommended mechanism

### The workhorse: `codex exec` **[VERIFIED]**

`codex exec` is the non-interactive path: "Run Codex non-interactively … streams JSONL and
self-terminates on turn completion" (this is also exactly how harmonik's own worker harness invokes
it — see §6). Relevant flags confirmed from `codex exec --help`:

| Flag | Purpose |
|---|---|
| `[PROMPT]` positional, or stdin, or `-` | the review instructions |
| `-m, --model <MODEL>` | **REQUIRED in practice** — the built-in default (`gpt-5.6-sol`) is broken on 0.142.5 (see §5) |
| `-C, --cd <DIR>` | working root the agent operates from |
| `-s, --sandbox read-only` | agent may read the whole disk but cannot mutate the repo — correct posture for review |
| `--output-schema <FILE>` | JSON-Schema the final message must match (OpenAI strict mode) |
| `-o, --output-last-message <FILE>` | write the final assistant message (the JSON) to a file |
| `--json` | stream all events as JSONL to stdout (progress/telemetry; optional) |
| `--skip-git-repo-check` | avoid the "not a git repo" guard on odd CWDs |
| `--ephemeral` | do **not** persist session files to disk — key for parallelism (§4) |
| `--add-dir` | extra writable dirs (not needed for read-only review) |

Note: harmonik's harness comment records that **codex 0.139.0 removed `-a/--ask-for-approval`** for
`exec`; sandboxing is controlled *only* by `--sandbox`. Confirmed — no `-a` on `codex exec --help`.

### Worked example — reviewing one subsystem **[VERIFIED, actually run]**

The schema (`schema.json`) — note `additionalProperties:false` is **mandatory** on every object
(OpenAI strict schema; a schema without it returns HTTP 400 `invalid_json_schema`):

```json
{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "properties": {
          "file":          {"type": "string"},
          "line":          {"type": "integer"},
          "severity":      {"type": "string", "enum": ["critical","high","medium","low","nit"]},
          "category":      {"type": "string"},
          "title":         {"type": "string"},
          "detail":        {"type": "string"},
          "suggested_fix": {"type": "string"}
        },
        "required": ["file","line","severity","category","title","detail","suggested_fix"]
      }
    }
  },
  "required": ["findings"]
}
```

The invocation (this exact form ran and produced valid structured findings):

```bash
codex exec -m gpt-5.5 \
  --cd /Users/gb/github/harmonik \
  --sandbox read-only \
  --skip-git-repo-check \
  --output-schema schema.json \
  --output-last-message findings-apptap.json \
  "You are a senior Go reviewer. Review ONLY the files under internal/apptap/ for
   correctness bugs, resource leaks, concurrency hazards, and error-handling gaps.
   Read the files yourself. Return findings strictly matching the provided JSON schema;
   empty array if none."
```

Result: `findings-apptap.json` contained 3 substantive, real findings (e.g. "Run can hang forever
after child exit while stdin is still open" — a genuine goroutine-leak/deadlock in `tap.go:139`),
all schema-conforming. **17,122 tokens** for a ~145-line target. Exit 0.

### The other path: `codex review` (diff-oriented) **[VERIFIED via --help, not run]**

`codex review` / `codex exec review` is a **diff reviewer**, not a whole-repo static reviewer. Flags
(`codex review --help`): `--uncommitted` (staged+unstaged+untracked), `--base <BRANCH>`,
`--commit <SHA>`, `--title`, plus a custom-instructions PROMPT. Use it for **incremental** review
(e.g. "review everything on `phase1-session-restart-substrate` vs `main`" — currently 106 commits
ahead), NOT for the mega static sweep. It does not take `--output-schema`, so its output is prose,
not structured — worse for merge/dedupe. **Recommendation: use `codex exec` for the mega review;
keep `codex review --base main` as a complementary diff pass.**

---

## 2. Chunking a 202k-LOC codebase

Repo size (non-test Go): **202,390 LOC across 836 files** in `internal/` + `cmd/`. The model
(`gpt-5.5`) has a **272,000-token context window** (from `~/.codex/models_cache.json`). **[VERIFIED]**

**Key insight that changes the math:** in `--sandbox read-only`, Codex *reads files itself* by
running shell (`cat`/`sed`/`rg`) as it works. You do NOT paste source into the prompt — you point it
at a scope. So the context budget is consumed by *what Codex chooses to read during the session*,
not by a pre-stuffed prompt. The 272k window therefore bounds **how much a single `codex exec`
session can read before it must compact** — empirically a single package of a few thousand LOC is
comfortable; tens of thousands of LOC in one session risks context pressure and shallower review.

**Chunk = one `codex exec` per subsystem directory**, sized to keep per-session reading well under
the window. Package sizes (non-test LOC) measured this session:

- **Must sub-chunk** (too big for one session): `internal/daemon/` **56,718**, `internal/core/` **31,541**.
  Split by file-group / feature cluster (e.g. daemon → dispatch, harness, billing/codex, workspace,
  supervisor, events; ~5–8k LOC each) with an explicit file-list in the prompt.
- **One session each** (digestible): `lifecycle` 8.8k, `keeper` 7.1k, `workspace` 6.5k, `queue`
  6.3k, `scenario` 4.8k, `handlercontract` 4.5k, `brcli` 3.8k, `workflow` 3.6k, `handler` 3.4k …
  down through the ~30 smaller packages (each < ~2k LOC).
- Rough plan: ~10–14 sub-chunks for daemon, ~5–6 for core, ~1 per remaining package →
  **~55–65 chunks total.**

**Shared context each chunk should carry** (put in the prompt or a small preamble file the prompt
names): (a) "this is harmonik, a Go multi-agent orchestration daemon"; (b) the locked architectural
decisions and idioms — point at `specs/`, `STATUS.md`, `docs/foundation/…/subsystem-organization.md`
and tell Codex it MAY read them; (c) the review rubric (correctness > concurrency/leaks > spec
drift > error handling > simplification); (d) the exact output schema. Keep the seed short and let
Codex pull context from the repo — that is what read-only sandbox is for.

---

## 3. Capturing findings in a structured, comparable form

- Every `codex exec` chunk writes `findings-<chunk>.json` via `--output-last-message`, conforming to
  the schema in §1. **[VERIFIED — the file is written and valid]**
- The **Claude review lane emits the identical schema** (same fields, same severity enum). That is
  the whole trick to merge/dedupe: one schema, two producers, add a `reviewer` field
  (`"codex"` / `"claude"`) and a `chunk` field per record.
- Merge step (a tiny script, must be written fresh — §6): concatenate all `findings-*.json` arrays,
  tag each with reviewer+chunk, then dedupe on a fuzzy key (`file` + nearby `line` +
  normalized `title`). Records both lanes hit independently → high-confidence; single-lane records →
  needs adjudication. Rank by `severity` then by cross-lane agreement.
- This maps cleanly onto harmonik's existing convention: findings can be filed as beads (once the
  daemon is back on) or dropped into a `findings.md` table. The schema deliberately mirrors the
  `ubs` `file:line:col + fix` shape the project already reads.

---

## 4. Running the Codex lane and the Claude lane in parallel

**Two independent lanes, mostly overlapping scope for cross-check, with a few specializations.**

- **Structure:** N Codex chunks run concurrently as background `codex exec` processes; the Claude
  lane runs its own sub-agents (via this repo's Agent/orchestrator tooling) over the same chunk map.
  Because both consume the same chunk list + same schema, a chunk reviewed by both yields a
  cross-checkable pair.
- **Parallelism hazard (real):** all `codex exec` processes share `CODEX_HOME` (`~/.codex`), which
  holds SQLite session stores (`logs_2.sqlite`, `state_5.sqlite`, WAL files — observed this
  session). Many concurrent writers to one SQLite/WAL can contend or corrupt. **Mitigations:**
  run each chunk with **`--ephemeral`** (don't persist session files) **[VERIFIED flag exists]**,
  and/or give each worker its **own `CODEX_HOME`** (copy `auth.json` + a `config.toml` carrying
  `forced_login_method="chatgpt"` — this is exactly what harmonik's `buildCodexEnv`/billing guard
  do, §6). Start with a modest fan-out (4–6 concurrent) and watch for rate-limit/429s.
- **Where each is stronger (suggested division, not a hard split):**
  - *Codex (gpt-5.5):* strong on localized correctness — concurrency/goroutine leaks, resource
    lifecycle, nil/error-path bugs (demonstrated on `tap.go`). Give it the leaf packages and the
    per-file mechanical sweep.
  - *Claude:* stronger on this repo's *spec-alignment and cross-subsystem* reasoning — it already
    has the AGENT_INDEX/STATUS/skills context and the `agent-reviewer` rubric (spec drift,
    unwanted-abstraction, bead/codename match). Give it the architectural seams, the giant
    `daemon`/`core` cross-cutting concerns, and adjudication of Codex's single-lane findings.
  - **Overlap the high-value packages fully** (harness, queue, dispatch, billing guard) so both
    lanes review them independently — that cross-check is the point of a "massive" review.

---

## 5. Cost / rate / context constraints

- **Auth = ChatGPT subscription, NOT API key. [VERIFIED]** `~/.codex/auth.json` has
  `auth_mode` set, `OPENAI_API_KEY` empty; `~/.codex/config.toml` has `forced_login_method =
  "chatgpt"`. Harmonik enforces this deliberately: there was a real **API-pool burn incident**
  ("project_flywheel_apikey_burn"). `codexbillingguard.go` (positive guard) materializes
  `forced_login_method=chatgpt` and fail-closed asserts the ChatGPT plan; `codexlaunchspec.go`
  (negative guard, `codexCredentialDenyKeys`) strips `OPENAI_API_KEY`/`CODEX_API_KEY` from the child
  env. **Load-bearing constraint: the review lane MUST run under ChatGPT auth with a clean env; a
  stray `OPENAI_API_KEY` in the shell would silently bill the metered API pool.** Before any big
  run, `unset OPENAI_API_KEY CODEX_API_KEY` (or use a per-worker CODEX_HOME as above).
- **Context window: 272k tokens** for gpt-5.5 / gpt-5.4 / gpt-5.4-mini / codex-auto-review;
  gpt-5.3-codex-spark is 128k. **[VERIFIED from models_cache.json]** This bounds per-chunk reading
  (§2).
- **Broken default model. [VERIFIED]** Running `codex exec` with no `-m` picks `gpt-5.6-sol` and
  fails with HTTP 400 "requires a newer version of Codex." **Always pass `-m gpt-5.5`** (or
  `gpt-5.4`, or the cheaper `gpt-5.4-mini` for leaf packages).
- **Token cost data point. [VERIFIED]** 17,122 tokens for a 145-line file. Extrapolation is rough
  because reading scales with what Codex opens, but ~55–65 chunks at, say, 40k–120k tokens each is a
  plausible envelope — meaningful subscription usage, not free. **[UNVERIFIED extrapolation.]**
  Prefer `gpt-5.4-mini` on small packages to conserve.
- **Rate limits:** ChatGPT-plan Codex has usage limits; a large concurrent fan-out can hit them.
  Cap concurrency (4–6), and consider `codex doctor` to check auth/runtime health before a big run.

---

## 6. Reusable vs. build-fresh

**Reusable (knowledge/config, not a callable review API):**
- The **exact invocation contract** — `codex exec --json --sandbox … --model … -C …`, resume via
  `codex exec resume <thread_id>`, `-a` removed in 0.139.0 — is documented and battle-tested in
  `internal/daemon/codexlaunchspec.go` + `codexharness.go`. Copy the argv shape.
- The **billing safety** — `codexbillingguard.go` (`materializeForcedLoginMethod`,
  `assertChatGPTPlan`) and the credential-deny-keys in `codexlaunchspec.go`. Reuse the *posture*
  (force chatgpt, strip API keys) even if you don't import the code.
- `internal/daemon/codexjsonlparser.go` parses `codex exec --json` JSONL (thread.started, token
  usage, final message) — reusable if you choose `--json` streaming over `--output-last-message`.
- `internal/codexwire`, `internal/apptap`, `codexdriver`, etc. exist but are aimed at the
  **app-server** integration (below), not one-shot review.

**Must be built fresh (small):**
- A **driver script** that: enumerates the chunk map, fans out `codex exec` per chunk with the
  schema, collects `findings-*.json`, and merges/dedupes across the Codex and Claude lanes. This is
  ~100 lines of shell/Python — there is no existing "review orchestrator."
- The **review prompt + schema files** (§1) and the shared-context preamble (§2).

**NOT reusable — the `codex-app-server` work is not what we need. [VERIFIED by reading the work]**
`.kerf/works/codex-app-server/` and `plans/2026-07-11-codex-app-server-replan/` are a **research /
design-spec phase** for running a *resident long-context Codex orchestrator* over JSON-RPC (to maybe
retire the keeper). Status: problem-space + decompose + some design docs exist, but `05-changelog`,
`06-integration`, and the spec draft are **still TODO skeletons** — no shipping app-server driver.
For a fire-and-collect review sweep, the app-server path is overkill and unbuilt; `codex exec` is
the right, available tool.

**No Codex agent-type in the Claude harness. [VERIFIED]** `.claude/agents/` holds only
`agent-reviewer.md` and `agent-config-reviewer.md` (both Claude). No codex subagent; `grep codex
.claude/settings*.json` is empty. oh-my-claude (`~/.claude/plugins`) is installed but nothing wires
Codex into it. The Codex lane is driven by shelling out to the `codex` CLI, not by a harness plugin.

---

## 7. Open questions / risks

1. **Context-pressure per chunk [UNVERIFIED].** I verified a 145-line target uses 17k tokens; I did
   NOT measure a multi-thousand-LOC package. If Codex reads too greedily it may compact mid-review
   and go shallow. Needs a calibration run on one ~5k-LOC package before committing the chunk sizes.
2. **Parallel CODEX_HOME contention [UNVERIFIED].** The SQLite-sharing risk is reasoned from the
   observed `~/.codex/*.sqlite*` files, not tested under concurrent load. Validate `--ephemeral`
   and/or per-worker CODEX_HOME with a 4-way fan-out smoke test.
3. **Cost ceiling [UNVERIFIED].** Full-repo token/subscription cost is extrapolated. Run a
   metered pilot (5–10 chunks) and project before the full sweep.
4. **Rate limits on ChatGPT-plan Codex [UNVERIFIED].** Unknown ceiling; a big concurrent sweep may
   throttle. Mitigate with modest concurrency + retries.
5. **Model choice.** gpt-5.5 verified working; gpt-5.4/5.4-mini assumed working (same family, same
   272k window) but not run. gpt-5.6-sol confirmed broken.
6. **Diff vs. static scope.** `codex review --base main` reviews only the 106-commit delta; the
   `codex exec` sweep reviews code as-it-stands. Decide whether the "whole system" mandate means
   static-everything (exec sweep) or delta-since-main (review) — likely both, exec-sweep primary.
7. **Findings volume / merge quality [UNVERIFIED].** Dedupe on fuzzy `file+line+title` may
   over/under-merge; the merge script needs a human spot-check pass.
8. **Verification of Codex findings.** Codex's tap.go findings looked real but were not
   independently confirmed as true bugs — every merged finding still needs adjudication (the Claude
   lane / a human) before it becomes a bead.
