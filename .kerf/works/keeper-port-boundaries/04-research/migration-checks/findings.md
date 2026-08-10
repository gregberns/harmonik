# Migration Check Research

## Questions

- Which paths need differential tests?
- How can tests expose false-green wiring?
- Which compatibility seams need removal checks?

## Findings

Default parity must compare every policy field, including derived fields and disabled sentinels. Production parity must compare the command composition root with the new production policy.

Differential scenarios should cover success, each transient gate, handoff timeout, clear-confirm timeout, restart recovery, and transcript collision.

Constructor tests should remove one required dependency at a time. They should prove that no effect runs before validation fails.

The incident scenario must write the automation transcript entry produced by handoff injection. It must then add a real operator turn between injection and clear.

## Removal checks

Track the legacy function fields, `fnPane`, `fnGauge`, and `fnHandoff` in an explicit compatibility list. Remove each entry with its migration task.

## Risks

Statement coverage does not prove production wiring. Prefer mutation checks at a real isolated boundary for origin classification and production composition.
