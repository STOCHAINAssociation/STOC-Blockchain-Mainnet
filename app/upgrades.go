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

// Upgrade history (chronological — handlers below MUST stay in source for
// new nodes to sync correctly from genesis):
//
//   v2-evm                   — mainnet block 4455467 (Mar 2026): introduce EVM
//                              (cosmos/evm rc2 → v0.6.0). Sets MinGasPrice=0.
//   v3-fix-evm-denom         — mainnet block 4705316 (Apr 2026): fix EVM denom
//                              ("atest" → derived from bond_denom). Sets
//                              MinGasPrice=10^9 (assumed 18-decimal scale, only
//                              correct when paired with module-cache sed patch
//                              that bypasses cosmos/evm v0.6.0 wrapper ×10^12).
//   v4-fix-feemarket-source  — TBD: deprecate the sed patch dependency.
//                              Overwrites stored MinGasPrice 10^9 → 0.001 in
//                              BASE denom (udstoc). Vanilla v0.6.0 wrapper then
//                              applies ×10^12 on read, yielding 10^9 adstoc/gas
//                              = 1 gwei for eth_gasPrice. Pairs with vanilla
//                              binary (built without scripts/patch-evm.sh).
//   v5-enable-base-fee       — TBD: enable EIP-1559 dynamic base fee.
//                              Sets NoBaseFee=false + BaseFee=0.001 in BASE
//                              denom. cosmos/evm v0.6.0 sends baseFee to
//                              fee_collector → validators (NOT burned). Fixes
//                              MetaMask first-click "Gas estimation failed"
//                              caused by inconsistent eth_feeHistory baseFeePerGas
//                              under NoBaseFee=true. baseFee has built-in floor
//                              at MinGasPrice (CalcGasBaseFee returns MaxDec)
//                              so MetaMask UX stable even when chain idle.
const (
	UpgradeName                   = "v2-evm"
	UpgradeNameFixEVMDenom        = "v3-fix-evm-denom"
	UpgradeNameFixFeemarketSource = "v4-fix-feemarket-source"
	UpgradeNameEnableBaseFee      = "v5-enable-base-fee"
	UpgradeNameFixDustRoundUp     = "v6-fix-dust-roundup"
	// v7-combined-fixes bundles the v5+v6 source patches plus the new
	// MaxPendingTxPerWalletDecorator (no on-chain params for the ante itself —
	// the decorator activates as soon as the binary is loaded). Single gov
	// proposal on devnet flips on:
	//   - EIP-1559 base_fee (NoBaseFee=false, BaseFee=0.001 udstoc/gas)
	//   - Min gas price 0.001 udstoc/gas (unchanged from v4, re-asserted for safety)
	//   - Dust round-up (purely source — see x/evmutil/keeper/bank_keeper.go)
	//   - Per-wallet pending-tx cap of 50 (source — app/ante/max_pending_tx.go)
	UpgradeNameCombinedV7 = "v7-combined-fixes"
	// v8-eth-mempool ships Ethereum-mempool-semantics fixes for cosmos/evm v0.6.0
	// via a vendored fork at stoc-current-dev/forks/cosmos-evm-v0.6.0/.
	// No on-chain params change — handler is a pure binary swap marker.
	//   - Bug A: per-tx balance check (was sum-queued-cost reject) —
	//            forks/cosmos-evm-v0.6.0/mempool/txpool/validation.go
	//   - Bug B: iterator PopCurrentAccount helper for PrepareProposal skip-
	//            and-mine — forks/cosmos-evm-v0.6.0/mempool/iterator.go
	//            (helper exposed; tighter cascade-skip wrapper is future work)
	//   - Bug C: eth_getTransactionCount(pending) reflects EVM pool nonce —
	//            forks/cosmos-evm-v0.6.0/rpc/backend/account_info.go
	UpgradeNameEthMempool = "v8-eth-mempool"
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

			// Set feemarket params: disable dynamic base fee for initial EVM deployment.
			// This ensures existing gas price config (0.01 ustoc) in wallets/FE remains valid.
			// Can be re-enabled later via governance proposal once ecosystem is ready.
			sdkCtx := sdk.UnwrapSDKContext(ctx)
			feemarketParams := feemarkettypes.DefaultParams()
			feemarketParams.NoBaseFee = true
			feemarketParams.BaseFee = math.LegacyZeroDec()
			feemarketParams.MinGasPrice = math.LegacyZeroDec()
			if err := app.FeeMarketKeeper.SetParams(sdkCtx, feemarketParams); err != nil {
				return vm, fmt.Errorf("failed to set feemarket params: %w", err)
			}

			// Fix EVM denom: sdk.DefaultBondDenom may not be set during upgrade init,
			// causing default "atest" instead of correct denom.
			// Derive from staking bond_denom (already loaded from genesis).
			if err := setEvmDenomFromStaking(app, sdkCtx); err != nil {
				return vm, err
			}

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

			sdkCtx := sdk.UnwrapSDKContext(ctx)
			if err := setEvmDenomFromStaking(app, sdkCtx); err != nil {
				return vm, err
			}

			// Fix feemarket MinGasPrice: v2-evm set it to 0 allowing free EVM tx spam.
			// Set 10^9 astoc/gas = 0.001 ustoc/gas = 1 gwei, matching Cosmos min-gas-prices.
			feemarketParams := feemarkettypes.DefaultParams()
			feemarketParams.NoBaseFee = true
			feemarketParams.BaseFee = math.LegacyZeroDec()
			feemarketParams.MinGasPrice = math.LegacyNewDec(1_000_000_000)
			if err := app.FeeMarketKeeper.SetParams(sdkCtx, feemarketParams); err != nil {
				return vm, fmt.Errorf("failed to set feemarket MinGasPrice: %w", err)
			}

			return vm, nil
		},
	)

	// v4-fix-feemarket-source: overwrite stored MinGasPrice from 10^9 (legacy patched-binary scale)
	// to 0.001 in BASE denom (udstoc). Vanilla cosmos/evm v0.6.0 FeeMarketWrapper applies
	// ConvertAmountTo18DecimalsLegacy (×10^12) on read, yielding 10^9 adstoc/gas = 1 gwei.
	// Deprecates the build-time scripts/patch-evm.sh sed-patch dependency.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameFixFeemarketSource,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			sdkCtx := sdk.UnwrapSDKContext(ctx)
			params := app.FeeMarketKeeper.GetParams(sdkCtx)
			params.MinGasPrice = math.LegacyNewDecWithPrec(1, 3)
			if err := app.FeeMarketKeeper.SetParams(sdkCtx, params); err != nil {
				return vm, fmt.Errorf("failed to overwrite feemarket MinGasPrice: %w", err)
			}

			return vm, nil
		},
	)

	// v6-fix-dust-roundup: fix EVM gas/fee precision mismatch causing MetaMask "deduct full gas cost"
	// error (e.g., "amount 1079383000000000 adstoc has dust remainder 383000000000 wei").
	// Cosmos minimum unit = 1 udstoc (10^12 wei) but EVM gasPrice operates at 1 gwei (10^9 wei) granularity.
	// gasUsed × gasPrice in wei is rarely a multiple of 10^12 → previously rejected → MetaMask click-twice.
	// v6 patches x/evmutil/keeper/bank_keeper.go convertAndValidateCoins to round UP wei → udstoc instead
	// of rejecting. Sender overpays by at most 1 udstoc (1e-6 dstoc) per coin, negligible. Result:
	// 1-click MetaMask contract deploy works. Pure source patch, no params change required.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameFixDustRoundUp,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			return app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
		},
	)

	// v5-enable-base-fee: enable EIP-1559 dynamic base fee for proper MetaMask UX.
	// Under NoBaseFee=true (v2/v3/v4 era), eth_feeHistory returns mixed 0/non-zero
	// baseFeePerGas, causing MetaMask EIP-1559 estimator to underestimate fees on
	// first deploy attempt ("Gas estimation failed: insufficient funds for intrinsic
	// transaction cost"). User must click Send Transaction twice for MetaMask to
	// re-estimate via eth_gasPrice fallback. Enabling EIP-1559 base_fee fixes this:
	//   - eth_feeHistory.baseFeePerGas becomes consistent (always >= MinGasPrice floor)
	//   - MetaMask estimation works first try
	//   - cosmos/evm v0.6.0 sends baseFee + tip to fee_collector → validators
	//     (NOT burned, validators receive baseFee revenue)
	//   - Floor enforced by CalcGasBaseFee returning MaxDec(baseFee.Sub(num), minGasPrice)
	//     so baseFee never drops below MinGasPrice even when chain idle.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameEnableBaseFee,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			sdkCtx := sdk.UnwrapSDKContext(ctx)
			params := app.FeeMarketKeeper.GetParams(sdkCtx)
			params.NoBaseFee = false
			params.BaseFee = math.LegacyNewDecWithPrec(1, 3) // 0.001 udstoc/gas, wrapper x10^12 -> 10^9 wei = 1 gwei
			if err := app.FeeMarketKeeper.SetParams(sdkCtx, params); err != nil {
				return vm, fmt.Errorf("failed to enable feemarket base_fee: %w", err)
			}

			return vm, nil
		},
	)

	// v7-combined-fixes: ship v5+v6 source patches + MaxPendingTxPerWalletDecorator
	// in a single binary swap. Params written here:
	//   - feemarket.NoBaseFee   = false       (enable EIP-1559)
	//   - feemarket.BaseFee     = 0.001       (1 gwei after wrapper x10^12)
	//   - feemarket.MinGasPrice = 0.001       (re-assert v4 value as a floor)
	// Source-only changes that ride along with the binary (no params needed):
	//   - x/evmutil/keeper/bank_keeper.go convertAndValidateCoins → dust round-up
	//   - app/ante/max_pending_tx.go MaxPendingTxPerWalletDecorator (cap 50)
	// Idempotent: re-running the v7 handler on a node that already applied v5
	// is safe; values are simply re-asserted.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameCombinedV7,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			sdkCtx := sdk.UnwrapSDKContext(ctx)
			params := app.FeeMarketKeeper.GetParams(sdkCtx)
			params.NoBaseFee = false
			params.BaseFee = math.LegacyNewDecWithPrec(1, 3)     // 1 gwei
			params.MinGasPrice = math.LegacyNewDecWithPrec(1, 3) // 1 gwei floor
			if err := app.FeeMarketKeeper.SetParams(sdkCtx, params); err != nil {
				return vm, fmt.Errorf("failed to apply v7 feemarket params: %w", err)
			}

			return vm, nil
		},
	)

	// v8-eth-mempool: pure binary swap, no params change. Three fixes ship in
	// the vendored fork at stoc-current-dev/forks/cosmos-evm-v0.6.0/ (Bug A
	// per-tx balance, Bug B iterator Pop helper, Bug C eth_getTransactionCount
	// pending nonce). Handler only runs migrations to honour the upgrade plan
	// commit. Idempotent.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameEthMempool,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			return app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
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

	// v3-fix-evm-denom: no new stores needed, only param update
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

	// Set bank denom_metadata — required for InitEvmCoinInfo to load decimals + display denom
	app.BankKeeper.SetDenomMetaData(sdkCtx, banktypes.Metadata{
		Base:    bondDenom,
		Display: displayDenom,
		DenomUnits: []*banktypes.DenomUnit{
			{Denom: bondDenom, Exponent: 0},
			{Denom: displayDenom, Exponent: 6},
		},
		Name:   strings.ToUpper(displayDenom),
		Symbol: strings.ToUpper(displayDenom),
	})

	// Initialize EVM coin info from bank metadata + params → stores in KV store
	if err := app.EVMKeeper.InitEvmCoinInfo(sdkCtx); err != nil {
		return fmt.Errorf("failed to init evm coin info: %w", err)
	}

	return nil
}
