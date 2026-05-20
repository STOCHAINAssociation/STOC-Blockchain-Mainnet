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
//   v4-final                 — TBD: consolidated post-v3 fixes for mainnet
//                              rollout. Bundles the previously-iterated devnet
//                              work (v4 feemarket source fix, v5 EIP-1559
//                              enable, v6 dust round-up, v7 MaxPendingTxPerWallet
//                              cap + bundle, v8 cosmos/evm fork mempool fixes,
//                              v8.3 Bug B PrepareProposal cascade-skip wire)
//                              into a single gov proposal + binary swap. See
//                              UpgradeNameV4Final block below for the full list
//                              of source/param changes shipped.
const (
	UpgradeName            = "v2-evm"
	UpgradeNameFixEVMDenom = "v3-fix-evm-denom"
	// UpgradeNameV4Final consolidates all post-v3 fixes into a single mainnet
	// rollout. Replaces the devnet-only v4/v5/v6/v7/v8 iteration chain. Single
	// gov proposal + binary swap delivers:
	//
	//   Params written by handler (gov-visible state changes):
	//     - feemarket.NoBaseFee   = false   (enable EIP-1559)
	//     - feemarket.BaseFee     = 0.001   (1 gwei after wrapper ×10^12)
	//     - feemarket.MinGasPrice = 0.001   (1 gwei floor for mempool inclusion)
	//
	//   Source-only changes that ride along with the binary (no params needed):
	//     - x/evmutil/keeper/bank_keeper.go     — wei→udstoc dust round-up
	//                                            (fixes "amount has dust remainder"
	//                                            MetaMask deduct-full-gas issue)
	//     - app/ante/max_pending_tx.go          — MaxPendingTxPerWalletDecorator
	//                                            cap 50 (Lê Minh 102-tx incident
	//                                            mitigation)
	//     - app/abci_proposal.go                — STOCProposalHandler wraps
	//                                            DefaultProposalHandler to call
	//                                            EVMMempoolIterator.PopCurrentAccount()
	//                                            when EVM ante fails — Bug B
	//                                            cascade-skip wired at runtime
	//                                            (was helper-only in upstream)
	//     - forks/cosmos-evm-v0.6.0/            — vendored cosmos/evm fork:
	//         mempool/txpool/validation.go      — Bug A per-tx balance check
	//                                            (was sum-queued-cost reject)
	//         mempool/iterator.go               — Bug B PopCurrentAccount helper
	//         rpc/backend/account_info.go       — Bug C eth_getTransactionCount
	//                                            (pending) reflects pool nonce
	//
	// Idempotent: re-running the handler is safe — params are deterministic
	// re-assertions and the source-only changes activate purely on binary load.
	UpgradeNameV4Final = "v4-final"
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

	// v4-final: consolidated post-v3 rollout. See UpgradeNameV4Final const block
	// above for the full list of source/param changes. Handler only writes the
	// feemarket params here — source-only changes (dust round-up,
	// MaxPendingTxPerWallet, Bug B wire, fork mempool fixes) activate at binary
	// load and need no migration call.
	app.UpgradeKeeper.SetUpgradeHandler(
		UpgradeNameV4Final,
		func(ctx context.Context, plan upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
			vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
			if err != nil {
				return vm, err
			}

			sdkCtx := sdk.UnwrapSDKContext(ctx)
			params := app.FeeMarketKeeper.GetParams(sdkCtx)
			params.NoBaseFee = false
			params.BaseFee = math.LegacyNewDecWithPrec(1, 3)     // 0.001 udstoc/gas → 1 gwei via ×10^12
			params.MinGasPrice = math.LegacyNewDecWithPrec(1, 3) // 0.001 udstoc/gas floor
			if err := app.FeeMarketKeeper.SetParams(sdkCtx, params); err != nil {
				return vm, fmt.Errorf("failed to apply v4-final feemarket params: %w", err)
			}

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

	// v3-fix-evm-denom + v4-final: no new stores needed, only param updates.
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
