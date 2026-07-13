#!/bin/bash
# =============================================================================
# STOC E2E Test Framework
# Lightweight bash test framework with assertions, counters, and colored output
# =============================================================================

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Counters
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

# Current test info
CURRENT_TEST=""
CURRENT_SUITE=""

# Results tracking
declare -a FAILED_LIST=()
declare -a SKIPPED_LIST=()

# =============================================================================
# Suite / Test lifecycle
# =============================================================================

suite() {
    CURRENT_SUITE="$1"
    echo ""
    echo -e "${BLUE}${BOLD}=================================================================${NC}"
    echo -e "${BLUE}${BOLD}  SUITE: $CURRENT_SUITE${NC}"
    echo -e "${BLUE}${BOLD}=================================================================${NC}"
    echo ""
}

test_start() {
    ((TOTAL_TESTS++))
    CURRENT_TEST="$1"
    echo -ne "  [${TOTAL_TESTS}] ${CURRENT_TEST} ... "
}

pass() {
    ((PASSED_TESTS++))
    echo -e "${GREEN}PASS${NC}"
}

fail() {
    ((FAILED_TESTS++))
    local msg="${1:-}"
    echo -e "${RED}FAIL${NC}"
    if [[ -n "$msg" ]]; then
        echo -e "      ${RED}-> ${msg}${NC}"
    fi
    FAILED_LIST+=("[$CURRENT_SUITE] $CURRENT_TEST${msg:+: $msg}")
}

skip() {
    ((SKIPPED_TESTS++))
    local msg="${1:-}"
    echo -e "${YELLOW}SKIP${NC}"
    if [[ -n "$msg" ]]; then
        echo -e "      ${YELLOW}-> ${msg}${NC}"
    fi
    SKIPPED_LIST+=("[$CURRENT_SUITE] $CURRENT_TEST${msg:+: $msg}")
}

# =============================================================================
# Assertions
# =============================================================================

# Assert command exit code is 0
assert_success() {
    local exit_code=$1
    local detail="${2:-}"
    if [[ $exit_code -eq 0 ]]; then
        pass
    else
        fail "expected success (exit 0), got exit ${exit_code}${detail:+. $detail}"
    fi
}

# Assert command exit code is non-zero
assert_failure() {
    local exit_code=$1
    local detail="${2:-}"
    if [[ $exit_code -ne 0 ]]; then
        pass
    else
        fail "expected failure (exit != 0), got exit 0${detail:+. $detail}"
    fi
}

# Assert output contains expected string (grep -q)
assert_contains() {
    local output="$1"
    local expected="$2"
    if echo "$output" | grep -q "$expected" 2>/dev/null; then
        pass
    else
        local snippet="${output:0:200}"
        fail "output does not contain '${expected}' (got: ${snippet}...)"
    fi
}

# Assert output does NOT contain string
assert_not_contains() {
    local output="$1"
    local unexpected="$2"
    if ! echo "$output" | grep -q "$unexpected" 2>/dev/null; then
        pass
    else
        fail "output unexpectedly contains '${unexpected}'"
    fi
}

# Assert two values are equal
assert_equals() {
    local actual="$1"
    local expected="$2"
    if [[ "$actual" == "$expected" ]]; then
        pass
    else
        fail "expected '${expected}', got '${actual}'"
    fi
}

# Assert actual > expected (numeric)
assert_gt() {
    local actual="$1"
    local expected="$2"
    if (( actual > expected )); then
        pass
    else
        fail "expected > ${expected}, got ${actual}"
    fi
}

# Assert actual >= expected (numeric)
assert_gte() {
    local actual="$1"
    local expected="$2"
    if (( actual >= expected )); then
        pass
    else
        fail "expected >= ${expected}, got ${actual}"
    fi
}

# Assert actual < expected (numeric)
assert_lt() {
    local actual="$1"
    local expected="$2"
    if (( actual < expected )); then
        pass
    else
        fail "expected < ${expected}, got ${actual}"
    fi
}

