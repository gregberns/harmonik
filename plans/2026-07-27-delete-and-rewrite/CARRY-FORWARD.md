# Carry-forward facts

Irreducible facts about the **outside world** — tmux, terminals, the agent CLIs, SSH —
discovered the expensive way. These are not requirements of our design; they are
constraints any implementation must satisfy. A rewrite does not prevent a single one.

Source: bug archaeology across ~53 defects in `internal/daemon/pasteinject.go` (56 commits).
Each fact cost at least one production incident.

**Status:** harvested from `pasteinject`, `tmuxsubstrate`, `codexwire`/`codexdriver`,
`harness/pi`, `brcli`, and the srt sandbox gate. **89 facts** — one is since retracted,
see pane fact 4. **214 structure-caused bugs were discarded** in the
process — those are the ones a rewrite makes unrepresentable.

**Staleness sweep 2026-07-30: nothing on this page was found false.** The fact count is 89 by
count (20 + 15 + 16 + 15 + 6 + 17). Every symbol and path this page names still resolves:
`internal/lifecycle/tmux/buffername.go` `BufferName`, `internal/daemon/sandboxprofile.go`
`GenerateSandboxProfile`, `internal/brcli/brerror.go` `BrErrorFromExit` and
`BrErrorFromExitCode`, and three `.db` files under `.beads/`. Pane fact 18's budgets all match
live constants in `internal/daemon/pasteinject.go` — `reviewFileTimeout` 10 min,
`reviewFilePerKLineBudget` 10 min, `reviewFileHardCeiling` 60 min, `commitPollTimeout` 30 min,
`commitHardCeiling` 90 min, `launchHeartbeatTimeout` 180 s, `launchSuppressionCeiling` 12 min,
`heartbeatStalenessThreshold` 8 min. Pane fact 9's 300-char bound is `reviewerSeedMaxLen`.
br fact 1's live defect is still live: `BrErrorFromExit` refines only exit 1, so exit 3 still
classifies as `BrDbLocked`. srt fact 5 is right that the write-to-main denial case is gone.
Not re-verified: the external-tool version behaviors (they need the tools, not the repo), and
the 214-bug and ~53-defect archaeology counts.

**Known overlap, not yet consolidated.** The pane-injection and tmux-substrate sections were
harvested independently and genuinely overlap on two things: **pane-PID / process-identity
semantics** (pane facts 5–6 vs tmux facts 7–9) and **malformed-target handling** (pane fact 3
vs tmux fact 1). They do *not* overlap on bracketed paste, `paste-buffer` exit 0, or splash
swallow — those appear only in the pane-injection section. Merge the two real overlaps when
this graduates to `specs/`; pane fact 5 and tmux fact 7 have already been reconciled against
live tmux and now cross-reference each other.

**Graduate this file to `specs/` once stable.** Any rewrite of an agent-substrate subsystem
must satisfy every fact below.

---

# Pane injection

## tmux / terminal

1. **Bracketed paste delivers `\n` as a literal LF byte, never as an Enter key event.** A
   trailing newline in the payload does not submit. Only a bare `send-keys Enter` (not
   `-l`) produces a key event.

2. **`tmux load-buffer` + `paste-buffer` exits 0 as soon as tmux hands the buffer to the
   pane — not when the TUI has rendered it.** Exit 0 is not delivery. The only proof of
   delivery is `capture-pane` showing a marker you know is in the seed's first line.

3. **tmux misparses a slash-bearing target** (`session:some/path/name`) and silently returns
   the *session's active pane*, not the named window's — so a paste can land in a previous
   run's pane. Capture the pane id atomically at creation:
   `tmux new-window -P -F '#{pane_id}'`.

4. ~~An invalid tmux buffer name drops the payload.~~ **RETRACTED — structure-caused, not
   external.** Verified against live tmux: buffer names `Bad_Name.x`, `has/slash` and
   `UPPER` all load successfully (exit 0, payload stored). tmux imposes no such restriction.
   The payload loss came from *our own* validator — `bufferNameRe` enforced with
   `ErrStructural`, which `internal/lifecycle/tmux/buffername.go` states rejects the name
   "before tmux is ever invoked." A rewrite that declines to impose a gratuitous regex
   eliminates this for free. Belongs in the discard pile.

