#!/bin/bash
# =============================================================================
# STOC E2E Full Upgrade Test
# Tests data integrity across upgrade path: no-evm -> v1.0.0-rc2 -> v0.6.0
#
# Requires pre-compiled binaries:
#   BINARY_NOEVM    — main branch, no EVM
#   BINARY_EVM_V1   — feat/evm (clean, WITHOUT v3 handler)
#   BINARY_V060     — fix/evm_v0.6.0
#
# Usage: ./run_full_upgrade.sh
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# ---------------------------------------------------------------------------
# Binary paths (override via env)
# ---------------------------------------------------------------------------
BINARY_NOEVM="${BINARY_NOEVM:-/tmp/stoc_noevm_d}"
BINARY_EVM_V1="${BINARY_EVM_V1:-/tmp/stoc_featevm_clean_d}"
BINARY_V060="${BINARY_V060:-/tmp/stoc_v0.6.0_d}"

# Chain config
CHAIN_ID="stoctest"
CHAIN_HOME="/tmp/stoc_e2e_upgrade_home"
CHAIN_LOG="/tmp/stoc_e2e_upgrade.log"
NODE="tcp://localhost:26657"
KEYRING_BACKEND="test"
DENOM="ustoc"
MONIKER="validator-test"
VOTING_PERIOD="30s"  # Fast governance for testing

# ---------------------------------------------------------------------------
# Source libraries
# ---------------------------------------------------------------------------
source "$SCRIPT_DIR/lib/test_framework.sh"
source "$SCRIPT_DIR/lib/chain_helpers.sh"
source "$SCRIPT_DIR/lib/wallet_setup.sh"

# Override chain helpers config
HOME_DIR="$CHAIN_HOME"
export HOME_DIR CHAIN_ID NODE KEYRING_BACKEND

# Source test suites
source "$SCRIPT_DIR/suites/01_token_lifecycle.sh"
source "$SCRIPT_DIR/suites/02_tax_system.sh"
source "$SCRIPT_DIR/suites/03_evm_isolation.sh"
source "$SCRIPT_DIR/suites/04_denom_conversion.sh"
source "$SCRIPT_DIR/suites/05_gas_enforcement.sh"
source "$SCRIPT_DIR/suites/06_ibc_restriction.sh"
source "$SCRIPT_DIR/suites/07_edge_cases.sh"

# ---------------------------------------------------------------------------
# Check binaries exist
# ---------------------------------------------------------------------------
check_binaries() {
    log_info "Checking binaries ..."
    local missing=false
    for bin in "$BINARY_NOEVM" "$BINARY_EVM_V1" "$BINARY_V060"; do
        if [[ ! -x "$bin" ]]; then
            log_error "Binary not found or not executable: $bin"
            missing=true
        else
            log_info "  OK: $bin ($(du -h "$bin" | awk '{print $1}'))"
        fi
    done
    if [[ "$missing" == "true" ]]; then
        echo ""
        echo "Required binaries:"
        echo "  BINARY_NOEVM   = $BINARY_NOEVM   (main branch, no EVM)"
        echo "  BINARY_EVM_V1  = $BINARY_EVM_V1   (feat/evm, clean)"
        echo "  BINARY_V060    = $BINARY_V060      (fix/evm_v0.6.0)"
        echo ""
        echo "Build them with:"
        echo "  git checkout main && go build -o /tmp/stoc_noevm_d ./cmd/stocd"
        echo "  git checkout feat/evm && go build -o /tmp/stoc_featevm_clean_d ./cmd/stocd"
        echo "  git checkout fix/evm_v0.6.0 && go build -o /tmp/stoc_v0.6.0_d ./cmd/stocd"
        return 1
    fi
}

# ---------------------------------------------------------------------------
# Chain lifecycle helpers
# ---------------------------------------------------------------------------
CHAIN_PID=""

