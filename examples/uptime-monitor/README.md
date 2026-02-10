# Uptime Monitor

Loop over a list of endpoints, perform an HTTP health check on each, log results, and send an alert notification when the cycle completes.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| endpoints | array | yes | List of objects with `name` and `url` fields |
| alert_webhook_url | string | yes | Webhook URL to send alert notifications |

## Steps

`check-endpoints` (loop: `ping` -> `check-status` -> `log-up` / `log-down`) -> `send-alert`

## Features

- for_each loop over dynamic endpoint list
- HTTP GET with 5s timeout and on_error ignore (loop continues on failure)
- Condition branch checking HTTP status code
- workflow.log for up/down state per endpoint
- HTTP POST alert notification with retry
