# Session-Keeper — Live De-Risking Experiments (E1–E4)

**Date:** 2026-06-03 (UTC) · **Claude Code version:** `2.1.161 (Claude Code)`
**Method:** throwaway `claude --dangerously-skip-permissions` session driven inside `tmux -s skexp -x 200 -y 50`, from `/tmp/skexp-dir` with a local `.claude/settings.json` (statusLine + Stop hook). Driver injected slash-commands via `tmux send-keys`. All evidence is from real runs — nothing fabricated.

**Harness artifacts:**
- statusLine probe: `/tmp/exp-statusline.sh` → appends `<ts> sid=<id> pct=<pct>` to `/tmp/exp-statusline.log`; dumps `.context_window` to `/tmp/exp-statusline-cw.log`.
- Stop hook: `date >> /tmp/exp-stop.marker`.
- Resume sentinel: `/tmp/exp-handoff.md` — instructs the resumed agent to run `echo RESUME-SENTINEL-OK`.

**statusLine stdin JSON shape (observed):** top-level keys include `session_id`, `context_window`, `cost`, `cwd`, `model`, `transcript_path`, `version`, `workspace`. The usable gauge field is `.context_window.used_percentage` (integer; also `.context_window.remaining_percentage`, `.context_window.current_usage.{input_tokens,...}`, `.context_window.context_window_size`). Example: `{"context_window_size":1000000,"used_percentage":2,"remaining_percentage":98,...}`. **NOTE for the design:** the field is `.context_window.used_percentage`, NOT `.cost.context_window.used_percentage`; the gauge reads `NA` on a freshly-cleared session until the first real turn populates usage.

---

## E2 (CRITICAL) — does `/clear` flush the pane's pending input queue? → **PASS (resume is NOT dropped)**

The in-place design works. Sending `/clear`⏎ then `/session-resume <path>`⏎ back-to-back does NOT drop the resume — `/clear` cleared the conversation and the very next queued `/session-resume` executed in the same pane, read the handoff, and ran the sentinel.

**Attempt 1 — back-to-back, ZERO settle wait (the hard case):** PASS.
Pane excerpt:
```
❯ /clear
  ⎿  (no content)
❯ /session-resume /tmp/exp-handoff.md
⏺ I'll read the handoff file ...
  Read 1 file
⏺ Bash(echo RESUME-SENTINEL-OK)
  ⎿  RESUME-SENTINEL-OK
⏺ Done. RESUME-SENTINEL-OK — proof-of-life confirmed. Stopping here as instructed.
```

**Attempt 2 — deliberate 5s wait for `/clear` to settle before resume:** PASS (identical outcome, `RESUME-SENTINEL-OK` emitted).

**Both orderings work.** No evidence of the feared "cleared + idle, resume swallowed" failure. `/clear` does not flush subsequently-queued pane input; Claude Code's input queue is FIFO across the `/clear` boundary, so the watcher can fire the two commands as a pair without an inter-command settle delay (a settle delay is harmless belt-and-suspenders, not required).

**Design implication:** the session-keeper's core mechanic — inject `/clear` then `/session-resume <handoff>` into one live pane in place — is empirically sound at v2.1.161; no separate "wait for clear" state is required, though adding one costs nothing.

---

## E1 — does `/clear` mint a NEW session_id visible to the statusLine? → **PASS**

Every `/clear` minted a fresh `session_id`, and the statusLine's stdin JSON reflected it immediately. Across the run the id walked: `d8bc3122…` (boot) → **`/clear`** → `15ad5eac…` → **`/clear`(E2-a)** → `0954aab9…` → **`/clear`(E2-b)** → `76dec454…`.

Log evidence (`/tmp/exp-statusline.log`):
```
...07 sid=d8bc3122-... pct=2        # before first /clear
===CLEAR-BOUNDARY-01:57:20===
...21 sid=15ad5eac-... pct=NA        # statusLine re-ran with NEW sid right after /clear
```

