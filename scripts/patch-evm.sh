#!/bin/bash
# patch-evm.sh — Apply cosmos/evm v0.6.0 patches for local development
#
# Run this AFTER `go mod download` and BEFORE `ignite chain build` or `ignite chain serve`
#
# Patches:
#   1. Gas price double-conversion fix (MinGasPrice + RPCMinGasPrice)
#   2. Receipt null fix for simple ETH transfers (DecodeMsgLogs)
#
# Usage:
#   ./scripts/patch-evm.sh          # Apply patches
#   ./scripts/patch-evm.sh --check  # Check if patches are applied
#   ./scripts/patch-evm.sh --revert # Re-download clean module (revert patches)

set -euo pipefail

# Detect Go module cache
if command -v gvm &>/dev/null || [ -f "$HOME/.gvm/scripts/gvm" ]; then
    source "$HOME/.gvm/scripts/gvm" 2>/dev/null || true
fi

MODCACHE=$(go env GOMODCACHE 2>/dev/null)
EVM_MOD="$MODCACHE/github.com/cosmos/evm@v0.6.0"

if [ ! -d "$EVM_MOD" ]; then
    echo "ERROR: cosmos/evm v0.6.0 not found in module cache"
    echo "Run: go mod download"
    exit 1
fi

# --- Check mode ---
if [ "${1:-}" = "--check" ]; then
    echo "Checking patches in: $EVM_MOD"
    PATCHED=0
    grep -q "PATCHED" "$EVM_MOD/x/vm/wrappers/feemarket.go" 2>/dev/null && echo "  [OK] feemarket.go MinGasPrice" && PATCHED=$((PATCHED+1)) || echo "  [--] feemarket.go MinGasPrice NOT patched"
    grep -q "PATCHED" "$EVM_MOD/rpc/backend/node_info.go" 2>/dev/null && echo "  [OK] node_info.go RPCMinGasPrice" && PATCHED=$((PATCHED+1)) || echo "  [--] node_info.go RPCMinGasPrice NOT patched"
    grep -q "PATCHED" "$EVM_MOD/x/vm/types/utils.go" 2>/dev/null && echo "  [OK] utils.go DecodeMsgLogs" && PATCHED=$((PATCHED+1)) || echo "  [--] utils.go DecodeMsgLogs NOT patched"
    echo ""
    echo "$PATCHED/3 patches applied"
    exit 0
fi

# --- Revert mode ---
if [ "${1:-}" = "--revert" ]; then
    echo "Reverting: re-downloading cosmos/evm v0.6.0..."
    GOFLAGS=-mod=mod go mod download github.com/cosmos/evm@v0.6.0
    echo "Done. Module cache restored to clean state."
    exit 0
fi

# --- Apply patches ---
echo "Applying patches to: $EVM_MOD"
chmod -R u+w "$EVM_MOD"

# Fix 1a: FeeMarketWrapper.GetParams() — MinGasPrice double-conversion
if grep -q 'params.MinGasPrice = types.ConvertAmountTo18DecimalsLegacy(params.MinGasPrice)' "$EVM_MOD/x/vm/wrappers/feemarket.go"; then
    sed -i.bak 's|params.MinGasPrice = types.ConvertAmountTo18DecimalsLegacy(params.MinGasPrice)|// PATCHED: MinGasPrice already in EVM 18-decimal scale, skip conversion|' \
        "$EVM_MOD/x/vm/wrappers/feemarket.go"
    echo "  [OK] feemarket.go MinGasPrice — patched"
else
    echo "  [--] feemarket.go MinGasPrice — already patched or changed"
fi

# Fix 1b: RPCMinGasPrice() — double-conversion
if grep -q 'return evmtypes.ConvertAmountTo18DecimalsLegacy(amt).TruncateInt().BigInt()' "$EVM_MOD/rpc/backend/node_info.go"; then
    sed -i.bak 's|return evmtypes.ConvertAmountTo18DecimalsLegacy(amt).TruncateInt().BigInt()|return amt.TruncateInt().BigInt() // PATCHED: already in EVM scale|' \
        "$EVM_MOD/rpc/backend/node_info.go"
    echo "  [OK] node_info.go RPCMinGasPrice — patched"
else
    echo "  [--] node_info.go RPCMinGasPrice — already patched or changed"
fi

# Fix 2: DecodeMsgLogs() — receipt null for simple transfers
if grep -q 'return nil, fmt.Errorf("invalid message index: %d", msgIndex)' "$EVM_MOD/x/vm/types/utils.go"; then
    sed -i.bak 's|return nil, fmt.Errorf("invalid message index: %d", msgIndex)|return nil, nil // PATCHED: simple transfers have no tx responses, return empty logs|' \
        "$EVM_MOD/x/vm/types/utils.go"
    echo "  [OK] utils.go DecodeMsgLogs — patched"
else
    echo "  [--] utils.go DecodeMsgLogs — already patched or changed"
fi

# Clean up backup files
find "$EVM_MOD" -name "*.bak" -delete 2>/dev/null || true

echo ""
echo "All patches applied. Run: ignite chain build"
