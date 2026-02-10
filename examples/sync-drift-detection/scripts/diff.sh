#!/usr/bin/env bash
# diff.sh - Reads two JSON objects from stdin (as an array or object with
# fetch-a/fetch-b keys), compares them, and outputs drift analysis.
#
# Input (stdin):  JSON containing both source responses
# Output (stdout): {"has_drift": bool, "changes": N, "details": "..."}

set -euo pipefail

input=$(cat)

python3 -c "
import json
import sys

data = json.loads(sys.argv[1])

# Handle various input formats
source_a = None
source_b = None

if isinstance(data, list) and len(data) >= 2:
    source_a = data[0]
    source_b = data[1]
elif isinstance(data, dict):
    # Try to find the two sources in the parallel output
    keys = list(data.keys())
    if 'fetch-a' in data and 'fetch-b' in data:
        source_a = data['fetch-a']
        source_b = data['fetch-b']
    elif len(keys) >= 2:
        source_a = data[keys[0]]
        source_b = data[keys[1]]

if source_a is None or source_b is None:
    print(json.dumps({
        'has_drift': False,
        'changes': 0,
        'details': 'Could not parse both sources from input'
    }))
    sys.exit(0)

# Normalize to strings for comparison
str_a = json.dumps(source_a, sort_keys=True)
str_b = json.dumps(source_b, sort_keys=True)

if str_a == str_b:
    print(json.dumps({
        'has_drift': False,
        'changes': 0,
        'details': 'Sources are identical'
    }))
else:
    # Count differing top-level keys
    changes = 0
    diff_keys = []

    if isinstance(source_a, dict) and isinstance(source_b, dict):
        all_keys = set(list(source_a.keys()) + list(source_b.keys()))
        for key in all_keys:
            val_a = source_a.get(key)
            val_b = source_b.get(key)
            if json.dumps(val_a, sort_keys=True) != json.dumps(val_b, sort_keys=True):
                changes += 1
                diff_keys.append(key)
    else:
        changes = 1
        diff_keys = ['(root)']

    details = f'Drift in {changes} field(s): {', '.join(diff_keys[:10])}'
    print(json.dumps({
        'has_drift': True,
        'changes': changes,
        'details': details
    }))
" "$input"
