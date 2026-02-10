#!/usr/bin/env python3
"""generate.py - Generate invoice data from an order JSON.

Input (stdin):  JSON order object with items, customer info, etc.
Output (stdout): {"invoice_number": "INV-...", "total": N, "tax": N, "line_items": [...], "formatted": "..."}
"""

import json
import sys
import hashlib
from datetime import datetime

order = json.load(sys.stdin)

# Extract order details
order_id = order.get("id", order.get("order_id", "unknown"))
items = order.get("items", order.get("line_items", []))
customer = order.get("customer", {})
tax_rate = order.get("tax_rate", 0.10)  # Default 10% tax

# Generate invoice number from order ID + date
date_str = datetime.utcnow().strftime("%Y%m%d")
hash_suffix = hashlib.md5(f"{order_id}-{date_str}".encode()).hexdigest()[:6].upper()
invoice_number = f"INV-{date_str}-{hash_suffix}"

# Calculate line items and totals
line_items = []
subtotal = 0.0

for item in items:
    name = item.get("name", "Unknown Item")
    quantity = item.get("quantity", 1)
    unit_price = item.get("price", item.get("unit_price", 0.0))
    line_total = quantity * unit_price
    subtotal += line_total
    line_items.append({
        "name": name,
        "quantity": quantity,
        "unit_price": unit_price,
        "total": round(line_total, 2),
    })

tax = round(subtotal * tax_rate, 2)
total = round(subtotal + tax, 2)

# Format as readable text
lines = [
    f"INVOICE: {invoice_number}",
    f"Date: {datetime.utcnow().strftime('%Y-%m-%d')}",
    f"Customer: {customer.get('name', 'N/A')} ({customer.get('email', 'N/A')})",
    "",
    "Items:",
    "-" * 50,
]

for li in line_items:
    lines.append(f"  {li['name']:30s} x{li['quantity']:3d}  ${li['unit_price']:>8.2f}  ${li['total']:>10.2f}")

lines.extend([
    "-" * 50,
    f"{'Subtotal':>48s}: ${subtotal:>10.2f}",
    f"{'Tax (' + str(int(tax_rate * 100)) + '%)':>48s}: ${tax:>10.2f}",
    f"{'TOTAL':>48s}: ${total:>10.2f}",
])

formatted = "\n".join(lines)

result = {
    "invoice_number": invoice_number,
    "total": total,
    "tax": tax,
    "line_items": line_items,
    "formatted": formatted,
}

print(json.dumps(result))
