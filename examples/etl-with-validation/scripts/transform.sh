#!/usr/bin/env bash
# transform.sh - Reads JSON data from stdin, transforms/filters it,
# and outputs cleaned JSON.
#
# Input (stdin):  Raw JSON data (e.g., API response with nested records)
# Output (stdout): {"records": [...], "count": N}

set -euo pipefail

input=$(cat)

# Use Python for JSON transformation (portable, no jq dependency)
python3 -c "
import json
import sys

data = json.loads(sys.argv[1])

# Handle various input formats
if isinstance(data, list):
    records = data
elif isinstance(data, dict):
    # Try common keys for record arrays
    for key in ['data', 'records', 'results', 'items', 'body']:
        if key in data and isinstance(data[key], list):
            records = data[key]
            break
    else:
        records = [data]
else:
    records = []

# Filter out empty or null records
records = [r for r in records if r]

# Remove null values from each record
cleaned = []
for record in records:
    if isinstance(record, dict):
        cleaned.append({k: v for k, v in record.items() if v is not None})
    else:
        cleaned.append(record)

output = {
    'records': cleaned,
    'count': len(cleaned)
}

print(json.dumps(output))
" "$input"
