# v0.6.0 Audit Fixes + Deploy + Mainnet Upgrade Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 3 audit issues (M1-M3), deploy to devnet full loop, update testnet, execute mainnet v0.6.0 upgrade at height 4,705,316.

**Architecture:** Fix code on `fix/evm_v0.6.0` branch, rebuild binary, test devnet (no-evm -> v1.0.0-rc2 -> v0.6.0), deploy testnet, prepare mainnet swap.

**Tech Stack:** Go 1.24, Cosmos SDK v0.53.4, cosmos/evm v0.6.0, CometBFT v0.38.18

---

## Phase 1: Code Fixes (M1-M3)

### Task 1: M1 — Add fee denom restriction to CosmosMinGasPriceDecorator

**Files:**
- Modify: `app/ante/cosmos_min_gas_price.go:73-86`

- [ ] **Step 1: Add fee denom validation before gas price check**

After line 54 (`return next(ctx, tx, simulate)`) and before the conversion logic, add fee denom validation:

```go
// After line 74 (feeCoins := feeTx.GetFee())
// Add fee denom validation (defense-in-depth: match upstream behavior)
if len(feeCoins) > 1 {
    return ctx, errorsmod.Wrapf(errortypes.ErrInvalidCoins,
        "expected only one fee coin, got %d: %s", len(feeCoins), feeCoins.String())
}
if len(feeCoins) == 1 && feeCoins[0].Denom != evmDenom {
    return ctx, errorsmod.Wrapf(errortypes.ErrInvalidCoins,
        "expected fee in %s, got %s", evmDenom, feeCoins[0].Denom)
}
```

Insert this between line 75 (after `feeCoins := feeTx.GetFee()`) and line 76 (the nil check). The nil check on line 76 stays as-is.

- [ ] **Step 2: Verify build**

```bash
cd /Volumes/bautd/minh-anh-corp/projects/sto-chain/stoc-compare-ignite-default/stoc-current-dev
go build ./...
```

Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add app/ante/cosmos_min_gas_price.go
git commit -m "fix(ante): add fee denom restriction to CosmosMinGasPriceDecorator"
```

---

### Task 2: M3 — Cache denom pair in EvmBankKeeper

**Files:**
- Modify: `x/evmutil/keeper/bank_keeper.go:19-38`

- [ ] **Step 1: Add cached fields to EvmBankKeeper struct**

Replace the struct and constructor (lines 19-28):

```go
type EvmBankKeeper struct {
	bankKeeper  types.BankKeeper
	evmDenom    string // cached at construction, avoids runtime panic
	cosmosDenom string // cached at construction
}

func NewEvmBankKeeper(bankKeeper types.BankKeeper) EvmBankKeeper {
	evmDenom, err := types.SafeGetEvmDenom()
	if err != nil {
		panic(fmt.Sprintf("evmutil: failed to resolve EVM denom at init: %v", err))
	}
	return EvmBankKeeper{
		bankKeeper:  bankKeeper,
		evmDenom:    evmDenom,
		cosmosDenom: types.GetCosmosDenom(),
	}
}
```

- [ ] **Step 2: Replace helper functions with methods**

Replace lines 30-38:

```go
// getEvmDenom returns the cached EVM denom
func (k EvmBankKeeper) getEvmDenom() string {
	return k.evmDenom
}

