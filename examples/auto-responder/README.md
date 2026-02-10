# Auto-Responder

Check if an incoming message matches known FAQ keywords. If it does, generate a tracking hash and send a template response. If not, use a free-form reasoning node to draft a custom reply.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| message | string | yes | Incoming message text |
| faq_keywords | array | yes | Keywords that indicate a known FAQ question |
| respond_api_url | string | yes | API endpoint to send the response |
| sender_id | string | yes | ID of the message sender |
| template_response | string | no | Canned response for FAQ matches |

## Steps

`check-faq` -> true: `hash-message` -> `send-template` / false: `draft-response` (reasoning) -> `send-custom`

## Features

- CEL expression checks if any FAQ keyword appears in the message
- crypto.hash generates a SHA-256 tracking hash for FAQ responses
- Free-form reasoning node (empty options) for custom response drafting
- HTTP retry on both response paths
