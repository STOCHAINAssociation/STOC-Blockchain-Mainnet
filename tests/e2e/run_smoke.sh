#!/bin/bash
# =============================================================================
# STOC E2E Smoke Test
# Runs all test suites against current binary via `ignite chain serve --reset-once`
# Usage: ./run_smoke.sh [--no-chain] [--suite 01,02,03]
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
START_CHAIN=true
SUITES_FILTER=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --no-chain)
            START_CHAIN=false
            shift
            ;;
        --suite)
            SUITES_FILTER="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: $0 [--no-chain] [--suite 01,02,03]"
            echo ""
            echo "Options:"
            echo "  --no-chain  Skip starting chain (assume already running)"
            echo "  --suite     Run specific suites (comma-separated: 01,02,03,04,05,06,07)"
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
# Chain management
# ---------------------------------------------------------------------------
IGNITE_PID=""

start_chain() {
    log_info "Starting chain with: ignite chain serve --reset-once"
    log_info "Project dir: $PROJECT_DIR"

    cd "$PROJECT_DIR"

    # Kill any existing ignite process
    pkill -f "ignite chain serve" 2>/dev/null || true
    sleep 2

    # Start ignite in background, capture output
    ignite chain serve --reset-once > /tmp/stoc_e2e_chain.log 2>&1 &
    IGNITE_PID=$!
    log_info "Ignite PID: $IGNITE_PID"

    # Wait for chain to compile and start
    log_info "Waiting for chain to compile and start (this may take a few minutes) ..."
    local max_wait=300  # 5 minutes for compilation + start
    local elapsed=0
    while [[ $elapsed -lt $max_wait ]]; do
        if ! kill -0 $IGNITE_PID 2>/dev/null; then
            log_error "Ignite process died. Check /tmp/stoc_e2e_chain.log"
            tail -20 /tmp/stoc_e2e_chain.log 2>/dev/null
            return 1
        fi
        if $BINARY status --node "$NODE" 2>/dev/null | jq -e '.sync_info.catching_up == false' >/dev/null 2>&1; then
            local height
            height=$($BINARY status --node "$NODE" 2>/dev/null | jq -r '.sync_info.latest_block_height')
            log_info "Chain started at height $height"
            return 0
        fi
        sleep 5
        ((elapsed += 5))
        # Show progress every 30s
        if (( elapsed % 30 == 0 )); then
            log_info "Still waiting... (${elapsed}s / ${max_wait}s)"
            tail -1 /tmp/stoc_e2e_chain.log 2>/dev/null | head -c 200
            echo ""
        fi
    done

    log_error "Chain failed to start within ${max_wait}s"
    tail -30 /tmp/stoc_e2e_chain.log 2>/dev/null
    return 1
}

stop_chain() {
    if [[ -n "$IGNITE_PID" ]] && kill -0 $IGNITE_PID 2>/dev/null; then
        log_info "Stopping chain (PID: $IGNITE_PID) ..."
        kill $IGNITE_PID 2>/dev/null
        wait $IGNITE_PID 2>/dev/null || true
        log_info "Chain stopped"
    fi
    # Also kill any leftover processes
    pkill -f "ignite chain serve" 2>/dev/null || true
    pkill -f "stocd start" 2>/dev/null || true
}

# Cleanup on exit
cleanup() {
    if [[ "$START_CHAIN" == "true" ]]; then
        stop_chain
    fi
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Check suite filter
# ---------------------------------------------------------------------------
should_run_suite() {
    local suite_num="$1"
    if [[ -z "$SUITES_FILTER" ]]; then
        return 0  # Run all
    fi
    echo "$SUITES_FILTER" | tr ',' '\n' | grep -q "^${suite_num}$"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
echo ""
echo -e "${BOLD}=================================================================${NC}"
echo -e "${BOLD}  STOC E2E SMOKE TEST${NC}"
echo -e "${BOLD}  $(date '+%Y-%m-%d %H:%M:%S')${NC}"
echo -e "${BOLD}=================================================================${NC}"
echo ""

# Step 1: Start chain
if [[ "$START_CHAIN" == "true" ]]; then
    start_chain || { log_error "Failed to start chain"; exit 1; }
else
    log_info "Using existing chain at $NODE"
    wait_for_chain 30 || { log_error "Chain not available"; exit 1; }
fi

# Wait a few blocks for genesis state to settle
log_info "Waiting for genesis state to settle ..."
wait_for_block 2

# Step 2: Setup wallets
setup_wallets
print_wallet_summary

# Step 3: Run test suites
if should_run_suite "01"; then run_token_lifecycle_tests; fi
if should_run_suite "02"; then run_tax_system_tests; fi
if should_run_suite "03"; then run_evm_isolation_tests; fi
if should_run_suite "04"; then run_denom_conversion_tests; fi
if should_run_suite "05"; then run_gas_enforcement_tests; fi
if should_run_suite "06"; then run_ibc_restriction_tests; fi
if should_run_suite "07"; then run_edge_case_tests; fi

# Step 4: Print summary
print_summary
exit_code=$?

echo ""
log_info "Chain log: /tmp/stoc_e2e_chain.log"
echo ""

exit $exit_code
