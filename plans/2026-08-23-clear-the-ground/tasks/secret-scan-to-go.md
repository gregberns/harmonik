---
id: secret-scan-to-go
title: Move the credential scan out of shell, where a swallowed exit code once hid every private key
type: task
priority: 1
labels: [scripts, shell-to-go, gate, security, clear-the-ground]
depends_on: []
blocks: []
workstream: unassigned
batch: 6
---

> **Workstream unassigned.** These shell-to-Go conversions do not belong to `PLAN.md` §W6,
> which is the `crew-cleanup` skill. Whether they form a workstream of their own is an open
> operator decision, so this field reads `unassigned` rather than carrying a number that is
> already taken. Do not invent one. This task is parked until that ruling lands.

## Problem

`scripts/secret-scan.sh` is 755 lines and `scripts/secret-scan-test.sh` is 1,100 — 1,855 lines of
shell standing between this repository and a leaked credential.

**It is gate-critical in both directions.** `gate-static-product` runs
`scripts/secret-scan.sh --head-only`, so it is in `make fast`, `make core` and `make full`. `make
full` additionally runs `scripts/secret-scan.sh --range` over the whole branch range, and the
`Makefile` comment on that line states the reason plainly: a bad commit message has no repair after
a merge and a leaked key has one, so refusing over the range is a demand that can be met.

**Its own header records the defect that shell caused.** Quoting the file: the scan body was
byte-identical from 2026-05-31 to 2026-08-12 with no test of any kind for seventy-three days, and the
first test ever written against it found it admitting a key in an ordinary-sized commit — `grep -q`
behind a pipe under `pipefail` read a match as a no-match, and the private-key pattern's leading
dashes made `grep` exit 2 with the message discarded, so **that pattern had never matched anything**.

That is not a bug that a careful author avoids. It is the interaction of `set -o pipefail`, `grep`'s
three-valued exit code and `grep -q`'s early exit — three shell behaviours that a Go
`regexp.Regexp.Match` does not have. The repo's response was to add a second shell script
(`scripts/gate-fails-closed-test.sh`, 501 lines) whose job is to watch for exactly this shape in
other shell scripts. The scanner is a pure function — bytes in, findings out — and it is the least
justifiable shell in the tree.

The 1,100-line self-test is the strongest evidence for conversion, not against it: the behaviour is
already specified in detail, so the Go test has a corpus to be ported from rather than invented.

## Scope

- `scripts/secret-scan.sh` — the pattern set, the `--head-only`, `--range` and default index modes,
  the allow/ignore handling, and the exit contract.
- `scripts/secret-scan-test.sh` — the case corpus to port.
- A new Go package under `tools/`, invoked from `gate-static-product` and from `full` in the
  `Makefile`, plus the standalone `secret-scan` target.
- `scripts/gate-fails-closed-test.sh` — its behavioural probe and its structural `make -n` pass both
  read these recipe lines.

## Done when

1. A Go program under `tools/` performs the scan and supports the same three modes the shell
   supported, selected the same way.
2. The `Makefile` calls it in `gate-static-product`, in `full`, and in the standalone `secret-scan`
   target. No target names `scripts/secret-scan.sh`.
3. `scripts/secret-scan.sh` and `scripts/secret-scan-test.sh` are gone.
4. Every case in the shell self-test corpus exists as a Go table-test case. State the ported case
   count in the commit body and name any case deliberately dropped, with a reason.
5. A Go test proves the scanner **finds** each pattern class — including the private-key pattern that
   the shell version never matched — on a synthesized positive input. A scanner with only
   negative-direction tests is the failure this task exists to remove.
6. A Go test proves a scan failure produces a non-zero exit through the real Makefile step, so the
   fails-closed property is asserted in Go rather than only by `scripts/gate-fails-closed-test.sh`.
7. `make fast` and `make full` are green.

## Limits

- **Do not change what the scan enforces.** Same pattern classes, same modes, same allow behaviour.
  If a shell pattern turns out to be broken (as the private-key one was), the Go version implements
  what the pattern was **meant** to match and the commit body says so explicitly — a silent widening
  and a silent narrowing are both unacceptable, but only one of them is visible in a green build.
- **Do not weaken the `--range` scope in `make full`.** Whole-branch, and it fails on a finding.
- **Do not widen `tools/lintreport/allow.txt`.**
- **Do not convert and delete in one landing if that leaves the gate unrunnable mid-way.** If it goes
  in two commits, the first adds the Go scanner and points the Makefile at it while the shell file
  still exists, and the second deletes the shell. Never a commit where no scanner runs.
- **Put it under `tools/`, not in `cmd/harmonik`** — W4 is shrinking `package main`.
- Do not delete `scripts/gate-fails-closed-test.sh`. It still guards the other shell in the gate.
