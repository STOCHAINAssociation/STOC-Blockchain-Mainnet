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
		logger       log.Logger

		// the address capable of executing a MsgUpdateParams message. Typically, this
		// should be the x/gov module account.
		authority     string
		accountKeeper types.AccountKeeper
		bankKeeper    types.BankKeeper
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
	// SA-L8 audit-2026-05-29: fail loudly at boot when expected keepers are
	// nil rather than panicking deep inside a message handler later. The
	// previous nil-tolerant constructor would survive depinject misconfig and
	// surface as a hard-to-diagnose nil deref the first time a token was
	// created or burned.
	if bankKeeper == nil {
		panic("x/stoc: BankKeeper must not be nil")
	}
	if accountKeeper == nil {
		panic("x/stoc: AccountKeeper must not be nil")
	}
	if storeService == nil {
		panic("x/stoc: store.KVStoreService must not be nil")
	}
	if _, err := sdk.AccAddressFromBech32(authority); err != nil {
		panic(fmt.Sprintf("invalid authority address: %s", authority))
	}

	return Keeper{
		cdc:           cdc,
		storeService:  storeService,
		logger:        logger,
		authority:     authority,
		bankKeeper:    bankKeeper,
		accountKeeper: accountKeeper,
	}
}

// GetBankKeeper returns the bank keeper for controlled access
func (k Keeper) GetBankKeeper() types.BankKeeper {
	return k.bankKeeper
}

// GetAuthority returns the module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger returns a module-specific logger.
func (k Keeper) Logger() log.Logger {
	return k.logger.With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetTokenCounter returns the current token counter value.
func (k Keeper) GetTokenCounter(ctx sdk.Context) (uint64, error) {
	store := k.storeService.OpenKVStore(ctx)
	bz, err := store.Get([]byte(types.TokenCounterKey))
	if err != nil {
		return 0, fmt.Errorf("failed to get token counter: %w", err)
	}
	if bz == nil {
		return 0, nil
	}
	if len(bz) != 8 {
		return 0, fmt.Errorf("corrupted token counter: expected 8 bytes, got %d", len(bz))
	}
	return binary.BigEndian.Uint64(bz), nil
}

// SetTokenCounter updates the token counter value.
func (k Keeper) SetTokenCounter(ctx sdk.Context, counter uint64) error {
	store := k.storeService.OpenKVStore(ctx)
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, counter)
	if err := store.Set([]byte(types.TokenCounterKey), bz); err != nil {
		return fmt.Errorf("failed to set token counter: %w", err)
	}
	return nil
}
