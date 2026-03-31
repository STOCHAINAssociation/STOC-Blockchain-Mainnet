# HANDOFF

> 2026-03-31 17:00

## CURRENT REQUEST

Migrate cosmos/evm từ v1.0.0-rc2 (dead-end) sang v0.6.0 (latest stable).
Chain history: no EVM → EVM v1.0.0-rc2 → EVM v0.6.0

## CONTEXT

- Branch: `fix/evm-from-v1.0.0-to-v0.6.0` (created from `feat/evm`, pushed)
- `feat/evm` đã deploy security fixes (`f032b64`) — deploy trên VPS trước
- v1.0.0-rc line đã bị skip/abandoned. Chỉ STOC + 1 chain khác dùng
- v0.6.0 là stable line đang active, được nhiều chain adopt (Warden, BitBadges, Qorechain, KiiChain, XRP EVM...)

## REFERENCE REPOS (clone để tham khảo)

```bash
# 1. cosmos/evm official example app (evmd)
git clone --branch v0.6.0 --depth 1 https://github.com/cosmos/evm.git ~/refs/cosmos-evm-v0.6.0
# Key dir: evmd/app.go, evmd/mempool.go, evmd/upgrades.go

# 2. Warden Protocol — cleanest v0.6.0 integration (no fork)
git clone --depth 1 https://github.com/warden-protocol/wardenprotocol.git ~/refs/wardenprotocol
# Key dir: warden/app/
```

## VERSION COMPATIBILITY

| Component | STOC hiện tại | Target (v0.6.0) | Notes |
|-----------|--------------|-----------------|-------|
| cosmos/evm | v1.0.0-rc2 | **v0.6.0** | Main change |
| Cosmos SDK | v0.53.4 | **v0.53.6** | Minor bump |
| IBC-go | v10.2.0 | **v10.3.1+** | Minor bump |
| CometBFT | v0.38.21 | v0.38.21 | No change |
| go-ethereum | cosmos/go-ethereum | cosmos/go-ethereum **v1.16.2-cosmos-1** | Check replace directive |
| Go | 1.24.x | 1.23.8+ | Compatible |

## FULL MIGRATION PLAN

v1.0.0-rc2 → v0.6.0 không có official migration guide. Phải follow composite path:
v1.0.0-rc2 ≈ pre-v0.4 feature set → cần apply tất cả breaking changes v0.4→v0.5→v0.6.

### Phase 1: Preparation

- [ ] Clone reference repos (cosmos/evm evmd + wardenprotocol)
- [ ] Read cosmos/evm migration docs:
  - `docs/protocol/migration/v0.3-v0.4.md`
  - `docs/protocol/migration/v0.4-v0.5.md`
  - `docs/protocol/migration/v0.5-v0.6.md`
- [ ] Check if GetBlock panic fix (MinhAnh-Corp/evm fork) was upstreamed in v0.6.0
  - If yes → remove fork replace directive
  - If no → rebase patch onto v0.6.0 tag

### Phase 2: Dependency Bumps (`go.mod`)

- [ ] Change `github.com/cosmos/evm` v1.0.0-rc2 → v0.6.0
- [ ] Bump `github.com/cosmos/cosmos-sdk` v0.53.4 → v0.53.6
- [ ] Bump `github.com/cosmos/ibc-go/v10` v10.2.0 → v10.3.1+
- [ ] Update `github.com/cosmos/go-ethereum` replace → v1.16.2-cosmos-1
- [ ] Update MinhAnh-Corp/evm fork replace (rebase or remove)
- [ ] Run `go mod tidy`

### Phase 3: Breaking Changes — `app/evm.go`

Đối chiếu với `evmd/app.go` (v0.6.0 tag) và wardenprotocol.

#### 3a. Import Path Changes (v0.4→v0.5)
- [ ] `github.com/cosmos/evm/types` → split thành:
  - `github.com/cosmos/evm/ante/types`
  - `github.com/cosmos/evm/encoding/address`
  - `github.com/cosmos/evm/utils`
  - `github.com/cosmos/evm/crypto/hd`
- [ ] Update all import paths in `app/`, `cmd/`, `x/evmutil/`

#### 3b. EVM Keeper Constructor Changes
- [ ] Check if `evmkeeper.NewKeeper()` signature changed
- [ ] Check if `feemarketkeeper.NewKeeper()` signature changed
- [ ] Check if `erc20keeper.NewKeeper()` signature changed
- [ ] `StateDB` is now a parameter to `CallEVM`, `CallEVMWithData`, `ApplyMessage`
  - Search codebase for these calls and add StateDB param

#### 3c. Mempool Changes (v0.5)
- [ ] `EVMMempoolConfig` API may have changed — new `cosmosPoolMaxTx` parameter
- [ ] Compare `app/evm.go:setEVMMempool()` with `evmd/mempool.go`

#### 3d. EVM Chain ID (v0.5→v0.6)
- [ ] Chain ID now from `appOpts`, not function parameter
- [ ] Compare `app/evm.go:getEVMChainID()` with evmd pattern

#### 3e. Denom Config Changes (v0.6)
- [ ] `EvmAppOptions` calls may be removed — denom configs moved to state/genesis
- [ ] `EvmAppOptionsWithConfig()` → check if still exists or replaced
- [ ] `InitEvmCoinInfo` must be called in upgrade handler

