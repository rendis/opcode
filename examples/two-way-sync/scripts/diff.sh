#!/usr/bin/env bash
# diff.sh - Compare records from two systems and output differences.
#
# Input (stdin):  JSON array of 2 objects (system A records, system B records)
# Output (stdout): {"diffs": [{"key": "...", "target": "a|b", "value": ...}], "count": N}

set -euo pipefail

input=$(cat)

python3 -c "
import json, sys

data = json.loads(sys.argv[1])
records_a = data[0] if isinstance(data[0], dict) else {}
records_b = data[1] if isinstance(data[1], dict) else {}

diffs = []
all_keys = set(list(records_a.keys()) + list(records_b.keys()))

for key in sorted(all_keys):
    in_a = key in records_a
    in_b = key in records_b
    if in_a and not in_b:
        # Present in A but missing from B -> upsert to B
        diffs.append({'key': key, 'target': 'b', 'value': records_a[key]})
    elif in_b and not in_a:
        # Present in B but missing from A -> upsert to A
        diffs.append({'key': key, 'target': 'a', 'value': records_b[key]})
    elif records_a[key] != records_b[key]:
        # Both have the key but values differ -> sync newer to older
        # Default: prefer A as source of truth
        diffs.append({'key': key, 'target': 'b', 'value': records_a[key]})

result = {'diffs': diffs, 'count': len(diffs)}
print(json.dumps(result))
" "$input"
