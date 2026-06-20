# Captain Initiatives Board

> SUPERSEDED 2026-06-20 — the live medium-term lane/epic tracker is now `.harmonik/context/captain-lanes.md`. Kept for history.

> **The high-level tracker.** One line per major initiative: epic id · status ·
> beads done/total. The captain keeps this current at every hourly heartbeat and
> whenever an initiative completes. For task-level detail, see the beads tracker
> (`br show <epic>`); for the lane→crew mapping, see `.claude/skills/captain/SKILL.md §A`.
>
> Last refreshed: 2026-06-10 (post version-tagging + daemon-restart pass).

## Major initiatives

| Initiative (plain English) | Epic | Status | Done/Total |
|---|---|---|---|
| Daemon/infra reliability | `hk-3js5m` | ✅ done (drained; epic open) | ~20/22 |
| Second AI engine (Codex) — *integration* | `hk-w4tmz` | ✅ code done | 8/8 |
| Captain & Crew system | `hk-4adgn` | ✅ done | 3/3 |
| Auto test/CI restoration | `hk-kjkbw` | 🟡 near-done | ~26/27 |
| Logmine self-improvement | `hk-mhmaw` | 🟡 in progress | 1 child OAuth-blocked |
| Release pipeline (versioned releases) | `hk-brc3z` | ⛔ blocked — GitHub perm (tabled ~days) | 4/8 |
| Session-keeper / worker context mgmt | `hk-ekap1` | ✅ mechanism done · 🎯 **testing = priority** | mechanism complete |
| Named-queues (multi-queue) | `hk-tigaf` | ⏸️ parked (superseded) | ~2/11 |

### New initiatives (opened 2026-06-10)

| Initiative | Anchor | Status |
|---|---|---|
| Local version tagging + per-version issue tracking | `scripts/hk-tag-version.sh` | ✅ **live** — `daemon-YYYYMMDD.NN` tags + `found-in:<ver>` labels; report via `scripts/hk-version-report.sh` |
| Codex enablement (run it for real) | `hk-w4tmz` + bug `hk-n5lfz` | ⛔ blocked — needs (1) `-a never` flag-fix `hk-n5lfz`, (2) operator one-time ChatGPT OAuth login |
| Session-keeper crew-restart **testing** | `hk-ekap1` | 🎯 priority — test plan ready, exec on throwaway `kk-test` crew |

## Loose beads to pass on (→ churn crew)

**Spec-drift cluster (P2, ~17 — ideal churn batch, all `kind:spec-drift`):**
`hk-ezo2f hk-emggz hk-cy8rp hk-iljnj hk-bjatv hk-vj96j hk-c6idw hk-3jcqm hk-pbmsq hk-a6e24 hk-p1uz5 hk-u2ko5 hk-ek3fl hk-qv3bc hk-9321v hk-xizhl hk-79x3v`

**Small self-contained wins (P2, ~8):**
`hk-my7y8` (restore-staged index) · `hk-agouy` (gofumpt/gci format) · `hk-jzdy5` (refresh AGENT_INDEX/STATUS) · `hk-4lxne` (captain spec sync) · `hk-7u002` (auto-reap orphan tmux) · `hk-nf111` (comms presence send-only) · `hk-37ra4` (comms `--wake`) · `hk-jzpqo` (crew-start mission-seed Enter)

**Medium daemon-reliability (P2, ~10):**
`hk-gq3my hk-5pwv5 hk-ycxfa hk-a2okh hk-lpbu7 hk-sah87 hk-n7fw3 hk-ycp62 hk-pk3p1 hk-7evda`

**P1 — needs care, NOT churn:**
`hk-h8u7p` (concurrent worktree-add race) · `hk-n5lfz` (codex `-a never` flag-fix) · `hk-805f7` (review-agents-read-only directive, docs)

## Blocked on the operator

- **GitHub `workflow` permission** (`gh auth refresh -s workflow`) — gates the release pipeline + 1 CI bead. *Tabled ~several days* (operator can't grant remotely).
- **Codex ChatGPT login** — one-time interactive OAuth; the agent can relay the auth URL to the operator's phone once they have a subscription.
- **`standard-bead-dot`** next-phase ranking (per-task custom workflows) — the top Phase-3 candidate; awaiting operator's go.
