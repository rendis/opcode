# Onboarding Drip

Time-delayed onboarding email sequence that sends a welcome email, a tips email, and then prompts an agent to decide whether the user is engaged enough for advanced content.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| user_email | string | yes | Email address of the newly onboarded user |
| user_name | string | yes | Display name of the user |
| email_api_url | string | yes | API endpoint for sending emails |

## Steps

`log-onboarded` -> `wait-welcome` -> `send-welcome` -> `wait-tips` -> `send-tips` -> `wait-engagement` -> `engagement-decision`

## Features

- Wait steps for time-delayed email delivery (1s/2s/3s for testing; real use: hours/days)
- HTTP POST for email sending with retry
- Reasoning node for engagement assessment with 1h timeout
- Fallback to pause-drip if no decision is made
- on_timeout suspend so the workflow can be resumed later
