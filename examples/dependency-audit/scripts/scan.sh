#!/usr/bin/env bash
# scan.sh - Mock dependency vulnerability scanner.
# Simulates scanning project dependencies and outputs results as JSON.
#
# Output (stdout): {"vulnerabilities_found": bool, "critical": N, "high": N, "medium": N, "packages": [...]}

set -euo pipefail

# Simulated scan results
cat <<'EOF'
{
  "vulnerabilities_found": true,
  "critical": 1,
  "high": 3,
  "medium": 7,
  "packages": [
    {"name": "lodash", "version": "4.17.15", "severity": "critical", "cve": "CVE-2024-00001"},
    {"name": "express", "version": "4.17.1", "severity": "high", "cve": "CVE-2024-00002"},
    {"name": "axios", "version": "0.21.1", "severity": "high", "cve": "CVE-2024-00003"},
    {"name": "jsonwebtoken", "version": "8.5.1", "severity": "high", "cve": "CVE-2024-00004"},
    {"name": "minimist", "version": "1.2.5", "severity": "medium", "cve": "CVE-2024-00005"}
  ]
}
EOF
