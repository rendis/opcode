# Invoice Generator

Fetch order data from an API, generate a formatted invoice using a Python script, write the invoice to a file, and send it via an email API.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| order_api_url | string | yes | API endpoint to fetch order data |
| order_id | string | yes | Order ID to generate an invoice for |
| output_path | string | yes | File path to write the generated invoice |
| email_api_url | string | yes | Email API endpoint to send the invoice |
| recipient_email | string | yes | Recipient email address |

## Steps

`fetch-order` -> `generate-invoice` -> `write-invoice` -> `send-invoice`

## Features

- HTTP fetch with retry for order data retrieval
- Python script calculates totals, tax, and formats the invoice
- File system write persists the invoice locally
- Email delivery with retry and exponential backoff
