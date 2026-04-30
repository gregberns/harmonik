#!/usr/bin/env python3
"""Load a decompose-to-tasks pilot's bead set into the harmonik .beads workspace.

Reads a single pilot-data.yaml file (one per spec; sibling of <spec>-pilot.md) and
materialises its epic, beads, and edges via `br` (Beads CLI) calls. Resumable:
re-runs against a partially-loaded workspace skip beads already present in the
mnem-map CSV. Cross-spec edges resolve mnemonics through the named cross_specs CSVs.

USAGE:
    python3 scripts/load-pilot.py docs/decompose-to-tasks/hc-pilot-data.yaml
    python3 scripts/load-pilot.py <data.yaml> --dry-run
    python3 scripts/load-pilot.py <data.yaml> --map /tmp/hc-mnem-map.csv
    python3 scripts/load-pilot.py <data.yaml> --skip-edges
    python3 scripts/load-pilot.py <data.yaml> --skip-beads   # edges-only mode

DESIGN NOTES:
  - Mnem-map CSV is the resume ledger. Default path: /tmp/<prefix>-mnem-map.csv.
    On every successful create, mnem,assigned_id,title is appended.
    On startup, the file is read and any mnemonic in it is treated as already loaded.
  - Edges:
      * intra-spec (`from` and `to` mnemonic prefixes match `spec.prefix`)
        resolve via own map.
      * cross-spec (mnemonic prefix differs) resolves via cross_specs[<prefix>] CSV.
      * `forward:*` mnemonics are logged-only — no `br dep add` issued.
      * `br dep add --json` returning `{action: already_exists}` is treated as success
        (idempotent re-runs); `Cycle detected ...` and other errors are recorded.
  - Edge convention from discipline §2.5: row tables list `<citing> -> <prerequisite>`,
    meaning the citing bead BLOCKS-ON the prerequisite. We invoke
        `br dep add <citing> <prereq> -t blocks`
    which Beads interprets as "<citing> depends on <prereq>".
  - All beads (including the epic) are created with status `draft` per discipline §2.9.
  - KNOWN LIMITATION (kill-mid-create race): if killed between `br create` and the
    map-row append, the next run will create a duplicate. Mitigation in
    `docs/decompose-to-tasks/loader-tooling.md` "Resume protocol" section: dry-run
    first, then `br list -l spec:<spec> --status draft` and reconcile DB-vs-map diff
    by hand before resuming.
"""

import argparse
import csv
import json
import os
import re
import subprocess
import sys
from pathlib import Path

import yaml


VALID_KINDS = {"req", "invariant", "schema", "error-taxonomy", "test-infra", "workflow"}
VALID_SCHEMA_KINDS = {"interface", "record", "enum", "schema"}

# Beads-assigned IDs are <prefix>-<base36-suffix> (top-level) or <parent-id>.<n> (children).
# We validate the captured create-output against this shape so a future banner / warning
# prefix from `br` doesn't get silently written into the mnem-map.
BEAD_ID_RE = re.compile(r"^[a-z]+-[a-z0-9]+(\.\d+)*$")


# ---------------------------------------------------------------------------
# YAML load + validate
# ---------------------------------------------------------------------------

def load_yaml(path: Path) -> dict:
    with path.open() as f:
        data = yaml.safe_load(f)
    validate(data, path)
    return data


def validate(data: dict, path: Path) -> None:
    def fail(msg):
        sys.exit(f"ERROR in {path}: {msg}")

    for top in ("spec", "epic", "beads", "edges"):
        if top not in data:
            fail(f"missing top-level key '{top}'")

    spec = data["spec"]
    for k in ("prefix", "title"):
        if k not in spec:
            fail(f"spec.{k} missing")

    epic = data["epic"]
    for k in ("mnem", "title", "description"):
        if k not in epic:
            fail(f"epic.{k} missing")

    seen = {epic["mnem"]}
    for i, b in enumerate(data["beads"]):
        for k in ("mnem", "kind", "title", "description"):
            if k not in b:
                fail(f"beads[{i}] missing '{k}'")
        if b["mnem"] in seen:
            fail(f"beads[{i}] duplicate mnem {b['mnem']!r}")
        seen.add(b["mnem"])
        if b["kind"] not in VALID_KINDS:
            fail(f"beads[{i}] bad kind {b['kind']!r}; valid: {sorted(VALID_KINDS)}")
        if b["kind"] == "schema":
            if "schema_kind" not in b:
                fail(f"beads[{i}] kind=schema requires explicit 'schema_kind' "
                     f"(one of {sorted(VALID_SCHEMA_KINDS)})")
            if b["schema_kind"] not in VALID_SCHEMA_KINDS:
                fail(f"beads[{i}] bad schema_kind {b['schema_kind']!r}")
        if b["kind"] in ("req", "invariant") and "req" not in b:
            fail(f"beads[{i}] kind={b['kind']} requires 'req' field")

    for i, e in enumerate(data["edges"]):
        if "from" not in e or "to" not in e:
            fail(f"edges[{i}] needs 'from' and 'to'")


