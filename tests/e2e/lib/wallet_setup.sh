#!/bin/bash
# =============================================================================
# STOC E2E Test — Wallet Setup
# Imports EVM/STOC test wallets and funds all accounts from admin
# =============================================================================

# ---------------------------------------------------------------------------
# Test wallet mnemonics (devnet / local only — NOT production keys)
# ---------------------------------------------------------------------------
EVM_WALLET1_MNEMONIC="symbol suggest museum quit slab maze eager assume pond announce stumble accident"
EVM_WALLET1_ETH="0x71DAaF51bbcC392a9f46ffA77a6a8668B0fd2c97"
EVM_WALLET1_COSMOS="stoc1w8d275dmesuj486xl7nh565xdzc06tyhadq7tr"

EVM_WALLET2_MNEMONIC="memory inside viable season because coyote robust olympic key awesome achieve fly"
EVM_WALLET2_ETH="0x34da4323ff1905142795221eF1e39681f1F9dbD7"
EVM_WALLET2_COSMOS="stoc1xndyxgllryz3gfu4yg00rcuks8clnk7h3hjqv0"

# ---------------------------------------------------------------------------
# Wallet names in keyring
# ---------------------------------------------------------------------------
WALLET_ADMIN="admin"
WALLET_VAL1="validator1"
WALLET_VAL2="validator2"
WALLET_EVM1="evm_wallet1"
WALLET_EVM2="evm_wallet2"

# Addresses (populated by setup_wallets)
ADDR_ADMIN=""
ADDR_VAL1=""
ADDR_VAL2=""
ADDR_EVM1=""
ADDR_EVM2=""

# ---------------------------------------------------------------------------
# Import wallets from mnemonics
# ---------------------------------------------------------------------------
import_wallet() {
    local name="$1"
    local mnemonic="$2"
    local home_flag=""
    [[ -n "$HOME_DIR" ]] && home_flag="--home $HOME_DIR"

    # Check if key already exists
    if $BINARY keys show "$name" --keyring-backend "$KEYRING_BACKEND" $home_flag -a >/dev/null 2>&1; then
        log_info "Key '$name' already exists, skipping import"
        return 0
    fi

    echo "$mnemonic" | $BINARY keys add "$name" \
        --keyring-backend "$KEYRING_BACKEND" \
        $home_flag \
        --recover \
        --coin-type 118 \
        --index 0 2>/dev/null

    if [[ $? -eq 0 ]]; then
        log_info "Imported key '$name'"
    else
        log_error "Failed to import key '$name'"
        return 1
    fi
}

# ---------------------------------------------------------------------------
# Fund a wallet from admin
# ---------------------------------------------------------------------------
fund_wallet() {
    local to_addr="$1"
    local amount="${2:-1000000000ustoc}"  # default 1000 STOC
    local label="$3"

    local current_balance
    current_balance=$(get_balance "$to_addr" "ustoc")

    # Skip if already funded (> 100 STOC)
    if [[ -n "$current_balance" && "$current_balance" -gt 100000000 ]]; then
        log_info "$label already funded (${current_balance} ustoc)"
        return 0
    fi

    log_info "Funding $label with $amount ..."
    local result
    result=$(bank_send "$WALLET_ADMIN" "$to_addr" "$amount")
    local code
    code=$(echo "$result" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" == "0" ]]; then
        log_info "Funded $label OK"
        return 0
    else
        log_error "Failed to fund $label: $(echo "$result" | jq -r '.raw_log // .log // "unknown"' 2>/dev/null)"
        return 1
    fi
}

# ---------------------------------------------------------------------------
# Main setup function — call this at the start of test runs
# ---------------------------------------------------------------------------
setup_wallets() {
    log_info "=== Setting up test wallets ==="

    # Import EVM wallets
    import_wallet "$WALLET_EVM1" "$EVM_WALLET1_MNEMONIC"
    import_wallet "$WALLET_EVM2" "$EVM_WALLET2_MNEMONIC"

    # Resolve addresses
    ADDR_ADMIN=$(get_address "$WALLET_ADMIN")
    ADDR_VAL1=$(get_address "$WALLET_VAL1")
    ADDR_VAL2=$(get_address "$WALLET_VAL2")
    ADDR_EVM1=$(get_address "$WALLET_EVM1")
    ADDR_EVM2=$(get_address "$WALLET_EVM2")

    # Fallback: if evm wallets failed to import, use hardcoded addresses
    [[ -z "$ADDR_EVM1" ]] && ADDR_EVM1="$EVM_WALLET1_COSMOS"
    [[ -z "$ADDR_EVM2" ]] && ADDR_EVM2="$EVM_WALLET2_COSMOS"

    log_info "Addresses:"
    log_info "  admin      = $ADDR_ADMIN"
    log_info "  validator1 = $ADDR_VAL1"
    log_info "  validator2 = $ADDR_VAL2"
    log_info "  evm_wallet1= $ADDR_EVM1 / $EVM_WALLET1_ETH"
    log_info "  evm_wallet2= $ADDR_EVM2 / $EVM_WALLET2_ETH"

    # Fund EVM wallets from admin (each gets 10000 STOC for testing)
    fund_wallet "$ADDR_EVM1" "10000000000ustoc" "evm_wallet1"
    wait_for_block 1
    fund_wallet "$ADDR_EVM2" "10000000000ustoc" "evm_wallet2"
    wait_for_block 1

    # Fund validator2 extra if needed (validator1 is the active validator)
    fund_wallet "$ADDR_VAL2" "1000000000ustoc" "validator2"
    wait_for_block 1

    log_info "=== Wallet setup complete ==="
    echo ""
}

# ---------------------------------------------------------------------------
# Print wallet summary (balances)
# ---------------------------------------------------------------------------
print_wallet_summary() {
    log_info "=== Wallet Balances ==="
    for name in "$WALLET_ADMIN" "$WALLET_VAL1" "$WALLET_VAL2" "$WALLET_EVM1" "$WALLET_EVM2"; do
        local addr
        addr=$(get_address "$name")
        if [[ -n "$addr" ]]; then
            local bal
            bal=$(get_balance "$addr" "ustoc")
            log_info "  $name ($addr): ${bal} ustoc"
        fi
    done
    echo ""
}
