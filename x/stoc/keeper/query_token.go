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

	// Get all tokens with the same symbol
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
		return nil, status.Errorf(codes.InvalidArgument, "minimal_denom is required")
	}

	token, found = k.GetToken(ctx, req.MinimalDenom)

	if !found {
		return nil, status.Errorf(codes.NotFound, "token with minimal_denom '%s' not found", req.MinimalDenom)
	}

	return &types.QueryTokenResponse{Token: token}, nil
}

func (k Keeper) Tokens(goCtx context.Context, req *types.QueryTokensRequest) (*types.QueryTokensResponse, error) {
	const MaxLimit = types.MaxQueryLimit
	const DefaultLimit = types.DefaultQueryLimit
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

func (k Keeper) BalancesWithMetadata(goCtx context.Context, req *types.QueryBalancesWithMetadataRequest) (*types.QueryBalancesWithMetadataResponse, error) {
	const maxBalances = types.MaxBalancesResult

	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Address == "" {
		return nil, status.Error(codes.InvalidArgument, "address cannot be empty")
	}

	ctx := sdk.UnwrapSDKContext(goCtx)

	// Parse address
	addr, err := sdk.AccAddressFromBech32(req.Address)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid address: %v", err)
	}

	// Iterate balances with early termination to avoid loading all denoms into memory.
	// This prevents memory DoS via addresses with thousands of airdropped denoms.
	var balancesWithMetadata []types.BalanceWithMetadata
	k.bankKeeper.IterateAccountBalances(ctx, addr, func(coin sdk.Coin) bool {
		if len(balancesWithMetadata) >= maxBalances {
			return true // stop iteration
		}
		bwm := types.BalanceWithMetadata{
			Denom:  coin.Denom,
			Amount: coin.Amount.String(),
		}
		token, found := k.GetToken(ctx, coin.Denom)
		if found {
			bwm.Metadata = &token
		}
		balancesWithMetadata = append(balancesWithMetadata, bwm)
		return false
	})

	// Pagination response — BalancesWithMetadata does not support cursor-based pagination
	// because it aggregates bank balances with stoc metadata. Total reflects returned count.
	// If truncated at maxBalances, set NextKey to signal more results exist.
	pageRes := &query.PageResponse{
		Total: uint64(len(balancesWithMetadata)),
	}
	if len(balancesWithMetadata) >= maxBalances {
		pageRes.NextKey = []byte("truncated")
	}

	return &types.QueryBalancesWithMetadataResponse{
		Balances:   balancesWithMetadata,
		Pagination: pageRes,
	}, nil
}