# ---------------------------------------------------------------------------
# Mnem-map I/O (CSV-backed resume ledger)
# ---------------------------------------------------------------------------

def read_map(path: Path) -> dict:
    m = {}
    if path.exists():
        with path.open() as f:
            for row in csv.reader(f):
                if not row or row[0] == "mnemonic":
                    continue
                m[row[0]] = row[1]
    return m


def init_map_if_absent(path: Path, dry_run: bool = False) -> None:
    if path.exists() or dry_run:
        return
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w") as f:
        f.write("mnemonic,assigned_id,title\n")


def append_map_row(path: Path, mnem: str, bead_id: str, title: str) -> None:
    t = title.replace('"', '""')
    if "," in t or '"' in t:
        t = f'"{t}"'
    with path.open("a") as f:
        f.write(f"{mnem},{bead_id},{t}\n")


# ---------------------------------------------------------------------------
# br shell-out
# ---------------------------------------------------------------------------

class Runner:
    def __init__(self, dry_run: bool):
        self.dry_run = dry_run
        self.create_calls = 0
        self.update_calls = 0
        self.dep_calls = 0

    def run(self, cmd: list, capture: bool = True, check: bool = True):
        if self.dry_run:
            print(f"[dry] {' '.join(repr(c) if ' ' in c else c for c in cmd[:6])}"
                  + (" ..." if len(cmd) > 6 else ""))
            class _R:
                returncode = 0
                stdout = "DRY-DRY-DRY"
                stderr = ""
            return _R()
        # check=False here because callers handle non-zero per-command (dep_add tolerates
        # duplicates; create/update should still hard-fail). Set check=True to revert.
        res = subprocess.run(cmd, check=False, capture_output=capture, text=True)
        if check and res.returncode != 0:
            sys.stderr.write(
                f"FAILED: {cmd}\n  stdout: {res.stdout}\n  stderr: {res.stderr}\n"
            )
            raise SystemExit(1)
        return res

    def _capture_id(self, out: str, cmd: list) -> str:
        """Validate `br create --silent` stdout against the bead-id shape.
        Aborts loud if `br` ever emits a banner or warning instead of pure ID — better
        a hard fail than silently writing junk into the mnem-map.
        """
        bead_id = out.strip()
        if not BEAD_ID_RE.match(bead_id):
            sys.exit(
                f"FAILED: br create returned non-id stdout {bead_id!r} "
                f"(cmd: {cmd})\n"
                f"This usually means br injected a banner / warning. Inspect with "
                f"`br --version` and `br create --json --dry-run ...` to diagnose, "
                f"then update BEAD_ID_RE if the ID format changed."
            )
        return bead_id

    def create_epic(self, title: str, description: str, labels: list) -> str:
        cmd = ["br", "create", "--silent", "--type", "epic",
               "--title", title, "--description", description,
               "--labels", ",".join(labels)]
        if self.dry_run:
            self.run(cmd)
            self.create_calls += 1
            return "DRY-EPIC"
        res = self.run(cmd)
        self.create_calls += 1
        return self._capture_id(res.stdout, cmd)

    def create_child(self, parent_id: str, title: str, description: str,
                     labels: list) -> str:
        cmd = ["br", "create", "--silent", "--type", "task",
               "--parent", parent_id, "--title", title,
               "--description", description, "--labels", ",".join(labels)]
        if self.dry_run:
            self.run(cmd)
            self.create_calls += 1
            return None  # caller substitutes a placeholder for own_map
        res = self.run(cmd)
        self.create_calls += 1
        return self._capture_id(res.stdout, cmd)

    def set_draft(self, bead_id: str) -> None:
        self.run(["br", "update", bead_id, "--status", "draft"], capture=True)
        self.update_calls += 1

    def dep_add(self, citing_id: str, prereq_id: str) -> tuple:
        """Return (ok, msg). ok=True if added or already present; False on real reject.
        Uses `--json` so duplicate detection is structural, not text-pattern matching.
        Cycle errors and missing-bead errors come back as non-zero exit + non-JSON stderr.
        """
        cmd = ["br", "dep", "add", citing_id, prereq_id, "-t", "blocks", "--json"]
        res = self.run(cmd, check=False)
        self.dep_calls += 1
        if self.dry_run:
            return True, "dry-run"
        if res.returncode == 0:
            try:
                payload = json.loads(res.stdout)
                action = payload.get("action") or payload.get("status")
                if action == "already_exists" or action == "exists":
                    return True, "duplicate"
                # any other rc=0 result is a fresh add
                return True, "added"
            except json.JSONDecodeError:
                # rc=0 but non-JSON → tolerate but warn
                return True, f"added (non-json: {res.stdout.strip()[:80]})"
        msg = (res.stdout + res.stderr).strip()
        # Belt-and-suspenders: even with --json, if rc!=0 we may get plain-text stderr.
        # Recognise duplicate (shouldn't happen since rc=0 above, but doesn't cost us).
        if "already exists" in msg.lower() or "duplicate" in msg.lower():
            return True, "duplicate"
        return False, msg


