#!/bin/bash
# =============================================================================
# STOC E2E Devnet Test
# Runs test suites against devnet (remote chain, no start/stop)
#
# Usage: ./run_devnet.sh [--suite 01,02,03]
#
# Prerequisites:
#   - stocd binary available in PATH
#   - Test wallets funded on devnet
#   - Devnet node accessible
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Devnet configuration
# ---------------------------------------------------------------------------
export CHAIN_ID="${CHAIN_ID:-dstoc_13061999-1}"
export NODE="${NODE:-tcp://localhost:26657}"  # Override with devnet RPC
export KEYRING_BACKEND="${KEYRING_BACKEND:-test}"
export HOME_DIR="${HOME_DIR:-}"
export BINARY="${BINARY:-stocd}"
export EVM_RPC="${EVM_RPC:-http://localhost:8545}"

# Devnet-specific overrides
# For devnet, override these with the actual devnet node:
#   export NODE="tcp://DEVNET_IP:26657"
#   export EVM_RPC="http://DEVNET_IP:8545"

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
SUITES_FILTER=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --suite)
            SUITES_FILTER="$2"
            shift 2
            ;;
        --node)
            NODE="$2"
            shift 2
            ;;
        --chain-id)
            CHAIN_ID="$2"
            shift 2
            ;;
        --evm-rpc)
            EVM_RPC="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --suite ID,ID  Run specific suites (01-07)"
            echo "  --node URL     RPC endpoint (default: tcp://localhost:26657)"
            echo "  --chain-id ID  Chain ID (default: dstoc_13061999-1)"
            echo "  --evm-rpc URL  EVM JSON-RPC (default: http://localhost:8545)"
            echo ""
            echo "Examples:"
            echo "  $0 --node tcp://devnet-ip:26657 --suite 01,04"
            echo "  CHAIN_ID=dstoc_13061999-1 NODE=tcp://10.0.0.1:26657 $0"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Source libraries
# ---------------------------------------------------------------------------
source "$SCRIPT_DIR/lib/test_framework.sh"
source "$SCRIPT_DIR/lib/chain_helpers.sh"
source "$SCRIPT_DIR/lib/wallet_setup.sh"

# Source test suites
source "$SCRIPT_DIR/suites/01_token_lifecycle.sh"
source "$SCRIPT_DIR/suites/02_tax_system.sh"
source "$SCRIPT_DIR/suites/03_evm_isolation.sh"
source "$SCRIPT_DIR/suites/04_denom_conversion.sh"
source "$SCRIPT_DIR/suites/05_gas_enforcement.sh"
source "$SCRIPT_DIR/suites/06_ibc_restriction.sh"
source "$SCRIPT_DIR/suites/07_edge_cases.sh"

# ---------------------------------------------------------------------------
# Check suite filter
# ---------------------------------------------------------------------------
should_run_suite() {
    local suite_num="$1"
    if [[ -z "$SUITES_FILTER" ]]; then
        return 0
    fi
    echo "$SUITES_FILTER" | tr ',' '\n' | grep -q "^${suite_num}$"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
echo ""
echo -e "${BOLD}=================================================================${NC}"
echo -e "${BOLD}  STOC E2E DEVNET TEST${NC}"
echo -e "${BOLD}  Chain: $CHAIN_ID${NC}"
echo -e "${BOLD}  Node:  $NODE${NC}"
echo -e "${BOLD}  EVM:   $EVM_RPC${NC}"
echo -e "${BOLD}  $(date '+%Y-%m-%d %H:%M:%S')${NC}"
echo -e "${BOLD}=================================================================${NC}"
echo ""

# Verify chain is accessible
wait_for_chain 30 || { log_error "Devnet not accessible at $NODE"; exit 1; }

# Setup wallets (import if not already in keyring)
log_info "Setting up wallets for devnet testing ..."

# On devnet, admin/validator keys might be different
# Check if admin key exists, if not, check for alternative names
if ! get_address "$WALLET_ADMIN" >/dev/null 2>&1; then
    log_warn "Admin key '$WALLET_ADMIN' not in keyring"
    log_info "Trying to import EVM wallets ..."
fi

import_wallet "$WALLET_EVM1" "$EVM_WALLET1_MNEMONIC"
import_wallet "$WALLET_EVM2" "$EVM_WALLET2_MNEMONIC"

ADDR_EVM1=$(get_address "$WALLET_EVM1" 2>/dev/null || echo "$EVM_WALLET1_COSMOS")
ADDR_EVM2=$(get_address "$WALLET_EVM2" 2>/dev/null || echo "$EVM_WALLET2_COSMOS")
ADDR_ADMIN=$(get_address "$WALLET_ADMIN" 2>/dev/null || echo "")
ADDR_VAL1=$(get_address "$WALLET_VAL1" 2>/dev/null || echo "")
ADDR_VAL2=$(get_address "$WALLET_VAL2" 2>/dev/null || echo "")

log_info "Wallet addresses:"
log_info "  admin      = ${ADDR_ADMIN:-NOT SET}"
log_info "  validator1 = ${ADDR_VAL1:-NOT SET}"
log_info "  evm_wallet1= $ADDR_EVM1 / $EVM_WALLET1_ETH"
log_info "  evm_wallet2= $ADDR_EVM2 / $EVM_WALLET2_ETH"

# Check balances
log_info "Checking wallet balances ..."
for addr_var in ADDR_ADMIN ADDR_EVM1 ADDR_EVM2; do
    local addr="${!addr_var}"
    if [[ -n "$addr" ]]; then
        local bal
        bal=$(get_balance "$addr" "ustoc" 2>/dev/null || echo "0")
        log_info "  $addr_var ($addr): ${bal} ustoc"
    fi
done

echo ""

# Note: On devnet, wallets need to be pre-funded
# If wallets don't have enough balance, tests will fail
if [[ -n "$ADDR_ADMIN" ]]; then
    local admin_bal
    admin_bal=$(get_balance "$ADDR_ADMIN" "ustoc" 2>/dev/null || echo "0")
    if [[ "$admin_bal" -lt 200000000 ]]; then
        log_warn "Admin balance too low ($admin_bal ustoc). Fund admin with at least 200 STOC for testing."
        log_warn "Some tests may fail due to insufficient funds."
    fi
fi

# Run test suites
if should_run_suite "01"; then run_token_lifecycle_tests; fi
if should_run_suite "02"; then run_tax_system_tests; fi
if should_run_suite "03"; then run_evm_isolation_tests; fi
if should_run_suite "04"; then run_denom_conversion_tests; fi
if should_run_suite "05"; then run_gas_enforcement_tests; fi
if should_run_suite "06"; then run_ibc_restriction_tests; fi
if should_run_suite "07"; then run_edge_case_tests; fi

# Summary
print_summary
exit $?