// getCosmosDenom returns the cached Cosmos denom
func (k EvmBankKeeper) getCosmosDenom() string {
	return k.cosmosDenom
}
```

- [ ] **Step 3: Update all callers from package-level to method calls**

Replace ALL occurrences in bank_keeper.go:
- `types.GetEvmDenom()` -> `k.getEvmDenom()` (except in custom token check pattern)
- `types.GetCosmosDenom()` -> `k.getCosmosDenom()`
- `getEvmDenom()` -> `k.getEvmDenom()`
- `getCosmosDenom()` -> `k.getCosmosDenom()`

Key locations:
- Line 44-45: `evmDenom := types.GetEvmDenom()` -> `evmDenom := k.getEvmDenom()`
- Line 127-128: same pattern
- All other GetBalance, SendCoins, MintCoins, BurnCoins, etc.

For custom token checks (lines like `if denom != evmDenom && denom != cosmosDenom`), use cached values.

- [ ] **Step 4: Verify build**

```bash
go build ./...
```

- [ ] **Step 5: Run existing tests**

```bash
go test -mod=readonly -v -timeout 30m ./x/evmutil/keeper/...
```

Expected: all tests pass

- [ ] **Step 6: Commit**

```bash
git add x/evmutil/keeper/bank_keeper.go
git commit -m "fix(evmutil): cache denom pair in EvmBankKeeper to prevent runtime panic"
```

---

### Task 3: M2 — Set mempool limit

**Files:**
- Modify: `app/evm.go:215`

- [ ] **Step 1: Change cosmosPoolMaxTx from 0 to 5000**

Line 215, change the last parameter:

```go
// Before:
evmMempool := evmmempool.NewExperimentalEVMMempool(app.CreateQueryContext, app.Logger(), app.EVMKeeper, app.FeeMarketKeeper, app.txConfig, app.clientCtx, mempoolConfig, 0)

// After:
evmMempool := evmmempool.NewExperimentalEVMMempool(app.CreateQueryContext, app.Logger(), app.EVMKeeper, app.FeeMarketKeeper, app.txConfig, app.clientCtx, mempoolConfig, 5000)
```

- [ ] **Step 2: Update comment on line 214**

```go
// v0.6.0: cosmosPoolMaxTx=5000 limits pending Cosmos txs in EVM mempool (DoS mitigation)
```

- [ ] **Step 3: Verify build**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add app/evm.go
git commit -m "fix(evm): set mempool cosmosPoolMaxTx=5000 to prevent DoS"
```

---

### Task 4: Build binaries

**Files:** None (build only)

- [ ] **Step 1: Build v0.6.0 binary with fixes**

```bash
cd /Volumes/bautd/minh-anh-corp/projects/sto-chain/stoc-compare-ignite-default/stoc-current-dev
go build -mod=readonly -o /tmp/stoc_v060_fixed_d ./cmd/stocd
chmod +x /tmp/stoc_v060_fixed_d
ls -la /tmp/stoc_v060_fixed_d
```

- [ ] **Step 2: Verify binary version**

```bash
/tmp/stoc_v060_fixed_d version
```

- [ ] **Step 3: Push fixes to remote**

```bash
GIT_SSH_COMMAND="ssh -i ~/.ssh/trdbau.github" git push origin fix/evm_v0.6.0
```

---

## Phase 2: Devnet Full Loop

### Task 5: Reset devnet and test full upgrade cycle

**VPS IPs:**
- Val1: 160.191.50.254
- Val2: 160.191.51.60
- Val3: 160.191.51.25
- Fullnode: 157.66.100.71

**SSH keys:** `vps-ssh-key/devnet-net/`
**Chain ID:** `dstoc_13061999-1`
**Denom:** `udstoc`

**Binaries needed on each VPS:**
- `stocd_noevm` — from archive/no-evm branch (already on VPS)
- `stocd_evm` — from archive/feat-evm-v1.0.0-rc2 (need clean version without v3 handler)
- `stocd_v060` — from fix/evm_v0.6.0 with M1-M3 fixes (rebuild + upload)

- [ ] **Step 1: Build clean feat/evm binary (no v3 handler) for devnet step 2**

```bash
# Local: checkout archive branch, build
git stash
git checkout archive/feat-evm-v1.0.0-rc2
go build -mod=readonly -o /tmp/stoc_featevm_clean_devnet_d ./cmd/stocd
git checkout fix/evm_v0.6.0
git stash pop
```

- [ ] **Step 2: Cross-compile v0.6.0 for Linux (VPS)**

```bash
GOOS=linux GOARCH=amd64 go build -mod=readonly -o /tmp/stocd_v060_linux ./cmd/stocd
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -tags netgo -o /tmp/stocd_v060_linux ./cmd/stocd
```

Note: If CGO issues, use Docker:
```bash
docker build --platform linux/amd64 -f Dockerfile.stocd -t stoc-v060 .
docker create --name stoc-v060-tmp stoc-v060
docker cp stoc-v060-tmp:/usr/local/bin/stocd /tmp/stocd_v060_linux
docker rm stoc-v060-tmp
```