# ---------------------------------------------------------------------------
# Label expansion per bead kind
# ---------------------------------------------------------------------------

def expand_labels(bead: dict, defaults: list) -> list:
    labels = list(defaults)
    kind = bead["kind"]
    if kind == "req":
        req = bead["req"]
        if isinstance(req, str):
            labels.append(f"req:{req}")
        else:
            for r in req:
                labels.append(f"req:{r}")
    elif kind == "invariant":
        labels.append(f"req:{bead['req']}")
        labels.append("kind:invariant")
    elif kind == "schema":
        sk = bead.get("schema_kind", "schema")
        labels.append(f"kind:{sk}")
    elif kind == "error-taxonomy":
        # Beads label convention is `kind:taxonomy` (matches BI/EM/EV); the YAML
        # `kind: error-taxonomy` is a more descriptive semantic name for authors.
        labels.append("kind:taxonomy")
    elif kind == "test-infra":
        labels.append("kind:test-infra")
    elif kind == "workflow":
        # Workflow beads are not spec decomposition — they are tasks-to-do-tasks
        # (author yaml, run review, backfill, etc.). No req field required.
        labels.append("kind:workflow")
    for extra in bead.get("extra_labels", []) or []:
        labels.append(extra)
    # de-dupe preserving order
    seen = set()
    return [x for x in labels if not (x in seen or seen.add(x))]


# ---------------------------------------------------------------------------
# Edge resolution
# ---------------------------------------------------------------------------

_FORWARD_PFX = "forward:"
_PREFIX_RE = re.compile(r"^([a-z][a-z0-9]*)[-.]")


def edge_target_kind(mnem: str, own_prefix: str) -> str:
    """Return one of: 'forward', 'intra', 'cross:<prefix>', 'unknown'."""
    if mnem.startswith(_FORWARD_PFX):
        return "forward"
    m = _PREFIX_RE.match(mnem)
    if not m:
        return "unknown"
    pfx = m.group(1)
    if pfx == own_prefix:
        return "intra"
    return f"cross:{pfx}"


def resolve(mnem: str, own_prefix: str, own_map: dict, cross: dict):
    kind = edge_target_kind(mnem, own_prefix)
    if kind == "forward":
        return None, "forward-deferred"
    if kind == "intra":
        if mnem not in own_map:
            return None, f"intra mnem {mnem!r} not in own map"
        return own_map[mnem], "intra"
    if kind.startswith("cross:"):
        pfx = kind[len("cross:"):]
        if pfx not in cross:
            return None, f"no cross_specs entry for prefix {pfx!r} (mnem={mnem!r})"
        if mnem not in cross[pfx]:
            return None, f"cross-spec mnem {mnem!r} not in {pfx} map"
        return cross[pfx][mnem], f"cross-{pfx}"
    return None, f"unrecognised mnemonic shape: {mnem!r}"


