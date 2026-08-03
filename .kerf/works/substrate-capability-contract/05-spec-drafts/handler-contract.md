# Handler Contract Spec-Draft Disposition

## No normative change

`specs/handler-contract.md` remains unchanged. `handler.Substrate`,
`Session`, and `InputPort` stay narrow public seams.

The declared capability record stays daemon-owned. This work does not move
tmux, pane, session-sweep, or launch capabilities into the handler contract.
