# Adversarial Review — Mega Codex Review Plan (lens: MECHANISM & FEASIBILITY)

Reviewer: adversarial mechanism/feasibility pass. Working tree verified against
`codex-cli 0.142.5`, live `~/.codex`, and `internal/daemon/codexbillingguard.go` /
`codexlaunchspec.go` on 2026-07-16.

## Verdict: **APPROVE-WITH-CHANGES**

The mechanism is real and the auth posture prevents *metered billing* (that is the
danger the plan names, and it is genuinely covered). It is **not** a BLOCK — no
surprise-dollar burn, no unexecutable step. But there is one near-blocking feasibility
fact the plan is factually wrong about (the account is a **free** ChatGPT plan, not a
"subscription"), plus two correctness gaps in the fresh-built merge/collect driver that
will manufacture false-greens if unfixed. The pilot gate must be hardened before Wave 1
fires.

---

## What I verified (evidence, not assertion)

- **codex 0.142.5** present at `/opt/homebrew/bin/codex`. Confirmed.
- **Models real:** `models_cache.json` lists `gpt-5.5`, `gpt-5.4`, `gpt-5.4-mini` (all
  272k window), `gpt-5.3-codex-spark` (128k), `gpt-5.6-sol` (272k), `codex-auto-review`.
  So `-m gpt-5.5` is a real, available model id; `gpt-5.6-sol` exists in cache but the
  research's HTTP-400 claim is plausible (it's the broken default). `gpt-5.4-mini` exists
  but was **never run** — still unverified as working *and* as "cheaper" (see F2).
- **Auth posture:** `~/.codex/auth.json` → `auth_mode: "chatgpt"`, `OPENAI_API_KEY: null`,
  OAuth token set present. `~/.codex/config.toml` is exactly one line:
  `forced_login_method = "chatgpt"`. Shell env has **no** `OPENAI_API_KEY` / `CODEX_API_KEY`
  / `CODEX_HOME` / base-url override. So *at this moment* the metered-billing surface is
  closed on all three paths the guard cares about (env key, auth.json key, config fallback).
- **The billing guard actually asserts less than the plan implies (this is GOOD for the
  free-plan case but changes the framing):** `assertChatGPTPlan` (codexbillingguard.go:212)
  checks only (a) `forced_login_method = "chatgpt"` in config.toml and (b) `auth.json` has
  no populated `OPENAI_API_KEY`. It does **not** inspect `chatgpt_plan_type`. So it is a
  guard against *API-pool billing*, not a guard that a paid plan exists. A free plan passes
  it. Good — but it means the guard gives you **no** protection against the real failure
  mode here, which is quota/throttle, not billing.

## The fact the plan gets wrong

