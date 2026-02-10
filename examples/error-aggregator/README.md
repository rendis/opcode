# Error Aggregator

Fetch error logs from an API, parse and group them by type using a Python script, and raise a reasoning decision if new errors are found. On approval, create an investigation ticket.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| error_logs_url | string | yes | API endpoint to fetch error log entries |
| ticket_api_url | string | yes | API endpoint to create investigation tickets |

## Steps

`fetch-logs` -> `parse-errors` -> `check-new-errors` -> `triage-decision` -> `create-ticket`

## Features

- HTTP fetch with retry and exponential backoff
- Shell script (Python) for error log parsing and grouping
- Condition branch on new error count
- Reasoning node with investigate/defer/ignore options
- HTTP POST to create investigation ticket on approval
