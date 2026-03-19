package keeper_test

import (
	"context"
	"fmt"
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
	// Deduct from sender, return error if insufficient balance
	senderBal := m.Balances[senderAddr.String()]
	newBal, hasNeg := senderBal.SafeSub(amt...)
	if hasNeg {
		return fmt.Errorf("insufficient funds: %s is smaller than %s", senderBal, amt)
	}
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
func (m *MockBankKeeper) GetSupply(ctx context.Context, denom string) sdk.Coin {
	// Sum all balances for the denom across all accounts
	total := math.ZeroInt()
	for _, coins := range m.Balances {
		total = total.Add(coins.AmountOf(denom))
	}
	return sdk.NewCoin(denom, total)
}
func (m *MockBankKeeper) SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata) {}
func (m *MockBankKeeper) IterateAccountBalances(ctx context.Context, addr sdk.AccAddress, cb func(coin sdk.Coin) bool) {
	for _, coin := range m.Balances[addr.String()] {
		if cb(coin) {
			break
		}
	}
}

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

func TestBurnToken_NativeDenom_Allowed(t *testing.T) {
	_, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "ustoc"

	// Setup initial balance
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1000)))

	// Native denom burn is allowed — goes through bank module burn
	resp, err := ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator: creator,
		Amount:  math.NewInt(100),
		Denom:   denom,
		BurnAll: false,
	})
	require.NoError(t, err)
	require.True(t, resp.Success)

	// Balance should be reduced
	balance := mockBank.Balances[creator].AmountOf(denom)
	require.Equal(t, math.NewInt(900), balance)
}

func TestBurnManagedToken(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "mytoken"

	// Setup managed token with all required fields to pass validation
	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYT",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

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

func TestBurnToken_BurnAll(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYT",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Set balance to 500
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(500)))

	// BurnAll=true, Amount should be zero
	resp, err := ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator: creator,
		Amount:  math.ZeroInt(),
		Denom:   denom,
		BurnAll: true,
	})
	require.NoError(t, err)
	require.True(t, resp.Success)

	// Verify all tokens burned from user
	balance := mockBank.Balances[creator].AmountOf(denom)
	require.True(t, balance.IsZero(), "user balance should be zero after BurnAll")

	// Verify TotalSupply reduced by 500
	updatedToken, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(500), updatedToken.TotalSupply)
}

func TestBurnToken_BurnAllWithAmount_Rejected(t *testing.T) {
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	// BurnAll=true AND Amount=100 → should fail at ValidateBasic
	// ValidateBasic is called by ante handler, not by MsgServer, so we test it directly.
	msg := &types.MsgBurnToken{
		Creator: creator,
		Amount:  math.NewInt(100),
		Denom:   denom,
		BurnAll: true,
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot set both burn_all=true and a specific amount")
}

func TestBurnToken_ZeroAmount(t *testing.T) {
	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	// BurnAll=false, Amount=0 → should fail at ValidateBasic
	// ValidateBasic is called by ante handler, not by MsgServer, so we test it directly.
	msg := &types.MsgBurnToken{
		Creator: creator,
		Amount:  math.ZeroInt(),
		Denom:   denom,
		BurnAll: false,
	}
	err := msg.ValidateBasic()
	require.Error(t, err)
	require.Contains(t, err.Error(), "burn amount must be positive")
}

func TestBurnToken_ExceedsTotalSupply(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYT",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// User has 1500 (more than TotalSupply tracks)
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1500)))

	// Try to burn 1500 → exceeds TotalSupply of 1000
	_, err := ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator: creator,
		Amount:  math.NewInt(1500),
		Denom:   denom,
		BurnAll: false,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "burn amount")
	require.Contains(t, err.Error(), "exceeds tracked total supply")
}

func TestBurnToken_BurnAllZeroBalance(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYT",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Set balance to 0
	mockBank.Balances[creator] = sdk.NewCoins()

	// BurnAll=true with zero balance → should fail
	_, err := ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator: creator,
		Amount:  math.ZeroInt(),
		Denom:   denom,
		BurnAll: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no tokens remaining")
}

func TestBurnToken_RemainingSupplyAdjusted(t *testing.T) {
	k, ms, ctx, mockBank := setupMsgServerWithMock(t)

	creatorAddr := sdk.AccAddress([]byte("creator_address_123"))
	creator := creatorAddr.String()
	denom := "MYTOKEN_0"
	moduleAddr := authtypes.NewModuleAddress(types.ModuleName)

	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYT",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(1000),
		TotalSupply:     math.NewInt(1000),
		RemainingSupply: math.NewInt(800),
		MinimalDenom:    denom,
		Creator:         creator,
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	// User has 300, module has 800
	mockBank.Balances[creator] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(300)))
	mockBank.Balances[moduleAddr.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(800)))

	// Burn 300 from user
	// After burn: TotalSupply = 1000 - 300 = 700
	// RemainingSupply was 800, which is > new TotalSupply 700
	// excess = 800 - 700 = 100
	// moduleBalance = 800 (mock BurnCoins is no-op, so module balance stays)
	// actualBurnable = min(100, 800) = 100
	// RemainingSupply = 800 - 100 = 700
	resp, err := ms.BurnToken(ctx, &types.MsgBurnToken{
		Creator: creator,
		Amount:  math.NewInt(300),
		Denom:   denom,
		BurnAll: false,
	})
	require.NoError(t, err)
	require.True(t, resp.Success)

	updatedToken, found := k.GetToken(ctx, denom)
	require.True(t, found)
	require.Equal(t, math.NewInt(700), updatedToken.TotalSupply, "TotalSupply should be 700")
	require.Equal(t, math.NewInt(700), updatedToken.RemainingSupply, "RemainingSupply should be adjusted to 700")
}
