package keeper

import (
	"context"

	"github.com/cosmos/evm/x/feemarket/types"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
)

// UpdateParams implements the gRPC MsgServer interface. When an UpdateParams
// proposal passes, it updates the module parameters. The update can only be
// performed if the requested authority is the Cosmos SDK governance module
// account.
func (k *Keeper) UpdateParams(goCtx context.Context, req *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if k.authority.String() != req.Authority {
		return nil, errorsmod.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", k.authority.String(), req.Authority)
	}

	// FEE1 audit-2026-07-27: SetParams does NOT validate, and in SDK v0.53 the
	// gov execution path does NOT invoke MsgUpdateParams.ValidateBasic (which is
	// where Params.Validate() lives). So a passed gov param-change proposal could
	// set params that bypass every feemarket guard — e.g. BaseFeeChangeDenominator=0
	// or ElasticityMultiplier=0 (next BeginBlock divides by zero -> CHAIN HALT), or
	// BaseFee=0 with base fee enabled (removes the cosmos-tx fee floor -> free spam).
	// Validate here so the gov path enforces the same invariants as the upgrade
	// handler. This gates ONLY the gov MsgUpdateParams path; per-block BeginBlock
	// (SetBaseFee) and the upgrade handlers call SetParams directly, unaffected.
	if err := req.Params.Validate(); err != nil {
		return nil, err
	}

	ctx := sdk.UnwrapSDKContext(goCtx)
	if err := k.SetParams(ctx, req.Params); err != nil {
		return nil, err
	}

	return &types.MsgUpdateParamsResponse{}, nil
}
