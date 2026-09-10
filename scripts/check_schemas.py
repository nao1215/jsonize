#!/usr/bin/env python3
"""Validate the published output schemas with a validator jsonize did not write.

jsonize derives the schema of each definition's output and checks every
fixture against it with its own validator, which covers exactly the
keywords its generator emits. This script runs the same check through the
Python jsonschema package, so that the two agreeing is evidence rather than
one piece of code agreeing with itself:

- every schema under registry/schemas is a valid draft 2020-12 schema;
- every golden JSON file under registry/parsers fits the schema of its
  definition, both as published and with its objects closed to the keys
  they name, which is the strict reading jsonize's own validator applies.

It needs `python3 -m pip install jsonschema` and exits 1 on any failure.
"""

import copy
import glob
import json
import os
import sys

from jsonschema import Draft202012Validator

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "registry")


def closed(schema):
    """Return schema with every object that names its keys closed to them."""
    schema = copy.deepcopy(schema)

    def walk(node):
        if not isinstance(node, dict):
            return
        if "properties" in node and "additionalProperties" not in node:
            node["additionalProperties"] = False
        for key, value in node.items():
            if key in ("properties", "$defs"):
                for sub in value.values():
                    walk(sub)
            elif isinstance(value, dict):
                walk(value)

    walk(schema)
    return schema


def main():
    failures = 0
    documents = 0
    schemas = sorted(glob.glob(os.path.join(ROOT, "schemas", "*", "*.json")))
    if not schemas:
        print("no schemas under registry/schemas", file=sys.stderr)
        return 1
    for path in schemas:
        command = os.path.basename(os.path.dirname(path))
        variant = os.path.basename(path)[: -len(".json")]
        with open(path, encoding="utf-8") as f:
            schema = json.load(f)
        Draft202012Validator.check_schema(schema)
        validators = (Draft202012Validator(schema), Draft202012Validator(closed(schema)))
        for golden in sorted(glob.glob(os.path.join(ROOT, "parsers", command, variant, "testdata", "*.json"))):
            with open(golden, encoding="utf-8") as f:
                document = json.load(f)
            documents += 1
            for validator in validators:
                error = next(validator.iter_errors(document), None)
                if error is not None:
                    failures += 1
                    print(f"{os.path.relpath(golden, ROOT)}: {error.json_path}: {error.message}", file=sys.stderr)
                    break
    print(f"{len(schemas)} schemas, {documents} documents, {failures} failed")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
