# Escalation Ladder

Auto-check a service health endpoint. If unhealthy, wait briefly then present an agent with a resolve-or-escalate decision. On timeout the decision auto-escalates. Escalation sends a webhook notification.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| health_url | string | yes | URL to perform the health check against |
| escalation_webhook_url | string | yes | Webhook URL for escalation notifications |

## Steps

`auto-check` -> `check-health` -> (if failed) `wait-before-decision` -> `agent-triage` -> `check-decision` -> `send-escalation` / `log-resolve`

## Features

- HTTP health check with error ignored (to capture status codes)
- Wait step before agent decision (cooldown period)
- Reasoning with short timeout and auto-escalate fallback
- Conditional branching on both health status and agent decision
- Webhook POST with retry for escalation delivery