# ---------------------------------------------------------------------------
# Driver
# ---------------------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("data_file", help="path to <spec>-pilot-data.yaml")
    ap.add_argument("--map", help="own mnem-map CSV path (default: /tmp/<prefix>-mnem-map.csv)")
    ap.add_argument("--dry-run", action="store_true", help="print br calls; do not execute")
    ap.add_argument("--skip-beads", action="store_true",
                    help="don't create beads (assume already loaded); only process edges")
    ap.add_argument("--skip-edges", action="store_true",
                    help="don't process edges; only create beads")
    args = ap.parse_args()

    data_path = Path(args.data_file).resolve()
    if not data_path.exists():
        sys.exit(f"data file not found: {data_path}")

    data = load_yaml(data_path)
    spec = data["spec"]
    prefix = spec["prefix"]
    epic = data["epic"]
    beads = data["beads"]
    edges = data["edges"]
    defaults = data.get("default_labels", []) or []
    cross_specs = data.get("cross_specs", {}) or {}

    own_map_path = Path(args.map) if args.map else Path(f"/tmp/{prefix}-mnem-map.csv")
    init_map_if_absent(own_map_path, dry_run=args.dry_run)
    own_map = read_map(own_map_path)

    cross_maps = {}
    for pfx, p in cross_specs.items():
        cross_maps[pfx] = read_map(Path(p))
        print(f"cross_specs[{pfx}] from {p}: {len(cross_maps[pfx])} entries")

    runner = Runner(dry_run=args.dry_run)

    print(f"\n=== loading {data_path.name} (prefix={prefix}) ===")
    print(f"existing in own map: {len(own_map)} mnemonics")
    print(f"target: 1 epic + {len(beads)} beads, {len(edges)} edges\n")

    # 1. Epic
    if not args.skip_beads:
        if epic["mnem"] in own_map:
            print(f"epic {epic['mnem']} already loaded as {own_map[epic['mnem']]}")
        else:
            print(f"creating epic {epic['mnem']}...")
            epic_labels = list(defaults) + (epic.get("labels") or [])
            # de-dupe
            seen = set()
            epic_labels = [x for x in epic_labels if not (x in seen or seen.add(x))]
            bid = runner.create_epic(epic["title"], epic["description"], epic_labels)
            if not args.dry_run:
                runner.set_draft(bid)
                own_map[epic["mnem"]] = bid
                append_map_row(own_map_path, epic["mnem"], bid, epic["title"])
            print(f"  epic: {epic['mnem']} -> {bid}")

        # 2. Children
        epic_id = own_map.get(epic["mnem"], "DRY-EPIC")

        created = 0
        skipped = 0
        for i, b in enumerate(beads, 1):
            if b["mnem"] in own_map:
                skipped += 1
                continue
            labels = expand_labels(b, defaults)
            bid = runner.create_child(epic_id, b["title"], b["description"], labels)
            if not args.dry_run:
                runner.set_draft(bid)
                own_map[b["mnem"]] = bid
                append_map_row(own_map_path, b["mnem"], bid, b["title"])
            else:
                # Populate placeholder so downstream edge-resolution exercises real paths.
                own_map[b["mnem"]] = f"DRY-{i:03d}"
            created += 1
            if created % 10 == 0:
                print(f"  created [{created}] last: {b['mnem']} -> {bid or own_map[b['mnem']]}")
        print(f"\nbeads: created={created}, skipped(already in map)={skipped}")
    else:
        print("--skip-beads set; not creating any beads")

    # 3. Edges
    if args.skip_edges:
        print("\n--skip-edges set; done.")
        return

    print(f"\nprocessing {len(edges)} edges...")
    ok = 0
    forward = 0
    rejected = []
    by_kind = {"intra": 0, "forward-deferred": 0}
    for pfx in cross_maps:
        by_kind[f"cross-{pfx}"] = 0
    for e in edges:
        f_mnem = e["from"]
        t_mnem = e["to"]

        # Resolve `from`
        fid, fkind = resolve(f_mnem, prefix, own_map, cross_maps)
        if fkind == "forward-deferred":
            rejected.append((f_mnem, t_mnem, "from is forward-deferred (illegal)"))
            continue
        if fid is None:
            rejected.append((f_mnem, t_mnem, fkind))
            continue

        # Resolve `to`
        tid, tkind = resolve(t_mnem, prefix, own_map, cross_maps)
        if tkind == "forward-deferred":
            forward += 1
            by_kind.setdefault(tkind, 0)
            by_kind[tkind] += 1
            continue
        if tid is None:
            rejected.append((f_mnem, t_mnem, tkind))
            continue

        success, msg = runner.dep_add(fid, tid)
        if success:
            ok += 1
            by_kind.setdefault(tkind, 0)
            by_kind[tkind] += 1
        else:
            rejected.append((f_mnem, t_mnem, msg))

    print(f"\nedges: ok={ok}, forward-deferred={forward}, rejected={len(rejected)}")
    print("  by kind:", dict(by_kind))
    if rejected:
        print("\nREJECTED:")
        for f, t, msg in rejected:
            print(f"  {f} -> {t}: {msg}")

    print(f"\nbr calls: create={runner.create_calls}, "
          f"update={runner.update_calls}, dep={runner.dep_calls}")


if __name__ == "__main__":
    main()
