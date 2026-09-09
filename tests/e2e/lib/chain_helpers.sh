#!/bin/bash
# =============================================================================
# STOC Chain Interaction Helpers
# Wraps stocd CLI commands for E2E testing
# =============================================================================

# ---------------------------------------------------------------------------
# Configuration (override via environment before sourcing)
# ---------------------------------------------------------------------------
BINARY="${BINARY:-stocd}"
CHAIN_ID="${CHAIN_ID:-stoc}"
NODE="${NODE:-tcp://localhost:26657}"
KEYRING_BACKEND="${KEYRING_BACKEND:-test}"
HOME_DIR="${HOME_DIR:-}"
EVM_RPC="${EVM_RPC:-http://localhost:8545}"
BLOCK_TIME="${BLOCK_TIME:-6}"

# Native denom — set per environment (ustoc/udstoc/utstoc)
DENOM="${DENOM:-ustoc}"
# EVM extended denom (astoc/adstoc/atstoc)
EVM_DENOM="${EVM_DENOM:-astoc}"

# Gas: use FIXED gas + explicit fees to avoid --gas auto simulation race condition.
# The simulation step in --gas auto queries account sequence, creating a race
# when multiple txs are submitted in quick succession.
TX_GAS="${TX_GAS:-500000}"
TX_FEE="${TX_FEE:-5000}"  # fee amount in DENOM units

# ---------------------------------------------------------------------------
# Internal: build common CLI flags
# ---------------------------------------------------------------------------
_common_flags() {
    local flags="--chain-id $CHAIN_ID --node $NODE --keyring-backend $KEYRING_BACKEND -y --output json"
    [[ -n "$HOME_DIR" ]] && flags="$flags --home $HOME_DIR"
    echo "$flags"
}

_gas_flags() {
    echo "--gas $TX_GAS --fees ${TX_FEE}${DENOM}"
}

# ---------------------------------------------------------------------------
# Chain readiness
# ---------------------------------------------------------------------------

wait_for_chain() {
    local max_wait=${1:-120}
    local elapsed=0
    log_info "Waiting for chain to be ready (max ${max_wait}s) ..."
    while [[ $elapsed -lt $max_wait ]]; do
        local status_json
        status_json=$($BINARY status --node "$NODE" 2>/dev/null)
        if [[ $? -eq 0 ]]; then
            local catching_up
            catching_up=$(echo "$status_json" | jq -r '.sync_info.catching_up | tostring' 2>/dev/null)
            if [[ "$catching_up" == "false" ]]; then
                local height
                height=$(echo "$status_json" | jq -r '.sync_info.latest_block_height' 2>/dev/null)
                log_info "Chain ready at height $height"
                return 0
            fi
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done
    log_error "Chain not ready after ${max_wait}s"
    return 1
}

wait_for_block() {
    local blocks=${1:-1}
    local current
    current=$($BINARY status --node "$NODE" 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null)
    local target=$((current + blocks))
    local max_wait=$((BLOCK_TIME * (blocks + 3)))
    local elapsed=0
    while [[ $elapsed -lt $max_wait ]]; do
        sleep 1
        elapsed=$((elapsed + 1))
        local height
        height=$($BINARY status --node "$NODE" 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null)
        if [[ "$height" -ge "$target" ]]; then
            return 0
        fi
    done
    return 1
}

get_height() {
    $BINARY status --node "$NODE" 2>/dev/null | jq -r '.sync_info.latest_block_height // "0"' 2>/dev/null
}

# ---------------------------------------------------------------------------
# Transaction helpers
# ---------------------------------------------------------------------------

wait_for_tx() {
    local txhash="$1"
    local max_wait=${2:-30}
    local elapsed=0
    while [[ $elapsed -lt $max_wait ]]; do
        local result
        result=$($BINARY query tx "$txhash" --node "$NODE" --output json 2>/dev/null)
        if [[ $? -eq 0 && -n "$result" ]]; then
            echo "$result"
            return 0
        fi
        sleep 2
        elapsed=$((elapsed + 2))
    done
    return 1
}

# Execute a tx command, wait for COMMITTED confirmation, then wait 1 extra block.
# This ensures the next tx sees the updated account sequence.
send_tx() {
    local cmd="$1"
    local gas="${CUSTOM_GAS_FLAGS:-$(_gas_flags)}"

    local raw
    raw=$(eval "$cmd $(_common_flags) $gas --broadcast-mode sync" 2>&1)
    local ec=$?

    # Handle non-JSON CLI errors (e.g., RPC errors printed as plain text)
    if ! echo "$raw" | jq -e '.' >/dev/null 2>&1; then
        # Not JSON — wrap in a fake JSON so assertions work
        echo "{\"code\":\"99\",\"raw_log\":\"$(echo "$raw" | head -1 | sed 's/"/\\"/g')\"}"
        return 1
    fi

    if [[ $ec -ne 0 ]]; then
        echo "$raw"
        return $ec
    fi

    # Check for CheckTx failure
    local code
    code=$(echo "$raw" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]]; then
        echo "$raw"
        return 1
    fi

    # Extract txhash and wait for DeliverTx
    local txhash
    txhash=$(echo "$raw" | jq -r '.txhash // empty' 2>/dev/null)
    if [[ -z "$txhash" ]]; then
        echo "$raw"
        return 0
    fi

    local tx_result
    tx_result=$(wait_for_tx "$txhash" 30)
    if [[ -n "$tx_result" ]]; then
        echo "$tx_result"
        local deliver_code
        deliver_code=$(echo "$tx_result" | jq -r '.code // "0"' 2>/dev/null)
        # Wait for 1 block after confirmation to ensure sequence is updated
        wait_for_block 1
        [[ "$deliver_code" != "0" ]] && return 1
        return 0
    fi

    echo "$raw"
    return 0
}

