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

func (k Keeper) TokensBySymbol(goCtx context.Context, req *types.QueryTokensBySymbolRequest) (*types.QueryTokensBySymbolResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Lấy tất cả các token có cùng symbol
	tokens := k.GetTokensBySymbol(ctx, req.Symbol)

	if len(tokens) == 0 {
		return nil, status.Errorf(codes.NotFound, "no tokens with symbol '%s' found", req.Symbol)
	}

	return &types.QueryTokensBySymbolResponse{Tokens: tokens}, nil
}

func (k Keeper) Token(goCtx context.Context, req *types.QueryTokenRequest) (*types.QueryTokenResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	var token types.Token
	var found bool

	if req.MinimalDenom == "" {
		return nil, status.Errorf(codes.InvalidArgument, "minimal_denom is not supported")
	} else {

		token, found = k.GetToken(ctx, req.MinimalDenom)
	}

	if !found {
		return nil, status.Errorf(codes.NotFound, "token with symbol '%s' not found", req.MinimalDenom)
	}

	return &types.QueryTokenResponse{Token: token}, nil
}

func (k Keeper) Tokens(goCtx context.Context, req *types.QueryTokensRequest) (*types.QueryTokensResponse, error) {
	const MaxLimit = 100
	const DefaultLimit = 20
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	if req.Pagination == nil {
		req.Pagination = &query.PageRequest{Limit: DefaultLimit}
	} else {
		if req.Pagination.Limit == 0 {
			req.Pagination.Limit = DefaultLimit
		}
		if req.Pagination.Limit > MaxLimit {
			req.Pagination.Limit = MaxLimit
		}
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
