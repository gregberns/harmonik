# Contract findings

The continuity controller is generic. Attached tmux and structured input are
edge adapters. The existing `InputPort` has no durable controller effect ID.
Ambiguous structured handoff therefore fails closed.
