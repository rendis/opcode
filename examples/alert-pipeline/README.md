# Alert Pipeline

Route alerts by severity level. Critical alerts trigger parallel notifications across Slack DM, email, and a workflow event. Warnings go to a Slack channel. Info-level alerts are logged.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| severity | string | yes | Alert severity: critical, warning, or info |
| title | string | yes | Alert title |
| message | string | yes | Alert message body |
| slack_dm_url | string | no | Slack webhook for direct messages (critical) |
| slack_channel_url | string | no | Slack webhook for channel posts (warning) |
| email_api_url | string | no | Email API endpoint (critical) |

## Steps

`route-severity` -> critical: `critical-notify` (parallel: `slack-dm` + `send-email` + `emit-critical`) / warning: `slack-channel` / info: `log-info` / default: `log-unknown`

## Features

- Condition-based routing on severity level
- Parallel notification fan-out for critical alerts
- workflow.emit broadcasts a signal for downstream consumers
- Retry with exponential backoff on all HTTP calls
- Default branch handles unexpected severity values
