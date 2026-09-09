package ante

import (
	cosmosante "github.com/cosmos/evm/ante/cosmos"
	evmante "github.com/cosmos/evm/ante/evm"
	ibcante "github.com/cosmos/ibc-go/v10/modules/core/ante"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"

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

	// SA-AUDIT-2026-06-07 M1 + LOW-4 (round 2 R2-M1, R2-LOW4), refined by
	// SA-AUDIT-2026-06-08 fix15-2: the disabled msg type list is shared
	// between the pre-sig AuthzPreSigScreenDecorator and the post-fee
	// recursive cosmosante.NewAuthzLimiterDecorator. The pre-sig screen
	// catches the common shallow attacks (authz.MsgExec up to depth
	// AuthzPreSigScreenMaxDepth, and authz.MsgGrant at depth 0) without
	// spending ecrecover; the recursive AuthzLimiter post-fee catches deep
	// nesting (up to maxNestedMsgs=7 in cosmos-evm). Both layers MUST agree
	// on the disabled list — both call AuthzDisabledMsgTypes() so a single
	// source of truth feeds both decorators. The package-level
	// TestAuthzDisabledMsgTypes_StableInvariant test in
	// top_level_authz_screen_test.go pins the slice contents so a future
	// edit to ONE layer's blocklist that forgets the other surfaces in CI.
	authzDisabledMsgTypes := AuthzDisabledMsgTypes()

	return sdk.ChainAnteDecorators(
		cosmosante.NewRejectMessagesDecorator(), // reject MsgEthereumTxs
		ante.NewSetUpContextDecorator(),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		// SA-AUDIT-2026-06-07 M1 + LOW-4 (round 2 re-audit): depth-0 pre-sig
		// screen for the COMMON shallow attack (single authz.MsgExec wrapping a
		// disabled type). Runs BEFORE the signature verification block so a
		// disabled-type tx never consumes ecrecover. Constant cost per top-
		// level msg, no Any-unpack, no recursion.
		//
		// Why both layers exist: round 2 audit confirmed that the recursive
		// AuthzLimiter at its post-fee position (below) does NOT economically
		// deter spammers — Cosmos SDK baseapp wraps the entire ante chain in
		// cacheTxContext (baseapp.go:925) and discards the cache on any error,
		// so DeductFeeDecorator's bank.SendCoins for the fee is reverted along
		// with everything else when AuthzLimiter returns ErrUnauthorized. The
		// fee is never actually charged. The pre-sig screen is what restores
		// the pre-fix13 ecrecover-avoidance for shallow attacks; the post-fee
		// recursive AuthzLimiter remains as defence-in-depth for deep nesting.
		NewTopLevelAuthzMsgExecScreenDecorator(authzDisabledMsgTypes...),
		// SA-2026-06-02 MED-7 (senior-skeptic audit), updated by
		// SA-AUDIT-2026-06-07 M1 (round 2 re-audit R2-M1): the original MED-7
		// godoc claimed that moving the message-walking decorators
		// (IBCCustomTokenRestriction, CustomTokenChainOpsRestriction,
		// RedundantRelayDecorator, AuthzLimiterDecorator) AFTER DeductFee
		// "means any tx that survives ValidateBasic + signature checks pays
		// for the deep traversal that follows." Round 2 audit established
		// that claim is INACCURATE: baseapp aborts ante via msCache discard
		// on any error, so the fee debit DeductFeeDecorator writes into the
		// branched store is reverted when a downstream decorator returns
		// error — the spammer pays zero whether the message walkers run pre-
		// or post-fee. The remaining genuine benefit of the post-fee placement
		// is that it forces a valid signature + sufficient balance + correct
		// sequence number BEFORE the walker runs, raising the cost of
		// nested-Any amplification attacks from "any unsigned attacker" to
		// "any funded, signed attacker willing to burn one tx per attempt
		// (sequence is reverted along with the fee on ante failure, so the
		// same nonce can be reused indefinitely)". The depth-0 pre-sig screen
		// above closes the shallow-attack ecrecover-avoidance gap left by
		// this reorder.
		NewCosmosMinGasPriceDecorator(&feemarketParams),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		ante.NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, txFeeChecker),
		// SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
		// SA-AUDIT-2026-06-06 MED-3 (deep re-audit M3, audit batch B), updated
		// by SA-AUDIT-2026-06-07 M1 (round 2 R2-M1): NewAuthzLimiterDecorator
		// was previously placed at position 2, BEFORE fee deduction. Fix13
		// moved it post-fee under the (inaccurate, see above) belief that the
		// fee gate would deter nested-Any CPU amplification. The pre-sig
		// shallow screen above now catches the common single-MsgExec attack;
		// this recursive walker stays here as defence-in-depth for deep
		// nesting (depth up to maxNestedMsgs=7 in cosmos-evm v0.6.0
		// ante/cosmos/authz.go:14). Disabled msg type list is shared with the
		// pre-sig screen via authzDisabledMsgTypes above so both layers agree
		// on what to reject.
		cosmosante.NewAuthzLimiterDecorator(authzDisabledMsgTypes...),
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
