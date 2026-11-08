package ante

import (
	"math/big"

	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
)

// CosmosMinGasPriceDecorator checks that Cosmos tx fees meet the feemarket
// minimum gas price, with proper denomination conversion.
//
// The upstream cosmos/evm MinGasPriceDecorator has a bug: it applies the
// feemarket min_gas_price (which is in 18-decimal EVM scale, e.g. 10^9 = 1 gwei)
// directly to the Cosmos base denom (e.g. ustoc, 6 decimals) without dividing by
// the ConversionFactor (10^12 for 6-decimal chains). This makes Cosmos tx fees
// ~10^12x too expensive.
//
// This decorator fixes the conversion:
//
//	min_gas_price = 10^9 (EVM scale, astoc)
//	ConversionFactor = 10^12 (for 6-decimal coins)
//	Cosmos price = 10^9 / 10^12 = 0.001 ustoc/gas ✓
//
// SA-M2 / SA-L7 audit-2026-05-29 (FALSE POSITIVE — by design):
//   - `evmDenom` is not literally hardcoded; it is read from the EvmCoinInfo
//     singleton (initialized once at chain start via `WithDefaultEvmCoinInfo`,
//     re-initialized in upgrade handler via `InitEvmCoinInfo` from bank
//     metadata). For STOChain this denom is `ustoc` (the Cosmos base denom),
//     not the 18-decimal EVM extended denom (`astoc`).
//   - Rejection of non-EVM-denom fees (line 87) matches upstream
//     cosmos/evm behavior. IBC-relayer-paying-in-IBC-denom scenarios are
//     not supported on STOChain at this layer (LOW #7); if a future
//     governance proposal needs to enable multi-denom fee acceptance,
//     a `feemarket` extension is the correct surface, not this decorator.
//   - A future governance upgrade that changes the base denom must
//     re-initialize the EvmCoinInfo singleton inside the same upgrade
//     handler (pattern in `app/upgrades.go:InitEvmCoinInfo` call) so the
//     decorator picks up the new denom atomically with the migration.
type CosmosMinGasPriceDecorator struct {
	feemarketParams *feemarkettypes.Params
}

// NewCosmosMinGasPriceDecorator creates a decorator that properly converts
// feemarket min_gas_price to Cosmos denomination before checking tx fees.
func NewCosmosMinGasPriceDecorator(feemarketParams *feemarkettypes.Params) CosmosMinGasPriceDecorator {
	return CosmosMinGasPriceDecorator{feemarketParams: feemarketParams}
}

func (mpd CosmosMinGasPriceDecorator) AnteHandle(
	ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler,
) (newCtx sdk.Context, err error) {
	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrapf(errortypes.ErrInvalidType,
			"invalid transaction type %T, expected sdk.FeeTx", tx)
	}

	minGasPrice := mpd.feemarketParams.MinGasPrice

	// Short-circuit if min gas price is 0 or simulating
	if minGasPrice.IsZero() || simulate {
		return next(ctx, tx, simulate)
	}

	// Convert min_gas_price from 18-decimal EVM scale to Cosmos denom scale.
	// For SixDecimals (ustoc): 10^9 / 10^12 = 0.001 ustoc/gas
	// For EighteenDecimals: 10^9 / 1 = 10^9 (no change, denom IS the EVM unit)
	evmDecimals := evmtypes.GetEVMCoinDecimals()
	convFactor := math.LegacyNewDecFromInt(evmDecimals.ConversionFactor())
	cosmosMinGasPrice := minGasPrice.Quo(convFactor)

	evmDenom := evmtypes.GetEVMCoinDenom()

	gas := feeTx.GetGas()
	gasLimit := math.LegacyNewDecFromBigInt(new(big.Int).SetUint64(gas))
	requiredFeeAmount := cosmosMinGasPrice.Mul(gasLimit).Ceil().RoundInt()

	if !requiredFeeAmount.IsPositive() {
		return next(ctx, tx, simulate)
	}

	requiredFees := sdk.Coins{sdk.Coin{Denom: evmDenom, Amount: requiredFeeAmount}}

	feeCoins := feeTx.GetFee()
	if feeCoins == nil {
		return ctx, errorsmod.Wrapf(errortypes.ErrInsufficientFee,
			"fee not provided. Please use the --fees flag or the --gas-price flag along with the --gas flag to estimate the fee. The minimum global fee for this tx is: %s",
			requiredFees)
	}

	// Reject multi-denom fees and wrong denom (defense-in-depth, matches upstream behavior)
	if len(feeCoins) > 1 {
		return ctx, errorsmod.Wrapf(errortypes.ErrInvalidCoins,
			"expected only one fee coin, got %d: %s", len(feeCoins), feeCoins.String())
	}
	if len(feeCoins) == 1 && feeCoins[0].Denom != evmDenom {
		return ctx, errorsmod.Wrapf(errortypes.ErrInvalidCoins,
			"expected fee in %s, got %s", evmDenom, feeCoins[0].Denom)
	}

	if !feeCoins.IsAnyGTE(requiredFees) {
		return ctx, errorsmod.Wrapf(errortypes.ErrInsufficientFee,
			"provided fee < minimum global fee (%s < %s). Please increase the gas price.",
			feeCoins, requiredFees)
	}

	return next(ctx, tx, simulate)
}
