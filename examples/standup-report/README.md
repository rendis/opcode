# Standup Report

Fetch tickets, PRs, and activity logs in parallel from three APIs, merge them into a markdown-formatted standup report via a Python script, and write the result to a file.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| tickets_url | string | yes | API URL to fetch open tickets |
| prs_url | string | yes | API URL to fetch recent pull requests |
| logs_url | string | yes | API URL to fetch activity logs |
| output_path | string | yes | File path to write the standup report |

## Steps

`gather-data` (parallel: `fetch-tickets`, `fetch-prs`, `fetch-logs`) -> `merge-report` -> `write-report`

## Features

- Parallel HTTP fetches for all three data sources
- Python script merges and formats data into markdown
- Script outputs structured JSON with report, date, and item count
- File write for persisting the final report
