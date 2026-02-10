#!/usr/bin/env bash
# check_backup.sh - Checks for backup files in the given directory.
# Finds the most recent backup and reports its status.
#
# Usage: check_backup.sh <backup_dir>
# Output (stdout): {"exists": true/false, "size_mb": N, "age_hours": N, "path": "..."}

set -euo pipefail

BACKUP_DIR="${1:-.}"

# Find the most recent backup file (macOS + Linux compatible).
LATEST=""
LATEST_EPOCH=0

for ext in bak sql tar.gz zip; do
  while IFS= read -r -d '' file; do
    # Get modification time as epoch seconds (macOS: stat -f%m, Linux: stat -c%Y).
    EPOCH=$(stat -f%m "$file" 2>/dev/null || stat -c%Y "$file" 2>/dev/null || echo 0)
    if [ "$EPOCH" -gt "$LATEST_EPOCH" ]; then
      LATEST_EPOCH="$EPOCH"
      LATEST="$file"
    fi
  done < <(find "$BACKUP_DIR" -maxdepth 1 -type f -name "*.$ext" -print0 2>/dev/null || true)
done

if [ -z "$LATEST" ]; then
  echo '{"exists": false, "size_mb": 0, "age_hours": 0, "path": ""}'
  exit 0
fi

NOW_EPOCH=$(date +%s)
AGE_SECONDS=$((NOW_EPOCH - LATEST_EPOCH))
AGE_HOURS=$((AGE_SECONDS / 3600))
SIZE_BYTES=$(stat -f%z "$LATEST" 2>/dev/null || stat -c%s "$LATEST" 2>/dev/null || echo 0)
SIZE_MB=$((SIZE_BYTES / 1048576))

python3 -c "
import json, sys
print(json.dumps({
    'exists': True,
    'size_mb': int(sys.argv[1]),
    'age_hours': int(sys.argv[2]),
    'path': sys.argv[3]
}))
" "$SIZE_MB" "$AGE_HOURS" "$LATEST"
