package app

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	evmutiltypes "stoc/x/evmutil/types"
)

// Upgrade history (chronological). Keeping every handler in source is NECESSARY
// but NOT SUFFICIENT for a new node to reach the tip:
//   - NECESSARY: a node that has ALREADY applied an upgrade must still carry that
//     handler or x/upgrade ApplyUpgrade panics "wrong app version, upgrade handler
//     is missing" (cosmossdk.io/x/upgrade abci.go).
//   - NOT SUFFICIENT: a SINGLE binary carrying ALL handlers CANNOT cold-sync from
//     genesis. During every window [gov-prop-exec-height, upgrade-trigger-height)
//     the x/upgrade PreBlocker sees the plan scheduled-but-not-triggered while the
//     handler is present and panics "BINARY UPDATED BEFORE TRIGGER".
//     Cold-sync of post-upgrade history therefore requires a phased-binary
//     lineage (one binary per upgrade phase, each carrying handlers only up to
//     its own phase, e.g. managed by cosmovisor) or state-sync.
//
//   v2-evm                   — mainnet block 4455467: introduce EVM
//                              (cosmos/evm v0.6.0). Sets MinGasPrice=0.
//   v3-fix-evm-denom         — mainnet block 4705316: fix EVM denom
//                              ("atest" → derived from bond_denom). Sets
//                              MinGasPrice=10^9 (interpreted on the
//                              18-decimal EVM scale).
//   v5.0.0                   — consolidated post-v3 fixes delivered as a
//                              single upgrade: feemarket EIP-1559 enable,
//                              EVM dust round-up, MaxPendingTxPerWallet cap,
//                              cosmos/evm fork mempool fixes, and the
//                              PrepareProposal cascade-skip. See the
//                              UpgradeNameV5 block below for the full list
//                              of source/param changes.
const (
	UpgradeName            = "v2-evm"
	UpgradeNameFixEVMDenom = "v3-fix-evm-denom"
	// UpgradeNameV5 consolidates all post-v3 fixes into a single upgrade
	// (one gov proposal + binary swap). It delivers:
	//
	// =========================================================================
	// ISSUES FIXED BY v5.0.0 (explicit list):
	// =========================================================================
	//
	//   1. Feemarket disabled / EVM wallet double-signing UX
	//     Symptom: EVM wallets open the signing prompt twice; first submit fails
	//              with "Gas estimation failed: insufficient funds for intrinsic
	//              transaction cost"; user must send the transaction twice.
	//     Root cause: feemarket NoBaseFee=true → eth_feeHistory returns mixed
	//                 0/non-zero baseFeePerGas → wallet EIP-1559 estimator
	//                 underestimates fee → first submit gets rejected by
	//                 validator min-gas-price → wallet auto-retries via gasPrice
	//                 fallback → user sees a second prompt.
	//     Fix: enable EIP-1559 base_fee with valid params (see params block below).
	//
	//   2. wei→ustoc dust precision mismatch
	//     Symptom: EVM tx with non-multiple-of-10^12-wei amount rejected with
	//              "amount X astoc has dust remainder Y wei"; "deduct full gas
	//              cost" error on contract deploy.
	//     Root cause: Cosmos minimum unit = 1 ustoc (10^12 wei) but EVM
	//                 gasPrice operates at 1 gwei (10^9 wei) granularity.
	//                 gasUsed × gasPrice rarely a multiple of 10^12.
	//     Fix: x/evmutil/keeper/bank_keeper.go convertAndValidateCoins rounds
	//          UP wei → ustoc (sender overpays by ≤ 1 ustoc, negligible).
	//
	//   3. High-volume sender mempool flood
	//     Symptom: a single wallet submitting many pending EVM transactions can
	//              fill the mempool head and starve other users' transactions
	//              from inclusion.
	//     Root cause: no per-wallet cap on pending EVM mempool txs.
	//     Fix: app/ante/max_pending_tx.go MaxPendingTxPerWalletDecorator cap 50
	//          (configurable). Plus nil-check on pool.GetTxPool() to prevent
	//          startup-race panic during CheckTx.
	//
	//   4. cosmos/evm mempool sum-queued-cost balance check
	//     Symptom: tx rejected even though sender has enough balance for
	//              individual tx, because sum of all pending tx costs exceeds.
	//     Root cause: upstream geth-style aggregate balance check unsuited to
	//                 Cosmos block-by-block balance semantics.
	//     Fix: forks/cosmos-evm-v0.6.0/mempool/txpool/validation.go switches to
	//          per-tx balance check (skip cumulative sum-queued cost).
	//
	//   5. PrepareProposal cascade-skip
	//     Symptom: after EVM ante fails for sender A's tx, block builder
	//              iterates ALL of A's subsequent pending txs (also fail with
	//              nonce-too-high cascade), wasting block-building time.
	//     Root cause: upstream cosmos/evm v0.6.0 only exposed PopCurrentAccount
	//                 helper on EVMMempoolIterator — no caller invokes it.
	//     Fix: app/abci_proposal.go STOCProposalHandler wraps DefaultProposal
	//          handler, calls iter.PopCurrentAccount() when EVM ante fails.
	//          Cascade-skip activates at runtime.
	//
	//   6. eth_getTransactionCount pending nonce mismatch
	//     Symptom: wallets get a stale nonce from the chain (does not include
	//              pending EVM mempool entries), submit tx with wrong nonce
	//              → rejected.
	//     Root cause: upstream cosmos/evm v0.6.0 eth_getTransactionCount
	//                 (pending) only consulted CometBFT mempool, missed EVM
	//                 legacypool queued/pending buckets.
	//     Fix: forks/cosmos-evm-v0.6.0/rpc/backend/account_info.go pending
	//          nonce = max(chainNonce, txpool.PoolNonce(addr)).
	//
	//   7. Legacypool (L2) stuck pending eviction
	//     Symptom: a wallet whose balance is depleted by other operations
	//              accumulates pending transactions (cumulative cost > balance)
	//              that never evict from the legacy pool, allowing the pool to
	//              grow unbounded over time.
	//     Root cause: upstream go-ethereum legacypool eviction loop only
	//                 scanned pool.queue bucket, not pool.pending. Assumption
	//                 (pending = always-mineable) breaks for Cosmos-EVM where
	//                 a permanently-unminable pending tx is possible.
	//     Fix: forks/cosmos-evm-v0.6.0/mempool/txpool/legacypool/legacypool.go
	//          extend evict loop to also scan pool.pending and drop entries
	//          where beats[addr] idle > Lifetime. Lifetime = 1h (matches
	//          CometBFT ttl-duration for consistency).
	//
	//   8. CometBFT mempool (L1) orphan eviction
	//     Symptom: a transaction may live in the CometBFT mempool while the
	//              legacy pool has already evicted it (cumulative-balance reject,
	//              replacement, pool lifetime). Single-tx ante validation passes
	//              on Recheck so the CometBFT mempool retains the entry; the
	//              proposer skips it every block because L2's view disagrees.
	//              Without explicit cross-layer signalling, the CometBFT mempool
	//              grows unbounded.
	//     Root cause: no propagation from legacypool removal → CometBFT
	//                 mempool. CheckTx wrapper had no orphan detection.
	//     Fix: forks/cosmos-evm-v0.6.0/mempool/check_tx.go on RecheckTx flag,
	//          query legacypool.Has(hash) — if false, return ErrInvalidRequest
	//          so CometBFT drops from its mempool. Companion helper
	//          ExperimentalEVMMempool.IsOrphanEVMTx() exposes lookup.
	//
	// =========================================================================
	// State changes applied by v5.0.0 handler:
	// =========================================================================
	//
	//   Gov-visible params:
	//     - feemarket.NoBaseFee   = false   (enable EIP-1559)
	//     - feemarket.BaseFee     = 0.001   (Cosmos scale: 0.001 ustoc/gas = 1 gwei effective)
	//     - feemarket.MinGasPrice = 0.001   (same — feemarket floor for mempool admission)
	//
	//     FeeChecker (in NewDeductFeeDecorator) compares feeCap vs BaseFee in
	//     Cosmos scale directly; 0.001 enforces 1 gwei. The custom
	//     CosmosMinGasPriceDecorator's .Quo(10^12) is effectively a no-op at
	//     this value (it silently passes; FeeChecker enforces the correct
	//     value afterward).
	//
	//   Source-only changes that ride along with the binary (no params needed):
	//     - x/evmutil/keeper/bank_keeper.go     — item 2 wei→ustoc round-up
	//     - app/ante/max_pending_tx.go          — item 3 MaxPendingTxPerWallet
	//                                              + nil-check defense
	//     - app/abci_proposal.go                — item 5 cascade-skip
	//                                              wire (STOCProposalHandler)
	//     - forks/cosmos-evm-v0.6.0/...         — items 4, 5 (helper), 6, 7, 8
	//
	// Idempotent: re-running the handler is safe — params are deterministic
	// re-assertions and the source-only changes activate purely on binary load.
	//
	// Intended to be applied as a single gov proposal on top of
	// v3-fix-evm-denom state (1 halt + 1 binary swap).
	UpgradeNameV5 = "v5.0.0"
)

