package app

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	evmutiltypes "stoc/x/evmutil/types"
)

const (
	// UpgradeName defines the on-chain upgrade name for the STOC upgrade to add EVM support
	UpgradeName = "v2-evm"
	// UpgradeNameFixEVMDenom fixes the EVM denom from default "atest" to correct value
	// derived from staking bond_denom (e.g. "ustoc" → "astoc", "utstoc" → "atstoc")
	UpgradeNameFixEVMDenom = "v3-fix-evm-denom"
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

	// v3-fix-evm-denom: fix EVM denom from default "atest" to correct value
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

	// v3-fix-evm-denom: no new stores needed, only param update
}

// setEvmDenomFromStaking derives and sets evm_denom from staking bond_denom.
// "ustoc" → "astoc" (mainnet), "utstoc" → "atstoc" (testnet)
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
	evmDenom := "a" + bondDenom[1:] // "ustoc" → "astoc", "utstoc" → "atstoc"

	evmParams := app.EVMKeeper.GetParams(sdkCtx)
	evmParams.EvmDenom = evmDenom
	if err := app.EVMKeeper.SetParams(sdkCtx, evmParams); err != nil {
		return fmt.Errorf("failed to set evm_denom to %s: %w", evmDenom, err)
	}

	return nil
}