init_chain() {
    local binary="$1"
    log_info "Initializing chain with $binary ..."

    # Clean previous state
    rm -rf "$CHAIN_HOME"

    # Init
    $binary init "$MONIKER" --chain-id "$CHAIN_ID" --home "$CHAIN_HOME" 2>/dev/null

    # Create validator key
    $binary keys add validator --keyring-backend test --home "$CHAIN_HOME" 2>/dev/null
    $binary keys add admin --keyring-backend test --home "$CHAIN_HOME" 2>/dev/null

    # Add genesis accounts
    local val_addr
    val_addr=$($binary keys show validator --keyring-backend test --home "$CHAIN_HOME" -a)
    local admin_addr
    admin_addr=$($binary keys show admin --keyring-backend test --home "$CHAIN_HOME" -a)

    $binary genesis add-genesis-account "$val_addr" "100000000000000${DENOM}" --home "$CHAIN_HOME" 2>/dev/null
    $binary genesis add-genesis-account "$admin_addr" "100000000000000${DENOM}" --home "$CHAIN_HOME" 2>/dev/null

    # Fast governance
    local genesis_file="$CHAIN_HOME/config/genesis.json"
    # Update voting period to 30s
    local tmp
    tmp=$(jq ".app_state.gov.params.voting_period = \"$VOTING_PERIOD\" | \
              .app_state.gov.params.max_deposit_period = \"30s\" | \
              .app_state.gov.params.expedited_voting_period = \"15s\"" "$genesis_file")
    echo "$tmp" > "$genesis_file"

    # Create gentx
    $binary genesis gentx validator "10000000000${DENOM}" \
        --chain-id "$CHAIN_ID" \
        --keyring-backend test \
        --home "$CHAIN_HOME" \
        --moniker "$MONIKER" 2>/dev/null

    $binary genesis collect-gentxs --home "$CHAIN_HOME" 2>/dev/null

    # Set minimum-gas-prices in app.toml
    sed -i.bak 's/minimum-gas-prices = ""/minimum-gas-prices = "0.0001ustoc"/' "$CHAIN_HOME/config/app.toml" 2>/dev/null || \
    sed -i '' 's/minimum-gas-prices = ""/minimum-gas-prices = "0.0001ustoc"/' "$CHAIN_HOME/config/app.toml" 2>/dev/null

    log_info "Chain initialized at $CHAIN_HOME"
}

start_node() {
    local binary="$1"
    log_info "Starting node with $binary ..."

    $binary start --home "$CHAIN_HOME" \
        --minimum-gas-prices "0.0001${DENOM}" \
        > "$CHAIN_LOG" 2>&1 &
    CHAIN_PID=$!
    log_info "Node PID: $CHAIN_PID"

    # Update BINARY for chain_helpers
    BINARY="$binary"
}

stop_node() {
    if [[ -n "$CHAIN_PID" ]] && kill -0 "$CHAIN_PID" 2>/dev/null; then
        log_info "Stopping node (PID: $CHAIN_PID) ..."
        kill "$CHAIN_PID" 2>/dev/null
        wait "$CHAIN_PID" 2>/dev/null || true
        sleep 2
        log_info "Node stopped"
    fi
    CHAIN_PID=""
}

swap_binary_and_restart() {
    local new_binary="$1"
    stop_node
    start_node "$new_binary"
    wait_for_chain 60
}