#### 3f. Precompile Registration
- [ ] `DefaultStaticPrecompiles` now requires `clientKeeper` parameter
- [ ] Compare precompile setup with evmd

### Phase 4: Breaking Changes — `app/ibc.go`

#### 4a. IBC Transfer Keeper (v0.6 CRITICAL)
- [ ] **IBC Transfer wrapper removed** — `cosmos/evm/x/ibc/transfer` may not exist in v0.6.0
- [ ] Check if `ibctransferevm.NewIBCModule()` still exists
- [ ] If removed → switch to standard ibc-go transfer keeper + ERC20 precompile
- [ ] Compare with wardenprotocol's IBC setup

#### 4b. ERC20 IBC Middleware
- [ ] `erc20.NewIBCMiddleware()` — verify still exists
- [ ] Transfer stack may need restructuring

### Phase 5: Breaking Changes — `app/ante/`

- [ ] EVM ante handler imports — check path changes
- [ ] `evmante.HandlerOptions` struct — check if fields changed
- [ ] `cosmosevmante.NewDynamicFeeChecker` — verify exists
- [ ] Compare with evmd ante handler setup

### Phase 6: Breaking Changes — `x/evmutil/`

- [ ] `EvmBankKeeper` — check if cosmos/evm's bank keeper interface changed
- [ ] `evmtypes.EvmCoinInfo` struct — verify fields
- [ ] `evmutiltypes.GetEvmDenom()` — verify still works with new config approach

### Phase 7: Upgrade Handler

- [ ] Create `v4-evm-v060` upgrade in `app/upgrades.go`
- [ ] Call `InitEvmCoinInfo` in upgrade handler (required by v0.6.0)
- [ ] Add store migrations if cosmos/evm v0.6.0 changed store schemas
- [ ] Check evmd/upgrades.go for reference

### Phase 8: Build & Test

- [ ] `go build ./...` — fix all compilation errors
- [ ] `make test-unit` — all tests pass
- [ ] `ignite chain serve --reset-once` — devnet starts OK
- [ ] Test EVM functionality: deploy contract, send ETH tx
- [ ] Test custom token: create, transfer, tax works
- [ ] Test IBC restriction: custom tokens blocked from IBC
- [ ] Test denomination conversion: ustoc ↔ astoc

### Phase 9: Deploy

- [ ] Deploy on testnet first
- [ ] Submit upgrade proposal with correct height
- [ ] Validators upgrade binary at proposal height
- [ ] Verify post-upgrade: EVM, custom tokens, IBC all work

## KEY BREAKING CHANGES SUMMARY

| Change | Source | Impact |
|--------|--------|--------|
| Import paths split | v0.4→v0.5 | All EVM imports in app/, cmd/ |
| StateDB param added | v0.5→v0.6 | Any direct EVM call |
| IBC Transfer wrapper removed | v0.6 | app/ibc.go completely |
| Denom config → state/genesis | v0.6 | app/evm.go, upgrade handler |
| DefaultStaticPrecompiles needs clientKeeper | v0.6 | app/evm.go precompile setup |
| InitEvmCoinInfo in upgrade | v0.6 | app/upgrades.go |
| Mempool config changes | v0.5 | app/evm.go mempool setup |

## FILES TO MODIFY

| File | Changes | Reference |
|------|---------|-----------|
| `go.mod` | Version bumps | — |
| `app/evm.go` | Keeper constructors, precompiles, mempool, denom config | `evmd/app.go` |
| `app/ibc.go` | IBC transfer keeper, middleware stack | wardenprotocol |
| `app/app.go` | Import paths, any wiring changes | `evmd/app.go` |
| `app/ante/ante.go` | StocAnteOptions fields | `evmd/app.go` ante section |
| `app/ante/evm_handler.go` | EVM ante imports | evmd |
| `app/ante/cosmos_handler.go` | Cosmos ante imports | evmd |
| `app/upgrades.go` | New v4 upgrade handler | `evmd/upgrades.go` |
| `app/config.go` | EVM config changes | evmd |
| `cmd/stocd/cmd/root.go` | EVM server/key imports | evmd/cmd |
| `cmd/stocd/cmd/commands.go` | Server command changes | evmd/cmd |
| `x/evmutil/keeper/bank_keeper.go` | If EVM bank interface changed | — |
| `x/evmutil/types/keys.go` | If denom derivation changed | — |

## PREVIOUS SESSION AUDIT (for context)

Security fixes already deployed on `feat/evm` (`f032b64`):
- CRITICAL: BankKeeper SendRestriction blocks ICA/Group/Gov IBC bypass
- MEDIUM: detectBondDenom heuristic removed
- MEDIUM: MaxTxGasWanted default 50M
- MEDIUM: CreateToken negative remainder guard

## DO NOT RE-READ

- `x/stoc/` module — fully audited, no changes needed for migration
- `x/stoc/ante/` — tax + IBC restriction verified, defense-in-depth
- `app/export.go` — no EVM migration impact
- `documents/` — docs only

## NEXT ACTIONS

1. `git checkout fix/evm-from-v1.0.0-to-v0.6.0`
2. Clone reference repos (see REFERENCE REPOS section)
3. Start Phase 1: read migration docs from cosmos/evm
4. Start Phase 2: bump go.mod dependencies
5. Work through Phase 3-7 systematically, comparing with evmd + wardenprotocol
6. Phase 8: build + test
