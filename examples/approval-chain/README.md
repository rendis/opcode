# Approval Chain

Sequential two-reviewer approval chain. Both reviewers must approve for the request to pass. Each reviewer has a 1-hour timeout with automatic rejection as fallback.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| request_id | string | yes | ID of the request requiring approval |
| request_summary | string | yes | Human-readable summary of what is being approved |

## Steps

`reviewer1` -> `reviewer2` -> `check-outcome` -> `log-approved` / `log-rejected`

## Features

- Chained reasoning nodes with sequential dependency
- Timeout with fallback to reject on each reviewer
- Condition branch evaluating both reviewer decisions
- Second reviewer skipped if first rejects
