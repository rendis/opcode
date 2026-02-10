# Two-Way Sync

Fetch records from two systems in parallel, compute differences via a shell script, and upsert each difference to the appropriate target system.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| system_a_url | string | yes | API endpoint to fetch records from system A |
| system_b_url | string | yes | API endpoint to fetch records from system B |
| upsert_a_url | string | yes | API endpoint to upsert records into system A |
| upsert_b_url | string | yes | API endpoint to upsert records into system B |

## Steps

`fetch-both` (parallel: `fetch-a` + `fetch-b`) -> `compute-diff` -> `sync-diffs` (loop) -> `log-result`

## Features

- Parallel fetch from two systems simultaneously
- Shell script diff compares records and identifies mismatches
- Loop iterates over each difference and upserts to the correct target
- Retry with exponential backoff on fetches, linear on upserts