cleanup() {
    stop_node
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Upgrade helpers
# ---------------------------------------------------------------------------

# Submit upgrade proposal, vote, wait for halt, swap binary
do_upgrade() {
    local upgrade_name="$1"
    local new_binary="$2"
    local blocks_ahead="${3:-10}"

    log_info "=== Starting upgrade: $upgrade_name ==="

    # Calculate upgrade height
    local current_height
    current_height=$(get_height)
    local upgrade_height=$((current_height + blocks_ahead))
    log_info "Current height: $current_height, upgrade height: $upgrade_height"

    # Submit proposal
    log_info "Submitting upgrade proposal ..."
    local prop_result
    prop_result=$(send_tx "$BINARY tx gov submit-proposal software-upgrade '$upgrade_name' \
        --title 'Upgrade to $upgrade_name' \
        --description 'E2E test upgrade' \
        --upgrade-height $upgrade_height \
        --deposit 10000000${DENOM} \
        --from validator \
        --no-validate")

    wait_for_block 1

    # Find proposal ID (usually 1, 2, 3...)
    local proposals
    proposals=$($BINARY query gov proposals --node "$NODE" --output json --home "$CHAIN_HOME" 2>/dev/null)
    local proposal_id
    proposal_id=$(echo "$proposals" | jq -r '.proposals[-1].id // "1"')
    log_info "Proposal ID: $proposal_id"

    # Vote YES
    log_info "Voting YES on proposal $proposal_id ..."
    send_tx "$BINARY tx gov vote $proposal_id yes --from validator" >/dev/null 2>&1

    # Wait for voting period to end
    log_info "Waiting for voting period ($VOTING_PERIOD) ..."
    sleep 35

    # Wait for chain to halt at upgrade height
    log_info "Waiting for chain to halt at height $upgrade_height ..."
    local max_wait=120
    local elapsed=0
    while [[ $elapsed -lt $max_wait ]]; do
        local height
        height=$(get_height 2>/dev/null || echo "0")
        if [[ "$height" -ge "$upgrade_height" ]] || ! kill -0 "$CHAIN_PID" 2>/dev/null; then
            log_info "Chain halted (height=$height)"
            break
        fi
        sleep 2
        ((elapsed += 2))
    done

    # Swap binary and restart
    log_info "Swapping to new binary: $new_binary"
    swap_binary_and_restart "$new_binary"

    log_info "=== Upgrade $upgrade_name complete ==="
}

# ---------------------------------------------------------------------------
# Stage-specific verification
# ---------------------------------------------------------------------------

verify_stage1_data() {
    suite "Stage 1 Verification — Pre-EVM Data Intact"

    test_start "Tokens from Stage 1 still exist"
    local token
    token=$(query_token "ALPHA_0")
    assert_json_field "$token" ".token.symbol" "ALPHA"

    test_start "Balances preserved"
    local bal
    bal=$(get_balance "$(get_address admin)" "ustoc")
    assert_gt "$bal" "0"

    test_start "Custom token balances preserved"
    bal=$(get_balance "$(get_address admin)" "ALPHA_0")
    assert_gt "$bal" "0"
}

verify_stage2_data() {
    suite "Stage 2 Verification — Post-v1.0.0-rc2 Data Intact"

    test_start "Stage 1 tokens still exist after EVM upgrade"
    local token
    token=$(query_token "ALPHA_0")
    assert_json_field "$token" ".token.symbol" "ALPHA"

    test_start "EVM module active"
    local evm_params
    evm_params=$(query_evm_params)
    if [[ -n "$evm_params" ]]; then
        pass
    else
        fail "EVM params not available after upgrade"
    fi

    test_start "evm_denom = ustoc"
    local evm_denom
    evm_denom=$(echo "$evm_params" | jq -r '.params.evm_denom // empty')
    assert_equals "$evm_denom" "ustoc"
}

verify_stage3_data() {
    suite "Stage 3 Verification — Post-v0.6.0 Data Intact"

    test_start "All tokens from Stage 1 intact after v0.6.0"
    local token
    token=$(query_token "ALPHA_0")
    assert_json_field "$token" ".token.symbol" "ALPHA"

    test_start "EVM denom still ustoc (not aatom)"
    local evm_params
    evm_params=$(query_evm_params)
    local evm_denom
    evm_denom=$(echo "$evm_params" | jq -r '.params.evm_denom // empty')
    assert_equals "$evm_denom" "ustoc"

    test_start "REGRESSION: extended_denom = astoc (not atest)"
    local ext_denom
    ext_denom=$(echo "$evm_params" | jq -r '.params.extended_denom_options.extended_denom // .params.extended_denom // empty')
    if [[ "$ext_denom" == "astoc" ]]; then
        pass
    else
        fail "expected astoc, got $ext_denom"
    fi

    test_start "Feemarket min_gas_price set by v3 handler"
    local fm_params
    fm_params=$(query_feemarket_params)
    local min_gas
    min_gas=$(echo "$fm_params" | jq -r '.params.min_gas_price // "0"')
    log_info "min_gas_price after v3: $min_gas"
    # v3 handler should set min_gas_price = 10^9
    if [[ "$min_gas" != "0" && "$min_gas" != "0.000000000000000000" ]]; then
        pass
    else
        fail "min_gas_price should be non-zero after v3 upgrade"
    fi

    test_start "Cosmos balances preserved"
    local bal
    bal=$(get_balance "$(get_address admin)" "ustoc")
    assert_gt "$bal" "0"

    test_start "Custom token balances preserved"
    bal=$(get_balance "$(get_address admin)" "ALPHA_0")
    assert_gt "$bal" "0"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
echo ""
echo -e "${BOLD}=================================================================${NC}"
echo -e "${BOLD}  STOC E2E FULL UPGRADE TEST${NC}"
echo -e "${BOLD}  no-evm -> v1.0.0-rc2 -> v0.6.0${NC}"
echo -e "${BOLD}  $(date '+%Y-%m-%d %H:%M:%S')${NC}"
echo -e "${BOLD}=================================================================${NC}"
echo ""

# Check binaries
check_binaries || exit 1

# ===== STAGE 1: No-EVM Chain =====
echo -e "\n${BOLD}========== STAGE 1: No-EVM Chain ==========${NC}\n"

BINARY="$BINARY_NOEVM"
init_chain "$BINARY_NOEVM"
start_node "$BINARY_NOEVM"
wait_for_chain 60 || { log_error "Chain failed to start"; exit 1; }
wait_for_block 3

# Setup wallets (admin is auto-created by init_chain)
WALLET_ADMIN="admin"
WALLET_VAL1="validator"
ADDR_ADMIN=$(get_address "$WALLET_ADMIN")
ADDR_VAL1=$(get_address "$WALLET_VAL1")

# Import EVM wallets
import_wallet "$WALLET_EVM1" "$EVM_WALLET1_MNEMONIC"
import_wallet "$WALLET_EVM2" "$EVM_WALLET2_MNEMONIC"
ADDR_EVM1=$(get_address "$WALLET_EVM1")
ADDR_EVM2=$(get_address "$WALLET_EVM2")
[[ -z "$ADDR_EVM1" ]] && ADDR_EVM1="$EVM_WALLET1_COSMOS"
[[ -z "$ADDR_EVM2" ]] && ADDR_EVM2="$EVM_WALLET2_COSMOS"

# Fund test wallets
fund_wallet "$ADDR_EVM1" "10000000000ustoc" "evm_wallet1"
wait_for_block 1
fund_wallet "$ADDR_EVM2" "10000000000ustoc" "evm_wallet2"
wait_for_block 1

# Create test tokens on no-EVM chain
log_info "Creating test tokens on Stage 1 (no-EVM) ..."
create_token "$WALLET_ADMIN" "Alpha Token" "ALPHA" "1000000" "10000000" 6 "https://example.com/logo.png" false >/dev/null 2>&1
wait_for_block 1
create_token "$WALLET_ADMIN" "Beta Token" "BETA" "500000" "500000" 6 "https://example.com/logo.png" true >/dev/null 2>&1
wait_for_block 1

# Transfer some tokens
bank_send "$WALLET_ADMIN" "$ADDR_EVM1" "100000ALPHA_0" >/dev/null 2>&1
wait_for_block 1
bank_send "$WALLET_ADMIN" "$ADDR_EVM2" "50000ALPHA_0" >/dev/null 2>&1
wait_for_block 1

log_info "Stage 1 test data created"

# ===== STAGE 2: Upgrade to v1.0.0-rc2 (v2-evm) =====
echo -e "\n${BOLD}========== STAGE 2: Upgrade to v1.0.0-rc2 ==========${NC}\n"

do_upgrade "v2-evm" "$BINARY_EVM_V1" 15

verify_stage1_data
verify_stage2_data

# Test EVM functionality after upgrade
log_info "Testing EVM after v1.0.0-rc2 upgrade ..."
run_evm_isolation_tests
run_denom_conversion_tests

# ===== STAGE 3: Upgrade to v0.6.0 (v3-fix-evm-denom) =====
echo -e "\n${BOLD}========== STAGE 3: Upgrade to v0.6.0 ==========${NC}\n"

do_upgrade "v3-fix-evm-denom" "$BINARY_V060" 15

verify_stage3_data

# Run full test suite on v0.6.0
log_info "Running full test suite on v0.6.0 ..."
run_token_lifecycle_tests
run_tax_system_tests
run_evm_isolation_tests
run_denom_conversion_tests
run_gas_enforcement_tests
run_ibc_restriction_tests
run_edge_case_tests

# ===== Summary =====
print_summary
exit_code=$?

echo ""
log_info "Chain log: $CHAIN_LOG"
log_info "Chain home: $CHAIN_HOME"
echo ""

exit $exit_code
