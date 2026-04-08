# HANDOFF — E2E Test Suite + Mainnet Upgrade

> Generated: 2026-04-08 03:30 UTC | Branch: fix/evm_v0.6.0 | Commit: 966962f

## CRITICAL — Mainnet v0.6.0 Upgrade Today

**Proposal #4**: 3/3 YES, voting ends ~03:57 UTC (10:57 ICT)
**Upgrade height**: 4,705,316 (~17:30-20:00 ICT)
**Binary `stocd_v060` already on all 4 mainnet nodes** at `/root/go/bin/stocd_v060`

### Swap instructions:
```bash
systemctl stop stocd
cp /root/go/bin/stocd_v060 /root/go/bin/stocd
chmod +x /root/go/bin/stocd
systemctl start stocd
```
Mainnet IPs: Val1=64.176.4.207, Val2=202.182.110.150, Val3=45.32.180.48, Fullblocks=160.191.50.204

---

## Current Task: Fix E2E Test Suite (41 remaining failures)

### Root Cause: Token counter is GLOBAL

After `create_token`, minimal_denom = `SYMBOL_N` where N is global counter (not per-symbol).
Tests hardcode `BETA_0` but actual denom is `BETA_1`. Fix: query actual denom after creation.

Add helper to `chain_helpers.sh`:
```bash
get_last_token_denom() {
    local symbol="$1"
    query_tokens_by_symbol "$symbol" | jq -r '.tokens[-1].minimal_denom'
}
```

### Other Fixes Needed
- Gas enforcement: replace `--gas auto` with fixed gas in CUSTOM_GAS_FLAGS
- EVM tests: check JSON-RPC config in ignite serve
- Distribution: verify repeated flag vs JSON array format

### Test Score: 92/139 pass (66%) — 6 skipped

### Files to Fix
- `lib/chain_helpers.sh` — add get_last_token_denom()
- `suites/01_token_lifecycle.sh` — dynamic minimal_denom
- `suites/02_tax_system.sh` — dynamic minimal_denom
- `suites/05_gas_enforcement.sh` — fix --gas auto
- `suites/07_edge_cases.sh` — dynamic counter

---

## Completed Today

| Task | Status |
|------|--------|
| E2E test suite (141 tests, 7 suites) | Committed 966962f |
| Security audit (3 agents) | 0 CRITICAL runtime |
| C-1 fix (IBC cache threshold) | Committed 81f5c91 |
| PM2 migration mainnet (180.93.43.89) | Done: 91G→38G |
| PM2 migration testnet (157.66.219.214) | Done: 39G→30G |
| Devnet genesis re-init | Done: v0.6.0, producing blocks |
| ecosystem.config.js | Created in stoc-backend-sync-chain |

## DO NOT RE-READ
- app/ante/* (audited, no changes needed)
- x/stoc/ante/* (audited)
- x/evmutil/keeper/bank_keeper.go (audited)
- app/upgrades.go (reviewed, C-1 fixed)

## Next Actions
1. Fix E2E test suite (global counter bug + 3 minor fixes)
2. Re-run local + devnet
3. Monitor mainnet upgrade (~17:30-20:00 ICT)
4. After mainnet v0.6.0: submit gas price governance proposal
