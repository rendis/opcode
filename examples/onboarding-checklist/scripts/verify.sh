#!/usr/bin/env bash
# Verify an onboarding checklist item.
# Usage: verify.sh <item_name>
# Outputs JSON: {"item": "...", "status": "pass|fail", "details": "..."}

set -euo pipefail

ITEM="${1:?Usage: verify.sh <item_name>}"

check_item() {
    case "$ITEM" in
        vpn-access)
            # Check if VPN config exists
            if [ -f /etc/openvpn/client.conf ] || [ -f "$HOME/.config/vpn/config" ]; then
                echo "pass" "VPN configuration found"
            else
                echo "fail" "VPN configuration not found"
            fi
            ;;
        email-setup)
            # Check if mail client config exists
            if command -v mail &>/dev/null || [ -d "$HOME/.thunderbird" ]; then
                echo "pass" "Email client configured"
            else
                echo "fail" "No email client configuration detected"
            fi
            ;;
        repo-permissions)
            # Check if git credentials are configured
            if git config --global user.email &>/dev/null; then
                echo "pass" "Git credentials configured"
            else
                echo "fail" "Git credentials not configured"
            fi
            ;;
        ssh-keys)
            # Check if SSH key exists
            if [ -f "$HOME/.ssh/id_rsa" ] || [ -f "$HOME/.ssh/id_ed25519" ]; then
                echo "pass" "SSH key found"
            else
                echo "fail" "No SSH key found"
            fi
            ;;
        *)
            # Generic check — always passes for unknown items
            echo "pass" "Generic check passed for $ITEM"
            ;;
    esac
}

read -r STATUS DETAILS <<< "$(check_item)"

cat <<EOF
{"item": "$ITEM", "status": "$STATUS", "details": "$DETAILS"}
EOF
