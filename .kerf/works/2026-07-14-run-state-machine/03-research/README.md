# 03-research — two convergent sets (coordination note, 2026-07-14)

Two research sets coexist here after a concurrent-session collision:

1. **`c1-…/ … c6-…/`** — the per-component set (one findings.md per 02-components
   component), independently reviewed in `../research-review.md` (verdict:
   APPROVE, 8 load-bearing line-cites spot-checked exact).
2. **`merge-queue/ workloop-ports/ runexec/ liveness/`** — the synthesis set
   (four cross-component dossiers) that the **04-design docs cite by section**
   (as MF/PF/RF/LF). Restored from `.history/2026-07-14T15:24:33Z` after the
   collision replaced them.

The two sets were produced independently and **agree on every load-bearing
fact** checked (mergeMu regions + the escape/reset hk-zguy6 coupling; the two
dead workLoopDeps fields; the 85-field census and RUN cut; the byte-identical
exit-0/agent_completed terminal blocks; the 2s resume caulk's missing run_id;
the DOT path's missing caulk). Where granularity differs, the c* set is the
finer census; the synthesis set is what the design's section-anchored citations
resolve against. No contradiction between them is known; if one is found, the
working tree is authoritative — re-verify against source.
