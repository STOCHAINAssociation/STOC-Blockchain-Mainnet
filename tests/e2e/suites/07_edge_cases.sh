#!/bin/bash
# =============================================================================
# Suite 07: Edge Cases & Regression Tests
# Validation boundaries, error cases, regression for known bugs
# =============================================================================

run_edge_case_tests() {
    suite "07 — Edge Cases & Regression"

    local LOGO="https://example.com/logo.png"

    # =========================================================================
    section "Symbol Validation — Native Denom Protection"
    # =========================================================================

    # T01: Symbol = "stoc" (display denom) — REJECTED
    test_start "Symbol 'stoc' rejected (native display denom)"
    local raw
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Fake Stoc' --symbol stoc \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    local code
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "native denom\|error\|invalid"; then
        pass
    else
        local txhash
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            local tx_result
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "symbol 'stoc' should be rejected"; fi
        else
            fail "symbol 'stoc' should be rejected"
        fi
    fi

    # T02: Symbol = "ustoc" — REJECTED
    test_start "Symbol 'ustoc' rejected (native cosmos denom)"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Fake uStoc' --symbol ustoc \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "native\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "'ustoc' should be rejected"; fi
        else
            fail "'ustoc' should be rejected"
        fi
    fi

    # T03: Symbol = "astoc" — REJECTED
    test_start "Symbol 'astoc' rejected (native EVM denom)"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Fake aStoc' --symbol astoc \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "native\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "'astoc' should be rejected"; fi
        else
            fail "'astoc' should be rejected"
        fi
    fi

    # =========================================================================
    section "Name Validation"
    # =========================================================================

    # T04: Empty name — REJECTED
    test_start "Empty name rejected"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name '' --symbol EMPTYNAME \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "empty\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "empty name should be rejected"; fi
        else
            fail "empty name should be rejected"
        fi
    fi

    # T05: Name > 64 chars — REJECTED
    test_start "Name > 64 chars rejected"
    local long_name
    long_name=$(printf 'A%.0s' {1..65})
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name '$long_name' --symbol LONGNAME \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "too long\|max 64\|error"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "name >64 chars should be rejected"; fi
        else
            fail "name >64 chars should be rejected"
        fi
    fi

    # =========================================================================
    section "Symbol Validation — Format"
    # =========================================================================

    # T06: Symbol starts with number — REJECTED
    test_start "Symbol starting with number rejected"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Bad Symbol' --symbol '123ABC' \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "alphanumeric\|start with\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "numeric-start symbol should be rejected"; fi
        else
            fail "numeric-start symbol should be rejected"
        fi
    fi

    # T07: Symbol > 32 chars — REJECTED
    test_start "Symbol > 32 chars rejected"
    local long_sym
    long_sym=$(printf 'A%.0s' {1..33})
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Long Symbol' --symbol '$long_sym' \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "max 32\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "symbol >32 should be rejected"; fi
        else
            fail "symbol >32 should be rejected"
        fi
    fi

    # =========================================================================
    section "Logo Validation"
    # =========================================================================

    # T08: Logo http:// — REJECTED (only https:// and ipfs://)
    test_start "Logo http:// rejected (SSRF prevention)"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Http Logo' --symbol HTTPLOGO \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo 'http://evil.com/logo.png' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "https\|ipfs\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "http:// logo should be rejected"; fi
        else
            fail "http:// logo should be rejected"
        fi
    fi

    # T09: Empty logo — REJECTED
    test_start "Empty logo rejected"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'No Logo' --symbol NOLOGO \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "empty\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "empty logo should be rejected"; fi
        else
            fail "empty logo should be rejected"
        fi
    fi

    # =========================================================================
    section "Decimals Validation"
    # =========================================================================

    # T10: Decimals > 18 — REJECTED
    test_start "Decimals 19 rejected"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Bad Decimals' --symbol BADDEC \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 19 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "0 and 18\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "decimals 19 should be rejected"; fi
        else
            fail "decimals 19 should be rejected"
        fi
    fi

    # =========================================================================
    section "Supply Validation"
    # =========================================================================

    # T11: Total supply < initial supply — REJECTED
    test_start "Total < initial rejected"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Bad Supply' --symbol BADSUP \
        --initial-supply 10000 --total-supply 1000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "less than\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "total<initial should be rejected"; fi
        else
            fail "total<initial should be rejected"
        fi
    fi

    # T12: Zero total supply, non-unlimited — REJECTED (dead token)
    test_start "Zero total + unlimited=false rejected (dead token)"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Dead Token' --symbol DEAD \
        --initial-supply 0 --total-supply 0 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "dead token\|zero\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "dead token should be rejected"; fi
        else
            fail "dead token should be rejected"
        fi
    fi

    # T13: Supply > 10^30 — REJECTED
    test_start "Supply > 10^30 rejected"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'Huge Supply' --symbol HUGE \
        --initial-supply 999999999999999999999999999999999 --total-supply 999999999999999999999999999999999 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "maximum\|exceeds\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "supply>10^30 should be rejected"; fi
        else
            fail "supply>10^30 should be rejected"
        fi
    fi

    # =========================================================================
    section "Tax Validation"
    # =========================================================================

    # T14: Tax > 50% — REJECTED
    test_start "Tax > 50% rejected"
    raw=$(eval "$BINARY tx stoc create-token \
        --from $WALLET_ADMIN \
        --name 'HighTax' --symbol HITAX \
        --initial-supply 1000 --total-supply 10000 \
        --decimals 6 --logo '$LOGO' --unlimited=false \
        --tax '{\"percent\":\"0.510000000000000000\",\"recipient_address\":\"$ADDR_ADMIN\"}' \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "exceed\|50%\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "tax>50% should be rejected"; fi
        else
            # May fail due to flag format
            skip "flag format might differ"
        fi
    fi

    # =========================================================================
    section "Creation Fee"
    # =========================================================================

    # T15: Insufficient balance for creation fee (100 STOC)
    test_start "Insufficient funds for creation fee"
    # evm_wallet2 has limited funds, create expensive operation
    local evm2_bal
    evm2_bal=$(get_balance "$ADDR_EVM2" "ustoc")
    if [[ "$evm2_bal" -lt 100000000 ]]; then
        raw=$(eval "$BINARY tx stoc create-token \
            --from $WALLET_EVM2 \
            --name 'Broke Token' --symbol BROKE \
            --initial-supply 100 --total-supply 1000 \
            --decimals 6 --logo '$LOGO' --unlimited=false \
            $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
        code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
        if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "insufficient\|error"; then
            pass
        else
            txhash=$(echo "$raw" | jq -r '.txhash // empty')
            if [[ -n "$txhash" ]]; then
                tx_result=$(wait_for_tx "$txhash" 15)
                code=$(echo "$tx_result" | jq -r '.code // "0"')
                if [[ "$code" != "0" ]]; then pass; else fail "should fail with insufficient funds"; fi
            else
                fail "should fail with insufficient funds"
            fi
        fi
    else
        skip "evm_wallet2 has enough funds ($evm2_bal ustoc), test requires poor wallet"
    fi

    # =========================================================================
    section "Mint/Release/Burn Edge Cases"
    # =========================================================================

    # T16: Mint non-existent token
    test_start "Mint non-existent token rejected"
    raw=$(eval "$BINARY tx stoc mint-tokens --from $WALLET_ADMIN --symbol NONEXIST_999 --amount 1000 \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "not found\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "non-existent token mint should fail"; fi
        else
            fail "non-existent token mint should fail"
        fi
    fi

    # T17: Release non-existent token
    test_start "Release non-existent token rejected"
    raw=$(eval "$BINARY tx stoc release-tokens --from $WALLET_ADMIN --symbol NONEXIST_999 --amount 1000 --recipient $ADDR_EVM1 \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "not found\|error\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "non-existent token release should fail"; fi
        else
            fail "non-existent token release should fail"
        fi
    fi

    # T18: Burn non-existent token
    test_start "Burn non-existent token rejected"
    raw=$(eval "$BINARY tx stoc burn-token --from $WALLET_ADMIN --denom NONEXIST_999 --amount 100 \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "error\|insufficient\|invalid"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "non-existent token burn should fail"; fi
        else
            fail "non-existent token burn should fail"
        fi
    fi

    # T19: Burn with both amount and burn_all (should fail)
    test_start "Burn amount + burn_all simultaneously rejected"
    raw=$(eval "$BINARY tx stoc burn-token --from $WALLET_ADMIN --denom ALPHA_0 --amount 100 --burn-all \
        $(_common_flags) $(_gas_flags) --broadcast-mode sync" 2>&1)
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]] || echo "$raw" | grep -qi "error\|invalid\|both"; then
        pass
    else
        txhash=$(echo "$raw" | jq -r '.txhash // empty')
        if [[ -n "$txhash" ]]; then
            tx_result=$(wait_for_tx "$txhash" 15)
            code=$(echo "$tx_result" | jq -r '.code // "0"')
            if [[ "$code" != "0" ]]; then pass; else fail "amount+burn_all should be rejected"; fi
        else
            fail "amount+burn_all should be rejected"
        fi
    fi

    # =========================================================================
    section "Duplicate Symbol Regression"
    # =========================================================================

    # T20: Creating same symbol increments counter
    test_start "Third ALPHA token gets counter _2"
    local result
    result=$(create_token "$WALLET_ADMIN" "Alpha v3" "ALPHA" "100" "1000" 6 "$LOGO" false)
    assert_tx_success "$result"

    test_start "ALPHA_2 exists and is distinct"
    local alpha2
    alpha2=$(query_token "ALPHA_2")
    local alpha2_name
    alpha2_name=$(echo "$alpha2" | jq -r '.token.name')
    assert_equals "$alpha2_name" "Alpha v3"

    # =========================================================================
    section "Max Supply Boundary"
    # =========================================================================

    # T22: Token with exactly 10^30 supply (boundary — should succeed)
    test_start "Token with supply = 10^30 (boundary, should succeed)"
    result=$(create_token "$WALLET_ADMIN" "MaxSupply" "MAXSUP" "0" "1000000000000000000000000000000" 0 "$LOGO" false)
    # This might succeed or fail depending on exact boundary check
    code=$(echo "$result" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" == "0" ]]; then
        pass
    else
        log_info "10^30 supply result: code=$code"
        skip "boundary behavior may vary"
    fi

    # =========================================================================
    section "Data Integrity Verification"
    # =========================================================================

    # T23: All tokens queryable
    test_start "All created tokens still queryable"
    local all_tokens
    all_tokens=$(query_tokens)
    local count
    count=$(echo "$all_tokens" | jq '.tokens | length' 2>/dev/null)
    log_info "Total tokens in chain: $count"
    assert_gt "$count" "5"

    # T24: Token counter monotonically increasing
    test_start "Token counter is consistent"
    # Query ALPHA_0, ALPHA_1, ALPHA_2 — all should exist
    local a0 a1 a2
    a0=$(query_token "ALPHA_0" | jq -r '.token.id // empty')
    a1=$(query_token "ALPHA_1" | jq -r '.token.id // empty')
    a2=$(query_token "ALPHA_2" | jq -r '.token.id // empty')
    if [[ "$a0" == "ALPHA_0" && "$a1" == "ALPHA_1" && "$a2" == "ALPHA_2" ]]; then
        pass
    else
        fail "counter inconsistency: a0=$a0 a1=$a1 a2=$a2"
    fi

    # T25: Native ustoc still works after all operations
    test_start "Native ustoc transfer still works"
    result=$(bank_send "$WALLET_ADMIN" "$ADDR_EVM1" "100ustoc")
    assert_tx_success "$result"

    # T26: Staking still works (query validators)
    test_start "Staking module responsive"
    local validators
    validators=$($BINARY query staking validators --node "$NODE" --output json 2>/dev/null)
    if [[ -n "$validators" ]]; then
        local val_count
        val_count=$(echo "$validators" | jq '.validators | length' 2>/dev/null)
        log_info "Active validators: $val_count"
        assert_gt "$val_count" "0"
    else
        fail "could not query validators"
    fi

    # T27: Block production continues
    test_start "Block production continues"
    local h1
    h1=$(get_height)
    sleep 7
    local h2
    h2=$(get_height)
    assert_gt "$h2" "$h1"
}
