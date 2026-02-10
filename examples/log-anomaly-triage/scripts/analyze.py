#!/usr/bin/env python3
"""analyze.py - Reads log text from a file (argv[1]) or stdin, detects anomalies, outputs JSON.

Output (stdout): {"anomaly_detected": bool, "anomaly_count": N, "severity": "low|medium|high", "details": "..."}
"""

import sys
import json
import re

def analyze_logs(log_text):
    """Analyze log text for common anomaly patterns."""
    lines = log_text.strip().split("\n") if log_text.strip() else []

    error_patterns = [
        r"(?i)\bERROR\b",
        r"(?i)\bFATAL\b",
        r"(?i)\bCRITICAL\b",
        r"(?i)\bOOM\b",
        r"(?i)out of memory",
        r"(?i)connection refused",
        r"(?i)timeout exceeded",
        r"(?i)panic:",
        r"(?i)segfault",
    ]

    anomalies = []
    for i, line in enumerate(lines):
        for pattern in error_patterns:
            if re.search(pattern, line):
                anomalies.append({"line": i + 1, "pattern": pattern, "text": line[:200]})
                break

    anomaly_count = len(anomalies)
    anomaly_detected = anomaly_count > 0

    if anomaly_count == 0:
        severity = "low"
    elif anomaly_count <= 5:
        severity = "medium"
    else:
        severity = "high"

    # Build details summary
    if anomaly_detected:
        sample = anomalies[:3]
        details = f"Found {anomaly_count} anomalies in {len(lines)} log lines. "
        details += "Sample: " + "; ".join(
            f"line {a['line']}: {a['text'][:80]}" for a in sample
        )
    else:
        details = f"No anomalies found in {len(lines)} log lines."

    return {
        "anomaly_detected": anomaly_detected,
        "anomaly_count": anomaly_count,
        "severity": severity,
        "details": details,
    }


if __name__ == "__main__":
    if len(sys.argv) > 1:
        with open(sys.argv[1], 'r') as f:
            log_text = f.read()
    else:
        log_text = sys.stdin.read()
    result = analyze_logs(log_text)
    print(json.dumps(result))