// RegisterUpgradeHandlers registers the upgrade handlers for the app.
func (app *App) RegisterUpgradeHandlers() {
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeName,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			// Run module migrations first (initializes EVM, feemarket, erc20, evmutil stores)
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			// Wrap discretionary post-migration writes in
			// CacheContext so SetParams + setEvmDenomFromStaking commit atomically.
			// RunMigrations stays outside (idempotent per SDK contract — re-runs
			// against new fromVM skip already-migrated modules).
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			cacheCtx, writeCache := sdkCtx.CacheContext()

			// Set feemarket params: disable dynamic base fee when EVM is introduced so
			// existing gas price configs (0.01 ustoc) in wallets/clients remain valid.
			// It can be re-enabled later via an upgrade or governance proposal.
			//
			// Read existing params and apply targeted overrides so any future governance
			// changes to unrelated fields are preserved across handler re-runs.
			feemarketParams := app.FeeMarketKeeper.GetParams(cacheCtx)
			// Mirror the v5.0.0 defensive checks — abort if upstream migration left a
			// divide-by-zero invariant rather than masking it with our overwrites.
			if feemarketParams.BaseFeeChangeDenominator == 0 {
				return vm, fmt.Errorf("v2-evm upgrade aborted: post-migration feemarket BaseFeeChangeDenominator=0 (divide-by-zero); restore from snapshot")
			}
			if feemarketParams.ElasticityMultiplier == 0 {
				return vm, fmt.Errorf("v2-evm upgrade aborted: post-migration feemarket ElasticityMultiplier=0 (freezes base-fee); restore from snapshot")
			}
			feemarketParams.NoBaseFee = true
			feemarketParams.BaseFee = math.LegacyZeroDec()
			feemarketParams.MinGasPrice = math.LegacyZeroDec()
			// Full Validate() catches
			// any other internal invariant the cherry-picked checks above
			// don't cover (MinBaseFee bounds, gas-target relationships).
			if err := feemarketParams.Validate(); err != nil {
				return vm, fmt.Errorf("v2-evm upgrade aborted: post-migration feemarket params invalid: %w", err)
			}
			if err := app.FeeMarketKeeper.SetParams(cacheCtx, feemarketParams); err != nil {
				return vm, fmt.Errorf("failed to set feemarket params: %w", err)
			}

			// Fix EVM denom: sdk.DefaultBondDenom may not be set during upgrade init,
			// causing default "atest" instead of correct denom.
			// Derive from staking bond_denom (already loaded from genesis).
			if err := setEvmDenomFromStaking(app, cacheCtx); err != nil {
				return vm, err
			}

			writeCache()
			return vm, nil
		},
	)

	// v3-fix-evm-denom: fix EVM denom and MinGasPrice
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameFixEVMDenom,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			// Wrap discretionary post-migration writes
			// (setEvmDenomFromStaking + SetParams) in CacheContext.
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			cacheCtx, writeCache := sdkCtx.CacheContext()

			if err := setEvmDenomFromStaking(app, cacheCtx); err != nil {
				return vm, err
			}

			// Fix feemarket MinGasPrice: v2-evm set it to 0 allowing free EVM tx spam.
			// Set 10^9 astoc/gas = 0.001 ustoc/gas = 1 gwei, matching Cosmos min-gas-prices.
			//
			// Read existing params and apply targeted overrides so any future governance
			// changes to unrelated fields are preserved across handler re-runs.
			feemarketParams := app.FeeMarketKeeper.GetParams(cacheCtx)
			// Mirror the v5.0.0 defensive checks — abort
			// if upstream migration left a divide-by-zero invariant.
			if feemarketParams.BaseFeeChangeDenominator == 0 {
				return vm, fmt.Errorf("v3-fix-evm-denom upgrade aborted: post-migration feemarket BaseFeeChangeDenominator=0 (divide-by-zero); restore from snapshot")
			}
			if feemarketParams.ElasticityMultiplier == 0 {
				return vm, fmt.Errorf("v3-fix-evm-denom upgrade aborted: post-migration feemarket ElasticityMultiplier=0 (freezes base-fee); restore from snapshot")
			}
			feemarketParams.NoBaseFee = true
			feemarketParams.BaseFee = math.LegacyZeroDec()
			feemarketParams.MinGasPrice = math.LegacyNewDec(1_000_000_000)
			// Full Validate() defense-in-depth.
			if err := feemarketParams.Validate(); err != nil {
				return vm, fmt.Errorf("v3-fix-evm-denom upgrade aborted: post-migration feemarket params invalid: %w", err)
			}
			if err := app.FeeMarketKeeper.SetParams(cacheCtx, feemarketParams); err != nil {
				return vm, fmt.Errorf("failed to set feemarket MinGasPrice: %w", err)
			}

			writeCache()
			return vm, nil
		},
	)

	// v5.0.0: consolidated post-v3 upgrade. See UpgradeNameV5 const block
	// above for the full list of source/param changes. Handler only writes the
	// feemarket params here — source-only changes (dust round-up,
	// MaxPendingTxPerWallet, cascade-skip wire, fork mempool fixes) activate at
	// binary load and need no migration call.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameV5,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			// Wrap discretionary post-migration writes
			// in CacheContext. Only 1 write here but consistent pattern across handlers.
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			cacheCtx, writeCache := sdkCtx.CacheContext()

			// Defense-in-depth
			// re-read of feemarket params before we SetParams. If an
			// upstream migration inside RunMigrations bumps the schema and
			// happens to reset a field we care about (BaseFeeChangeDenominator,
			// ElasticityMultiplier) to a known-broken zero value, our
			// overwrites below would mask the breakage and ship a chain
			// with a silently-zeroed knob. Read first, validate the
			// non-overwritten fields, then patch only the ones this handler
			// owns. If the read itself fails or returns a struct that fails
			// internal validation, halt the upgrade loudly so operators
			// recover from snapshot instead of booting a half-upgraded chain.
			params := app.FeeMarketKeeper.GetParams(cacheCtx)
			if params.BaseFeeChangeDenominator == 0 {
				return vm, fmt.Errorf("v5.0.0 upgrade aborted: post-migration feemarket params have BaseFeeChangeDenominator=0 (would divide-by-zero at next base-fee update); upstream migration is corrupt — restore from snapshot")
			}
			if params.ElasticityMultiplier == 0 {
				return vm, fmt.Errorf("v5.0.0 upgrade aborted: post-migration feemarket params have ElasticityMultiplier=0 (would freeze base-fee adjustment); upstream migration is corrupt — restore from snapshot")
			}

			// BaseFee / MinGasPrice are expressed in Cosmos scale.
			// The fee-enforcement authority is `evmante.FeeChecker` (wired via
			// `NewDeductFeeDecorator`'s txFeeChecker) — see
			// forks/cosmos-evm-v0.6.0/ante/evm/fee_checker.go:99 `feeCap.LT(baseFee)`.
			// FeeChecker uses BaseFee in COSMOS SCALE (ustoc/gas) DIRECTLY.
			// Therefore 0.001 raw Dec = 0.001 ustoc/gas = 1 gwei effective floor.
			// The custom `CosmosMinGasPriceDecorator.Quo(10^12)` is a no-op
			// in practice — it runs before FeeChecker but its `.Quo` silent-passes;
			// FeeChecker catches afterward at the right value. E.g. a tx with 1 ustoc
			// fee + 200k gas is rejected with "got: 0.000005 ustoc/gas required:
			// 0.001 ustoc/gas".
			params.NoBaseFee = false
			params.BaseFee = math.LegacyNewDecWithPrec(1, 3)     // 0.001 ustoc/gas = 1 gwei effective
			params.MinGasPrice = math.LegacyNewDecWithPrec(1, 3) // same — feemarket floor
			// Full Validate() defense-in-depth.
			if err := params.Validate(); err != nil {
				return vm, fmt.Errorf("v5.0.0 upgrade aborted: post-migration feemarket params invalid: %w", err)
			}
			if err := app.FeeMarketKeeper.SetParams(cacheCtx, params); err != nil {
				return vm, fmt.Errorf("failed to apply v5.0.0 feemarket params: %w", err)
			}

			writeCache()
			return vm, nil
		},
	)

	upgradeInfo, err := app.UpgradeKeeper.ReadUpgradeInfoFromDisk()
	if err != nil {
		panic(fmt.Sprintf("failed to read upgrade info from disk: %s", err))
	}

	if upgradeInfo.Name == UpgradeName && !app.UpgradeKeeper.IsSkipHeight(upgradeInfo.Height) {
		storeUpgrades := storetypes.StoreUpgrades{
			Added: []string{
				evmtypes.ModuleName,
				feemarkettypes.ModuleName,
				erc20types.ModuleName,
				evmutiltypes.ModuleName,
			},
		}

		// Configure store loader that checks if version == upgradeHeight and applies store upgrades
		app.SetStoreLoader(upgradetypes.UpgradeStoreLoader(upgradeInfo.Height, &storeUpgrades))
	}

	// v3-fix-evm-denom + v5.0.0: no new stores needed, only param updates.
}