# Assert JSON field equals expected value
# Usage: assert_json_field "$json_output" ".field.path" "expected_value"
assert_json_field() {
    local json="$1"
    local path="$2"
    local expected="$3"
    local actual
    actual=$(echo "$json" | jq -r "$path" 2>/dev/null)
    if [[ "$actual" == "$expected" ]]; then
        pass
    else
        fail "JSON${path} expected '${expected}', got '${actual}'"
    fi
}

# Assert tx result code is 0 (successful transaction)
assert_tx_success() {
    local tx_result="$1"
    local code
    code=$(echo "$tx_result" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" == "0" ]]; then
        pass
    else
        local raw_log
        raw_log=$(echo "$tx_result" | jq -r '.raw_log // .log // "unknown"' 2>/dev/null)
        fail "tx failed with code ${code}: ${raw_log:0:200}"
    fi
}

# Assert tx result code is non-zero (failed transaction)
assert_tx_failure() {
    local tx_result="$1"
    local expected_msg="${2:-}"
    local code
    code=$(echo "$tx_result" | jq -r '.code // "null"' 2>/dev/null)
    if [[ "$code" != "0" && "$code" != "null" ]]; then
        if [[ -n "$expected_msg" ]]; then
            local raw_log
            raw_log=$(echo "$tx_result" | jq -r '.raw_log // .log // ""' 2>/dev/null)
            if echo "$raw_log" | grep -qi "$expected_msg" 2>/dev/null; then
                pass
            else
                fail "tx failed as expected (code ${code}) but error msg mismatch. Expected '${expected_msg}', got '${raw_log:0:200}'"
            fi
        else
            pass
        fi
    else
        fail "expected tx failure, but tx succeeded (code ${code})"
    fi
}

# =============================================================================
# Summary
# =============================================================================

print_summary() {
    echo ""
    echo -e "${BLUE}${BOLD}=================================================================${NC}"
    echo -e "${BLUE}${BOLD}  TEST SUMMARY${NC}"
    echo -e "${BLUE}${BOLD}=================================================================${NC}"
    echo -e "  Total:   ${BOLD}${TOTAL_TESTS}${NC}"
    echo -e "  ${GREEN}Passed:  ${PASSED_TESTS}${NC}"
    echo -e "  ${RED}Failed:  ${FAILED_TESTS}${NC}"
    echo -e "  ${YELLOW}Skipped: ${SKIPPED_TESTS}${NC}"

    if [[ ${#FAILED_LIST[@]} -gt 0 ]]; then
        echo ""
        echo -e "  ${RED}${BOLD}FAILED TESTS:${NC}"
        for item in "${FAILED_LIST[@]}"; do
            echo -e "    ${RED}x ${item}${NC}"
        done
    fi

    if [[ ${#SKIPPED_LIST[@]} -gt 0 ]]; then
        echo ""
        echo -e "  ${YELLOW}${BOLD}SKIPPED TESTS:${NC}"
        for item in "${SKIPPED_LIST[@]}"; do
            echo -e "    ${YELLOW}~ ${item}${NC}"
        done
    fi

    echo -e "${BLUE}${BOLD}=================================================================${NC}"
    echo ""

    if [[ $FAILED_TESTS -gt 0 ]]; then
        return 1
    fi
    return 0
}

# =============================================================================
# Utility
# =============================================================================

# Log info message
log_info() {
    echo -e "  ${CYAN}[INFO]${NC} $*"
}

# Log warning
log_warn() {
    echo -e "  ${YELLOW}[WARN]${NC} $*"
}

# Log error
log_error() {
    echo -e "  ${RED}[ERROR]${NC} $*"
}

# Section separator (within a suite)
section() {
    echo ""
    echo -e "  ${CYAN}--- $1 ---${NC}"
    echo ""
}
