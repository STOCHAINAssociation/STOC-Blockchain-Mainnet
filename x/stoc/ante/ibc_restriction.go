package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"

	"stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"
)

// IBCCustomTokenRestriction is an AnteDecorator that blocks IBC transfers
// of custom tokens created via x/stoc. Custom tokens are Cosmos-only and
// should not be sent cross-chain because:
// 1. Tax enforcement cannot be applied on other chains
// 2. Token metadata/rules are lost when crossing chains
// 3. Returning tokens via IBC would bypass tax on the outgoing path
//
// IBC is ONLY allowed for native chain denoms (detected dynamically via sdk.DefaultBondDenom):
//   - Mainnet: ustoc, astoc, stoc
//   - Testnet: utstoc, atstoc, tstoc
//
// All tokens created via x/stoc module (e.g., MYTOKEN_0) are blocked from IBC.
type IBCCustomTokenRestriction struct {
	k keeper.Keeper
}

func NewIBCCustomTokenRestriction(k keeper.Keeper) IBCCustomTokenRestriction {
	return IBCCustomTokenRestriction{k: k}
}

func (d IBCCustomTokenRestriction) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.checkMsgs(ctx, tx.GetMsgs(), 0); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// checkMsgs recursively checks messages including authz-wrapped MsgExec.
func (d IBCCustomTokenRestriction) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *ibctransfertypes.MsgTransfer:
			if err := d.checkTransfer(ctx, m); err != nil {
				return err
			}
		case *authz.MsgExec:
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("authz MsgExec nesting depth exceeded (%d) in IBC restriction check", depth)
			}
			innerMsgs, err := m.GetMessages()
			if err != nil {
				return fmt.Errorf("failed to unwrap authz MsgExec for IBC restriction: %w", err)
			}
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d IBCCustomTokenRestriction) checkTransfer(ctx sdk.Context, transfer *ibctransfertypes.MsgTransfer) error {
	coin := transfer.Token

	// Skip native denoms — always allowed for IBC
	if stoctypes.IsNativeDenom(coin.Denom) {
		return nil
	}

	// Check if this denom is a custom stoc token
	if d.k.HasToken(ctx, coin.Denom) {
		ctx.Logger().Warn("Blocked IBC transfer of custom token",
			"denom", coin.Denom,
			"sender", transfer.Sender,
			"source_port", transfer.SourcePort,
			"source_channel", transfer.SourceChannel,
		)
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"ibc_custom_token_blocked",
				sdk.NewAttribute("denom", coin.Denom),
				sdk.NewAttribute("sender", transfer.Sender),
			),
		)
		return fmt.Errorf(
			"IBC transfer of custom token %q is not allowed: custom tokens created via x/stoc are Cosmos-only and cannot be transferred cross-chain",
			coin.Denom,
		)
	}
	return nil
}
