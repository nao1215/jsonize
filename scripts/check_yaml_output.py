#!/usr/bin/env python3
"""Read the YAML jz writes with a reader jsonize did not write.

`jz --yaml` writes the values the JSON output carries, and jz's own YAML
reader checks that in the unit tests. This script runs every golden
fixture of the registry through `jz --yaml` and reads the result with
PyYAML, holding it to the golden JSON, so that the YAML is shown to be
what an independent reader takes it for rather than what jz's own reader
does.

It needs a built `jz` (first argument, or `jz` on PATH) and
`python3 -m pip install pyyaml`, and exits 1 on any failure.
"""

import glob
import json
import os
import subprocess
import sys

import yaml

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "registry")


def cases():
    """Yield (command, variant, fixture path, golden path) for every case
    that has a golden file and is not expected to fail."""
    for golden in sorted(glob.glob(os.path.join(ROOT, "parsers", "*", "*", "testdata", "*.json"))):
        base = golden[: -len(".json")]
        fixture = base + ".txt"
        if not os.path.exists(fixture):
            continue
        meta = base + ".yaml"
        if os.path.exists(meta):
            with open(meta, encoding="utf-8") as f:
                if "expect_error" in (yaml.safe_load(f) or {}):
                    continue
        parts = golden.split(os.sep)
        yield parts[-4], parts[-3], fixture, golden


def main():
    jz = sys.argv[1] if len(sys.argv) > 1 else "jz"
    failures = 0
    checked = 0
    for command, variant, fixture, golden in cases():
        proc = subprocess.run(
            [jz, "--parser", command, "--variant", variant, "--yaml", "--file", fixture],
            capture_output=True,
            text=True,
            check=False,
        )
        if proc.returncode != 0:
            print(f"{fixture}: jz exited {proc.returncode}: {proc.stderr.strip()}")
            failures += 1
            continue
        try:
            read = yaml.safe_load(proc.stdout)
        except yaml.YAMLError as err:
            print(f"{fixture}: PyYAML cannot read the output: {err}")
            failures += 1
            continue
        with open(golden, encoding="utf-8") as f:
            want = json.load(f)
        if read != want:
            print(f"{fixture}: PyYAML read something else than the golden JSON holds")
            failures += 1
            continue
        checked += 1
    print(f"{checked} fixtures read back as their golden JSON, {failures} failures")
    return 1 if failures or checked == 0 else 0


if __name__ == "__main__":
    sys.exit(main())
