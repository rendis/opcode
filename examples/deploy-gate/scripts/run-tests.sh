#!/usr/bin/env bash
# run-tests.sh - Mock test runner that outputs test results as JSON.
#
# Output (stdout): {"tests_passed": N, "tests_failed": N, "total": N}
# Exit code: 0 if all pass, 1 if any fail

set -euo pipefail

# Simulate test execution
passed=42
failed=0
total=$((passed + failed))

echo "{\"tests_passed\": ${passed}, \"tests_failed\": ${failed}, \"total\": ${total}}"

if [ "$failed" -gt 0 ]; then
  exit 1
fi

exit 0