- [ ] **Step 3: Cross-compile feat/evm clean for Linux**

```bash
git checkout archive/feat-evm-v1.0.0-rc2
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -mod=readonly -tags netgo -o /tmp/stocd_featevm_linux ./cmd/stocd
git checkout fix/evm_v0.6.0
```

- [ ] **Step 4: Upload binaries to all 4 devnet nodes**

```bash
for ip in 160.191.50.254 160.191.51.60 160.191.51.25 157.66.100.71; do
  echo "=== Uploading to $ip ==="
  scp /tmp/stocd_v060_linux root@$ip:/root/go/bin/stocd_v060
  scp /tmp/stocd_featevm_linux root@$ip:/root/go/bin/stocd_evm_clean
  ssh root@$ip "chmod +x /root/go/bin/stocd_v060 /root/go/bin/stocd_evm_clean"
done
```

- [ ] **Step 5: Stop all devnet nodes**

```bash
for ip in 160.191.50.254 160.191.51.60 160.191.51.25 157.66.100.71; do
  ssh root@$ip "systemctl stop stocd"
done
```

- [ ] **Step 6: Reset chain data on all nodes**

```bash
for ip in 160.191.50.254 160.191.51.60 160.191.51.25 157.66.100.71; do
  ssh root@$ip "stocd tendermint unsafe-reset-all --home /root/.stoc"
done
```

- [ ] **Step 7: Re-create genesis with no-evm binary**

Use genesis files from `genesis/` directory or re-create with stocd_noevm.
Ensure chain-id=`dstoc_13061999-1`, denom=`udstoc`.

```bash
# On Val1 only:
ssh root@160.191.50.254 "cp /root/go/bin/stocd_noevm /root/go/bin/stocd && stocd init val1 --chain-id dstoc_13061999-1 --home /root/.stoc --overwrite"
# Copy genesis + config to all nodes
```

- [ ] **Step 8: Start with no-evm binary, verify chain runs**

```bash
for ip in 160.191.50.254 160.191.51.60 160.191.51.25 157.66.100.71; do
  ssh root@$ip "cp /root/go/bin/stocd_noevm /root/go/bin/stocd && systemctl start stocd"
done
# Wait 15s
ssh root@160.191.50.254 "stocd status | jq '.sync_info.latest_block_height'"
```

Expected: height increasing

- [ ] **Step 9: Send test tx on no-evm chain**

```bash
stocd tx bank send devnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1udstoc \
  --gas 200000 --gas-prices 0.001udstoc \
  --chain-id dstoc_13061999-1 --keyring-backend test \
  --node http://160.191.50.254:26657 -y
```

- [ ] **Step 10: Submit v2-evm upgrade proposal**

```bash
stocd tx gov submit-proposal software-upgrade v2-evm \
  --title "Enable EVM" --summary "Add EVM support" \
  --upgrade-height <current_height+20> \
  --deposit 10000000udstoc \
  --from devnet-admin --keyring-backend test \
  --chain-id dstoc_13061999-1 --node http://160.191.50.254:26657 -y

# Vote YES from all 3 validators
for key in devnet-validator-1 devnet-validator-2 devnet-validator-3; do
  stocd tx gov vote 1 yes --from $key --keyring-backend test \
    --chain-id dstoc_13061999-1 --node http://160.191.50.254:26657 -y
done
```

- [ ] **Step 11: Wait for chain halt at upgrade height, swap to feat/evm binary**

```bash
# When chain halts:
for ip in 160.191.50.254 160.191.51.60 160.191.51.25 157.66.100.71; do
  ssh root@$ip "systemctl stop stocd && cp /root/go/bin/stocd_evm_clean /root/go/bin/stocd && systemctl start stocd"
done
# Wait 15s, verify
ssh root@160.191.50.254 "stocd status | jq '.sync_info.latest_block_height'"
```

- [ ] **Step 12: Send test tx after v2-evm upgrade**

```bash
stocd tx bank send devnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1udstoc \
  --gas 200000 --gas-prices 0.001udstoc \
  --chain-id dstoc_13061999-1 --keyring-backend test \
  --node http://160.191.50.254:26657 -y
```

