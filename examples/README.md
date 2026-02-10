# Opcode Example Workflows

44 ready-to-use workflow definitions covering agent automation, DevOps, data pipelines, integrations, and more.

## Quick Start

```jsonc
// 1. Register a template
// opcode.define
{
  "name": "content-summarizer",
  "agent_id": "my-agent",
  "definition": { /* copy from workflow.json → definition */ }
}

// 2. Execute it
// opcode.run
{
  "template_name": "content-summarizer",
  "agent_id": "my-agent",
  "params": { "url": "https://example.com/article" }
}
```

## Script Convention

Scripts receive input via **stdin** (JSON) and produce output via **stdout** (JSON). `shell.exec` auto-parses JSON stdout, so next steps can access fields directly with `${{steps.cmd.output.stdout.field}}`.

| Language | Command                | Boilerplate                                                |
| -------- | ---------------------- | ---------------------------------------------------------- |
| Bash     | `bash scripts/x.sh`    | `set -euo pipefail; input=$(cat -)`                        |
| Python   | `python3 scripts/x.py` | `json.load(sys.stdin)` / `json.dump(result, sys.stdout)`   |
| Node     | `node scripts/x.js`    | stdin stream / `JSON.parse` / `JSON.stringify`             |
| Go       | `go run scripts/x.go`  | `json.NewDecoder(os.Stdin)` / `json.NewEncoder(os.Stdout)` |

Use `${{steps.X.output.stdout_raw}}` for raw unprocessed text.

## Catalog

### Agent Ops

| #   | Example                                         | Features                          | Script |
| --- | ----------------------------------------------- | --------------------------------- | ------ |
| 1   | [content-summarizer](content-summarizer/)       | http, shell, reasoning, fs        | Bash   |
| 2   | [multi-source-research](multi-source-research/) | parallel, reasoning, condition    | -      |
| 3   | [iterative-refinement](iterative-refinement/)   | loop(while), reasoning(free-form) | -      |

### DevOps / CI-CD

| #   | Example                                   | Features                          | Script |
| --- | ----------------------------------------- | --------------------------------- | ------ |
| 4   | [deploy-gate](deploy-gate/)               | shell, condition, reasoning, http | Bash   |
| 5   | [log-anomaly-triage](log-anomaly-triage/) | fs, shell, condition, reasoning   | Python |
| 6   | [health-check-sweep](health-check-sweep/) | parallel, loop, condition         | -      |

### Data Pipelines

| #   | Example                                       | Features                              | Script |
| --- | --------------------------------------------- | ------------------------------------- | ------ |
| 7   | [etl-with-validation](etl-with-validation/)   | http, shell, assert, retry, fallback  | Bash   |
| 8   | [batch-file-processor](batch-file-processor/) | fs, loop(for_each), crypto            | -      |
| 9   | [sync-drift-detection](sync-drift-detection/) | parallel, shell, condition, reasoning | Bash   |

### Security / Compliance

| #   | Example                               | Features                           | Script |
| --- | ------------------------------------- | ---------------------------------- | ------ |
| 10  | [secret-rotation](secret-rotation/)   | crypto, http, assert, workflow.log | -      |
| 11  | [dependency-audit](dependency-audit/) | shell, condition, reasoning, http  | Bash   |

### Agentic / Human-in-the-Loop

| #   | Example                                   | Features                                     | Script |
| --- | ----------------------------------------- | -------------------------------------------- | ------ |
| 12  | [approval-chain](approval-chain/)         | reasoning(x2), condition                     | -      |
| 13  | [free-form-decision](free-form-decision/) | http, reasoning(free-form), condition        | -      |
| 14  | [escalation-ladder](escalation-ladder/)   | http, condition, reasoning(timeout+fallback) | -      |

### Daily / Management

| #   | Example                                           | Features                                      | Script |
| --- | ------------------------------------------------- | --------------------------------------------- | ------ |
| 15  | [standup-report](standup-report/)                 | parallel, shell, fs                           | Python |
| 16  | [task-triage](task-triage/)                       | http, loop, reasoning                         | -      |
| 17  | [meeting-prep-brief](meeting-prep-brief/)         | parallel, fs                                  | -      |
| 18  | [knowledge-base-updater](knowledge-base-updater/) | http, crypto, condition, reasoning, fs        | -      |
| 19  | [onboarding-checklist](onboarding-checklist/)     | loop, shell, condition, reasoning             | Bash   |
| 20  | [notification-router](notification-router/)       | condition, parallel, reasoning, workflow.emit | -      |

### Integrations / Sync

