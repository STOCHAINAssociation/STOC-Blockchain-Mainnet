package keeper

import (
	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetToken sets a token in the store
func (k Keeper) SetToken(ctx sdk.Context, token types.Token) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	b := k.cdc.MustMarshal(&token)
	store.Set([]byte(token.Symbol), b)
}

// GetToken gets a token from the store
func (k Keeper) GetToken(ctx sdk.Context, symbol string) (val types.Token, found bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	b := store.Get([]byte(symbol))
	if b == nil {
		return val, false
	}

	k.cdc.MustUnmarshal(b, &val)
	return val, true
}

// HasToken returns whether a token exists in the store
func (k Keeper) HasToken(ctx sdk.Context, symbol string) bool {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	return store.Has([]byte(symbol))
}

// DeleteToken removes a token from the store
func (k Keeper) DeleteToken(ctx sdk.Context, symbol string) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	store.Delete([]byte(symbol))
}

// GetAllTokens returns all tokens
func (k Keeper) GetAllTokens(ctx sdk.Context) (list []types.Token) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	iterator := storetypes.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Token
		k.cdc.MustUnmarshal(iterator.Value(), &val)
		list = append(list, val)
	}

	return
}

// MintToken if the token is unlimited, mint the token to the address
func (k Keeper) MintToken(ctx sdk.Context, owner sdk.AccAddress, symbol string, amount math.Int) error {
	token, found := k.GetToken(ctx, symbol)
	if !found {
		return sdkerrors.Wrapf(types.ErrTokenNotFound, "token %s not found", symbol)
	}

	if token.Creator != owner.String() {
		return sdkerrors.Wrapf(types.ErrUnauthorized, "only token owner can mint", symbol)
	}

	if !token.Unlimited {
		return sdkerrors.Wrapf(types.ErrCannotMint, "token is not configured for unlimited minting", symbol)
	}

	//logic mint token
	coins := sdk.NewCoins(sdk.NewCoin(symbol, amount))
	err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins)
	if err != nil {
		return err
	}

	//send coins to the owner
	err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, coins)
	if err != nil {
		return err
	}

	//update total supply
	token.TotalSupply = token.TotalSupply.Add(amount)
	k.SetToken(ctx, token)

	//Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeMintToken,
			sdk.NewAttribute(types.AttributeKeyTokenSymbol, symbol),
			sdk.NewAttribute(types.AttributeKeyTokenCreator, owner.String()),
			sdk.NewAttribute(types.AttributeKeyMintToken, amount.String()),
		),
	)

	return nil

}
