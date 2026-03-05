package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
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

// MockBankKeeper implements types.BankKeeper
type MockBankKeeper struct {
	Balances map[string]sdk.Coins
}

func NewMockBankKeeper() *MockBankKeeper {
	return &MockBankKeeper{
		Balances: make(map[string]sdk.Coins),
	}
}

func (m *MockBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.Balances[addr.String()]
}
func (m *MockBankKeeper) SendCoins(ctx context.Context, fromAddr sdk.AccAddress, toAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}
func (m *MockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}
func (m *MockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	// Deduct from sender
	senderBal := m.Balances[senderAddr.String()]
	newBal, _ := senderBal.SafeSub(amt...) // Simplified, assuming enough balance
	m.Balances[senderAddr.String()] = newBal
	return nil
}
func (m *MockBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}
func (m *MockBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	return nil
}
func (m *MockBankKeeper) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.Balances[addr.String()]
}
func (m *MockBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	amt := m.Balances[addr.String()].AmountOf(denom)
	return sdk.NewCoin(denom, amt)
}
func (m *MockBankKeeper) SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata) {}

func setupMsgServerWithMock(t testing.TB) (keeper.Keeper, types.MsgServer, sdk.Context, *MockBankKeeper) {
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)

	mockBank := NewMockBankKeeper()

	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		log.NewNopLogger(),
		authority.String(),
		mockBank,
		nil,
	)

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())

	// Initialize params
	if err := k.SetParams(ctx, types.DefaultParams()); err != nil {
		panic(err)
	}

	return k, keeper.NewMsgServerImpl(k), ctx, mockBank
}

func TestBurnToken(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "ustoc"

	// Setup initial balance
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000)))

	// Test Burn Specific Amount
	_, err := ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator:           creator,
		Amount:            math.NewInt(100),
		Denom:             denom,
		BurnAll:           false,
	})
	require.NoError(t, err)

	// Verify balance decreased in mock
	// Note: SendCoinsFromAccountToModule in mock updates the balance
	bal := mockBank.GetBalance(ctx, creatorAddr, denom)
	require.Equal(t, math.NewInt(900), bal.Amount)

	// Test Burn All
	_, err = ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator:           creator,
		Denom:             denom,
		BurnAll:           true,
	})
	require.NoError(t, err)

	bal = mockBank.GetBalance(ctx, creatorAddr, denom)
	require.Equal(t, math.NewInt(0), bal.Amount)
}

func TestBurnManagedToken(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "mytoken"

	// Setup managed token
	token := types.Token{
		Id:           denom,
		Symbol:       "MYT",
		TotalSupply:  math.NewInt(1000),
		MinimalDenom: denom,
		Creator:      creator,
	}
	k.SetToken(ctx, token)

	// Setup initial balance
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(500)))

	// Burn 100
	_, err := ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator:           creator,
		Amount:            math.NewInt(100),
		Denom:             denom,
		BurnAll:           false,
	})
	require.NoError(t, err)

	// Verify TotalSupply decreased
	token, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(900), token.TotalSupply)
}
