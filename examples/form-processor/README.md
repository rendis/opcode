# Form Processor

Validate incoming form data against a schema, classify it using a Node.js script, and route to the appropriate backend: support tickets, CRM leads, or a general inbox.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| form_data | object | yes | Form submission with name, email, subject, and message |
| ticket_api_url | string | no | Support ticket creation endpoint |
| crm_api_url | string | no | CRM lead creation endpoint |
| inbox_api_url | string | no | General inbox endpoint |

## Steps

`validate` -> `classify` -> `route` -> `create-ticket` / `create-lead` / `send-inbox`

## Features

- Schema validation ensures all required form fields are present
- Node.js classifier scores keywords to determine type and priority
- Condition branch routes to support, sales, or general inbox
- Retry with exponential backoff on all HTTP POST calls