5. **Whether the pane PID is the agent or the shell depends on whether `sh -c`
   exec-optimizes, and BOTH regimes occur.** tmux runs the pane command through `sh -c`.
   A **single simple command** (redirections included) is `exec`'d, so the pane PID *is* the
   agent and `pgrep -P <panepid>` is empty for a *healthy* agent. A **compound or
   multi-statement command** leaves the shell in place as parent, so the pane PID is the
   shell and `comm` reads `zsh`/`sh`. Verified live:

   | pane command | `comm` of pane PID | `pgrep -P` |
   |---|---|---|
   | `sleep 300` | `sleep` (the agent) | empty |
   | `sleep 300 < /dev/null` | `sleep` (the agent) | empty |
   | `sleep 300; true` | `zsh` (the shell) | the child |

   Note the `< /dev/null` mandated by tmux fact 6 does **not** flip the regime — the natural
   but wrong guess. Liveness must therefore accept a descendant **or** the pane PID itself
   when its `comm` matches the agent binary, and must not assume either regime. See tmux
   fact 7 for the kill-side consequence.

6. **"Pane has an active child process" means alive, not working.** An idle agent sitting
   at a prompt is indistinguishable from a working one. Liveness alone must never extend a
   budget indefinitely.

## Claude Code / agent CLI

7. **The welcome splash is an ink/React TUI that processes only key events.** It must be
   dismissed with a real Enter before a paste, and under concurrent cold-boots it takes
   **>750 ms** to clear — a single submit Enter at a fixed delay gets swallowed. Send the
   submit Enter with bounded retries; redundant Enters at a submitted REPL are harmless
   empty lines.

8. **While the TUI is absorbing a bracketed paste it swallows keystrokes.** A retry loop
   entirely inside that absorption window (3 attempts / 800 ms) helps nothing. Wait *after*
   the write, plus a late one-shot re-seed Enter (~75 s) as a net.

9. **The TUI collapses a long single-line paste into a `[Pasted text #N]` chip**, so the
   literal text never renders and capture-based verification fails deterministically. Keep
   the seed short (currently bounded at 300 chars) and push detail into a file the agent
   reads.
   **⚠ The collapse threshold changed under us in a Claude Code update (~2026-07-11) and
   wedged the whole fleet.** This is an external dependency that moves — no rewrite
   protects against it. Detect, do not assume.

10. **On `claude --resume <id>` the REPL input handler is intermittently not yet ready**;
    the first Enter is dropped.

11. **Sending a second message before the agent returns to the input prompt drops it
    silently.** There is no ready signal. The only reliable pattern is one paste, one Enter.

12. **The initial context-load/planning phase runs 8–10 minutes emitting nothing** — no
    heartbeat, no output, no file change — and is indistinguishable from a dead pane on any
    single signal. Three independent signals are required: heartbeats, worktree fingerprint
    (HEAD + `git status --porcelain`), and pane output growth (`history_size` + `cursor_y`).
    A read-heavy agent trips only the third.

13. **Claude Code's Stop hook fires on session exit only, not per response.** The agent
    stays alive at the REPL after finishing; the daemon must inject `/quit` itself, and must
    still force-kill after a grace because `/quit` does not reliably end the pane.

14. **The agent's Write tool creates the file before flushing.** Acting on `stat()`
    existence kills it mid-write and permanently truncates the output. Parse for a complete,
    valid document before acting.

