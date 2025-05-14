package keeper

import (
	"encoding/binary"

	"fmt"

	"stoc/x/stoc/types"

	"cosmossdk.io/core/store"
	"cosmossdk.io/log"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	
)

type (
	Keeper struct {
		cdc          codec.BinaryCodec
		storeService store.KVStoreService
		// tokenStoreKey storetypes.StoreKey
		logger log.Logger

		// the address capable of executing a MsgUpdateParams message. Typically, this
		// should be the x/gov module account.
		authority     string
		accountKeeper types.AccountKeeper
		BankKeeper    types.BankKeeper
	}
)

func NewKeeper(
	cdc codec.BinaryCodec,
	storeService store.KVStoreService,
	logger log.Logger,
	authority string,
	bankKeeper types.BankKeeper,
	accountKeeper types.AccountKeeper,
) Keeper {
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address: %s", authority))
	}

	return Keeper{
		cdc:           cdc,
		storeService:  storeService,
		logger:        logger,
		authority:     authority,
		BankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
	}
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger returns a module-specific logger.
func (k Keeper) Logger() log.Logger {
	return k.logger.With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetTokenCounter lấy giá trị counter hiện tại
func (k Keeper) GetTokenCounter(ctx sdk.Context) uint64 {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get([]byte(types.TokenCounterKey))
	if err != nil {
		panic(err)
	}
	if bz == nil {
		return 0
	}
	return binary.BigEndian.Uint64(bz)
}

// SetTokenCounter cập nhật giá trị counter
func (k Keeper) SetTokenCounter(ctx sdk.Context, counter uint64) {
	store := k.storeService.OpenKVStore(ctx)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, counter)
	if err := store.Set([]byte(types.TokenCounterKey), bz); err != nil {
		panic(err)
	}
}

