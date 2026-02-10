# Deploy Gate

Run tests via a shell script, gate deployment on passing results and agent approval, then fire a deploy webhook. If tests fail or the agent rejects, deployment is blocked.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| deploy_webhook_url | string | yes | Webhook URL to trigger deployment |
| deploy_environment | string | no | Target environment (e.g., staging, production) |

## Steps

`run-tests` -> `check-tests` -> (if pass) `approve-deploy` -> `check-approval` -> (if approve) `deploy` / (if reject) `log-rejected` / (if fail) `log-failed`

## Features

- Shell script test execution with timeout
- Condition gating on test exit code
- Reasoning node for human/agent deployment approval
- HTTP POST webhook with retry on deploy trigger
- Defense in depth: tests must pass AND agent must approve
