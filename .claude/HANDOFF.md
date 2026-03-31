# HANDOFF

> 2026-03-31 16:00

## CURRENT REQUEST

1. Audit source code, so sanh voi PR #73, tim CRITICAL/HIGH issues
2. Fix all audit issues
3. Migrate cosmos/evm tu v1.0.0-rc2 sang v0.6.0 (branch rieng)

## PLAN

- [x] Audit song song (4 agents): keeper, ante/types, app/evmutil, IBC bypass vectors
- [x] Fix CRITICAL: BankKeeper SendRestriction block custom token IBC escrow
- [x] Fix MEDIUM: Remove config.toml heuristic in detectBondDenomFromGenesis
- [x] Fix MEDIUM: Default MaxTxGasWanted = 50M
- [x] Fix MEDIUM: Guard CreateToken negative remainder panic
- [x] Commit + push `feat/evm` → `f032b64`
- [x] Create branch `fix/evm-from-v1.0.0-to-v0.6.0` → pushed to remote
- [ ] Migrate cosmos/evm v1.0.0-rc2 → v0.6.0 <- NEXT

## KEY NUMBERS

- 2 CRITICAL bypass vectors fixed (ICA Host, x/group)
- 1 HIGH bypass vector fixed (x/gov)
- 3 MEDIUM issues fixed
- MaxTxGasWanted default: 50,000,000 (50M = half of BlockGasLimit 100M)
- Build: OK, Tests: OK (only pre-existing protobuf issue in stoc/module)

## DECISIONS

- **SendRestriction over middleware**: Ante handler only catches user-submitted txs. ICA/Group/Gov execute via MsgServiceRouter, bypassing ante. BankKeeper.SendRestriction catches ALL paths because IBC escrow always calls BankKeeper.SendCoins(). (app/app.go:blockCustomTokenIBCTransfers)
- **Keep ante handler + add SendRestriction**: Ante handler stays for early rejection + user-facing errors. SendRestriction is the enforcement layer. Defense in depth.
- **Iterate channels per send**: GetAllChannels() called on each custom token send to check escrow addresses. O(channels) per send, typically <100 channels. Acceptable for blockchain.
- **Remove config.toml heuristic**: Substring matching "tstoc"/"testnet" could trigger on moniker/comments → wrong denom on mainnet. Genesis is the only reliable source. (app/app.go:detectBondDenomFromGenesis)
- **cosmos/evm v0.6.0 over v1.0.0-rc2**: User confirmed v1.0.0-rc line was skipped/abandoned. v0.6.0 is latest stable with active bug fixes.

## COSMOS/EVM MIGRATION: v1.0.0-rc2 → v0.6.0

### Branch: `fix/evm-from-v1.0.0-to-v0.6.0` (created from `feat/evm`)

### Version Compatibility Matrix

| Component | Current (v1.0.0-rc2) | Target (v0.6.0) | Your project |
|-----------|---------------------|-----------------|--------------|
| Cosmos SDK | v0.53.0 | **v0.53.6** | v0.53.4 |
| CometBFT | v0.38.17 | v0.38.21 | v0.38.21 |
| IBC-go | v10 | v10.3.1 | v10.2.0 |

### Migration Steps (TODO)

1. **Bump cosmos/evm**: `go.mod` change `github.com/cosmos/evm` from v1.0.0-rc2 → v0.6.0
2. **Bump Cosmos SDK**: v0.53.4 → v0.53.6 (v0.6.0 requires it)
3. **Bump IBC-go**: v10.2.0 → v10.3.1 (v0.6.0 requires it)
4. **Rebase fork patch**: `github.com/MinhAnh-Corp/evm` replace directive — rebase GetBlock panic fix onto v0.6.0
5. **API changes to handle**:
   - StateDB is now a parameter to internal EVM calls (check `app/evm.go` usage)
   - IBC Transfer wrapper removed — users must use precompile directly for ERC20
   - Check if `EVMMempoolConfig` API changed
   - Check if keeper constructor signatures changed
6. **Update upgrade handler**: Add `v4-evm-upgrade` (or similar) with store migrations if needed
7. **Fix compilation errors**: Adapt to any renamed/moved types
8. **Run tests**: `make test-unit`
9. **Test on devnet**: `ignite chain serve --reset-once`

