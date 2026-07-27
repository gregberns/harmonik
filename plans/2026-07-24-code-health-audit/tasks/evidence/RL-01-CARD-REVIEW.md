# RL-01 card review

- Task: `RL-01`
- Card: `tasks/RL-01.md`
- Reviewer: `/root/arch00_review`
- Reviewer profile: `sol_xhigh`
- Verdict: `APPROVE`
- Final reviewed card commit:
  `5299fa39e9ce8cebca6272e8b4da1448675d98bc`

The final card is independently executable and reviewable. It pins the analysis,
integration, reviewloop, L8, and kerf inputs; defines exact artifact schemas and
disposition semantics; separates direct and compatibility-mediated production
reachability; reconciles the 18/29, 19/32, and 26/48 dependency claims; requires
the L8 compile probe; and fails closed on ref, worktree, kerf, source, or lease
drift.

The analysis base and coordinator claim base are deliberately separate. The
worker reads the exact claim from the coordinator-owned `TASK-INDEX.yaml`,
requires it to equal the task-worktree HEAD, and uses it for the zero-source-diff
proof. All builder commands use one coordinator-issued token and the fixed
RL-01 lane cache.

No production behavior, future architecture, task authority, or worktree
mutation is delegated to the worker.
