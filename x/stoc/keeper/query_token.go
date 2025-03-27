package keeper

import (
	"context"

	"cosmossdk.io/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"stoc/x/stoc/types"

	"github.com/cosmos/cosmos-sdk/runtime"
)

func (k Keeper) Token(goCtx context.Context, req *types.QueryTokenRequest) (*types.QueryTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	
	ctx := sdk.UnwrapSDKContext(goCtx)
	
	token, found := k.GetToken(ctx, req.Symbol)
	if !found {
		return nil, status.Errorf(codes.NotFound, "token with symbol '%s' not found", req.Symbol)
	}
	
	return &types.QueryTokenResponse{Token: token}, nil
}

func (k Keeper) Tokens(goCtx context.Context, req *types.QueryTokensRequest) (*types.QueryTokensResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	
	ctx := sdk.UnwrapSDKContext(goCtx)
	
	var tokens []types.Token
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	
	pageRes, err := query.Paginate(store, req.Pagination, func(key []byte, value []byte) error {
		var token types.Token
		if err := k.cdc.Unmarshal(value, &token); err != nil {
			return err
		}
		
		tokens = append(tokens, token)
		return nil
	})
	
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	
	return &types.QueryTokensResponse{
		Tokens:     tokens,
		Pagination: pageRes,
	}, nil
}