// setEvmDenomFromStaking derives and sets EVM denom config from staking bond_denom.
// For 6-decimal chains: evm_denom=ustoc (Cosmos), extended_denom=astoc (18-dec EVM).
// Also sets bank denom_metadata and initializes EVM coin info in KV store.
func setEvmDenomFromStaking(app *App, sdkCtx sdk.Context) error {
	if app.StakingKeeper == nil {
		return fmt.Errorf("staking keeper not initialized during upgrade")
	}
	if app.EVMKeeper == nil {
		return fmt.Errorf("evm keeper not initialized during upgrade")
	}

	stakingParams, err := app.StakingKeeper.GetParams(sdkCtx)
	if err != nil {
		return fmt.Errorf("failed to get staking params: %w", err)
	}

	bondDenom := stakingParams.BondDenom
	if len(bondDenom) < 2 || bondDenom[0] != 'u' {
		return fmt.Errorf("invalid bond_denom %q: must start with 'u' (e.g. 'ustoc', 'utstoc')", bondDenom)
	}
	extendedDenom := "a" + bondDenom[1:] // "ustoc" → "astoc", "utstoc" → "atstoc"
	displayDenom := bondDenom[1:]        // "ustoc" → "stoc", "utstoc" → "tstoc"

	// Set EVM params: evm_denom = Cosmos base denom, extended_denom = 18-decimal EVM denom
	evmParams := app.EVMKeeper.GetParams(sdkCtx)
	evmParams.EvmDenom = bondDenom
	evmParams.ExtendedDenomOptions = &evmtypes.ExtendedDenomOptions{
		ExtendedDenom: extendedDenom,
	}
	if err := app.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
		return fmt.Errorf("failed to set evm params: %w", err)
	}

	// Set bank denom_metadata — required for InitEvmCoinInfo to load decimals + display denom.
	//
	// READ the existing metadata first, mutate only the fields this handler
	// actually owns (Base/Display/DenomUnits/Name/Symbol), and write back.
	// SetDenomMetaData blanket-overwrites the bank store entry, so writing a
	// fresh Metadata literal would silently drop governance-curated metadata
	// (description, logo URI, sha256 URI hash) that explorers and wallet UIs
	// consume, every time this handler runs (v2-evm, v3-fix-evm-denom, v5.0.0,
	// and any cold-sync replay through them). Description / URI / URIHash
	// survive untouched. If no metadata existed previously we write a fresh
	// zero-value record for a brand-new chain.
	existing, foundMeta := app.BankKeeper.GetDenomMetaData(sdkCtx, bondDenom)
	existing.Base = bondDenom
	existing.Display = displayDenom
	if !foundMeta {
		// Only write Name/Symbol/DenomUnits on first-time init. Overwriting them
		// unconditionally on every replay of this upgrade height would silently
		// revert any subsequent governance MsgSetDenomMetadata (e.g. adding an
		// "mstoc" DenomUnit, renaming Symbol) whenever a new validator
		// cold-synced from genesis through this upgrade height — producing
		// app-hash divergence vs live-synced nodes on the next block that read
		// metadata-derived state. On replay, leave Name/Symbol/DenomUnits alone
		// and let governance own them.
		existing.DenomUnits = []*banktypes.DenomUnit{
			{Denom: bondDenom, Exponent: 0},
			{Denom: displayDenom, Exponent: 6},
		}
		existing.Name = strings.ToUpper(displayDenom)
		existing.Symbol = strings.ToUpper(displayDenom)
	}
	app.BankKeeper.SetDenomMetaData(sdkCtx, existing)

	// Initialize EVM coin info from bank metadata + params → stores in KV store
	if err := app.EVMKeeper.InitEvmCoinInfo(sdkCtx); err != nil {
		return fmt.Errorf("failed to init evm coin info: %w", err)
	}

	return nil
}
