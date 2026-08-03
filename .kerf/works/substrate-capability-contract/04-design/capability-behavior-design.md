# Capability Behavior Design

## Disposition

This planning component has no separate implementation boundary. Its behavior
is the declared capability record in `daemon-contract-design.md`.

## Binding

The record classifies every capability as optional, construction-required, or
operation-required. It also states the absent result and the focused proof for
each consumer. This file adds no second record and no new public contract.

## Reason

The capability behavior is a daemon composition concern. A second design
would duplicate the approved table and could make two sources disagree.
