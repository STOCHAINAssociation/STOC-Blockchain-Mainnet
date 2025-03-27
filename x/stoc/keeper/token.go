package keeper

import (
	"stoc/x/stoc/types"

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