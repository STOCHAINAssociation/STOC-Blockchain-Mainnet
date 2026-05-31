package keeper

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/keeper"
	"stoc/x/stoc/types"
)

// noopBankKeeper satisfies types.BankKeeper with no-op behavior — used by the
// test harness so x/stoc keeper construction passes SA-L8 nil-keeper checks
// without pulling production bank state into unit tests.
type noopBankKeeper struct{}

func (noopBankKeeper) SpendableCoins(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (noopBankKeeper) SendCoins(context.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (noopBankKeeper) SendCoinsFromModuleToAccount(context.Context, string, sdk.AccAddress, sdk.Coins) error {
	return nil
}
func (noopBankKeeper) SendCoinsFromAccountToModule(context.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}
func (noopBankKeeper) MintCoins(context.Context, string, sdk.Coins) error { return nil }
func (noopBankKeeper) BurnCoins(context.Context, string, sdk.Coins) error { return nil }
func (noopBankKeeper) GetAllBalances(context.Context, sdk.AccAddress) sdk.Coins { return nil }
func (noopBankKeeper) GetBalance(_ context.Context, _ sdk.AccAddress, denom string) sdk.Coin {
	return sdk.NewInt64Coin(denom, 0)
}
func (noopBankKeeper) GetSupply(_ context.Context, denom string) sdk.Coin {
	return sdk.NewInt64Coin(denom, 0)
}
func (noopBankKeeper) SetDenomMetaData(context.Context, banktypes.Metadata) {}
func (noopBankKeeper) IterateAccountBalances(context.Context, sdk.AccAddress, func(sdk.Coin) bool) {
}
func (noopBankKeeper) BlockedAddr(sdk.AccAddress) bool { return false }

// noopAccountKeeper satisfies types.AccountKeeper for tests.
type noopAccountKeeper struct{}

func (noopAccountKeeper) GetAccount(context.Context, sdk.AccAddress) sdk.AccountI { return nil }

func StocKeeper(t testing.TB) (keeper.Keeper, sdk.Context) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)

	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		log.NewNopLogger(),
		authority.String(),
		noopBankKeeper{},
		noopAccountKeeper{},
	)

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())

	// Initialize params
	if err := k.SetParams(ctx, types.DefaultParams()); err != nil {
		panic(err)
	}

	return k, ctx
}
