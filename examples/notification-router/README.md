# Notification Router

Route notifications based on severity level. Critical alerts fan out to Slack, email, and an agent acknowledgment in parallel. Warnings go to Slack. Info notifications are logged only. All paths emit tracking events.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| severity | string | yes | Notification severity: critical, warning, or info |
| title | string | yes | Notification title |
| message | string | yes | Notification message body |
| slack_webhook_url | string | no | Slack webhook URL for notifications |
| email_api_url | string | no | Email API URL for critical notifications |

## Steps

`emit-received` -> `route-severity` -> (critical) `critical-alerts` (parallel: `slack-critical`, `email-critical`, `acknowledge`) -> `emit-critical-sent` | (warning) `slack-warning` -> `emit-warning-sent` | (info) `log-info` -> `emit-info-logged`

## Features

- Three-way condition branch on severity level
- Parallel multi-channel delivery for critical alerts
- Reasoning node for agent acknowledgment of critical alerts
- Event emission (workflow.emit) for tracking at every stage
- Retry with backoff on all HTTP deliveries