# Send tx expecting failure — no block wait needed
send_tx_expect_fail() {
    local cmd="$1"
    local gas="${CUSTOM_GAS_FLAGS:-$(_gas_flags)}"
    local raw
    raw=$(eval "$cmd $(_common_flags) $gas --broadcast-mode sync" 2>&1)
    # Wrap non-JSON output
    if ! echo "$raw" | jq -e '.' >/dev/null 2>&1; then
        echo "{\"code\":\"99\",\"raw_log\":\"$(echo "$raw" | head -1 | sed 's/"/\\"/g')\"}"
    else
        echo "$raw"
    fi
    return 0
}

# ---------------------------------------------------------------------------
# Account / balance queries
# ---------------------------------------------------------------------------

get_address() {
    local key_name="$1"
    local home_flag=""
    [[ -n "$HOME_DIR" ]] && home_flag="--home $HOME_DIR"
    $BINARY keys show "$key_name" --keyring-backend "$KEYRING_BACKEND" $home_flag -a 2>/dev/null
}

get_balance() {
    local address="$1"
    local denom="$2"
    $BINARY query bank balance "$address" "$denom" --node "$NODE" --output json 2>/dev/null \
        | jq -r '.balance.amount // "0"'
}

get_all_balances() {
    local address="$1"
    $BINARY query bank balances "$address" --node "$NODE" --output json 2>/dev/null
}

# ---------------------------------------------------------------------------
# Bank send
# ---------------------------------------------------------------------------

bank_send() {
    local from_key="$1"
    local to_addr="$2"
    local amount="$3"
    send_tx "$BINARY tx bank send $from_key $to_addr $amount"
}

bank_send_custom() {
    local from_key="$1"
    local to_addr="$2"
    local amount="$3"
    local gas_flags="$4"
    CUSTOM_GAS_FLAGS="$gas_flags" send_tx "$BINARY tx bank send $from_key $to_addr $amount"
}

# ---------------------------------------------------------------------------
# x/stoc token operations
# ---------------------------------------------------------------------------

query_token() {
    local minimal_denom="$1"
    $BINARY query stoc token "$minimal_denom" --node "$NODE" --output json 2>/dev/null
}

query_tokens() {
    $BINARY query stoc tokens --node "$NODE" --output json 2>/dev/null
}

query_tokens_by_symbol() {
    local symbol="$1"
    $BINARY query stoc tokens-by-symbol "$symbol" --node "$NODE" --output json 2>/dev/null
}

# Get the minimal denom of the most recently created token for a given symbol.
# Token counter is GLOBAL (not per-symbol), so BETA's first token might be BETA_1 not BETA_0.
# Usage: get_last_token_denom "ALPHA"  → e.g., "ALPHA_0" or "ALPHA_9"
get_last_token_denom() {
    local symbol="$1"
    query_tokens_by_symbol "$symbol" | jq -r '
        .tokens | map(.id) | sort_by(split("_")[-1] | tonumber) | last // ""
    ' 2>/dev/null
}

# create_token FROM_KEY NAME SYMBOL INITIAL_SUPPLY TOTAL_SUPPLY DECIMALS LOGO UNLIMITED [EXTRA_FLAGS]
# Tax: --tax '{"percent":"PROTO_INT","recipient_address":"stoc1..."}'
#   Proto int for percent: 5% = 50000000000000000 (5 * 10^16)
# Distributions: --distributions '{"address":"stoc1...","percent":60}' --distributions '...'
create_token() {
    local from_key="$1"
    local name="$2"
    local symbol="$3"
    local initial_supply="$4"
    local total_supply="$5"
    local decimals="$6"
    local logo="$7"
    local unlimited="${8:-false}"
    local extra_flags="${9:-}"

    send_tx "$BINARY tx stoc create-token \
        --from $from_key \
        --name $name \
        --symbol $symbol \
        --initial-supply $initial_supply \
        --total-supply $total_supply \
        --decimals $decimals \
        --logo $logo \
        --unlimited=$unlimited \
        $extra_flags"
}

mint_tokens() {
    local from_key="$1"
    local symbol="$2"
    local amount="$3"
    send_tx "$BINARY tx stoc mint-tokens --from $from_key --symbol $symbol --amount $amount"
}

