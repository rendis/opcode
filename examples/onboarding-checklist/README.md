# Onboarding Checklist

Loop through a list of onboarding checklist items, verify each via a shell script, and escalate failures to an agent who decides whether to retry, skip, or block.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| checklist_items | array | yes | Array of item names to verify (e.g. ["vpn-access", "email-setup"]) |

## Steps

`verify-items` (for each item: `verify` -> `check-result` -> `decide-action` / `log-pass`)

## Features

- For-each loop over input checklist array
- Shell script verification with structured JSON output
- Condition branch on verification pass/fail
- Reasoning node for agent-driven failure triage (retry/skip/block)
- Error ignored on shell step to capture exit codes gracefully
