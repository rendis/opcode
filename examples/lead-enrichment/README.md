# Lead Enrichment

Enrich a sales lead by looking up company data and social profile in parallel, then push the combined enriched profile to a CRM system.

## Inputs

| Param | Type | Required | Description |
|-------|------|----------|-------------|
| company_lookup_url | string | yes | API URL for company data lookup |
| social_lookup_url | string | yes | API URL for social profile lookup |
| crm_url | string | yes | CRM API URL to POST the enriched lead to |
| lead_email | string | yes | Email address of the lead to enrich |

## Steps

`enrich` (parallel: `lookup-company`, `lookup-social`) -> `push-to-crm`

## Features

- Parallel HTTP lookups for company and social data
- Retry with exponential backoff on all HTTP calls
- Combined enriched payload pushed to CRM in a single POST
- Parameterized URLs for flexibility across environments