Decoding the live `auth.json` id_token/access_token (last refreshed 2026-07-15):
`"chatgpt_plan_type":"free"`, `"chatgpt_subscription_active_until":null`. **This is a free
ChatGPT account, not a subscription.** The plan (§2.1 "ChatGPT-subscription auth", §7 Q3/Q4)
and the research (§5 "meaningful subscription usage, not free") both assume a paid tier.
Codex *does* run on it (the research's `apptap` sweep succeeded), but a 55–65-chunk sweep at
40k–120k tokens/chunk, fanned out 4–6 wide, is a completely different load than one 17k-token
smoke run — and free-tier Codex limits are the tightest ceiling OpenAI ships. This is the
dominant feasibility risk and it is currently mis-stated as a solved cost question.

---

## Findings (numbered, severity-tagged)

### F1 — [MAJOR, near-blocking] Account is FREE tier; the rate/quota calibration is built on a false "subscription" premise, and there is no mid-sweep abort protocol.

- **Prevents:** a Wave-1 fan-out that throttles to zero after N requests, leaving half the
  Tier-1 units (the ack-free channels, the god-function) silently un-reviewed — or worse,
  reads as "clean" because a throttled chunk produced an empty findings file (see F4).
- **Evidence:** live token → `plan_type:"free"`, `subscription_active_until:null`. Plan §7
  Q4 marks rate limits `[UNVERIFIED]` and mitigates only with "concurrency cap 4–6 + retries"
  — inadequate if the ceiling is a hard per-window free-tier quota that retries cannot beat.
- **Change:** (1) Correct the plan's language: it is a free-tier account, not a subscription.
  (2) Make the §6/§7 pilot a **hard go/no-go gate** that explicitly measures: requests-until-
  first-429, the throttle *reset window* (per-min? per-5h? per-day?), and whether 4–6-wide
  fan-out trips the limit faster than serial. (3) Add a **mid-sweep abort/resume protocol**:
  on sustained 429s the driver must checkpoint which chunks completed, stop (not spin on
  retries), and be resumable — the plan currently says nothing about what happens when a
  limit is hit *during* a sweep. (4) State a fallback if free-tier cannot sustain the sweep
  (upgrade the plan, or serialize + spread over days) since Codex is operator-MANDATORY and
  the whole plan is blocked on this lane's throughput.

### F2 — [MINOR] "`gpt-5.4-mini` to conserve subscription usage" is unverified and possibly a category error.

- **Prevents:** a leaf-package tier that silently fails (mini rejected like `gpt-5.6-sol`
  did) or that saves nothing.
- **Evidence:** `gpt-5.4-mini` is in `models_cache` but was never run (research §7 Q5). On a
  ChatGPT-plan (subscription/free) the meter is message/rate-quota, **not** per-token API
  dollars — a smaller model may not reduce quota consumption at all. "Conserve subscription
  usage" conflates the API-dollar model with the plan-quota model.
- **Change:** in the pilot, actually run one chunk on `gpt-5.4-mini` and confirm (a) it works,
  (b) it consumes less quota per the metric F1 establishes. If quota is per-request, drop the
  mini tier — it adds a quality risk for no quota saving.

### F3 — [MAJOR] The env-unset guardrail is the *weaker* of the two guards; make per-worker `forced_login_method` materialization mandatory, not "optional."

- **Prevents:** a per-worker `CODEX_HOME` (the §2.1/§4 parallelism mitigation) that copies
  `auth.json` but forgets `config.toml`, so a *future* stray `OPENAI_API_KEY` (a different
  shell, a sourced profile) silently routes that worker to metered API — the exact
  `project_flywheel_apikey_burn` failure.
- **Evidence:** the guard comment (codexbillingguard.go:16-17) is explicit that
  `forced_login_method=chatgpt` is what makes "codex itself refuse to fall back to API-key
  login." `unset` only cleans the *current* shell; `forced_login_method` is the durable
  backstop. The plan §2.1 lists env-unset first and treats the config check as a "verify"
  afterthought, and frames per-worker `CODEX_HOME` config as "optional / copy … `config.toml`."
- **Change:** state the invariant as: **every `CODEX_HOME` the sweep uses (shared or
  per-worker) MUST contain `forced_login_method = "chatgpt"` before any `codex exec`**, and
  the driver should materialize/assert it (mirror `runCodexBillingGuard` exactly — reuse the
  code, not just "the posture") and fail-closed if absent. Add a one-time `codex doctor` +
  auth.json-key-empty check to the driver's pre-flight, per chunk, not just once.

### F4 — [MAJOR] The merge driver must treat a missing/empty/unparseable findings file as a FAILED chunk, not as "zero findings."

- **Prevents:** the single most dangerous outcome for a review whose entire thesis is
  "does green mean the product behaves, or that a fake behaved?" — a chunk that 429'd,
  400'd, or crashed leaves `--output-last-message` empty/absent; a naive collector reads
  `[]` and the unit reports **clean**. That is a false-green in the review of false-greens.
- **Evidence:** research verified schema-valid JSON on **one** successful run only. `codex
  exec` writes the final message on success; on model error / throttle the file may be
  missing, empty, or prose. Plan §2.4 / research §3 describe "collect `findings-*.json`" with
  no failure-vs-empty distinction.
- **Change:** the ~100-line driver must, per chunk, distinguish: file-absent/empty →
  **CHUNK-FAILED (re-queue / surface)**; present-but-not-schema-valid → **CHUNK-FAILED**;
  valid `{"findings":[]}` → genuine clean. Log codex exit code + `--json` token/turn
  telemetry per chunk so a truncated/compacted (shallow) review is visible. No chunk counts
  as reviewed without a positively-parsed result object.

### F5 — [MAJOR] Fuzzy dedupe must GROUP, never DROP — over-merge is a false-negative generator.

- **Prevents:** two *distinct* real bugs in the same function at nearby lines with similar
  titles (common in a god-function like `workloop.go` @ 8098 LOC) collapsing into one record;
  the second bug is discarded. A human "spot-check" (§7 Q6) of a large finding set will not
  reliably catch a silent drop.
- **Evidence:** plan §2.4 dedupes on fuzzy `file + nearby line + normalized title` and
  "dedupes … arrays"; §7 Q6 flags over/under-merge but only mitigates with spot-check.
- **Change:** the merge step must be non-destructive: collapse duplicates into a *group*
  that retains every original record (both lanes, all line/title variants) under one ranked
  entry, so nothing is ever removed — only clustered. Confidence = cluster's cross-lane
  membership; the underlying records stay auditable.

### F6 — [MINOR] "Cross-lane agreement = confidence" ranking degenerates for the ~15 single-lane units.

- **Prevents:** mis-ranking. The agreement signal only exists for the 7 BOTH units (§2.5).
  RU-07/11/13/14/16b/17/18/20/21 (Codex-only) and RU-06/10/12/15/16a/19/22 (Claude-only)
  have **no** second lane, so "single-lane → needs adjudication" (§2.4) would flag *every*
  finding from ~15 units as low-confidence, and a miss by the sole assigned lane is an
  unrecoverable silent gap with no cross-check — the plan's headline "cross-lane hits = high
  confidence" simply doesn't apply to most of the codebase.
- **Change:** state two confidence regimes explicitly: (a) BOTH-lane units use cross-lane
  agreement; (b) single-lane units rank by severity + adjudication only, and their findings
  are **not** down-weighted merely for being single-lane (they can't be otherwise). If review
  budget allows, consider promoting one or two structurally-risky single-lane units to BOTH.

### F7 — [MINOR] `--ephemeral` is on the plan's exec line but was **not** on the research's actually-run invocation.

- **Prevents:** assuming the verified command == the planned command. Research §1's
  worked/run example (line 79-89) has **no** `--ephemeral`; the plan §2.1 adds it. The flag
  is confirmed to *exist* (research §4) but the end-to-end "this exact form ran and produced
  valid findings" evidence covers the non-ephemeral form.
- **Change:** in the pilot, run the *exact* planned line (with `--ephemeral` + per-worker
  `CODEX_HOME`) and confirm it still emits a schema-valid file — don't carry the non-ephemeral
  run's success over to the ephemeral form untested. (This is also where F1's fan-out and F3's
  per-worker config get validated together.)

---

## Direct answers to the four attack questions

1. **Auth guardrail sufficient to prevent metered billing?** Yes, as currently configured —
   all three routes (shell env, `auth.json` key, `forced_login_method` fallback) are closed,
   verified live. The guardrail is *sound but under-specified*: it leans on env-unset (weakest
   link) and treats the durable `forced_login_method` backstop as a "verify" rather than a
   mandatory per-`CODEX_HOME` materialization (F3). No hidden metered path found (`config.json`
   holds only an MCP server entry; no base-url/provider override anywhere). The research's auth
   evidence is genuinely end-to-end (I re-ran the inspection). **But** the account is **free
   tier**, not a subscription — the plan's cost framing is wrong (F1).

2. **Calibration pilot adequate?** Design is right in spirit, under-specified in the ways that
   matter. It names the four unknowns but does not say *what to measure* (429 threshold, reset
   window, fan-out multiplier — F1), does not test the *exact* planned invocation (F7), does not
   verify the mini tier (F2), and — critically — has **no mid-sweep abort/resume protocol** if a
   limit is hit during Wave 1. Harden per F1/F2/F7 and make it a hard go/no-go gate.

3. **Merge/dedupe logic sound? False-negative risk?** Core idea sound for the 7 BOTH units; two
   real false-negative generators: over-merge that drops distinct bugs (F5) and errored-chunk-
   reads-as-clean (F4). Single-lane findings are *not* discarded (they're adjudicated), but the
   confidence model doesn't apply to the ~15 single-lane units and needs a second regime (F6).

4. **Output-schema reliability / parse fallback?** Verified once, n=1. `additionalProperties:
   false` requirement is correct (OpenAI strict mode). No fallback specified for missing/empty/
   invalid output — that gap is F4 and is the highest-leverage fix because it converts a lane
   failure into a false "clean."

---

## Bottom line

Executable and safe on the billing axis. Fix F1 (free-tier reality + abort protocol — gate
Wave 1 on it), F3 (mandatory per-worker `forced_login_method`), F4 (failed-chunk ≠ clean), and
F5 (group-don't-drop) before firing. F2/F6/F7 are cheap correctness/clarity fixes for the pilot.
None rise to BLOCK; all four MAJORs are must-fix-before-fan-out.
