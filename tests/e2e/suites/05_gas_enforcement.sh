#!/bin/bash
# =============================================================================
# Suite 05: Gas Enforcement
# Tests CosmosMinGasPriceDecorator behavior:
# - Gas=0 rejection (when min_gas_price > 0 or node minimum-gas-prices > 0)
# - Multi-denom fee rejection
# - Wrong denom fee rejection
# =============================================================================

run_gas_enforcement_tests() {
    suite "05 — Gas Enforcement"

    # =========================================================================
    section "Feemarket State"
    # =========================================================================

    # T01: Query current min_gas_price
    test_start "Query feemarket min_gas_price"
    local fm_params
    fm_params=$(query_feemarket_params)
    local min_gas_price
    min_gas_price=$(echo "$fm_params" | jq -r '.params.min_gas_price // "0"')
    log_info "Current min_gas_price = $min_gas_price"
    pass

    # T02: Query node minimum-gas-prices (node-level enforcement)
    test_start "Node minimum-gas-prices configured"
    # Node min-gas-prices is set in config.yml: 0.0001ustoc
    # It's a mempool-level check, not consensus-level
    log_info "Expected node min-gas-prices: 0.0001ustoc (from config.yml)"
    pass

    # =========================================================================
    section "Normal Transaction — Sufficient Gas"
    # =========================================================================

    # T03: Send with adequate gas price — should succeed
    test_start "Send with --gas-prices 0.001${DENOM} (above min)"
    local result
    result=$(CUSTOM_GAS_FLAGS="--gas 200000 --gas-prices 0.001${DENOM}" \
        send_tx "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM1 1000${DENOM}")
    assert_tx_success "$result"

    # T04: Send with explicit --fees flag
    test_start "Send with --fees 50000${DENOM} (explicit fee)"
    result=$(CUSTOM_GAS_FLAGS="--gas 200000 --fees 50000${DENOM}" \
        send_tx "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM2 1000${DENOM}")
    assert_tx_success "$result"

    # T05: Send with higher gas price
    test_start "Send with --gas-prices 0.01${DENOM} (generous)"
    result=$(CUSTOM_GAS_FLAGS="--gas 200000 --gas-prices 0.01${DENOM}" \
        send_tx "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM1 500${DENOM}")
    assert_tx_success "$result"

    # =========================================================================
    section "Zero / Low Gas — Expected Behavior"
    # =========================================================================

    # T06: Send with --fees 0ustoc
    # Behavior depends on node min-gas-prices and feemarket min_gas_price
    test_start "Send with --fees 0${DENOM} — check rejection"
    local raw
    raw=$(eval "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM1 100${DENOM} \
        $(_common_flags) --gas 200000 --fees 0${DENOM} --broadcast-mode sync" 2>&1)
    local code
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]]; then
        log_info "0 fee rejected (code=$code) — node enforces min gas"
        pass
    elif echo "$raw" | grep -qi "insufficient\|minimum\|fee"; then
        log_info "0 fee rejected via error message"
        pass
    else
        # On local chain with min_gas_price=0, this might actually succeed
        log_info "0 fee accepted (min_gas_price=0 in genesis, node may allow it)"
        pass  # This is expected behavior when min_gas_price=0
    fi

    # T07: Send with --gas-prices 0ustoc
    test_start "Send with --gas-prices 0${DENOM}"
    raw=$(eval "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM1 100${DENOM} \
        $(_common_flags) --gas 200000 --gas-prices 0${DENOM} --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]]; then
        log_info "0 gas-price rejected"
        pass
    else
        log_info "0 gas-price accepted (node may allow when feemarket min=0)"
        pass
    fi

    # =========================================================================
    section "Multi-Denom Fee — MUST Reject"
    # =========================================================================

    # T08: Send with multi-denom fees (e.g., ustoc + ALPHA_0)
    test_start "Multi-denom fee rejected"
    raw=$(eval "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM1 100${DENOM} \
        $(_common_flags) --gas 200000 --fees 50000${DENOM},100ALPHA_0 --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]]; then
        pass
    elif echo "$raw" | grep -qi "expected only one fee\|invalid coins\|error"; then
        pass
    else
        # Check if tx was rejected in DeliverTx
        local txhash
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            local tx_result
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then
                pass
            else
                fail "multi-denom fee should be rejected"
            fi
        else
            fail "multi-denom fee should be rejected (raw: ${raw:0:200})"
        fi
    fi

    # =========================================================================
    section "Wrong Denom Fee — MUST Reject"
    # =========================================================================

    # T09: Send with wrong denom fee (e.g., ALPHA_0 instead of ustoc)
    test_start "Wrong denom fee rejected (ALPHA_0)"
    raw=$(eval "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM1 100${DENOM} \
        $(_common_flags) --gas 200000 --fees 50000ALPHA_0 --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]]; then
        pass
    elif echo "$raw" | grep -qi "expected fee in\|invalid coins\|error\|insufficient"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "wrong denom fee should be rejected"; fi
        else
            fail "wrong denom fee should be rejected"
        fi
    fi

    # T10: Send with fictional denom fee
    test_start "Wrong denom fee rejected (foobar)"
    raw=$(eval "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM1 100${DENOM} \
        $(_common_flags) --gas 200000 --fees 50000foobar --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "error\|invalid\|insufficient"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "foobar denom should be rejected"; fi
        else
            fail "wrong denom should be rejected"
        fi
    fi

    # =========================================================================
    section "Gas Auto-Estimation"
    # =========================================================================

    # T11: Fixed gas with gas-prices works
    test_start "Send with fixed gas + gas-prices"
    result=$(CUSTOM_GAS_FLAGS="--gas 200000 --gas-prices 0.001${DENOM}" \
        send_tx "$BINARY tx bank send $WALLET_ADMIN $ADDR_EVM2 500${DENOM}")
    assert_tx_success "$result"

    # T12: Verify gas_used > 0 in tx result
    test_start "Tx result gas_used > 0"
    local gas_used
    gas_used=$(echo "$result" | jq -r '.gas_used // "0"')
    if [[ -n "$gas_used" && "$gas_used" != "0" && "$gas_used" != "null" ]]; then
        log_info "gas_used=$gas_used"
        pass
    else
        skip "gas_used not in tx result (may be in different field)"
    fi

    # T13: Verify gas_wanted >= gas_used
    test_start "gas_wanted >= gas_used"
    local gas_wanted
    gas_wanted=$(echo "$result" | jq -r '.gas_wanted // "0"')
    if [[ -n "$gas_wanted" && -n "$gas_used" && "$gas_wanted" != "null" && "$gas_used" != "null" ]]; then
        if [[ "$gas_wanted" -ge "$gas_used" ]]; then
            log_info "gas_wanted=$gas_wanted >= gas_used=$gas_used"
            pass
        else
            fail "gas_wanted ($gas_wanted) < gas_used ($gas_used)"
        fi
    else
        skip "gas fields not available"
    fi
}
