#!/usr/bin/env python3
"""Regenerate mnem-map CSVs for all 10 spec pilots.

Output: /Users/gb/github/harmonik/docs/decompose-to-tasks/mnem-maps/<prefix>-mnem-map.csv

Strategy:
  - For yaml-based pilots (cp, hc, on, pl, rc, wm): parse <prefix>-pilot-data.yaml
    for (mnem, title) pairs, including the epic.
  - For python-loaded pilots (ar, bi, em, ev): parse <prefix>-pilot.md table rows
    (| `mnem` | title | ... |) to extract (mnem, title) pairs. Epic mnem+title are
    fixed: "<prefix>" -> "<spec full title>".
  - Query Beads via `br list --limit 0 --json` and group by epic ID.
  - For each spec, build a Beads title -> assigned_id lookup over its child set
    (plus the epic itself), then map mnem->assigned_id by exact title match.
"""
import csv
import io
import json
import re
import subprocess
import sys
from pathlib import Path

BR = "/Users/gb/.local/bin/br"
ROOT = Path("/Users/gb/github/harmonik")
PILOTS_DIR = ROOT / "docs" / "decompose-to-tasks"
OUT_DIR = PILOTS_DIR / "mnem-maps"

# prefix -> epic_id mapping
SPECS = {
    "ar": "hk-zs0",
    "bi": "hk-872",
    "em": "hk-b3f",
    "ev": "hk-hqwn",
    "hc": "hk-8i31",
    "cp": "hk-a8bg",
    "wm": "hk-8mwo",
    "pl": "hk-8mup",
    "on": "hk-sx9r",
    "rc": "hk-63oh",
}

# Specs with yaml data files (loader-tooling-based)
YAML_SPECS = {"hc", "cp", "wm", "pl", "on", "rc"}
# Specs with only markdown pilot tables
MD_SPECS = {"ar", "bi", "em", "ev"}


def load_beads():
    """Return dict: epic_id -> dict[bead_id -> {title, labels}]."""
    res = subprocess.run(
        [BR, "list", "--limit", "0", "--json"],
        capture_output=True, text=True, check=True,
    )
    data = json.loads(res.stdout)
    by_epic = {eid: {} for eid in SPECS.values()}
    for issue in data["issues"]:
        bid = issue["id"]
        for eid in SPECS.values():
            if bid == eid or bid.startswith(eid + "."):
                by_epic[eid][bid] = {
                    "title": issue["title"],
                    "labels": issue.get("labels", []) or [],
                }
                break
    return by_epic


def parse_yaml_pilot(prefix):
    """Return list of (mnem, title) pairs from <prefix>-pilot-data.yaml.
    Includes the epic at the front.
    """
    import yaml
    path = PILOTS_DIR / f"{prefix}-pilot-data.yaml"
    with path.open() as f:
        data = yaml.safe_load(f)
    pairs = []
    epic = data["epic"]
    pairs.append((epic["mnem"], epic["title"]))
    for b in data.get("beads", []):
        pairs.append((b["mnem"], b["title"]))
    return pairs


# Match table rows like: | `mnem` | title | description | tags | edges | notes |
# Capture group 1 = mnem (without backticks), group 2 = title (raw).
MD_ROW_RE = re.compile(r"^\|\s*`([a-z][a-z0-9.-]*)`\s*\|\s*([^|]+?)\s*\|")


def parse_md_pilot(prefix):
    """Return list of (mnem, title) pairs from <prefix>-pilot.md table rows.
    Epic mnem == prefix is included with the canonical spec title.
    """
    path = PILOTS_DIR / f"{prefix}-pilot.md"
    pairs = []
    text = path.read_text()
    for line in text.splitlines():
        m = MD_ROW_RE.match(line)
        if not m:
            continue
        mnem = m.group(1)
        title = m.group(2).strip()
        # Skip rows whose mnem doesn't match the spec's prefix
        if not (mnem == prefix or mnem.startswith(prefix + "-")):
            continue
        pairs.append((mnem, title))

    # Pilot-md authors don't always list the §8 taxonomy umbrella as a table row,
    # but cross-spec edges may reference it (e.g. cp-pilot-data.yaml cites
    # ev-events.taxonomy). If the prose mentions such a mnem AND another pilot
    # actually depends on it, include it. A simpler robust rule: include only if
    # this prefix has at least one DB bead with kind:taxonomy NOT yet claimed by
    # a row-listed mnem. We check that here by name pattern; the actual
    # disambiguation happens at match time.
    pair_mnems = {m for m, _ in pairs}
    inferred = []
    seen_inferred = set()
    for ref in re.findall(r"`([a-z]+-(?:events|error)\.taxonomy)`", text):
        if not ref.startswith(prefix + "-"):
            continue
        if ref in pair_mnems or ref in seen_inferred:
            continue
        # Negative-context filter: if the line containing the ref says
        # "No <ref> bead" or "There is no <ref>", skip — pilot author is
        # explicitly disclaiming the bead.
        neg_re = re.compile(rf"[Nn]o `{re.escape(ref)}` bead")
        if neg_re.search(text):
            continue
        seen_inferred.add(ref)
        inferred.append((ref, ""))  # title looked up via tier 5b kind:taxonomy
    pairs.extend(inferred)
    return pairs