15. **An LLM hand-writing JSON produces invalid JSON** (a backtick in a string became a
    stray `` \` ``). Give it a CLI that marshals instead of asking for literal JSON.

16. **A different harness has a different pane surface entirely** — Pi/codex emit NDJSON, or
    have no pane at all. Pane-scrape verification and paste injection must be gated on
    harness capability, never assumed.

17. **Reviewer agents will run destructive git commands on the shared repo**, approve
    partial "all-X" changes, and miss named identifiers unless the prompt forbids or
    requires each explicitly.

## Budgets — empirical, not derivable

18. Every one of these was set by a production false-kill, several twice:
    - Reviewer: ~10 min base **+ 10 min per 1000 changed lines**, ceiling 60 min
    - Implementer: 30 min per-progress budget, **90 min absolute hard ceiling**
    - Launch window: 180 s · launch-suppression ceiling: 12 min · heartbeat staleness: 8 min

## SSH / remote

19. **A shared SSH `ControlMaster` under churn silently drops multiplexed
    `load-buffer`/`paste-buffer`.** Pin the tmux runner to
    `ControlMaster=no -o ControlPath=none`.

20. **…but then each `capture-pane` is a fresh connection**, so transient capture failures
    become common. Distinguish "capture failed (infra)" from "marker absent (paste lost)" —
    treating them alike killed runs whose paste had landed.

---

# tmux substrate

1. **`tmux` accepts a malformed `-t` target and silently answers about a different pane — exit 0, no error.** A window name containing `/` makes `display-message -t session:bead/i2 '#{pane_pid}'` fall back to the session's *currently-active* pane, so under concurrent spawns every run captured a sibling's PID and ended its phase when the sibling exited. Target the slash-free pane ID (`%NNNN`), never a `session:window` handle. (`paste-buffer` against `session:name.0` does exit 1 — the failure mode differs per subcommand.)

2. **The pane ID must be captured in the same invocation that creates the window: `tmux new-window -P -F "#{pane_id}"`.** A follow-up `list-panes`/`display-message` lookup races a concurrent spawn and returns a stale pane (observed `%22` instead of the fresh `%27`).

3. **`tmux new-window` can block forever with no return value and no error.** Under a busy server or FD pressure the exec never completes, so any caller that awaits it wedges; we ran to a 30-min budget expiry with `launch_stall_detected`. The call must be externally bounded — there is no tmux-side timeout.

4. **The tmux server holds one GLOBAL command lock; concurrent `new-window` execs serialize, and under load one crawled ~16 minutes behind the other.** A single spawn never reveals this. Window creation must be serialized process-wide and rate-staggered.

5. **`tmux new-window -- <cmd>` runs the command through `sh -c`, which re-word-splits it.** Joining argv with spaces shattered a multi-word codex seed prompt into 15 tokens; codex replied `error: unexpected argument` and exited ~3.5 s. Shell-quote each argv element before joining.

6. **A tmux pane's PTY never delivers EOF on stdin.** Non-TUI CLIs that read stdin to completion hang forever. Such spawns must append `< /dev/null`.

7. **tmux `setsid()`s every pane, so the pane PID is always its own process-group leader.** Kill must therefore target `-pgid`, not the positive PID. (Observed in the compound-command regime: signalling the positive PID killed the shell and reparented the agent to init, where it survived 40+ minutes burning CPU. That observation is the justification for `-pgid`, not a claim about both regimes.) **In the compound-command regime** (see pane fact 5), the post-SIGTERM poll must probe the *group*: the shell dies on the first TERM while a TERM-ignoring child lives, so polling the leader reads ESRCH and skips the SIGKILL that actually reaps it. In the simple-command regime there is no shell and no child, and that sequence cannot occur.
   **Prescription, since the regime is often not knowable at spawn time: kill by `-pgid` and probe the group, always.** Both are correct in the simple regime and merely redundant there; branching on a regime you cannot detect is the actual hazard.

8. **`kill(-pid, …)` with pid 0 signals the caller's own process group and pid 1 signals everything the caller may signal.** A `pid > 0` guard is insufficient; the guard must be `pid > 1`.

9. **`kill(pid, 0)` errno is load-bearing: ESRCH means dead, EPERM means alive-but-not-ours.** Treating any error as "dead" reports a false clean exit. A zombie also answers as live until reaped.

10. **`tmux kill-window` does not reliably signal the pane's children and can leave the OS process alive as a zombie while the pane is gone.** Pane presence and process liveness are independent signals; you need both or `Wait` hangs on a window that no longer exists.

11. **A remote worker's pane PID does not exist in the local process table.** `kill(workerPID, 0)` on the daemon host returned ESRCH on the first 500 ms tick, producing an instant false "clean exit" and worktree teardown under a live agent — on every remote run. Remote liveness must resolve on the worker's own tmux server.

12. **`ssh` exits 255 on transport failure, and that is the only reliable discriminator from a genuine remote-command failure (tmux's own exit 1).** An SSH drop mid-run must latch "unknown", not "exited cleanly". The code arrives wrapped, so a bare `*exec.ExitError` check misses it.

13. **A remote box has no tmux server until something starts one.** Remote spawns must `EnsureSession` first, and orphan sweeps that detect "idle" by a hardcoded shell name (`zsh`) misclassify bash/sh panes and reap live sessions. Any single shared spawn-target session is a fleet-wide SPOF: one external `kill-session` broke all dispatch for ~70 minutes.

14. **`tmux -V` prints non-numeric versions for development builds — `next-3.6`, `master`, `openbsd-7.4`.** A naive major-version parse rejects a modern tmux. ⚠ Version-dependent.

15. **Cold-start latency of a hosted agent CLI is the dominant timing constant, and it scales with concurrency and disk pressure.** At `--max-concurrent ≥ 4` with disk ≥90% used, agents exceeded a 30 s readiness timeout (raised 30 → 90 → 150 s). Readiness must be driven by observed output, not a fixed deadline; `display-message -p "#{history_size} #{cursor_y}"` is a working activity heartbeat.

---

# codex harness

1. **⚠ VERSION CHURN IS THE PRIMARY HAZARD — the codex CLI has already removed a working flag and changed a hang behaviour under us.** 0.139.0 dropped `-a`/`--ask-for-approval` from `exec`, blocking every run; on 0.139.0 a `--model`-less `codex exec` hung ~30 min printing `Reading additional input from stdin...`, but 0.142.5 completes in seconds. Every flag and enum below is evidence from a pinned version and must be re-verified on upgrade. The app-server schema is regenerable: `codex app-server generate-json-schema --out <DIR>`.

2. **Under ChatGPT-subscription auth every explicitly named `--model` is rejected HTTP 400, and the account's server-side default can outrun the installed CLI.** `o4-mini` → "not supported when using Codex with a ChatGPT account"; `gpt-5` → "not found"; unpinned default `gpt-5.6-sol` → "requires a newer version of Codex". Omitting `--model` is the only reliable invocation, and it is still not stable — the default rotates with no local change.

3. **`--sandbox workspace-write` makes codex's own `git commit` fail 100% of the time inside a linked git worktree, and codex still exits 0 with no commit — a silent no-op.** The worktree's gitdir resolves outside the sandbox root, producing seatbelt EPERM on `index.lock`. The only escape is naming `<repo>/.git` itself as a writable root.

4. **⚠ PLATFORM-DEPENDENT: on Linux bubblewrap, `.git` and its resolved gitdir are force-mounted read-only *after* writable roots are applied, defeating even the direct grant.** macOS seatbelt is unaffected. Upgrading codex does not fix worktree commits; on Linux it makes them impossible.

5. **`$CODEX_HOME` is process-global shared SQLite state with no advisory locking, and a stale `state_*.sqlite-wal` left by a SIGKILLed codex fast-fails the NEXT launch in under 10 seconds.** Staleness is size-independent, defeating any size gate. Mitigations: `--ephemeral`, or a per-worker `CODEX_HOME`. This is the strongest argument for graceful `turn/interrupt` over SIGKILL.

6. **The app-server's JSON-RPC is non-conformant twice over: server→client frames omit `jsonrpc` entirely, and there is no `type` discriminator.** Frame kind must be inferred from which of `id`/`method`/`result`/`error` are present. `RequestId = string | number`, so ids must be kept as opaque raw JSON and round-tripped byte-for-byte.

7. **The server sends JSON-RPC *requests* to the client, and silently dropping one hangs the turn forever.** Ten exist, including `item/commandExecution/requestApproval` and `item/tool/call`. Replying `-32601` for that id unblocks it deterministically. These can arrive *after* the client begins closing stdin, so stdin must stay open until the turn is terminal.

8. **`turn/completed` is not a success signal and carries no items.** Read `turn.status` ∈ `completed|interrupted|failed|inProgress`. `turn.items` is always `[]` with `itemsView:"notLoaded"` on the live stream, so content must accumulate from `item/completed` — and `thread/status/changed → idle` arrives *before* `turn/completed`, so treating idle as terminal races the real one.

9. **The server mints thread ids and `thread/resume` rejoins a *running* thread, not just a dead one.** An idle thread with no subscribers is evicted after 30 minutes; backpressure surfaces as `-32001` "Server overloaded"; any request before the `initialize`→`initialized` handshake is rejected.

10. **`codex exec resume <thread_id>` hard-rejects `-C`, `--sandbox`, and `--add-dir` with exit 2; only the global `-c key=value` override works on both subcommands.** Working directory on resume must come from the subprocess CWD. Multi-turn is a fresh process per turn, not injection into a live session.

11. **`codex exec --json` emits many unmodeled types that must not abort the stream, and `thread.started` can appear more than once** (a resumed turn re-emits it), so first-wins is required. `turn.completed` arrives strictly later, so a scanner that stops at thread-id capture loses all token accounting. `--json` and `--output-schema` are silently ignored when MCP servers are active.

12. **codex has no readiness handshake, no live REPL, and no rate-limit signal — it self-terminates on turn completion and the exit code is the done-signal.** Any dispatch state machine must skip the ready edge entirely. Stdin must be `/dev/null`; a tmux PTY blocks it indefinitely and yields a nil stdout handle.

13. **"codex did no work" has a measured signature: exit 0 in 3.3–5.2 s with a clean worktree, versus 28.9–55.3 s for a real run (10 samples, zero overlap).** Duration alone is unsafe — conjoin it with "produced no commit". codex frequently ignores an explicit commit instruction, and when it does commit it runs `git add -A`.

14. **Under codex's default approval policy a client that negotiates no approval capabilities gets every exec/apply-patch prompt auto-declined, so no writes ever land.** `approvalPolicy: "never"` alongside the sandbox posture is what makes a headless run produce work.

15. **⚠ Billing path is decided by three mechanisms whose precedence codex does not document and has changed across versions.** A ChatGPT sign-in has historically auto-generated an org key that still routes to API billing, so stripping env is a floor, not proof — only the usage dashboard confirms.

16. **Small protocol gotchas that cost real debugging time:** `turn/start` text input REQUIRES `text_elements: []` (snake_case in an otherwise camelCase protocol); `runtimeWorkspaceRoots` *replaces* rather than adds; turn timestamps are Unix **seconds** while item timestamps are **milliseconds**, distinguished only by field name; cancel is `turn/interrupt`, absent from the published method registry; codex emits substantive diagnostics on **stderr** only while the wire reports exit 0 and silence.

---

# pi harness

1. **`pi --mode json` streams NDJSON on stdout and the session id arrives only on the very first line.** There is no pane, no log file, and no flag to print it — an implementation must force an exec substrate with a real stdout pipe and intercept bytes in-stream. The captured id replays as `--session <id>`.

2. **`agent_end` means logical completion; the pi process frequently does NOT exit.** Upstream issues are open, not fixed. Key completion on the `agent_end` NDJSON line plus a harness-issued Kill. Exit codes are undocumented — never key completion on them.

3. **`pi -p --mode json` blocks forever in `epoll_wait` unless fd 0 sees EOF.** Any argv-driven launch must close/`/dev/null` stdin, and that flag has to survive every spec-copying layer or pi hangs ~30 minutes on the PTY.

4. **Token usage is emitted in three inconsistent shapes** — nested under `message.usage` at `message_start`, top-level at `message_end`, and requiring a sum across `messages[]` at `agent_end`. A parser assuming one location silently reports zero.

5. **pi auto-loads every extension under `.pi/extensions/*` from the operator's home on every launch.** An extension that shells out to the dispatcher re-spawns pi each turn — a genuine fork bomb we hit. `--no-extensions` must be on initial AND resume argv.

6. **pi takes its task as a positional argv string; there is no injection channel into a running or resumed session.** A resume turn that reuses the initial prompt makes the model believe it is already done, so any new instruction must be rewritten into the resume argv prompt.

7. **pi is unsandboxed and CWD-relative: no `--sandbox`, no `-C`.** Its `bash` tool can run `git commit` directly (unlike codex), and `edit`/`write` resolve against the process CWD, so the child's working directory is the only way to scope it.

8. **pi is a Node CLI with a `#!/usr/bin/env node` shebang, so an empty child PATH kills it with exit 127 before any NDJSON is emitted.** libc's fallback PATH excludes `/opt/homebrew/bin`; the failure looks identical to "agent did nothing".

9. **A non-default OpenAI-compatible endpoint can only be configured through a `models.json` located by `PI_CODING_AGENT_DIR` — there is no base-URL flag.** `api` defaults to `"openai"`; vLLM/DGX tool-calling required `"openai-completions"`. The model `id` in the file is the substring after the LAST `/` of `--model provider/id`.

10. **Every non-selected `*_API_KEY` must be *empty-overridden*, not merely omitted**, because the tmux `-e` transport is additive and would leave the server's value intact. `--api-key` exists but leaks via `ps`/argv — env only.

11. **pi refuses a model id that does not belong to the selected provider and fails fast (~3–4 s, no commit, no HEAD advance) rather than erroring loudly.** Any fleet-wide model default leaking into a pi launch produces a silent no-progress run.

12. **A failing pi run's diagnosis exists only on stderr and in stdout outside the NDJSON contract.** Fast-fail exits are exit-0-no-commit, so an implementation that discards stderr and cleans the worktree destroys all evidence. Tee both streams to disk before cleanup.

13. **⚠ OpenRouter free-tier limits are hard external policy** — 20 req/min for any `:free` variant, 50/day under $10 lifetime credit, 1000/day at ≥$10. Free endpoints also disappear without notice (404 / "no allowed providers"). This can change at any time.

14. **What pi emits on a 429 is still unknown.** Most plausibly `auto_retry_start`/`auto_retry_end`; pi may swallow the HTTP status entirely. Any rate-limit detector must be written as an inference, not a field read.

15. **⚠ macOS Seatbelt denies DIRECT sockets to `no_proxy` addresses regardless of the domain allowlist.** A model server on `192.168.1.86:8551` falls inside the default no_proxy set, bypasses the MITM proxy, and gets "Operation not permitted" — zero inbound requests, ~4 s failure. A remote LAN host stays blocked; a loopback SSH tunnel is the only working route.

---

# srt sandbox

srt is the sandbox wrapper that runs a harnessed agent under macOS seatbelt or Linux
bubblewrap. These six facts all sit at the temp directory, because that is the one
place the sandbox's write grants and the host's shared scratch state meet.

1. **`os.TempDir()` returns `$TMPDIR`, and falls back to the world-shared `/tmp` when
   `$TMPDIR` is not set.** macOS gives each login session a private `/var/folders/...`
   temp dir, so the same line of code reads a private path on a desktop and the shared
   root on a daemon started without that environment. Treat the shared-root case as
   routine here rather than exotic: this repo's own `go test` recipes in `Makefile` set
   `TMPDIR=/tmp` to keep socket paths short.

2. **Never feed an ambient temp dir into a sandbox write grant — srt expands each
   temp-dir entry into a RECURSIVE write rule.** One shared root therefore hands the run
   write access to every other process's scratch state, and to any socket or lock file
   that lives there. That is a hole in a mechanism whose only purpose is confinement. A
   grant must name a per-run directory such as `/tmp/harmonik-run-<id>`. On the harmonik
   side this is now mechanical rather than a review rule: `GenerateSandboxProfile` in
   `internal/daemon/sandboxprofile.go` rejects a world-shared root and fails the launch
   with a named error.

3. **srt 1.0.0 sets `TMPDIR=/tmp/claude` for every sandboxed child, whatever the
   parent's `TMPDIR` holds, and the child's own children inherit it.** So a consumer
   that honors `TMPDIR` never reaches the host temp root, with or without a grant. Grant
   `/tmp/claude` on its own merits. Do not widen the profile to the root above it.
   ⚠ Version-dependent: the value belongs to srt, not to us.

4. **Not every consumer honors `TMPDIR`, so a `TMPDIR`-derived grant covers the rest
   only by accident.** C's `tmpfile()` and `P_tmpdir`, and any `mkstemp("/tmp/...")`,
   hardcode a host temp root. tmux reads `TMUX_TMPDIR`, not `TMPDIR`, for its socket at
   `/tmp/tmux-<uid>`. Such a consumer falls inside a `TMPDIR`-derived grant only when
   the ambient value happens to equal the root it hardcodes — that is, under
   `TMPDIR=/tmp` or an unset `TMPDIR`, never under the macOS per-user default. Widening
   the grant to reach these consumers is the wrong trade, because it opens the whole
   shared root for the processes the sandbox exists to confine.

5. **A sandbox write-denial test that fails only under load can be a profile that is too
   wide, not a sandbox that fails to apply.** Measured: a write-to-main denial acceptance
   case failed 3 of 3 runs with `TMPDIR=/tmp` and passed 3 of 3 runs with a per-user temp
   dir, at load average 7.53 — the band in which the "srt fails to apply under fork
   saturation" theory predicted a failure. srt was applying the profile correctly the
   whole time. The profile granted the shared temp root, and the test's own fixture sat
   inside that grant. Do not go looking for the acceptance case — it went in the
   signature-pinning test deletion and no longer exists in the tree. The measurement is
   recorded here because the test that produced it is gone.

6. **⚠ PLATFORM-DEPENDENT — fact 3 is established on macOS only.** The `/tmp/claude`
   value is hardcoded with no `GOOS` gate, and the write-denial acceptance suite ran on
   darwin alone. Whether srt injects the same `TMPDIR` on Linux is untested, so fact 3's
   cover on Linux rests on a property of srt that nobody has measured there. Linux is
   uncertified either way.

---

# beads CLI (br)

1. **Real `br` 0.2.10 returns exit 3 — not exit 1 — for "Issue not found", and exit 1 is a *generic* failure code.** Verified live: `br show <missing>` → exit 3, empty stdout, stderr `Error: Issue not found:`. Exit 3 also means SQLite-busy, so **exit 3 alone cannot separate a missing bead from lock contention** — it must be refined by stderr (`"not found"` → NotFound, else DbLocked).
   *Live defect in our code, confirmed on review:* `BrErrorFromExit` already stderr-refines exit **1**, and every production call site uses it, so the inverted table in `BrErrorFromExitCode` is effectively dead. The half that is live and wrong is **exit 3, which gets no stderr refinement** — a genuine not-found is classified `BrDbLocked`, routing to Cat-0 infrastructure handling, bounded retry, and potentially daemon exit 8. Filed separately.

2. **On non-zero exit `br` writes its JSON error envelope to STDERR and leaves stdout EMPTY.** Any parser looking for it on stdout never fires against the real binary. Also: `br` exits 0 *with* stderr text, exits non-zero with *empty* stderr, exits 2 on argparse errors, and exits 101 on a Rust panic whose backtrace can exceed 1 MiB — cap stderr capture.

3. **The JSON flag has four spellings depending on subcommand, and the response three shapes.** `show`/`dep list`/`ready` take trailing `--format json`; `list` takes trailing `--json`; `audit log` requires the GLOBAL `--json` as the FIRST argv token; `--version` has no JSON mode. Shapes: `show` → array-of-one; `ready`/`dep list` → flat array; `list` → `{"issues":[…]}`.

4. **Field names are inconsistent across `br` surfaces for the same concept.** The body is always `description` (`--body` is a create-only alias). Edge kind is `dependency_type` in `show` but `type` in `dep list`. `show` emits a top-level `parent` that duplicates the `parent-child` entry inside `dependencies[]` — double-counts if you read both.

5. **`br ready` defaults to `--sort hybrid`, which weights bead age and will rank an older P1 above a newer P0 — and it PAGINATES by default.** An empty `br ready` is therefore not evidence that no work exists. Use `br ready --sort priority --limit 0`. Priority is numerically inverted (P0=0 outranks P1=1).

6. **`br update <id> --claim` hard-rejects any bead that already has an assignee: exit 4.** This fires routinely because `br create --assignee <crew>` is a normal pattern; `--status in_progress` performs the same transition with no precondition. Symmetrically, `br reopen` handles only closed→open and **silently no-ops on an `in_progress` bead**.

7. **`br` takes `fcntl LOCK_EX` on `.beads/.write.lock` before every write with NO acquisition timeout — it blocks indefinitely — and the DB ships `PRAGMA busy_timeout = 0`.** Concurrent `br close` calls return exit 3 to losers *even when one succeeded*, so a DB-locked exhaustion must be confirmed with `br show`, not treated as failure.

8. **Immediately after a process restart, a lingering write transaction from the dead process leaves `br show` returning exit 3 with empty stdout** — observed as ~31 consecutive failures in one restart window. `br sync --flush-only` first forces a full round-trip and clears it.

9. **`br` copies the ENTIRE `issues.jsonl` into `.beads/.br_history/` on every write command — a pre-write snapshot, never a delta.** At ~7 MB/ledger and ~1 snapshot/minute under active dispatch that is ~420 MB/hour; it reached **25 GiB across 15,072 snapshots and filled a 256 GB disk**. `br history prune` exists but **`br` never prunes automatically**.

10. **The store is dual — SQLite authoritative, `issues.jsonl` derived — and they demonstrably diverge.** Observed: DB held 2083 closed beads while the JSONL had 2065, so a rebuild-from-JSONL would have silently reverted 18 closes. Import guards (conflict markers, malformed JSONL) are NOT bypassable; export guards are.

11. **`issues.jsonl` is rewritten in place, sorted by id — not append-only — which makes git line-conflicts structural.** Two agents closing the same bead concurrently left conflict markers that `br`'s unbypassable import guard then rejected outright.

12. **`br` never runs git — no commits, no hooks, no merge driver — and its own `sync --merge` treats `labels` and `dependencies` as opaque last-writer-wins scalars**, so concurrent label additions drop one side. A custom union merge driver only fires if BOTH `.gitattributes` names it AND `merge.<name>.driver` is in per-clone local config, which is never committed.

13. **Because `br` flushes continuously, a git-TRACKED ledger keeps the working tree perpetually dirty and breaks real merges** — a live run failed at its final merge with `cannot rebase: You have unstaged changes`. Untracking it is the fix, at the cost that beads never travel between clones.

14. **`br` auto-discovers its DB by globbing `.beads/*.db` upward from the process working directory**, so an unpinned CWD silently finds the wrong DB or none. Every invocation must set `cmd.Dir`. Live hazard: this repo's `.beads/` holds three `.db` files with identical schemas; which one wins is undetermined.

15. **Write latency scales with total ledger size because every write copies the whole ledger.** Archiving 2086 closed beads took `br` from repeated 10 s timeouts to 0.10 s. Regrowth is ~70 beads/day here, so this is recurring, not one-time.

16. **Version skew against `br` is benign in practice and must not be a hard gate.** The fleet completed 754 runs on `br` 0.2.10 while pinned at 0.1.45 with zero adapter failures; exact-match enforcement was the sole blocker on every daemon restart in that period.

17. **`br` writes carry no idempotency key or request id, so an interrupted write is indistinguishable from one never issued.** Any exactly-once discipline must be built outside `br`. Note `br update --claim` is atomic and re-claiming an already-`in_progress` bead is safe.

⚠ **Version-dependence:** br facts 1, 3, 4, 5, 6, 9, 12 are pinned to `br` 0.2.10's CLI surface and have each changed once already.

---

## The shape of the problem

Three patterns run through all 89 facts. They are the actual design constraints.

**1. No external tool gives you an acknowledgement.** Pane-injection facts 7, 8, 10 and 11
share one root cause — the target TUI exposes no readiness or ack signal. codex fact 12 says
the same of the CLI (no readiness handshake, no rate-limit signal); pi fact 2 says logical
completion and process exit are different events. Ordering cannot be enforced by types on
our side of the boundary.

> **Design consequence:** every outbound operation is
> **act → verify by observation → retry with bound**, never **act → assume**.
> Any design that treats delivery, completion, or liveness as synchronous is wrong before it
> is written.

**2. Exit 0 means "the call returned", never "the work happened."** `tmux paste-buffer`
exits 0 before the TUI renders (pane fact 2). codex exits 0 with no commit when the sandbox
blocks git (codex fact 3) and again when it simply declines to work (codex fact 13). pi
fast-fails exit-0-no-commit on a bad model (pi fact 11). `br` exits 0 with stderr text and
non-zero with empty stderr (br fact 2).

> **Design consequence:** success is always confirmed by a *side effect you go and look at*
> — a commit, a rendered marker, a changed ref — never by a return code.

**3. ⚠ The external tools change under us, and have already done so repeatedly.** The Claude
Code paste-collapse threshold moved in a ~2026-07-11 update and wedged the fleet. codex
removed `--ask-for-approval` and changed a hang behaviour between 0.139 and 0.142. `br`
changed exit codes, JSON flag spellings, and default sort. tmux prints non-numeric versions.

> **Design consequence:** pin nothing you can detect, and hard-fail on nothing you can
> degrade through. Version *equality* checks have already cost more uptime than the skew
> they guarded against (br fact 16). Probe behaviour at startup; do not encode it as a
> constant.

**What this means for the rewrite.** These 89 facts are not a checklist to apply at the end.
Patterns 1 and 2 dictate the *shape* of the core loop, and pattern 3 dictates that harness
behaviour is runtime-discovered configuration rather than compile-time knowledge. A rewrite
that gets the structure right and ignores this document will reproduce roughly half the bug
history — that was the measured split on `pasteinject`: 27 structure-caused, 26 world-caused.
