#!/bin/bash
# =============================================================================
# Suite 02: Tax System
# Tax deducted from RECIPIENT after transfer. PostDecorator logic.
# =============================================================================

run_tax_system_tests() {
    suite "02 — Tax System"

    local LOGO="https://example.com/tax-logo.png"

    # =========================================================================
    section "Setup — Create taxed tokens"
    # =========================================================================

    # Create token with 5% tax, tax recipient = admin
    log_info "Creating TAX5 (5% tax, recipient=admin) ..."
    local result
    result=$(create_token "$WALLET_ADMIN" "Tax5 Token" "TAXX" "10000000" "100000000" 6 "$LOGO" false \
        "--tax '{\"percent\":\"0.050000000000000000\",\"recipient_address\":\"$ADDR_ADMIN\"}'")
    local tax5_ok=false
    if echo "$result" | jq -e '.code == 0' >/dev/null 2>&1; then
        tax5_ok=true
        log_info "TAX5 created: TAXX_0"
    else
        # Try alternative flag format
        result=$(create_token "$WALLET_ADMIN" "Tax5 Token" "TAXX" "10000000" "100000000" 6 "$LOGO" false \
            "--tax.percent 0.050000000000000000 --tax.recipient-address $ADDR_ADMIN")
        if echo "$result" | jq -e '.code == 0' >/dev/null 2>&1; then
            tax5_ok=true
            log_info "TAX5 created (alt flags): TAXX_0"
        else
            log_warn "Could not create taxed token — tax tests will be skipped"
            log_warn "Raw result: $(echo "$result" | head -c 300)"
        fi
    fi

    local TAX5_DENOM="TAXX_0"

    # Fund evm_wallet1 and evm_wallet2 with taxed tokens via release
    if [[ "$tax5_ok" == "true" ]]; then
        release_tokens "$WALLET_ADMIN" "$TAX5_DENOM" "500000" "$ADDR_EVM1" >/dev/null 2>&1
        wait_for_block 1
        release_tokens "$WALLET_ADMIN" "$TAX5_DENOM" "500000" "$ADDR_EVM2" >/dev/null 2>&1
        wait_for_block 1

        # Also send some directly via bank send (to test tax on send)
        bank_send "$WALLET_ADMIN" "$ADDR_VAL2" "100000${TAX5_DENOM}" >/dev/null 2>&1
        wait_for_block 1
    fi

    # =========================================================================
    section "Tax Calculation Tests"
    # =========================================================================

    if [[ "$tax5_ok" != "true" ]]; then
        for i in $(seq 1 13); do
            test_start "Tax test $i (skipped — token creation failed)"
            skip "taxed token not created"
        done
        return
    fi

    # T01: Transfer 100 TAX5 tokens: evm_wallet1 -> evm_wallet2
    # Tax = 100 * 5% = 5 (to admin), recipient gets 100 - 5 = 95 effective
    test_start "Transfer 100 TAX5: evm1->evm2 (5% tax = 5 to admin)"
    local before_evm2 before_admin_tax
    before_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    before_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    result=$(bank_send "$WALLET_EVM1" "$ADDR_EVM2" "100${TAX5_DENOM}")
    assert_tx_success "$result"

    # T02: Verify recipient got 100 tokens (tax deducted from recipient AFTER receive)
    test_start "Verify evm2 balance increased (net = 100 - 5 tax = 95)"
    local after_evm2
    after_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    local net_received=$((after_evm2 - before_evm2))
    assert_equals "$net_received" "95"

    # T03: Verify tax recipient (admin) got 5 tokens
    test_start "Verify admin received 5 tax"
    local after_admin_tax
    after_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    local tax_received=$((after_admin_tax - before_admin_tax))
    assert_equals "$tax_received" "5"

    # T04: Transfer 2 tokens — minimum tax = 1
    test_start "Transfer 2 TAX5: evm1->evm2 (min tax = 1)"
    before_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    before_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    result=$(bank_send "$WALLET_EVM1" "$ADDR_EVM2" "2${TAX5_DENOM}")
    assert_tx_success "$result"

    # T05: Verify net received = 1 (2 sent, 1 tax, 1 to recipient)
    test_start "Verify evm2 net +1 (2 sent, 1 tax)"
    after_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    net_received=$((after_evm2 - before_evm2))
    assert_equals "$net_received" "1"

    # T06: Verify tax = 1
    test_start "Verify admin tax = 1"
    after_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    tax_received=$((after_admin_tax - before_admin_tax))
    assert_equals "$tax_received" "1"

    # T07: Transfer 1 token — micro-transfer protection, tax = 0
    test_start "Transfer 1 TAX5: evm1->evm2 (micro-transfer, tax = 0)"
    before_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    before_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    result=$(bank_send "$WALLET_EVM1" "$ADDR_EVM2" "1${TAX5_DENOM}")
    assert_tx_success "$result"

    # T08: Verify recipient got full 1 token (no tax)
    test_start "Verify evm2 net +1 (no tax on 1-unit transfer)"
    after_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    net_received=$((after_evm2 - before_evm2))
    assert_equals "$net_received" "1"

    # T09: Verify no tax collected
    test_start "Verify admin tax = 0 on 1-unit transfer"
    after_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    tax_received=$((after_admin_tax - before_admin_tax))
    assert_equals "$tax_received" "0"

    # =========================================================================
    section "Tax — Self-transfer & Edge Cases"
    # =========================================================================

    # T10: Self-transfer (recipient = tax recipient = admin) — no tax
    test_start "Self-transfer to tax recipient — no tax"
    before_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    result=$(bank_send "$WALLET_EVM1" "$ADDR_ADMIN" "100${TAX5_DENOM}")
    assert_tx_success "$result"

    test_start "Verify no tax on self-transfer to tax recipient"
    after_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    # Admin should receive exactly 100 (no tax deducted since recipient IS tax recipient)
    local admin_increase=$((after_admin_tax - before_admin_tax))
    assert_equals "$admin_increase" "100"

    # T12: Transfer native ustoc — no custom token tax
    test_start "Transfer ustoc — no custom token tax"
    local before_evm2_ustoc
    before_evm2_ustoc=$(get_balance "$ADDR_EVM2" "ustoc")
    result=$(bank_send "$WALLET_ADMIN" "$ADDR_EVM2" "1000ustoc")
    assert_tx_success "$result"

    test_start "Verify ustoc not taxed (full amount received)"
    local after_evm2_ustoc
    after_evm2_ustoc=$(get_balance "$ADDR_EVM2" "ustoc")
    local ustoc_increase=$((after_evm2_ustoc - before_evm2_ustoc))
    assert_equals "$ustoc_increase" "1000"

    # =========================================================================
    section "Tax — Large Transfer"
    # =========================================================================

    # T14: Transfer 1000 tokens with 5% tax = 50
    test_start "Transfer 1000 TAX5: evm2->val2 (tax = 50)"
    local before_val2 before_admin_lg
    before_val2=$(get_balance "$ADDR_VAL2" "$TAX5_DENOM")
    before_admin_lg=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    result=$(bank_send "$WALLET_EVM2" "$ADDR_VAL2" "1000${TAX5_DENOM}")
    assert_tx_success "$result"

    # T15: Verify net = 950
    test_start "Verify val2 net +950"
    local after_val2
    after_val2=$(get_balance "$ADDR_VAL2" "$TAX5_DENOM")
    net_received=$((after_val2 - before_val2))
    assert_equals "$net_received" "950"

    # T16: Verify tax = 50
    test_start "Verify admin tax +50"
    local after_admin_lg
    after_admin_lg=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    tax_received=$((after_admin_lg - before_admin_lg))
    assert_equals "$tax_received" "50"

    # =========================================================================
    section "Tax — Cross-Wallet Transfers"
    # =========================================================================

    # T17: Transfer chain: val2 -> evm1 -> evm2 (each hop taxed)
    test_start "Transfer 100 TAX5: val2->evm1 (tax on each hop)"
    local before_evm1_chain
    before_evm1_chain=$(get_balance "$ADDR_EVM1" "$TAX5_DENOM")
    before_admin_tax=$(get_balance "$ADDR_ADMIN" "$TAX5_DENOM")
    result=$(bank_send "$WALLET_VAL2" "$ADDR_EVM1" "100${TAX5_DENOM}")
    assert_tx_success "$result"

    test_start "Verify evm1 net +95 (5% tax)"
    local after_evm1_chain
    after_evm1_chain=$(get_balance "$ADDR_EVM1" "$TAX5_DENOM")
    net_received=$((after_evm1_chain - before_evm1_chain))
    assert_equals "$net_received" "95"

    # T19: Second hop: evm1 -> evm2
    test_start "Transfer 50 TAX5: evm1->evm2 (second hop)"
    before_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    result=$(bank_send "$WALLET_EVM1" "$ADDR_EVM2" "50${TAX5_DENOM}")
    assert_tx_success "$result"

    test_start "Verify evm2 net +47 or +48 (5% of 50 = 2.5 -> 2)"
    after_evm2=$(get_balance "$ADDR_EVM2" "$TAX5_DENOM")
    net_received=$((after_evm2 - before_evm2))
    # 50 * 5% = 2.5, truncated to 2. But min tax is 1, and 2 > 1, so tax = 2
    # Net = 50 - 2 = 48
    if [[ "$net_received" -ge 47 && "$net_received" -le 48 ]]; then
        pass
    else
        fail "expected 47-48, got $net_received"
    fi
}
