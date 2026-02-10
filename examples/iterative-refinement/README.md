# Iterative Refinement

Iteratively refine a draft through agent feedback loops. Uses free-form reasoning (empty options) so the agent can either approve or provide arbitrary feedback. Loops up to 5 iterations or until approved.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| draft_content | string | yes | Initial draft content to refine |

## Steps

`gen-draft-id` -> `refine-loop` (while loop: `review` -> `check-approval` -> log-approved / log-feedback)

## Features

- Free-form reasoning (empty options list) for flexible agent feedback
- While loop with max 5 iterations
- Condition branching on approval vs continued refinement
- UUID generation for draft tracking
- Workflow suspends on timeout (allows resumption)