- [ ] **Step 13: Submit v3-fix-evm-denom upgrade proposal**

```bash
stocd tx gov submit-proposal software-upgrade v3-fix-evm-denom \
  --title "Fix EVM denom + MinGasPrice" --summary "Fix denom conversion and set MinGasPrice=1gwei" \
  --upgrade-height <current_height+20> \
  --deposit 10000000udstoc \
  --from devnet-admin --keyring-backend test \
  --chain-id dstoc_13061999-1 --node http://160.191.50.254:26657 -y

# Vote YES
for key in devnet-validator-1 devnet-validator-2 devnet-validator-3; do
  stocd tx gov vote 2 yes --from $key --keyring-backend test \
    --chain-id dstoc_13061999-1 --node http://160.191.50.254:26657 -y
done
```

- [ ] **Step 14: Wait for chain halt, swap to v0.6.0 binary (with fixes)**

```bash
for ip in 160.191.50.254 160.191.51.60 160.191.51.25 157.66.100.71; do
  ssh root@$ip "systemctl stop stocd && cp /root/go/bin/stocd_v060 /root/go/bin/stocd && systemctl start stocd"
done
# Wait 15s, verify
ssh root@160.191.50.254 "stocd status | jq '.sync_info.latest_block_height'"
```

- [ ] **Step 15: Verify v0.6.0 on devnet**

```bash
# Test 1: Cosmos tx gas=0 should FAIL
stocd tx bank send devnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1udstoc \
  --gas 200000 --gas-prices 0udstoc \
  --chain-id dstoc_13061999-1 --keyring-backend test \
  --node http://160.191.50.254:26657 -y
# Expected: FAIL (insufficient fee)

# Test 2: Cosmos tx gas=0.001 should SUCCEED
stocd tx bank send devnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1udstoc \
  --gas 200000 --gas-prices 0.001udstoc \
  --chain-id dstoc_13061999-1 --keyring-backend test \
  --node http://160.191.50.254:26657 -y
# Expected: SUCCESS

# Test 3: Check feemarket params
stocd q evm feemarket params --node http://160.191.50.254:26657
# Expected: min_gas_price = 1000000000

# Test 4: Check EVM denom
stocd q evm params --node http://160.191.50.254:26657
# Expected: evm_denom = udstoc, extended_denom = adstoc
```

---

## Phase 3: Testnet Update

### Task 6: Deploy fixed binary to testnet

**VPS IPs:**
- Val1: 157.66.219.215
- Val2: 157.66.219.218
- Val3: 157.66.219.221
- Fullnode: 157.66.219.214

**Chain ID:** `tstoc`
**Denom:** `utstoc`

Note: Testnet already runs v0.6.0. This is a binary update (no upgrade proposal needed).

- [ ] **Step 1: Upload fixed binary to all testnet nodes**

```bash
for ip in 157.66.219.215 157.66.219.218 157.66.219.221 157.66.219.214; do
  echo "=== Uploading to $ip ==="
  scp /tmp/stocd_v060_linux root@$ip:/root/go/bin/stocd_v060_fixed
  ssh root@$ip "chmod +x /root/go/bin/stocd_v060_fixed"
done
```

- [ ] **Step 2: Rolling restart (one node at a time to maintain consensus)**

```bash
# Val1
ssh root@157.66.219.215 "systemctl stop stocd && cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && systemctl start stocd"
sleep 15
ssh root@157.66.219.215 "stocd status | jq '.sync_info.latest_block_height'"

# Val2
ssh root@157.66.219.218 "systemctl stop stocd && cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && systemctl start stocd"
sleep 15

# Val3
ssh root@157.66.219.221 "systemctl stop stocd && cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && systemctl start stocd"
sleep 15

# Fullnode (last)
ssh root@157.66.219.214 "systemctl stop stocd && cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && systemctl start stocd"
sleep 15
```

- [ ] **Step 3: Verify testnet after update**

