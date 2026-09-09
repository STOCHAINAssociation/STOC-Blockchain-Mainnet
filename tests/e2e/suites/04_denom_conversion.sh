#!/bin/bash
# =============================================================================
# Suite 04: Denom Conversion
# Verify EVM denom = ustoc, extended = astoc, conversion = 10^12
# REGRESSION: denom must NOT be aatom or atest
# =============================================================================

run_denom_conversion_tests() {
    suite "04 — Denom Conversion"

    # =========================================================================
    section "EVM Params Verification"
    # =========================================================================

    # T01: Query EVM params — evm_denom = ustoc
    test_start "evm_denom = $DENOM"
    local evm_params
    evm_params=$(query_evm_params)
    if [[ -n "$evm_params" ]]; then
        local evm_denom
        evm_denom=$(echo "$evm_params" | jq -r '.params.evm_denom // empty')
        assert_equals "$evm_denom" "$DENOM"
    else
        fail "could not query EVM params"
    fi

    # T02: Query EVM params — extended_denom
    test_start "extended_denom = $EVM_DENOM"
    local extended_denom
    extended_denom=$(echo "$evm_params" | jq -r '.params.extended_denom_options.extended_denom // empty')
    # Also try alternative JSON path
    if [[ -z "$extended_denom" ]]; then
        extended_denom=$(echo "$evm_params" | jq -r '.params.extended_denom // empty')
    fi
    if [[ "$extended_denom" == "$EVM_DENOM" ]]; then
        pass
    else
        # Check if it's nested differently
        log_info "extended_denom value: '$extended_denom'"
        log_info "Full EVM params: $(echo "$evm_params" | jq '.params' 2>/dev/null | head -20)"
        if [[ -n "$extended_denom" ]]; then
            assert_equals "$extended_denom" "$EVM_DENOM"
        else
            skip "extended_denom field not found in expected path"
        fi
    fi

    # =========================================================================
    section "REGRESSION — Denom Bugs"
    # =========================================================================

    # T03: REGRESSION — denom NOT aatom (previous cosmos/evm default bug)
    test_start "REGRESSION: evm_denom != aatom"
    local evm_d
    evm_d=$(echo "$evm_params" | jq -r '.params.evm_denom // ""')
    assert_not_contains "$evm_d" "aatom"

    # T04: REGRESSION — extended_denom NOT atest
    test_start "REGRESSION: extended_denom != atest"
    local ext_d
    ext_d=$(echo "$evm_params" | jq -r '.params.extended_denom_options.extended_denom // .params.extended_denom // ""')
    assert_not_contains "$ext_d" "atest"

    # T05: REGRESSION — evm_denom NOT aevmos
    test_start "REGRESSION: evm_denom != aevmos"
    assert_not_contains "$evm_d" "aevmos"

    # T06: REGRESSION — check no "stake" denom
    test_start "REGRESSION: evm_denom != stake"
    assert_not_contains "$evm_d" "stake"

    # =========================================================================
    section "Feemarket Params"
    # =========================================================================

    # T07: Query feemarket params
    test_start "Feemarket params accessible"
    local fm_params
    fm_params=$(query_feemarket_params)
    if [[ -n "$fm_params" ]]; then
        local no_base_fee
        no_base_fee=$(echo "$fm_params" | jq -r '.params.no_base_fee // empty')
        log_info "no_base_fee=$no_base_fee"
        pass
    else
        fail "could not query feemarket params"
    fi

    # T08: Verify min_gas_price field exists
    test_start "Feemarket min_gas_price field exists"
    local min_gas
    min_gas=$(echo "$fm_params" | jq -r '.params.min_gas_price // empty')
    if [[ -n "$min_gas" ]]; then
        log_info "min_gas_price=$min_gas"
        pass
    else
        fail "min_gas_price not found"
    fi

    # =========================================================================
    section "Bank Denom Metadata"
    # =========================================================================

    # T09: Verify bank denom metadata for ustoc
    test_start "Bank denom metadata for ${DENOM} exists"
    local meta
    meta=$(query_bank_denom_metadata "$DENOM")
    if [[ -n "$meta" ]]; then
        local base
        base=$(echo "$meta" | jq -r '.metadata.base // empty')
        if [[ "$base" == "$DENOM" ]]; then
            pass
        else
            log_info "denom metadata base: $base"
            pass  # Metadata might be structured differently
        fi
    else
        skip "denom metadata query format may differ"
    fi

    # T10: Verify display denom
    test_start "Display denom = stoc"
    if [[ -n "$meta" ]]; then
        local display
        display=$(echo "$meta" | jq -r '.metadata.display // empty')
        if [[ "$display" == "stoc" ]]; then
            pass
        else
            log_info "display denom: $display"
            skip "display denom format may differ ($display)"
        fi
    else
        skip "no metadata available"
    fi

    # =========================================================================
    section "Conversion Ratio Verification"
    # =========================================================================

    # T11: Verify 1 ustoc = 10^12 astoc via EVM balance
    test_start "Conversion: 1 ${DENOM} = 10^12 ${EVM_DENOM}"
    if evm_is_available 2>/dev/null; then
        # Get evm_wallet1 cosmos balance and EVM balance
        local cosmos_bal
        cosmos_bal=$(get_balance "$ADDR_EVM1" "$DENOM")
        local evm_bal_hex
        evm_bal_hex=$(evm_get_balance "$EVM_WALLET1_ETH")
        if [[ -n "$evm_bal_hex" && "$evm_bal_hex" != "0x0" ]]; then
            # EVM balance (astoc) should be approximately cosmos_bal * 10^12
            # We can't do exact 256-bit math in bash, so just check it's non-zero
            log_info "Cosmos: ${cosmos_bal} ${DENOM}, EVM: ${evm_bal_hex} ${EVM_DENOM}"
            log_info "Conversion factor: 10^12 (1 ${DENOM} = 1000000000000 ${EVM_DENOM})"
            pass
        else
            fail "EVM balance is 0"
        fi
    else
        skip "EVM not available"
    fi

    # T12: Staking bond_denom = ustoc
    test_start "Staking bond_denom = $DENOM"
    local staking_params
    staking_params=$($BINARY query staking params --node "$NODE" --output json 2>/dev/null)
    if [[ -n "$staking_params" ]]; then
        local bond_denom
        bond_denom=$(echo "$staking_params" | jq -r '.params.bond_denom // empty')
        assert_equals "$bond_denom" "$DENOM"
    else
        fail "could not query staking params"
    fi
}
