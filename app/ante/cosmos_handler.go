package ante

import (
	cosmosante "github.com/cosmos/evm/ante/cosmos"
	evmante "github.com/cosmos/evm/ante/evm"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	ibcante "github.com/cosmos/ibc-go/v10/modules/core/ante"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"

	stocante "stoc/x/stoc/ante"
)

// NOTE: We use our custom CosmosMinGasPriceDecorator (cosmos_min_gas_price.go)
// instead of cosmosante.NewMinGasPriceDecorator because the upstream version
// does not convert feemarket min_gas_price from 18-decimal EVM scale to
// Cosmos denom scale, causing Cosmos tx fees to be 10^12x too expensive.

// newCosmosAnteHandler creates the default ante handler for Cosmos transactions
// v0.6.0: now takes ctx to fetch params from keepers
func newCosmosAnteHandler(ctx sdk.Context, options StocAnteOptions) sdk.AnteHandler {
	feemarketParams := options.FeeMarketKeeper.GetParams(ctx)
	var txFeeChecker ante.TxFeeChecker
	if options.DynamicFeeChecker {
		txFeeChecker = evmante.NewDynamicFeeChecker(&feemarketParams)
	}

	return sdk.ChainAnteDecorators(
		cosmosante.NewRejectMessagesDecorator(), // reject MsgEthereumTxs
		ante.NewSetUpContextDecorator(),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		// SA-2026-06-02 MED-7 (senior-skeptic audit): charge gas + fee BEFORE
		// the message-walking decorators (NewIBCCustomTokenRestriction,
		// NewCustomTokenChainOpsRestriction, NewRedundantRelayDecorator,
		// NewAuthzLimiterDecorator per SA-AUDIT-2026-06-06 MED-3 — see below)
		// so that authz / group / gov msg trees pay for the cost of being
		// unmarshalled + recursively inspected. Previously the message
		// walkers ran first and a flood of nested-Any txs amplified
		// CheckTx CPU at zero on-chain cost. Moving fee/gas earlier means
		// any tx that survives ValidateBasic + signature checks pays for
		// the deep traversal that follows.
		NewCosmosMinGasPriceDecorator(&feemarketParams),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, txFeeChecker),
		// SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		// SA-AUDIT-2026-06-06 MED-3 (deep re-audit M3, audit batch B):
		// NewAuthzLimiterDecorator was previously placed at position 2,
		// BEFORE fee deduction. checkDisabledMsgs (cosmos-evm v0.6.0
		// ante/cosmos/authz.go:44) recurses into MsgExec inner messages
		// with a depth cap (maxNestedMsgs=7) but NO width cap, and calls
		// msg.GetMessages() which Any-unpacks every inner message. A
		// MsgExec carrying 1000 inner Anys would force 1000 codec unpacks
		// per CheckTx at zero on-chain cost — the identical anti-pattern
		// SA-2026-06-02 MED-7 closed for IBCCustomTokenRestriction,
		// CustomTokenChainOpsRestriction, and RedundantRelayDecorator.
		// Move AuthzLimiter into the post-fee message-walking block so
		// the granter pays for the recursion cost. Disabled msg types
		// (MsgEthereumTx, MsgCreateVestingAccount, MsgCreatePermanentLockedAccount,
		// MsgCreatePeriodicVestingAccount per SA-AUDIT-2026-06-06 LOW
		// blocklist completion — see comment at the decorator constructor)
		// are still rejected before message-walking restrictions and
		// the gov/group/IBC checks below, preserving the original semantic.
		cosmosante.NewAuthzLimiterDecorator( // disable the Msg types that cannot be included on an authz.MsgExec msgs field
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreatePermanentLockedAccount{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreatePeriodicVestingAccount{}),
		),
		// Message-walking restriction layer (now post-fee per MED-7). These
		// decorators recurse into authz/group/gov msg trees, so charging
		// gas + fee before them prevents free CPU-DoS amplification.
		stocante.NewIBCCustomTokenRestriction(options.StocKeeper),      // block custom token IBC transfers
		stocante.NewCustomTokenChainOpsRestriction(options.StocKeeper), // block custom token in gov/pool/vesting/group/erc20 (tax evasion + chain entanglement)
		// SA-L6 audit-2026-05-29 (re-confirmed 2026-06-02): IBC redundant-relay
		// check stays in the post-fee block. The original SA-L6 rationale
		// (save CPU on duplicate relays at CheckTx) is preserved relative to
		// SignatureVerification but the senior-skeptic MED-7 finding
		// established that running BEFORE fee deduct enabled free CPU
		// amplification. Relayers now pay for the inspection slot, which is
		// matched by Osmosis / Evmos placement.
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
		evmante.NewGasWantedDecorator(options.EvmKeeper, options.FeeMarketKeeper, &feemarketParams),
	)
}