```bash
# Check height increasing
stocd status --node https://rpc-stoc-testnet.stochainscan.io:443 | jq '.sync_info.latest_block_height'

# Tx test: gas=0 should FAIL
stocd tx bank send testnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1utstoc \
  --gas 200000 --gas-prices 0utstoc \
  --chain-id tstoc --keyring-backend test \
  --node https://rpc-stoc-testnet.stochainscan.io:443 -y
# Expected: FAIL

# Tx test: gas=0.001 should SUCCEED
stocd tx bank send testnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1utstoc \
  --gas 200000 --gas-prices 0.001utstoc \
  --chain-id tstoc --keyring-backend test \
  --node https://rpc-stoc-testnet.stochainscan.io:443 -y
# Expected: SUCCESS
```

---

## Phase 4: Mainnet v0.6.0 Upgrade

### Task 7: Pre-upgrade preparation (DO NOW, before proposal passes)

- [ ] **Step 1: Upload fixed v0.6.0 binary to all 4 mainnet nodes**

**REQUIRES USER PERMISSION FOR MAINNET SSH**

```bash
# Mainnet IPs + SSH keys:
# Val1: 64.176.4.207 — key: vps-ssh-key/mainnet-net/mainnet_validator_1
# Val2: 202.182.110.150 — key: vps-ssh-key/mainnet-net/mainnet_validator_2
# Val3: 45.32.180.48 — key: vps-ssh-key/mainnet-net/mainnet_validator_3
# Fullblocks: 160.191.50.204 — key: vps-ssh-key/mainnet-net/mainnet_api_rpc_fullnode

for ip in 64.176.4.207 202.182.110.150 45.32.180.48 160.191.50.204; do
  echo "=== Uploading to $ip ==="
  scp /tmp/stocd_v060_linux root@$ip:/root/go/bin/stocd_v060_fixed
  ssh root@$ip "chmod +x /root/go/bin/stocd_v060_fixed"
done
```

- [ ] **Step 2: Verify binary exists on all nodes**

```bash
for ip in 64.176.4.207 202.182.110.150 45.32.180.48 160.191.50.204; do
  echo "=== $ip ==="
  ssh root@$ip "ls -la /root/go/bin/stocd_v060_fixed && /root/go/bin/stocd_v060_fixed version"
done
```

---

### Task 8: Mainnet upgrade execution (when chain halts at height 4,705,316)

**Timeline:**
- Proposal #4 voting ends: **2026-04-08 03:57 UTC**
- Upgrade height: **4,705,316** (~Apr 8 13:00 UTC estimated)
- Current height: ~4,670,700 (as of Apr 6)

**CRITICAL: Do NOT start until chain height = 4,705,316 and blocks stop**

- [ ] **Step 1: Monitor chain height**

```bash
# Run this periodically as height approaches 4,705,316
curl -s https://rpc-stoc-mainnet.stochainscan.io/status | jq '.result.sync_info.latest_block_height'
```

When height = 4,705,316 and NOT increasing for >30 seconds → chain has halted.

- [ ] **Step 2: Verify chain has halted (all validators stopped)**

```bash
# Check logs on Val1
ssh -i vps-ssh-key/mainnet-net/mainnet_validator_1 root@64.176.4.207 \
  "journalctl -u stocd -n 20 --no-pager"
# Expected: "UPGRADE \"v3-fix-evm-denom\" NEEDED at height: 4705316" or similar panic
```

- [ ] **Step 3: Swap binary on Val1 → Val2 → Val3 → Fullblocks (sequential)**

