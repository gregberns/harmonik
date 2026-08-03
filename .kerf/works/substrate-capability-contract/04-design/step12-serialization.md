# Design — Step 12 Serialization

## Rule

Do not edit `bootState.wireWatchersAndObservers` for Step 14 until the Step 12
subsystem-switch change is merged into the implementation base.

Alpha then makes one integrated daemon edit at that symbol. It replaces the
quiesce-adapter and diagnostic-hook assertions while preserving the Step 12
switch guards and their disabled-subsystem behavior. Bravo does not edit this
symbol.

## Required Evidence

The integrated change must prove all of these cases:

- A base-only substrate leaves the quiesce adapter absent and installs no
  diagnostic hooks.
- Each Step 12 disabled switch still prevents its existing construction path.
- A capability provider preserves the current quiesce and diagnostic behavior.

This rule serializes a shared hunk without treating Step 12 as a permanent
technical dependency for the other Step 14 consumers.