### Known Risks

- **Fork rebase**: `MinhAnh-Corp/evm` patches GetBlock panic. Must rebase onto v0.6.0 tag. Check if fix was upstreamed.
- **Store migration**: If v0.6.0 changed store schemas, need upgrade handler. Check cosmos/evm CHANGELOG.
- **ERC20 IBC middleware**: v0.6.0 removed IBC Transfer wrapper. `app/ibc.go` uses `ibctransferevm.NewIBCModule()` — verify this still exists or find replacement.

### Files to Modify

| File | Reason |
|------|--------|
| `go.mod` | Version bumps |
| `go.sum` | Auto-updated |
| `app/evm.go` | Keeper constructors, StateDB param, mempool config |
| `app/ibc.go` | IBC transfer module integration changes |
| `app/app.go` | Any import path changes |
| `app/ante/evm_handler.go` | EVM ante handler API changes |
| `app/upgrades.go` | New upgrade handler for v0.6.0 migration |
| `x/evmutil/keeper/bank_keeper.go` | If EvmBankKeeper interface changed |

## AUDIT FINDINGS (Session 2026-03-31)

### Fixed This Session

| # | Severity | Issue | Commit |
|---|----------|-------|--------|
| 1 | CRITICAL | ICA Host bypasses ante → custom tokens escape via IBC | `f032b64` |
| 2 | CRITICAL | x/group proposals bypass ante → same vector | `f032b64` |
| 3 | HIGH | x/gov proposals bypass ante → same vector (requires vote) | `f032b64` |
| 4 | MEDIUM | detectBondDenomFromGenesis config.toml heuristic → wrong denom | `f032b64` |
| 5 | MEDIUM | MaxTxGasWanted=0 → single-tx block monopolization | `f032b64` |
| 6 | MEDIUM | CreateToken negative remainder → sdk.NewCoin panic | `f032b64` |

### Not Fixed (LOW / future risk)

| # | Severity | Issue | Status |
|---|----------|-------|--------|
| 7 | HIGH (future) | ICS-20 v2 MsgSendPacket not covered by ante | Will be addressed in v0.6.0 migration |
| 8 | LOW | Validate() missing creator required check | Non-exploitable (MsgCreateToken.ValidateBasic covers it) |
| 9 | LOW | GetSigners() inconsistency across message types | Maintenance hazard only |
| 10 | LOW | EVM mempool no MaxTx limit | Economic barrier (min gas price) mitigates |

### Verified CLEAN Areas

- CEI pattern ✓, Supply invariant ✓, Authz unwrap ✓, EVM isolation ✓
- Precompile blocking ✓, Tax enforcement ✓, Query DoS protection ✓
- Counter overflow ✓, Genesis duplicate check ✓, Nil pointer safety ✓

## FILES MODIFIED (this session)

| File | Status | Notes |
|------|--------|-------|
| `app/app.go` | modified | SendRestriction, detectBondDenom fix, MaxTxGasWanted default |
| `x/stoc/keeper/msg_server_create_token.go` | modified | IsNegative guard for distribution remainder |

## DO NOT RE-READ

- All files from previous audit rounds (see previous HANDOFF section)
- `app/ante/cosmos_handler.go` — verified, ante handler chain correct
- `x/stoc/ante/ibc_restriction.go` — verified, kept as defense-in-depth
- `x/evmutil/keeper/bank_keeper.go` — verified, EVM isolation solid
- `app/evm.go` — verified, precompile blocking correct

## NEXT SESSION LOAD

- `go.mod` — version bumps needed for v0.6.0
- `app/evm.go` — keeper constructors will change
- `app/ibc.go` — IBC transfer wrapper may be removed
- `app/upgrades.go` — new upgrade handler needed
- cosmos/evm v0.6.0 CHANGELOG — check breaking changes

## NEXT ACTIONS

1. Switch to branch `fix/evm-from-v1.0.0-to-v0.6.0`
2. Check if GetBlock panic fix was upstreamed in v0.6.0 (if yes, remove fork)
3. Bump `go.mod`: cosmos/evm v0.6.0, cosmos-sdk v0.53.6, ibc-go v10.3.1
4. Fix compilation errors from API changes
5. Add upgrade handler
6. Test build + unit tests
7. Create PR → `feat/evm`
