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
	
)

// SetToken sets a token in the store after validating its structure.
// Returns error if the token fails state validation — prevents persisting corrupt data.
func (k Keeper) SetToken(ctx sdk.Context, token types.Token) error {
	if err := types.ValidateState(token); err != nil {
		return sdkerrors.Wrap(err, "refusing to persist invalid token")
	}

	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))

	if token.Id == "" {
		token.Id = token.MinimalDenom
	} // Ensure Id is always minimalDenom, never auto-generated uuid

	mainStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	bz, err := k.cdc.Marshal(&token)
	if err != nil {
		return sdkerrors.Wrap(err, "failed to marshal token")
	}
	mainStore.Set([]byte(token.Id), bz)

	indexStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenSymbolKey))
	indexKey := []byte(token.Symbol + ":" + token.Id)
	indexStore.Set(indexKey, []byte{1})
	return nil
}

// GetToken gets a token from the store
func (k Keeper) GetToken(ctx sdk.Context, minimalDenom string) (val types.Token, found bool) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))

	tokenId := minimalDenom

	b := store.Get([]byte(tokenId))
	if b == nil {
		k.Logger().Debug("Token not found", "minimalDenom", minimalDenom)
		return types.Token{}, false
	}

	var token types.Token
	if err := k.cdc.Unmarshal(b, &token); err != nil {
		k.Logger().Error("Failed to unmarshal token", "minimalDenom", minimalDenom, "error", err)
		return types.Token{}, false
	}
	return token, true
}

// HasToken returns whether a token exists in the store
func (k Keeper) HasToken(ctx sdk.Context, minimalDenom string) bool {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	return store.Has([]byte(minimalDenom))
}

// DeleteToken removes a token from the store and cleans up the symbol index
func (k Keeper) DeleteToken(ctx sdk.Context, minimalDenom string) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))

	// Get token first to find its Symbol for index cleanup
	mainStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	b := mainStore.Get([]byte(minimalDenom))
	if b != nil {
		var token types.Token
		if err := k.cdc.Unmarshal(b, &token); err != nil {
			k.Logger().Error("Failed to unmarshal token during delete", "minimalDenom", minimalDenom, "error", err)
		} else {
			// Clean up symbol index
			indexStore := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenSymbolKey))
			indexKey := []byte(token.Symbol + ":" + token.Id)
			indexStore.Delete(indexKey)
		}
	}

	mainStore.Delete([]byte(minimalDenom))
}

// GetAllTokens returns all tokens
func (k Keeper) GetAllTokens(ctx sdk.Context) (list []types.Token) {
	storeAdapter := runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx))
	store := prefix.NewStore(storeAdapter, types.KeyPrefix(types.TokenKey))
	iterator := storetypes.KVStorePrefixIterator(store, []byte{})

	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var val types.Token
		if err := k.cdc.Unmarshal(iterator.Value(), &val); err != nil {
			k.Logger().Error("Failed to unmarshal token during iteration", "error", err)
			continue
		}
		list = append(list, val)
	}

	return
}

// MintToken mints additional tokens for unlimited tokens and sends them to the owner
func (k Keeper) MintToken(ctx sdk.Context, owner sdk.AccAddress, minimalDenom string, amount math.Int) error {
	if amount.IsNil() || !amount.IsPositive() {
		return sdkerrors.Wrap(types.ErrInvalidTokenAmount, "mint amount must be positive")
	}

	token, found := k.GetToken(ctx, minimalDenom)
	if !found {
		return sdkerrors.Wrapf(types.ErrTokenNotFound, "token %s not found", minimalDenom)
	}

	if token.Creator != owner.String() {
		return sdkerrors.Wrapf(types.ErrUnauthorized, "only token owner can mint token %s", minimalDenom)
	}

	if !token.Unlimited {
		return sdkerrors.Wrapf(types.ErrCannotMint, "token %s is not configured for unlimited minting", minimalDenom)
	}

	// Check supply cap even for unlimited tokens
	newTotal := token.TotalSupply.Add(amount)
	if newTotal.GT(types.MaxTokenSupply) {
		return sdkerrors.Wrapf(types.ErrInvalidTokenAmount, "minting would exceed max supply (%s)", types.MaxTokenSupply.String())
	}

	// Pre-validate: ensure updated state will pass SetToken validation BEFORE any bank mutations.
	// This prevents a scenario where bank ops succeed but SetToken rejects the new state.
	token.TotalSupply = newTotal
	if err := types.ValidateState(token); err != nil {
		return sdkerrors.Wrap(err, "mint would produce invalid token state")
	}

	coins := sdk.NewCoins(sdk.NewCoin(token.MinimalDenom, amount))
	if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, coins); err != nil {
		return err
	}

	if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, owner, coins); err != nil {
		return err
	}

	// Persist state — already validated above, SetToken re-validates defensively
	if err := k.SetToken(ctx, token); err != nil {
		return err
	}

	//Emit event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeMintToken,
			sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.MinimalDenom),
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
		// Cap results to prevent unbounded iteration (DoS via symbol squatting)
		if len(tokens) >= types.MaxQueryLimit {
			break
		}

		key := string(iterator.Key())
		parts := strings.Split(key, ":")
		if len(parts) != 2 {
			continue
		}
		tokenId := parts[1]

		b := mainStore.Get([]byte(tokenId))
		if b != nil {
			var token types.Token
			if err := k.cdc.Unmarshal(b, &token); err != nil {
				k.Logger().Error("Failed to unmarshal token by symbol", "tokenId", tokenId, "error", err)
				continue
			}
			tokens = append(tokens, token)
		}
	}

	return tokens
}
