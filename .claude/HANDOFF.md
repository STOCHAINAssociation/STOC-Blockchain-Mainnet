# HANDOFF — v0.6.0 Audit + Fixes + Deploy + Testing

> Generated: 2026-04-07 | Branch: fix/evm_v0.6.0 | Commit: fd64dcd

## STATUS

Audit done (3 rounds GO), all fixes applied, devnet+testnet deployed, monitoring active, waiting mainnet upgrade Apr 8.
Next: create comprehensive E2E test suite.

## DONE

- [x] Audit v0.6.0 — 6 agents, 80 Go files, 3 audit rounds, ALL GO
- [x] Fix M1: fee denom restriction (`app/ante/cosmos_min_gas_price.go`)
- [x] Fix M2: mempool limit 5000 (`app/evm.go:215`)
- [x] Fix M3: cache denom EvmBankKeeper (`x/evmutil/keeper/bank_keeper.go`)
- [x] Fix L1: DisplayDenom TrimPrefix (`app/evm.go:117`)
- [x] Fix L5: escrow cache 100→10 blocks (`app/app.go:602`)
- [x] Fix L17: distribution overflow check (`x/stoc/types/token.go:234`)
- [x] Archive branches: `archive/no-evm`, `archive/feat-evm-v1.0.0-rc2` (remote)
- [x] Devnet full loop: no-evm(h1)→feat/evm(h40)→v0.6.0(h118) ALL PASS
- [x] Testnet rolling restart 4/4 nodes — PASS
- [x] Telegram monitor cronjob (Topic 40, bot 8288157775)
- [x] Binary: `/tmp/stocd_v060_fixed` (174MB Linux amd64)
- [x] Unit tests: M1 decorator (3), L17 overflow (7), M3 cached denom (57 existing)

## TODO — NEXT SESSION

### 1. E2E Test Suite (PRIORITY)
Create comprehensive test scripts using `ignite chain serve --reset-once` (local) + devnet.
See `memory/project_testing_strategy.md` for 8 test categories:
1. Token lifecycle (create/mint/release/burn with all options)
2. Tax system (transfer amounts, verify tax split)
3. EVM isolation (custom tokens NOT visible from EVM)
4. Denom conversion (regression: NOT aatom/atest)
5. Gas enforcement (0 rejected, 0.001 accepted, multi-denom rejected)
6. IBC restriction (custom tokens blocked)
7. Upgrade handler (v2-evm, v3-fix-evm-denom)
8. Edge cases (duplicate symbols, boundary values)

**How to test**: Use `stocd tx` / `stocd q` commands against running chain.
For EVM: use cast/curl with JSON-RPC or MetaMask.

### 2. Upload binary to mainnet (needs SSH permission)
Binary already built: `/tmp/stocd_v060_fixed`
Nodes: Val1=64.176.4.207, Val2=202.182.110.150, Val3=45.32.180.48, Full=160.191.50.204

### 3. Mainnet upgrade (~Apr 8 13:00 UTC)
Proposal #4: 3/3 YES, voting ends Apr 8 03:57 UTC
Upgrade height: 4,705,316
Swap: `cp /root/go/bin/stocd_v060_fixed /root/go/bin/stocd && systemctl restart stocd`
Telegram monitor already running (cronjob every minute)

## KEY NUMBERS

- Commits: `1944ea5` (M1-M3), `f231310` (L1,L5,L17), `fd64dcd` (tests)
- Mainnet upgrade height: 4,705,316 (~Apr 8 13:00 UTC)
- All tests: 67 tests pass (ante + stoc types + evmutil keeper)

## DECISIONS

- Permissionless token creation = BY DESIGN
- Tax: send 100, tax 5% = 5→creator, 95→recipient — BY DESIGN
- Devnet flow ALWAYS: no-evm → v1.0.0-rc2 → v0.6.0
- Testing: local (ignite serve) for dev + devnet scripts for pre-release
- v3 handler auto-sets MinGasPrice=10^9, NO separate gas proposal needed

## BRANCHES

| Branch | Purpose | Status |
|--------|---------|--------|
| `archive/no-evm` | No-EVM backup | Remote ✓ |
| `archive/feat-evm-v1.0.0-rc2` | v1.0.0-rc2 backup | Remote ✓ |
| **`fix/evm_v0.6.0`** | Current — v0.6.0 + all fixes | Commit `fd64dcd` |

## MONITORING

- Cronjob: `* * * * *` on local Mac
- Script: `scripts/mainnet-upgrade-monitor.sh`
- Lockfile: `/tmp/mainnet-upgrade-notified.lock`
- Telegram: bot 8288157775, channel -1003689918216, Topic 40

## DO NOT RE-READ

All files in app/ante/, app/evm.go, app/upgrades.go, app/app.go, x/evmutil/, x/stoc/