| #   | Example                             | Features                   | Script |
| --- | ----------------------------------- | -------------------------- | ------ |
| 21  | [webhook-handler](webhook-handler/) | assert, shell, http, retry | Node   |
| 22  | [lead-enrichment](lead-enrichment/) | parallel, http             | -      |
| 23  | [two-way-sync](two-way-sync/)       | parallel, shell, loop      | Bash   |
| 24  | [form-processor](form-processor/)   | assert, shell, condition   | Node   |

### Communication

| #   | Example                           | Features                              | Script |
| --- | --------------------------------- | ------------------------------------- | ------ |
| 25  | [alert-pipeline](alert-pipeline/) | condition, parallel, workflow.emit    | -      |
| 26  | [digest-builder](digest-builder/) | loop, shell, http                     | Bash   |
| 27  | [auto-responder](auto-responder/) | condition, reasoning(free-form), http | -      |

### E-Commerce / Business

| #   | Example                                 | Features                       | Script |
| --- | --------------------------------------- | ------------------------------ | ------ |
| 28  | [order-processing](order-processing/)   | assert, http, condition, retry | -      |
| 29  | [invoice-generator](invoice-generator/) | http, shell, fs                | Python |
| 30  | [price-monitor](price-monitor/)         | loop, http, condition          | -      |

### Content / Publishing

| #   | Example                                 | Features                             | Script |
| --- | --------------------------------------- | ------------------------------------ | ------ |
| 31  | [content-pipeline](content-pipeline/)   | reasoning, condition, http, parallel | -      |
| 32  | [image-processing](image-processing/)   | fs, shell, http                      | Bash   |
| 33  | [social-cross-post](social-cross-post/) | parallel, http, on_error(ignore)     | -      |

### Monitoring / Ops

| #   | Example                                     | Features                                | Script |
| --- | ------------------------------------------- | --------------------------------------- | ------ |
| 34  | [error-aggregator](error-aggregator/)       | http, shell, condition, reasoning       | Python |
| 35  | [uptime-monitor](uptime-monitor/)           | loop, http, condition, on_error(ignore) | -      |
| 36  | [backup-verification](backup-verification/) | shell, assert, condition, reasoning     | Bash   |

### Data Transformation

| #   | Example                               | Features                  | Script |
| --- | ------------------------------------- | ------------------------- | ------ |
| 37  | [csv-to-api](csv-to-api/)             | fs, shell, loop, http     | Python |
| 38  | [data-anonymizer](data-anonymizer/)   | fs, loop, crypto          | -      |
| 39  | [report-generator](report-generator/) | parallel, shell, fs, http | Go     |

### User Lifecycle

| #   | Example                             | Features                                | Script |
| --- | ----------------------------------- | --------------------------------------- | ------ |
| 40  | [onboarding-drip](onboarding-drip/) | wait, http, reasoning(timeout+fallback) | -      |

### Git / Version Control

| #   | Example                                               | Features                              | Script |
| --- | ----------------------------------------------------- | ------------------------------------- | ------ |
| 41  | [git-changelog](git-changelog/)                       | shell, condition, reasoning, fs       | Python |
| 42  | [git-branch-cleanup](git-branch-cleanup/)             | shell, loop, condition                | -      |
| 43  | [openclaw-version-checker](openclaw-version-checker/) | shell, condition, http                | Bash   |
| 44  | [repo-health-check](repo-health-check/)               | parallel, shell, condition, reasoning | -      |

## Feature Index

| Feature                      | Examples                                                           |
| ---------------------------- | ------------------------------------------------------------------ |
| reasoning (options)          | 1, 4, 5, 9, 11, 12, 16, 19, 27, 31, 34, 36, 41, 44                 |
| reasoning (free-form)        | 3, 13, 40                                                          |
| reasoning (timeout+fallback) | 3, 14, 36, 40                                                      |
| parallel                     | 2, 6, 15, 17, 20, 22, 25, 31, 33, 39, 44                           |
| loop (for_each)              | 8, 16, 19, 23, 26, 30, 35, 37, 38, 42                              |
| loop (while)                 | 3                                                                  |
| condition                    | 4, 5, 6, 9, 13, 18, 20, 24, 25, 27, 28, 30, 34, 35, 36, 41, 42, 43 |
| wait                         | 14, 40                                                             |
| retry + backoff              | 4, 7, 21, 28                                                       |
| fallback_step                | 7, 14                                                              |
| on_error ignore              | 6, 26, 33, 35                                                      |
| assert.\*                    | 7, 10, 21, 24, 28, 36                                              |
| workflow.emit                | 20, 25                                                             |
| sub-workflow                 | 15                                                                 |
| auto-parse stdout            | 5, 15, 21, 24, 29, 34, 37, 39, 41, 43                              |
