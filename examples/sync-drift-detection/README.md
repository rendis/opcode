# Sync Drift Detection

Fetch two data sources in parallel, compare them with a diff script, triage any detected drift via agent reasoning, and sync if approved.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| source_a_url | string | yes | URL for primary data source |
| source_b_url | string | yes | URL for secondary data source |
| sync_url | string | yes | URL to POST sync data to |

## Steps

`fetch-sources` (parallel: fetch-a, fetch-b) -> `diff` -> `check-drift` -> (if drift) `triage-drift` -> `sync-data` / (if clean) `log-no-drift`

## Features

- Parallel HTTP fetches for both data sources
- Shell script for JSON diffing
- Condition branching on drift detection
- Reasoning node for sync approval
- Conditional step execution (sync only if approved)
- Retry with exponential backoff on HTTP calls
