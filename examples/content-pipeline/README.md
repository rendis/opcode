# Content Pipeline

Generate a unique content ID, submit draft content for agent review with publish/revise/reject options, then publish with parallel notifications or log the rejection.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| content | string | yes | Draft content to be reviewed |
| author | string | no | Content author name |
| publish_api_url | string | yes | API endpoint to publish content |
| slack_webhook_url | string | yes | Slack webhook for notifications |
| email_api_url | string | yes | Email API for notifications |

## Steps

`generate-id` -> `review-content` (reasoning) -> `handle-decision` -> publish: `publish-content` -> `notify-published` (parallel: `notify-slack` + `notify-email`) / reject: `log-rejection` / revise: `log-revise`

## Features

- crypto.uuid generates a unique content identifier
- Reasoning node with three options (publish, revise, reject) and data injection
- Timeout fallback to reject ensures stale reviews do not block the pipeline
- Parallel notification fan-out on publish (Slack + email)
- on_error ignore on notifications so a failed alert does not block the workflow