Note: `/session-resume` did **not** itself mint a new id — it runs inside the `/clear`-minted session (E2-a stayed `0954aab9…` across the resume; E2-b stayed `76dec454…`). So the id rotation is caused by `/clear`, not by resume.

**Design implication:** a watcher can detect a successful `/clear` purely by observing `session_id` change in the statusLine stdin — no transcript parsing needed. But it also means session-keyed external state (e.g. a per-session gauge file or transcript path) rotates on every cycle; the keeper must follow the new `session_id` / `transcript_path` after each `/clear`.

---

## E3 — does the statusLine re-run after `/clear` with no assistant message? → **PASS**

The statusLine fired on the bare `/clear` (no prompt, no assistant turn): a new log line appeared at `01:57:21` immediately after the `/clear` at `01:57:20`, carrying the new `sid=15ad5eac…` and `pct=NA` (context reset to empty). The gauge therefore updates on `/clear` alone.

**Design implication:** the keeper's context gauge will correctly observe the post-`/clear` reset (pct → empty/NA, then climbs again) without waiting for a user/assistant turn — the statusLine is a reliable post-clear liveness/gauge signal. Treat `pct=NA`/empty as "freshly cleared, ~0%".

---

## E4 (stretch) — does the Stop hook fire on a true await-input boundary vs mid-response? → **PASS (fires at await-input boundaries)**

The Stop hook fired exactly at conversation-turn completions (await-input boundaries), once per completed turn, not mid-response. Six markers across the run aligned with: the two priming replies, the two E2 resume completions, and the intermediate primes — each at the moment the agent finished and returned to the prompt.

`/tmp/exp-stop.marker`:
```
2026-06-03T01:56:41Z   # after "say hi"
2026-06-03T01:57:07Z   # after "pong"
2026-06-03T01:58:00Z   # after "say ready" prime
2026-06-03T01:58:16Z   # after E2-a resume completed (echo sentinel)
2026-06-03T01:58:36Z   # after "say go" prime
2026-06-03T01:58:51Z   # after E2-b resume completed
```

No marker was written mid-`Warping…`/mid-response; markers only appeared once the pane returned to the idle `❯` prompt.

**Design implication:** the Stop hook is a usable "agent is now idle / awaiting input" edge for the keeper — it can use Stop as the trigger to evaluate the gauge and decide whether to fire a handoff→clear→resume cycle, rather than polling. Caveat not yet tested: behavior when the agent stops to ask a question vs. stops having finished work — both are await-input boundaries and would both fire Stop, so the keeper still needs the gauge (not just Stop) to decide *whether* to cycle.

---

## Summary table

| Exp | Question | Verdict | Key evidence |
|-----|----------|---------|--------------|
| E2  | `/clear` flush pending input? | **PASS** — resume NOT dropped (both back-to-back and with-wait) | `RESUME-SENTINEL-OK` emitted in both attempts |
| E1  | `/clear` mints new session_id to statusLine? | **PASS** | sid `d8bc3122→15ad5eac→0954aab9→76dec454` |
| E3  | statusLine re-runs on bare `/clear`? | **PASS** | new log line at 01:57:21, pct→NA, no assistant turn |
| E4  | Stop hook fires at await-input boundary? | **PASS** | 6 markers, all at idle-prompt return, none mid-response |

**Overall:** the session-keeper in-place mechanic is de-risked at Claude Code `2.1.161`. The critical E2 assumption holds: a watcher can inject `/clear` immediately followed by `/session-resume <handoff>` into one live tmux pane and the resume reliably executes. session_id rotation (E1) and post-clear statusLine re-run (E3) give the watcher clean external signals; the Stop hook (E4) gives an idle-edge trigger. Open follow-ups: (a) confirm at higher real context fills (these ran at ~2%), (b) distinguish "stopped to ask" vs "stopped done" for the Stop-trigger decision, (c) the statusLine pct field is `.context_window.used_percentage` — wire the gauge to that path.
