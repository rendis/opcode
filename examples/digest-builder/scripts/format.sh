#!/usr/bin/env bash
# format.sh - Format fetched results into a markdown digest.
#
# Input (stdin):  JSON array of fetched HTTP results (loop output)
# Output (stdout): {"digest": "# Daily Digest\n...", "item_count": N, "date": "YYYY-MM-DD"}

set -euo pipefail

input=$(cat)

python3 -c "
import json, sys
from datetime import datetime

data = json.loads(sys.argv[1])
date_str = datetime.utcnow().strftime('%Y-%m-%d')

items = []
if isinstance(data, list):
    for entry in data:
        # Each entry is a loop iteration result; extract body or status
        if isinstance(entry, dict):
            body = entry.get('body', entry.get('stdout', ''))
            status = entry.get('status_code', 'N/A')
            url = entry.get('url', 'unknown')
            if body:
                # Truncate body to first 200 chars for digest
                snippet = str(body)[:200]
                items.append(f'### {url}\n\nStatus: {status}\n\n{snippet}\n')

item_count = len(items)
digest_parts = [f'# Daily Digest - {date_str}\n']
digest_parts.append(f'**{item_count} sources processed**\n')

if items:
    digest_parts.extend(items)
else:
    digest_parts.append('No content fetched.\n')

digest = '\n'.join(digest_parts)

result = {
    'digest': digest,
    'item_count': item_count,
    'date': date_str
}
print(json.dumps(result))
" "$input"
