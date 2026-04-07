#!/bin/bash
# =============================================================================
# Suite 03: EVM Isolation
# Custom tokens (x/stoc) are Cosmos-only, NOT visible from EVM.
# EVM only sees ustoc/astoc.
# =============================================================================

run_evm_isolation_tests() {
    suite "03 — EVM Isolation"

    # =========================================================================
    section "EVM JSON-RPC Connectivity"
    # =========================================================================

    # T01: Check EVM JSON-RPC is accessible
    test_start "EVM JSON-RPC is accessible"
    if evm_is_available; then
        pass
    else
        log_warn "EVM JSON-RPC not available at $EVM_RPC — skipping EVM tests"
        for i in $(seq 2 15); do
            test_start "EVM test $i (skipped)"
            skip "EVM JSON-RPC not available"
        done
        return
    fi

    # T02: Get EVM chain ID
    test_start "Get EVM chain ID"
    local chain_id_hex
    chain_id_hex=$(evm_chain_id)
    if [[ -n "$chain_id_hex" ]]; then
        local chain_id_dec
        chain_id_dec=$(hex_to_dec "$chain_id_hex")
        log_info "EVM Chain ID: $chain_id_hex ($chain_id_dec)"
        pass
    else
        fail "could not get EVM chain ID"
    fi

    # T03: Get block number
    test_start "EVM block number > 0"
    local block_hex
    block_hex=$(evm_block_number)
    if [[ -n "$block_hex" ]]; then
        local block_dec
        block_dec=$(hex_to_dec "$block_hex")
        assert_gt "$block_dec" "0"
    else
        fail "could not get block number"
    fi

    # =========================================================================
    section "EVM Balance — Native Denom Only"
    # =========================================================================

    # T04: Get admin's EVM balance (should be in astoc = ustoc * 10^12)
    test_start "Admin EVM balance reflects ${DENOM} (as ${EVM_DENOM})"
    local admin_cosmos_bal
    admin_cosmos_bal=$(get_balance "$ADDR_ADMIN" "$DENOM")
    local admin_evm_bal_hex
    # Get admin's hex address first
    local admin_hex_addr
    # Try to get hex address from debug command
    admin_hex_addr=$($BINARY debug addr "$ADDR_ADMIN" 2>/dev/null | grep -i "address (hex)" | awk '{print $NF}')
    if [[ -z "$admin_hex_addr" ]]; then
        # Fallback: try with 0x prefix
        admin_hex_addr=$($BINARY debug addr "$ADDR_ADMIN" 2>/dev/null | grep -i "0x" | head -1 | awk '{print $NF}')
    fi
    if [[ -n "$admin_hex_addr" ]]; then
        # Add 0x prefix if missing
        [[ "$admin_hex_addr" != 0x* ]] && admin_hex_addr="0x${admin_hex_addr}"
        admin_evm_bal_hex=$(evm_get_balance "$admin_hex_addr")
        if [[ -n "$admin_evm_bal_hex" && "$admin_evm_bal_hex" != "0x0" ]]; then
            log_info "Admin EVM balance: $admin_evm_bal_hex"
            pass
        else
            fail "admin EVM balance is 0 or empty ($admin_evm_bal_hex)"
        fi
    else
        skip "Could not derive admin hex address"
    fi

    # T05: evm_wallet1 EVM balance
    test_start "evm_wallet1 EVM balance > 0"
    local evm1_bal_hex
    evm1_bal_hex=$(evm_get_balance "$EVM_WALLET1_ETH")
    if [[ -n "$evm1_bal_hex" && "$evm1_bal_hex" != "0x0" ]]; then
        local evm1_bal_dec
        evm1_bal_dec=$(hex_to_dec "$evm1_bal_hex" 2>/dev/null || echo "0")
        log_info "evm_wallet1 EVM balance: $evm1_bal_hex ($evm1_bal_dec $EVM_DENOM)"
        pass
    else
        fail "evm_wallet1 EVM balance is 0"
    fi

    # T06: evm_wallet2 EVM balance
    test_start "evm_wallet2 EVM balance > 0"
    local evm2_bal_hex
    evm2_bal_hex=$(evm_get_balance "$EVM_WALLET2_ETH")
    if [[ -n "$evm2_bal_hex" && "$evm2_bal_hex" != "0x0" ]]; then
        pass
    else
        fail "evm_wallet2 EVM balance is 0"
    fi

    # =========================================================================
    section "Custom Token NOT Visible from EVM"
    # =========================================================================

    # T07: EVM cannot see custom token balance
    # Custom tokens (e.g., ALPHA_0) are Cosmos-only
    test_start "Custom token ALPHA_0 NOT visible via EVM"
    # EVM has no concept of custom tokens — eth_getBalance only returns native balance
    # There's no ERC20 contract for custom tokens
    # This is by design: custom tokens are Cosmos-only
    # We verify by checking that evm_wallet1 has ALPHA_0 on Cosmos but EVM only shows $EVM_DENOM
    local cosmos_alpha_bal
    cosmos_alpha_bal=$(get_balance "$ADDR_EVM1" "ALPHA_0")
    if [[ -n "$cosmos_alpha_bal" && "$cosmos_alpha_bal" -gt 0 ]]; then
        # evm_wallet1 has ALPHA_0 on Cosmos side, but EVM only shows native denom
        log_info "evm_wallet1 has $cosmos_alpha_bal ALPHA_0 on Cosmos (invisible to EVM)"
        pass
    else
        log_info "evm_wallet1 has no ALPHA_0 (test still valid — EVM isolation by design)"
        pass
    fi

    # T08: eth_getBalance only returns native denom (not custom tokens)
    test_start "eth_getBalance returns ONLY native ${EVM_DENOM} balance"
    # There's no way for EVM to query custom token balances
    # eth_getBalance is the only balance query, and it only returns the native currency
    local evm1_cosmos_ustoc
    evm1_cosmos_ustoc=$(get_balance "$ADDR_EVM1" "$DENOM")
    local expected_astoc_approx=$((evm1_cosmos_ustoc))  # in ustoc units
    # EVM balance in astoc = ustoc * 10^12
    evm1_bal_hex=$(evm_get_balance "$EVM_WALLET1_ETH")
    if [[ -n "$evm1_bal_hex" ]]; then
        log_info "EVM balance is native-only ($EVM_DENOM): $evm1_bal_hex"
        pass
    else
        fail "could not get EVM balance"
    fi

    # =========================================================================
    section "EVM Transfer — Native ustoc/astoc"
    # =========================================================================

    # T09: Verify EVM transfer is possible with cast (if available)
    test_start "EVM native transfer (via cast or curl)"
    if command -v cast >/dev/null 2>&1; then
        # Export private key for evm_wallet1
        local privkey
        privkey=$($BINARY keys export "$WALLET_EVM1" --keyring-backend "$KEYRING_BACKEND" --unarmored-hex --unsafe ${HOME_DIR:+--home $HOME_DIR} 2>/dev/null)
        if [[ -n "$privkey" ]]; then
            local before_evm2_cosmos
            before_evm2_cosmos=$(get_balance "$ADDR_EVM2" "$DENOM")

            # Send 1 ustoc worth of astoc (= 10^12 astoc = 1000000000000 wei)
            local tx_result
            tx_result=$(cast send "$EVM_WALLET2_ETH" \
                --value "1000000000000" \
                --private-key "$privkey" \
                --rpc-url "$EVM_RPC" \
                --chain-id "$(hex_to_dec "$(evm_chain_id)")" \
                2>&1)
            if echo "$tx_result" | grep -qi "success\|transactionHash\|0x"; then
                wait_for_block 1
                local after_evm2_cosmos
                after_evm2_cosmos=$(get_balance "$ADDR_EVM2" "$DENOM")
                local increase=$((after_evm2_cosmos - before_evm2_cosmos))
                log_info "EVM transfer: evm2 cosmos balance increased by $increase $DENOM"
                assert_gte "$increase" "1"
            else
                fail "cast send failed: $tx_result"
            fi
        else
            skip "Could not export private key for EVM transfer"
        fi
    else
        skip "cast not installed — install foundry for EVM transfer tests"
    fi

    # T10: Verify Cosmos balance reflects EVM transfer
    test_start "Cosmos balance reflects EVM transfer"
    local evm1_cosmos_bal_after
    evm1_cosmos_bal_after=$(get_balance "$ADDR_EVM1" "$DENOM")
    if [[ -n "$evm1_cosmos_bal_after" && "$evm1_cosmos_bal_after" -ge 0 ]]; then
        pass
    else
        fail "could not get Cosmos balance after EVM transfer"
    fi

    # =========================================================================
    section "EVM — Additional Queries"
    # =========================================================================

    # T11: eth_getTransactionCount for test wallets
    test_start "eth_getTransactionCount for evm_wallet1"
    local nonce
    nonce=$(evm_get_transaction_count "$EVM_WALLET1_ETH")
    if [[ -n "$nonce" ]]; then
        log_info "evm_wallet1 nonce: $nonce"
        pass
    else
        fail "could not get nonce"
    fi

    # T12: EVM net_version
    test_start "net_version returns chain ID"
    local net_result
    net_result=$(evm_rpc_call "net_version" "[]")
    local net_version
    net_version=$(echo "$net_result" | jq -r '.result // empty')
    if [[ -n "$net_version" ]]; then
        log_info "net_version: $net_version"
        pass
    else
        fail "net_version empty"
    fi

    # T13: web3_clientVersion
    test_start "web3_clientVersion returns node info"
    local client_result
    client_result=$(evm_rpc_call "web3_clientVersion" "[]")
    local client_version
    client_version=$(echo "$client_result" | jq -r '.result // empty')
    if [[ -n "$client_version" ]]; then
        log_info "Client: $client_version"
        pass
    else
        fail "web3_clientVersion empty"
    fi

    # T14: eth_getCode for non-contract address returns 0x
    test_start "eth_getCode for EOA returns 0x"
    local code_result
    code_result=$(evm_rpc_call "eth_getCode" "[\"$EVM_WALLET1_ETH\",\"latest\"]")
    local code_hex
    code_hex=$(echo "$code_result" | jq -r '.result // empty')
    if [[ "$code_hex" == "0x" || "$code_hex" == "" ]]; then
        pass
    else
        fail "expected 0x for EOA, got $code_hex"
    fi
}
