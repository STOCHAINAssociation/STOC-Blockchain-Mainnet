package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"stoc/x/stoc/types"
)

func (k msgServer) UpdateParams(goCtx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if k.GetAuthority() != req.Authority {
		return nil, errorsmod.Wrapf(types.ErrInvalidSigner, "invalid authority; expected %s, got %s", k.GetAuthority(), req.Authority)
	}

	// Audit round-20 2026-07-09 (A4 LOW-1): validate params inside the keeper
	// handler. Gov proposal execution routes MsgUpdateParams through the msg
	// service router, which does NOT call ValidateBasic — so this handler is the
	// ONLY enforcement point for a gov-submitted param update. No-op today
	// (x/stoc Params has no bounded fields) but guards the moment a bounded field
	// (e.g. a gov-adjustable tax cap) is added, closing the ValidateBasic-bypass gap.
	if err := req.Params.Validate(); err != nil {
		return nil, errorsmod.Wrap(err, "invalid params")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := k.SetParams(ctx, req.Params); err != nil {
		return nil, err
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"update_params",
			sdk.NewAttribute("module", types.ModuleName),
			sdk.NewAttribute("authority", req.Authority),
		),
	)

	return &types.MsgUpdateParamsResponse{}, nil
}