release_tokens() {
    local from_key="$1"
    local symbol="$2"
    local amount="$3"
    local recipient="$4"
    send_tx "$BINARY tx stoc release-tokens --from $from_key --symbol $symbol --amount $amount --recipient $recipient"
}

burn_tokens() {
    local from_key="$1"
    local denom="$2"
    local amount="${3:-0}"
    local burn_all="${4:-false}"

    local flags=""
    if [[ "$burn_all" == "true" ]]; then
        flags="--burn-all"
    else
        flags="--amount $amount"
    fi
    send_tx "$BINARY tx stoc burn-token --from $from_key --denom $denom $flags"
}

# ---------------------------------------------------------------------------
# Tax helper — convert human-readable percent to proto integer
# Usage: tax_percent 5    → "50000000000000000"   (5%)
#        tax_percent 0.1  → "1000000000000000"    (0.1%)
#        tax_percent 50   → "500000000000000000"  (50%)
# ---------------------------------------------------------------------------
tax_percent() {
    local pct="$1"
    # Multiply by 10^16 (LegacyDec precision 10^18 / 100)
    # Use bc for decimal math
    echo "$(echo "$pct * 10000000000000000" | bc | sed 's/\..*$//')"
}

# Build --tax flag JSON
# Usage: tax_flag 5 stoc1addr...  → --tax '{"percent":"50000000000000000","recipient_address":"stoc1addr..."}'
tax_flag() {
    local pct_human="$1"
    local recipient="$2"
    local pct_proto
    pct_proto=$(tax_percent "$pct_human")
    echo "--tax '{\"percent\":\"${pct_proto}\",\"recipient_address\":\"${recipient}\"}'"
}

# ---------------------------------------------------------------------------
# EVM JSON-RPC helpers
# ---------------------------------------------------------------------------

evm_rpc_call() {
    local method="$1"
    local params="$2"
    curl -s -m 10 -X POST "$EVM_RPC" \
        -H "Content-Type: application/json" \
        -d "{\"jsonrpc\":\"2.0\",\"method\":\"$method\",\"params\":$params,\"id\":1}"
}

evm_is_available() {
    local result
    result=$(evm_rpc_call "eth_blockNumber" "[]" 2>/dev/null)
    echo "$result" | jq -e '.result' >/dev/null 2>&1
}

evm_chain_id() {
    evm_rpc_call "eth_chainId" "[]" | jq -r '.result // empty'
}

evm_block_number() {
    evm_rpc_call "eth_blockNumber" "[]" | jq -r '.result // empty'
}

evm_get_balance() {
    local address="$1"
    evm_rpc_call "eth_getBalance" "[\"$address\",\"latest\"]" | jq -r '.result // empty'
}

evm_get_transaction_count() {
    local address="$1"
    evm_rpc_call "eth_getTransactionCount" "[\"$address\",\"latest\"]" | jq -r '.result // empty'
}

evm_send_raw_transaction() {
    local signed_tx="$1"
    evm_rpc_call "eth_sendRawTransaction" "[\"$signed_tx\"]"
}

evm_get_transaction_receipt() {
    local txhash="$1"
    evm_rpc_call "eth_getTransactionReceipt" "[\"$txhash\"]"
}

hex_to_dec() {
    printf "%d" "$1" 2>/dev/null
}

dec_to_hex() {
    printf "0x%x" "$1" 2>/dev/null
}

# ---------------------------------------------------------------------------
# Governance helpers
# ---------------------------------------------------------------------------

submit_upgrade_proposal() {
    local from_key="$1"
    local upgrade_name="$2"
    local upgrade_height="$3"
    local deposit="${4:-10000000${DENOM}}"

    send_tx "$BINARY tx gov submit-proposal software-upgrade $upgrade_name \
        --title 'Upgrade to $upgrade_name' \
        --description 'Upgrade chain to $upgrade_name' \
        --upgrade-height $upgrade_height \
        --deposit $deposit \
        --from $from_key \
        --no-validate"
}

vote_yes() {
    local from_key="$1"
    local proposal_id="$2"
    send_tx "$BINARY tx gov vote $proposal_id yes --from $from_key"
}

query_proposal() {
    local proposal_id="$1"
    $BINARY query gov proposal "$proposal_id" --node "$NODE" --output json 2>/dev/null
}

# ---------------------------------------------------------------------------
# EVM / feemarket queries
# ---------------------------------------------------------------------------

query_evm_params() {
    $BINARY query evm params --node "$NODE" --output json 2>/dev/null
}

query_feemarket_params() {
    $BINARY query feemarket params --node "$NODE" --output json 2>/dev/null
}

query_bank_denom_metadata() {
    local denom="$1"
    $BINARY query bank denom-metadata "$denom" --node "$NODE" --output json 2>/dev/null
}

# ---------------------------------------------------------------------------
# IBC helpers
# ---------------------------------------------------------------------------

ibc_transfer() {
    local from_key="$1"
    local receiver="$2"
    local amount="$3"
    local channel="${4:-channel-0}"
    send_tx "$BINARY tx ibc-transfer transfer transfer $channel $receiver $amount --from $from_key --packet-timeout-height 0-0 --packet-timeout-timestamp 0"
}
