package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// TopLevelAuthzMsgExecScreenDecorator does a depth-0 prefilter on tx.GetMsgs()
// looking for authz.MsgExec entries whose inner messages carry any of the
// listed disabled type URLs. The check inspects Any.TypeUrl directly — no
// codec unpack, no recursion. The decorator is placed BEFORE the signature
// verification decorators so that simple "single MsgExec wrapping a
// disabled type" attacks (e.g. authz.MsgExec(MsgEthereumTx)) do not consume
// ecrecover before being rejected.
//
// Defence-in-depth context: the full recursive
// cosmosante.NewAuthzLimiterDecorator runs LATER in the post-fee message-
// walking block (depth cap maxNestedMsgs=7) and catches deeper nesting.
// This screen is the shallow first-pass that the recursive check is too
// expensive to run pre-sig.
//
// SA-AUDIT-2026-06-07 M1 + LOW-4 (round 2 re-audit R2-M1, R2-LOW4):
// pre-fix13 the recursive AuthzLimiter ran at position 2 of the ante chain
// (before NewSigVerificationDecorator). Fix13 (per SA-AUDIT-2026-06-06
// MED-3) moved it post-fee to charge gas + fee for nested-Any CPU
// amplification. Round 2 audit established that the fee-economics claim
// in that godoc is illusory: Cosmos SDK baseapp wraps the entire ante
// chain in cacheTxContext (baseapp.go:925) and aborts via msCache discard
// on any error, so the DeductFeeDecorator debit is never persisted —
// spammer pays zero either way. To restore the pre-fix13 ecrecover-
// avoidance for the COMMON shallow-attack case without re-introducing the
// deep-recursion amplification surface, this depth-0-only screen runs
// pre-sig and the recursive AuthzLimiter stays post-fee for deep nesting.
//
// The disabled msg type list MUST be kept in sync with the list passed to
// cosmosante.NewAuthzLimiterDecorator at the post-fee message-walking
// block — see app/ante/cosmos_handler.go.
type TopLevelAuthzMsgExecScreenDecorator struct {
	disabledMsgTypes []string
}

// NewTopLevelAuthzMsgExecScreenDecorator constructs the screen. Pass the
// same disabled msg type URLs handed to the post-fee recursive
// cosmosante.NewAuthzLimiterDecorator so the two layers agree.
func NewTopLevelAuthzMsgExecScreenDecorator(disabledMsgTypes ...string) TopLevelAuthzMsgExecScreenDecorator {
	return TopLevelAuthzMsgExecScreenDecorator{disabledMsgTypes: disabledMsgTypes}
}

// AnteHandle implements sdk.AnteDecorator.
func (d TopLevelAuthzMsgExecScreenDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	if len(d.disabledMsgTypes) == 0 {
		return next(ctx, tx, simulate)
	}
	for _, msg := range tx.GetMsgs() {
		exec, ok := msg.(*authz.MsgExec)
		if !ok {
			continue
		}
		for _, anyMsg := range exec.Msgs {
			if anyMsg == nil {
				continue
			}
			for _, disabled := range d.disabledMsgTypes {
				if anyMsg.TypeUrl == disabled {
					return ctx, errorsmod.Wrapf(
						errortypes.ErrUnauthorized,
						"top-level authz.MsgExec contains disabled inner msg type %q (pre-sig screen — see SA-AUDIT-2026-06-07 M1)",
						anyMsg.TypeUrl,
					)
				}
			}
		}
	}
	return next(ctx, tx, simulate)
}
