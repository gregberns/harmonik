# Narrow Port Research

## Questions

- Which methods does the automatic cycle use?
- Which current ports join unrelated sources?
- Which read guards must stay unchanged?

## Findings

The automatic cycle uses pane inject, Escape, and environment writes. Await-ack uses pane capture separately.

The gauge snapshot joins context files, queue state, sleep state, hold files, tmux clients, idle markers, and transcripts. Small source ports should provide raw facts. A shell sampler should compose the existing `GateSnapshot` value.

The activity port needs idle-marker time, last user turn, and last assistant turn. The first decomposition omitted the user turn.

The handoff document and journal have different lifecycles. The shell must preserve the rule that only an opened-journal write failure is fatal.

## Compatibility rules

- Skip sleep reads for an empty session ID.
- Skip operator attachment reads for an empty target.
- Skip transcript reads when the matching policy gate is disabled.
- Recovery must not read unrelated gates when it only needs managed state.
- Preserve nonce scrubbing and all on-disk bytes.

## Risks

A composed snapshot is sequential, not atomic. Documentation and tests must not claim cross-source atomicity.