# Canonical epic titles (from prior surviving CSVs / br show)
EPIC_TITLES = {
    "ar": "Architecture spec — implementation",
    "bi": "Beads Integration spec — implementation",
    "em": "Execution Model spec - implementation",
    "ev": "Event Model spec - implementation",
    "hc": "Handler Contract spec — implementation",
    "cp": "Control Points spec — implementation",
    "wm": "Workspace Model spec — implementation",
    "pl": "Process Lifecycle spec — implementation",
    "on": "Operator NFR spec — implementation",
    "rc": "Reconciliation spec — implementation",
}


def build_mnem_map(prefix, beads_for_epic):
    """Return list of (mnem, assigned_id, title) rows for the spec."""
    if prefix in YAML_SPECS:
        pairs = parse_yaml_pilot(prefix)
    else:
        pairs = parse_md_pilot(prefix)
        # Inject epic at front for md-only specs (their tables don't include epic)
        epic_title = EPIC_TITLES[prefix]
        # Only inject if not already present
        if not any(m == prefix for m, _ in pairs):
            pairs.insert(0, (prefix, epic_title))

    # Deduplicate by mnem preserving order
    seen_mnems = set()
    deduped = []
    for m, t in pairs:
        if m in seen_mnems:
            continue
        seen_mnems.add(m)
        deduped.append((m, t))

    def norm(s):
        return s.replace("`", "")

    # Indices over DB beads
    title_to_ids = {}
    norm_to_ids = {}
    label_to_ids = {}  # 'req:BI-002' -> [bid]
    event_name_to_ids = {}  # extracted event name like 'run_started' -> [bid]
    EVENT_ROW_RE = re.compile(r"^Event row:\s+(\S+?)\s*\(")
    for bid, info in beads_for_epic.items():
        title = info["title"]
        labels = info["labels"]
        title_to_ids.setdefault(title, []).append(bid)
        norm_to_ids.setdefault(norm(title), []).append(bid)
        for lab in labels:
            if lab.startswith("req:"):
                label_to_ids.setdefault(lab, []).append(bid)
        m = EVENT_ROW_RE.match(title)
        if m:
            event_name_to_ids.setdefault(m.group(1), []).append(bid)

    def mnem_to_req_label(mnem):
        """Convert mnem like 'bi-001', 'ev-inv-004', 'em-040a' to req label.
        Returns None if mnem doesn't fit a req-label shape (schema/test/taxonomy).
        Convention from Beads: prefix uppercased, optional trailing letter stays lowercase.
        e.g. bi-025a -> req:BI-025a; ev-inv-004 -> req:EV-INV-004."""
        m = re.match(r"^([a-z]+)-((?:inv|env)-)?(\d+)([a-z]?)$", mnem)
        if not m:
            return None
        prefix = m.group(1).upper()
        infix = m.group(2).upper() if m.group(2) else ""
        num = m.group(3)
        suffix = m.group(4)  # keep lowercase
        return f"req:{prefix}-{infix}{num}{suffix}"

    def mnem_event_name(mnem):
        """For ev-events.NNN-XXX style mnems, return underscore form 'NNN_XXX'."""
        m = re.match(r"^[a-z]+-events\.(.+)$", mnem)
        if not m:
            return None
        return m.group(1).replace("-", "_")

    def mnem_step_parent_and_n(mnem):
        """For mnems like 'bi-030.s1' return ('bi-030', 1). Else (None, None)."""
        m = re.match(r"^(.+)\.s(\d+)$", mnem)
        if not m:
            return (None, None)
        return (m.group(1), int(m.group(2)))

    rows = []
    unmatched = []
    used_ids = set()
    for mnem, title in deduped:
        candidates = []
        # Tier 1: exact title match
        for bid in title_to_ids.get(title, []):
            if bid not in used_ids:
                candidates.append(bid)
        # Tier 2: normalized (backticks stripped)
        if not candidates:
            for bid in norm_to_ids.get(norm(title), []):
                if bid not in used_ids:
                    candidates.append(bid)
        # Tier 3: req-label match
        if not candidates:
            req = mnem_to_req_label(mnem)
            if req:
                for bid in label_to_ids.get(req, []):
                    if bid not in used_ids:
                        candidates.append(bid)
        # Tier 4: event-row name match (EV ev-events.X)
        if not candidates:
            ev_name = mnem_event_name(mnem)
            if ev_name:
                for bid in event_name_to_ids.get(ev_name, []):
                    if bid not in used_ids:
                        candidates.append(bid)

        # Tier 5: step-bead match (BI/CP bi-030.s1 -> bead with title 'Step 1 — ...'
        # AND label req:BI-030).
        if not candidates:
            parent_mnem, step_n = mnem_step_parent_and_n(mnem)
            if parent_mnem is not None:
                req = mnem_to_req_label(parent_mnem)
                step_re = re.compile(rf"^Step {step_n} [—-]")
                for bid, info in beads_for_epic.items():
                    if bid in used_ids:
                        continue
                    if req and req not in info["labels"]:
                        continue
                    if "kind:step" not in info["labels"]:
                        continue
                    if step_re.match(info["title"]):
                        candidates.append(bid)

        # Tier 5b: taxonomy-umbrella fallback — single bead with kind:taxonomy.
        # Each spec has at most one such umbrella bead per discipline §2.11(c).
        if not candidates and (
            mnem.endswith("error.taxonomy") or mnem.endswith("events.taxonomy")
        ):
            for bid, info in beads_for_epic.items():
                if bid in used_ids:
                    continue
                if "kind:taxonomy" in info["labels"]:
                    candidates.append(bid)

        # Tier 6: ambiguous-label disambiguation by parent ID — for cases like
        # AMBIGUOUS where one of the matches has the SHORTEST id (the parent itself)
        # while others are sub-steps. The mnem with no .sN suffix is the parent.
        if len(candidates) > 1 and not mnem_step_parent_and_n(mnem)[0]:
            # Pick the one with the fewest dots in its ID (shallowest = parent)
            min_depth = min(c.count(".") for c in candidates)
            shallow = [c for c in candidates if c.count(".") == min_depth]
            if len(shallow) == 1:
                candidates = shallow

        if len(candidates) == 1:
            bid = candidates[0]
            # Use the DB title (ground truth) for the CSV output
            rows.append((mnem, bid, beads_for_epic[bid]["title"]))
            used_ids.add(bid)
        elif len(candidates) > 1:
            unmatched.append((mnem, title, f"AMBIGUOUS: {candidates}"))
        else:
            unmatched.append((mnem, title, "NOT_FOUND"))
    return rows, unmatched, deduped


def write_csv(prefix, rows):
    """Write CSV with header. Title gets CSV-quoted only if needed
    (matches loader's append_map_row behavior)."""
    out_path = OUT_DIR / f"{prefix}-mnem-map.csv"
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    with out_path.open("w") as f:
        f.write("mnemonic,assigned_id,title\n")
        for mnem, bead_id, title in rows:
            t = title.replace('"', '""')
            if "," in t or '"' in t:
                t = f'"{t}"'
            f.write(f"{mnem},{bead_id},{t}\n")
    return out_path


def main():
    by_epic = load_beads()
    summary = []
    total = 0
    for prefix, eid in SPECS.items():
        beads = by_epic[eid]
        # Beads count includes epic itself
        rows, unmatched, deduped = build_mnem_map(prefix, beads)
        out = write_csv(prefix, rows)
        total += len(rows)
        summary.append({
            "prefix": prefix,
            "epic_id": eid,
            "beads_in_db": len(beads),
            "yaml_or_md_pairs": len(deduped),
            "matched": len(rows),
            "unmatched": len(unmatched),
            "out_path": str(out),
            "unmatched_details": unmatched[:10],
        })
    print(json.dumps({"total_matched": total, "specs": summary}, indent=2))


if __name__ == "__main__":
    main()
