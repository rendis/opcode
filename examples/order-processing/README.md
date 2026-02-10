# Order Processing

Validate an incoming order against a schema, check product stock via API, and branch based on availability. If in stock: process payment, notify fulfillment, and send confirmation. If out of stock: notify the customer.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| order | object | yes | Order with product_id, quantity, customer_email, payment_method |
| stock_api_url | string | yes | Stock check API endpoint |
| payment_api_url | string | yes | Payment processing API endpoint |
| fulfillment_api_url | string | yes | Fulfillment notification API endpoint |
| notification_api_url | string | yes | Customer notification API endpoint |

## Steps

`validate-order` -> `check-stock` -> `stock-decision` -> in stock: `process-payment` -> `notify-fulfillment` -> `send-confirmation` / out of stock: `notify-out-of-stock`

## Features

- Schema validation on order structure before processing
- HTTP stock check with retry
- Condition branch on stock availability
- Sequential payment -> fulfillment -> confirmation chain when in stock
- Retry with exponential backoff on all HTTP calls