```bash
# === Val1 (64.176.4.207) ===
ssh -i vps-ssh-key/mainnet-net/mainnet_validator_1 root@64.176.4.207 \
  "systemctl stop stocd && \
   cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && \
   chmod +x /root/go/bin/stocd && \
   systemctl start stocd"
sleep 15
ssh -i vps-ssh-key/mainnet-net/mainnet_validator_1 root@64.176.4.207 \
  "stocd status 2>&1 | jq '.sync_info.latest_block_height' 2>/dev/null || journalctl -u stocd -n 5 --no-pager"

# === Val2 (202.182.110.150) ===
ssh -i vps-ssh-key/mainnet-net/mainnet_validator_2 root@202.182.110.150 \
  "systemctl stop stocd && \
   cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && \
   chmod +x /root/go/bin/stocd && \
   systemctl start stocd"
sleep 15
ssh -i vps-ssh-key/mainnet-net/mainnet_validator_2 root@202.182.110.150 \
  "stocd status 2>&1 | jq '.sync_info.latest_block_height' 2>/dev/null || journalctl -u stocd -n 5 --no-pager"

# === Val3 (45.32.180.48) ===
ssh -i vps-ssh-key/mainnet-net/mainnet_validator_3 root@45.32.180.48 \
  "systemctl stop stocd && \
   cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && \
   chmod +x /root/go/bin/stocd && \
   systemctl start stocd"
sleep 15
ssh -i vps-ssh-key/mainnet-net/mainnet_validator_3 root@45.32.180.48 \
  "stocd status 2>&1 | jq '.sync_info.latest_block_height' 2>/dev/null || journalctl -u stocd -n 5 --no-pager"

# === Fullblocks (160.191.50.204) ===
ssh -i vps-ssh-key/mainnet-net/mainnet_api_rpc_fullnode root@160.191.50.204 \
  "systemctl stop stocd && \
   cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && \
   chmod +x /root/go/bin/stocd && \
   systemctl start stocd"
sleep 15
ssh -i vps-ssh-key/mainnet-net/mainnet_api_rpc_fullnode root@160.191.50.204 \
  "stocd status 2>&1 | jq '.sync_info.latest_block_height' 2>/dev/null || journalctl -u stocd -n 5 --no-pager"
```

- [ ] **Step 4: Verify chain resumes (height > 4,705,316)**

```bash
curl -s https://rpc-stoc-mainnet.stochainscan.io/status | jq '.result.sync_info.latest_block_height'
# Expected: > 4705316 and increasing
```

- [ ] **Step 5: Verify v0.6.0 params**

```bash
# Feemarket params
stocd q evm feemarket params --node https://rpc-stoc-mainnet.stochainscan.io:443
# Expected: min_gas_price = 1000000000, no_base_fee = true

# EVM params
stocd q evm params --node https://rpc-stoc-mainnet.stochainscan.io:443
# Expected: evm_denom = ustoc, extended_denom_options.extended_denom = astoc
```

- [ ] **Step 6: Send test tx on mainnet**

```bash
# Test: gas=0.001 should succeed
stocd tx bank send mainnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1ustoc \
  --gas 200000 --gas-prices 0.001ustoc \
  --chain-id stoc --keyring-backend test \
  --node https://rpc-stoc-mainnet.stochainscan.io:443 -y

# Test: gas=0 should FAIL (MinGasPrice now enforced!)
stocd tx bank send mainnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1ustoc \
  --gas 200000 --gas-prices 0ustoc \
  --chain-id stoc --keyring-backend test \
  --node https://rpc-stoc-mainnet.stochainscan.io:443 -y
# Expected: FAIL (insufficient fee) — stress test bots BLOCKED!
```

---

### Task 9: Post-upgrade — Gas price governance proposal (if needed)

The v3-fix-evm-denom handler already sets MinGasPrice=10^9 (1 gwei). If this is working correctly (verified in Step 6), **NO separate gas price proposal is needed**.

Only submit if MinGasPrice needs adjustment:

- [ ] **Step 1: Check if MinGasPrice is already set by upgrade handler**

```bash
stocd q evm feemarket params --node https://rpc-stoc-mainnet.stochainscan.io:443 -o json | jq '.params.min_gas_price'
# If "1000000000.000000000000000000" → DONE, skip this task
# If "0.000000000000000000" → submit proposal below
```

- [ ] **Step 2: Submit gas price proposal (ONLY if min_gas_price still 0)**

```bash
cat > /tmp/gas_proposal.json << 'EOF'
{
  "messages": [{
    "@type": "/cosmos.evm.feemarket.v1.MsgUpdateParams",
    "authority": "stoc10d07y265gmmuvt4z0w9aw880jnsr700jzu6xvu",
    "params": {
      "no_base_fee": true,
      "base_fee_change_denominator": 8,
      "elasticity_multiplier": 2,
      "base_fee": "0",
      "min_gas_price": "1000000000000000000000000000",
      "min_gas_multiplier": "0.500000000000000000"
    }
  }],
  "deposit": "10000000ustoc",
  "title": "Set MinGasPrice to 1 gwei",
  "summary": "Set MinGasPrice=10^9 astoc/gas (0.001 ustoc/gas = 1 gwei)"
}
EOF

stocd tx gov submit-proposal /tmp/gas_proposal.json \
  --from mainnet-admin --keyring-backend test \
  --chain-id stoc --node https://rpc-stoc-mainnet.stochainscan.io:443 \
  --gas 500000 --gas-prices 0.001ustoc -y
```

