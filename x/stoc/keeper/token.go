package keeper

import (
	"stoc/x/stoc/types"
	"strings"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"cosmossdk.io/store/prefix"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/google/uuid"
)

// SetToken sets a token in the store
func (k Keeper) SetToken(ctx sdk.Context, token types.Token) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))

	if token.Id == "" {
		token.Id = uuid.New().String()
	}

	mainStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	mainStore.Set([]byte(token.Id), k.cdc.MustMarshal(&token))

	indexStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenSymbolKey))
	indexKey := []byte(token.Symbol + ":" + token.Id)
	indexStore.Set(indexKey, []byte{1})
}

// GetToken gets a token from the store
func (k Keeper) GetToken(ctx sdk.Context, minimalDenom string) (val types.Token, found bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))

	tokenId := minimalDenom

	b := store.Get([]byte(tokenId))
	if b == nil {
		k.Logger().Info("Token not found", "minimalDenom", minimalDenom)
		return types.Token{}, false
	}

	var token types.Token
	k.cdc.MustUnmarshal(b, &token)
	return token, true
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

// GetTokensBySymbol use index to find token
func (k Keeper) GetTokensBySymbol(ctx sdk.Context, symbol string) []types.Token {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))

	indexStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenSymbolKey))
	iterator := storetypes.KVStorePrefixIterator(indexStore, []byte(symbol+":"))
	defer iterator.Close()

	mainStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	var tokens []types.Token

	for ; iterator.Valid(); iterator.Next() {

		key := string(iterator.Key())
		parts := strings.Split(key, ":")
		if len(parts) != 2 {
			continue
		}
		tokenId := parts[1]

		b := mainStore.Get([]byte(tokenId))
		if b != nil {
			var token types.Token
			k.cdc.MustUnmarshal(b, &token)
			tokens = append(tokens, token)
		}
	}

	return tokens
}
