# Research findings — Pi (`earendil-works/pi`) machine interface

> Codename pilot · Epic hk-ag97p · Author: kynes (design crew), 2026-06-25.
> External research (WebSearch/WebFetch of the repo README + `packages/coding-agent/docs/`).
> **Confidence labels are load-bearing.** CONFIRMED = quoted from docs; INFERRED = derived from
> documented grammar; UNCONFIRMED = could not verify — treat as a Phase-0 implementation-time
> confirm item, NOT as fact. The design's normative clauses that depend on an UNCONFIRMED item
> carry an explicit "confirm-by-test" obligation.

## 1. Invocation & modes — CONFIRMED
- `pi [options] [@files...] [messages...]`. Four modes: interactive (default), print (`-p`/`--print`),
  `--mode json` (NDJSON event stream), `--mode rpc` (process-integration JSONL over stdin/stdout,
  strict LF framing).
- `--provider <name>` (anthropic, openai, google, openrouter, …) and `--model provider/id` (optional
  `:thinking`/`:high` suffix). `--api-key <key>` overrides env.
- Task input: **positional arg** (`pi --mode json "task"`); `@file` inclusion; piped stdin **in print
  mode only**.
- Resume: `-c`/`--continue` (most recent), `-r`/`--resume` (browse), `--session <path|id>` (specific),
  `--fork <path|id>`, `--no-session` (ephemeral).

## 2. `--mode json` NDJSON — CONFIRMED (schema asserted from docs, confirm-by-test in Phase 0)
- **First line carries the session id:** `{"type":"session","version":3,"id":"<uuid>","timestamp":…,"cwd":"/path"}`.
- Event types: `agent_start`, `agent_end`; `turn_start`, `turn_end`; `message_start`/`_update`/`_end`;
  `tool_execution_start`/`_update`/`_end`; `queue_update`; `auto_compaction_start`/`_end`;
  **`auto_retry_start`/`_end`** (← the likely provider-error/429 channel — see §7).
- **Terminal event = `agent_end`** (`{"type":"agent_end","messages":[…]}`).
- ⚠️ **Load-bearing caveat:** `agent_end` signals *logical* completion but **the process may not exit**
  (§3). The harness must treat `agent_end` as completion and kill the process itself.

## 3. Exit / termination — process-exit UNRELIABLE (CONFIRMED as a known bug class)
- Exit codes are **undocumented** (brief was right). Do NOT key completion on exit code.
- Known non-termination bugs: **#4303** (`pi -p --mode json` never exits with `/dev/null` stdin;
  sits in `epoll_wait`, "blocking any parent daemon"), **#161** (print mode doesn't exit after output),
  **#4942** (CLI doesn't exit after `main()`; Node keeps the process alive). #4303 is closed
  `closed-because-bigrefactor`/`weekend` — i.e. swept, no confirmed fix-and-verify.
- **Design consequence:** key completion on the `agent_end` NDJSON event + harness-imposed kill +
  90m ceiling backstop. (Design §3.4 / PI-014.)

## 4. Credentials — mechanism CONFIRMED; per-provider env names partly CONFIRMED
- Pi reads provider keys via env interpolation in provider config (`"$ENV_VAR"`); `--api-key` overrides.
- CONFIRMED names: `OPENROUTER_API_KEY`, `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `GEMINI_API_KEY` (Google).
- **UNCONFIRMED (Phase-0 confirm items, load-bearing for the billing guard, design §3.6 / PI-040):**
  (a) whether `--provider` is *authoritative* or Pi *sniffs* env to pick a provider — determines
  whether an un-stripped stray key can bill a different provider; (b) whether Pi persists any
  credential/login to disk (a `~/.pi`-style store, the analog of codex `auth.json`) that survives an
  env strip — determines whether the guard needs an on-disk check. Pi also supports interactive OAuth
  via `/login` for some providers (interactive only; not used headless).

## 5. Built-in tools & CWD — CONFIRMED
- Tools: `read`, `bash`, `edit`, `write`, `grep`, `find`, `ls`. Allowlist `--tools`/`-t`,
  `--exclude-tools`/`-xt`, `--no-builtin-tools`.
- Operates on **CWD** (`bash` runs shell, `edit`/`write` are CWD-relative; the `session` header echoes
  `cwd`). **No documented sandbox** → unlike codex, Pi's `bash` can run `git commit` directly. Launch
  with CWD = the worktree.

## 6. Prompt caching — CONFIRMED (moot on free tier)
- Auto per-provider via `cacheControlFormat: "anthropic"`; long-retention via `PI_CACHE_RETENTION=long`.
  Whether OpenRouter/qwen free requests get cache markers is UNCONFIRMED. **Not a justification for the
  plan on the free tier** (no spend to cut).

## 7. OpenRouter limits — CONFIRMED (match the brief)
- **20 req/min** for any `:free` variant (HTTP **429** over-limit).
- **<$10 lifetime credit → 50 `:free` req/day; ≥$10 (one-time) → 1000/day.**
- No-SLA: free endpoints throttle (429), or vanish (404 / "no allowed providers") without notice. The
  429-vs-404 mix per model/moment is provider-dependent and not a stable contract.
- **Rate-limit signal channel (design §8 / PI-071):** the most plausible NDJSON surface is the
  `auto_retry_start`/`_end` events (§2) and/or a tool/agent error event carrying the HTTP status. This
  is **UNCONFIRMED** — a Phase-0/Phase-1 confirm item: verify what Pi emits on a 429 vs a 404 before
  PI-071's `DetectRateLimit` can be implemented; if Pi swallows the status, the harness must infer
  from `auto_retry` cadence + a no-commit `agent_end`.

## Spec-relevant summary
| Need | Mechanism | Status |
|---|---|---|
| Headless one-shot impl (Phase 0) | `pi --mode json "<task>"` | CONFIRMED |
| Session id | `id` in first `{"type":"session"}` line | CONFIRMED |
| Completion | `agent_end` event (NOT process exit) | CONFIRMED + exit-bug caveat |
| Resume | `--session <id>` / `-c` | CONFIRMED |
| Auth | provider key env var (e.g. `OPENROUTER_API_KEY`) | CONFIRMED; provider-autodetect + disk-persist UNCONFIRMED |
| Resident multi-turn server (Phase 1 shim) | `--mode rpc` JSONL | **EXISTENCE CONFIRMED; resident-multi-turn-server semantics UNCONFIRMED** — Phase-1 spike item |
| Rate-limit signal | `auto_retry_*` / error events | UNCONFIRMED — confirm before PI-071 |
</content>
