# Price Monitor

Loop over a list of products, fetch current prices from their URLs, and send a notification when any price drops below its configured threshold. Every check is logged.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| products | array | yes | Products to monitor, each with name, url, and threshold |
| notification_api_url | string | yes | API endpoint for price drop alerts |

## Steps

`check-prices` (loop for_each) -> per item: `fetch-price` -> `evaluate-price` -> `send-alert` / `log-no-drop` -> `log-price`

## Features

- Loop iterates over arbitrary product list
- on_error ignore on fetch so one failed URL does not stop the monitor
- Condition compares fetched price against per-product threshold
- Notification sent only when price is below threshold
- Every price check logged for audit trail
