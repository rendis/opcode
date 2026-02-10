# Free-Form Decision

Fetch context data, present it to an agent for free-form assessment (no predefined options), and route based on whether the response contains a "critical" keyword.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| data_url | string | yes | URL to fetch context data for analysis |
| alert_url | string | yes | URL to POST critical alerts to |

## Steps

`fetch-context` -> `assess` -> `check-critical` -> `send-alert` / `log-ok`

## Features

- Free-form reasoning with empty options array (any text accepted)
- Keyword-based condition routing on agent response
- HTTP fetch with retry for context gathering
- Alert escalation via HTTP POST for critical findings