- [ ] **Step 3: Vote YES from all validators**

```bash
# Get proposal ID
stocd q gov proposals --status voting_period --node https://rpc-stoc-mainnet.stochainscan.io:443

for key in mainnet-validator-1 mainnet-validator-2 mainnet-validator-3; do
  stocd tx gov vote <PROPOSAL_ID> yes --from $key --keyring-backend test \
    --chain-id stoc --node https://rpc-stoc-mainnet.stochainscan.io:443 \
    --gas 200000 --gas-prices 0.001ustoc -y
done
```

- [ ] **Step 4: Wait 48h for voting, then verify**

```bash
# After proposal passes:
stocd q evm feemarket params --node https://rpc-stoc-mainnet.stochainscan.io:443
# Expected: min_gas_price = 1000000000

# Test gas=0 rejected
stocd tx bank send mainnet-admin stoc1dw0587kdg7f0ak0kqmr4dxl0tdv3zn9yuagya2 1ustoc \
  --gas 200000 --gas-prices 0ustoc \
  --chain-id stoc --keyring-backend test \
  --node https://rpc-stoc-mainnet.stochainscan.io:443 -y
# Expected: FAIL — stress test bots permanently blocked
```

---

## Post-Upgrade Checklist

| Check | Command | Expected |
|-------|---------|----------|
| Chain producing blocks | `curl -s <RPC>/status \| jq '.result.sync_info.latest_block_height'` | Height increasing |
| MinGasPrice enforced | `stocd q evm feemarket params` | min_gas_price = 1000000000 |
| EVM denom correct | `stocd q evm params` | evm_denom=ustoc, extended=astoc |
| Cosmos tx gas=0 rejected | Send MsgSend with gas=0 | FAIL code=13 |
| Cosmos tx gas=0.001 works | Send MsgSend with gas=0.001 | SUCCESS |
| EVM transfer works | Send ETH tx via MetaMask | SUCCESS |
| Stress test bots blocked | Check tx list via BE sync API | No more zero-fee bot txs |
| Explorer working | Visit explorer | Blocks/txs loading |
| BE sync indexing | Check BE sync API latest block | Matches chain height |

---

## Rollback Plan (if v0.6.0 fails on mainnet)

**If chain doesn't resume after binary swap:**

1. Check logs: `journalctl -u stocd -n 50 --no-pager`
2. If consensus issue: ensure ALL 3 validators have the new binary
3. If panic/crash: revert to old binary

```bash
# ROLLBACK — only if v0.6.0 fails
for ip in 64.176.4.207 202.182.110.150 45.32.180.48 160.191.50.204; do
  ssh root@$ip "systemctl stop stocd && cp /root/go/bin/stocd_backup /root/go/bin/stocd && systemctl start stocd"
done
```

Note: Rollback requires the upgrade framework to be reset. If the upgrade handler has already executed, a simple binary rollback may not work. In that case, coordinate with all validators to use `--unsafe-skip-upgrades <height>` flag.

---

## Timeline Summary

| When | Action | Status |
|------|--------|--------|
| Now | Fix M1-M3 code | TODO |
| Now | Build binaries | TODO |
| Now | Devnet full loop | TODO |
| Now | Testnet update | TODO |
| Now | Upload fixed binary to mainnet | TODO (needs permission) |
| Apr 8 ~03:57 UTC | Proposal #4 passes | WAITING |
| Apr 8 ~13:00 UTC | Chain halts at 4,705,316 | WAITING |
| Apr 8 ~13:00 UTC | Swap binary on all nodes | WAITING |
| Apr 8 ~13:05 UTC | Verify chain resumes | WAITING |
| Apr 8 ~13:10 UTC | Test transactions | WAITING |
| If needed | Submit gas price proposal | CONDITIONAL |
