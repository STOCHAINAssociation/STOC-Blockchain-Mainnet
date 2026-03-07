package keeper_test

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"stoc/x/evmutil/keeper"
	"stoc/x/evmutil/types"
)

// mockBankKeeper implements types.BankKeeper for testing
type mockBankKeeper struct {
	balances       map[string]sdk.Coins // addr -> coins
	supply         sdk.Coins
	blockedAddrs   map[string]bool
	sendEnabled    map[string]bool // denom -> enabled
	denomMetadata  map[string]banktypes.Metadata
	sendCoinsErr   error
	mintCoinsErr   error
	burnCoinsErr   error
	lastSentCoins  sdk.Coins // track what was actually sent
	lastMintCoins  sdk.Coins
	lastBurnCoins  sdk.Coins
	lastFromModule string
	lastToModule   string
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{
		balances:      make(map[string]sdk.Coins),
		supply:        sdk.NewCoins(),
		blockedAddrs:  make(map[string]bool),
		sendEnabled:   map[string]bool{"ustoc": true},
		denomMetadata: make(map[string]banktypes.Metadata),
	}
}

func (m *mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	coins, ok := m.balances[addr.String()]
	if !ok {
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	return sdk.NewCoin(denom, coins.AmountOf(denom))
}

func (m *mockBankKeeper) GetAllBalances(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	coins, ok := m.balances[addr.String()]
	if !ok {
		return sdk.NewCoins()
	}
	return coins
}

func (m *mockBankKeeper) SendCoins(_ context.Context, _, _ sdk.AccAddress, amt sdk.Coins) error {
	m.lastSentCoins = amt
	return m.sendCoinsErr
}

func (m *mockBankKeeper) MintCoins(_ context.Context, moduleName string, amt sdk.Coins) error {
	m.lastMintCoins = amt
	m.lastFromModule = moduleName
	return m.mintCoinsErr
}

func (m *mockBankKeeper) BurnCoins(_ context.Context, moduleName string, amt sdk.Coins) error {
	m.lastBurnCoins = amt
	m.lastFromModule = moduleName
	return m.burnCoinsErr
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, senderModule string, _ sdk.AccAddress, amt sdk.Coins) error {
	m.lastSentCoins = amt
	m.lastFromModule = senderModule
	return m.sendCoinsErr
}

func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, _ sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	m.lastSentCoins = amt
	m.lastToModule = recipientModule
	return m.sendCoinsErr
}

func (m *mockBankKeeper) SendCoinsFromModuleToModule(_ context.Context, senderModule string, recipientModule string, amt sdk.Coins) error {
	m.lastSentCoins = amt
	m.lastFromModule = senderModule
	m.lastToModule = recipientModule
	return m.sendCoinsErr
}

func (m *mockBankKeeper) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[addr.String()]
}

func (m *mockBankKeeper) SpendableCoin(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	coins, ok := m.balances[addr.String()]
	if !ok {
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	return sdk.NewCoin(denom, coins.AmountOf(denom))
}

func (m *mockBankKeeper) BlockedAddr(addr sdk.AccAddress) bool {
	return m.blockedAddrs[addr.String()]
}

func (m *mockBankKeeper) GetDenomMetaData(_ context.Context, denom string) (banktypes.Metadata, bool) {
	md, ok := m.denomMetadata[denom]
	return md, ok
}

func (m *mockBankKeeper) SetDenomMetaData(_ context.Context, denomMetaData banktypes.Metadata) {
	m.denomMetadata[denomMetaData.Base] = denomMetaData
}

func (m *mockBankKeeper) GetSupply(_ context.Context, denom string) sdk.Coin {
	return sdk.NewCoin(denom, m.supply.AmountOf(denom))
}

func (m *mockBankKeeper) IsSendEnabledCoin(_ context.Context, coin sdk.Coin) bool {
	enabled, ok := m.sendEnabled[coin.Denom]
	if !ok {
		return true
	}
	return enabled
}

func (m *mockBankKeeper) IsSendEnabledCoins(_ context.Context, coins ...sdk.Coin) error {
	for _, coin := range coins {
		enabled, ok := m.sendEnabled[coin.Denom]
		if ok && !enabled {
			return fmt.Errorf("send not enabled for %s", coin.Denom)
		}
	}
	return nil
}

func (m *mockBankKeeper) IterateAccountBalances(_ context.Context, account sdk.AccAddress, cb func(coin sdk.Coin) bool) {
	coins, ok := m.balances[account.String()]
	if !ok {
		return
	}
	for _, coin := range coins {
		if cb(coin) {
			return
		}
	}
}

func (m *mockBankKeeper) IterateAllBalances(_ context.Context, cb func(address sdk.AccAddress, coin sdk.Coin) (stop bool)) {
	for addrStr, coins := range m.balances {
		addr, _ := sdk.AccAddressFromBech32(addrStr)
		for _, coin := range coins {
			if cb(addr, coin) {
				return
			}
		}
	}
}

func (m *mockBankKeeper) IterateTotalSupply(_ context.Context, cb func(coin sdk.Coin) bool) {
	for _, coin := range m.supply {
		if cb(coin) {
			return
		}
	}
}

// Helper to set balance for a given address
func (m *mockBankKeeper) setBalance(addr sdk.AccAddress, coins sdk.Coins) {
	m.balances[addr.String()] = coins
}

// Setup helpers
func setup() (keeper.EvmBankKeeper, *mockBankKeeper) {
	// Ensure DefaultBondDenom is set for tests
	sdk.DefaultBondDenom = "ustoc"

	mock := newMockBankKeeper()
	ebk := keeper.NewEvmBankKeeper(mock)
	return ebk, mock
}

func testAddr() sdk.AccAddress {
	return sdk.AccAddress([]byte("test_address_1234567"))
}

func testAddr2() sdk.AccAddress {
	return sdk.AccAddress([]byte("test_address_2345678"))
}

// ===================== GetBalance Tests =====================

func TestGetBalance_EvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(100))))

	result := ebk.GetBalance(context.Background(), addr, "astoc")
	require.Equal(t, "astoc", result.Denom)
	// 100 ustoc * 10^12 = 100_000_000_000_000 astoc
	require.Equal(t, math.NewInt(100).Mul(types.ConversionMultiplier), result.Amount)
}

func TestGetBalance_CosmosDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(500))))

	result := ebk.GetBalance(context.Background(), addr, "ustoc")
	require.Equal(t, "ustoc", result.Denom)
	require.Equal(t, math.NewInt(500), result.Amount)
}

func TestGetBalance_CustomToken_ReturnsZero(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(1000))))

	result := ebk.GetBalance(context.Background(), addr, "MYTOKEN_0")
	require.Equal(t, "MYTOKEN_0", result.Denom)
	require.True(t, result.Amount.IsZero(), "custom token should return zero from EVM context")
}

func TestGetBalance_ZeroBalance(t *testing.T) {
	ebk, _ := setup()
	addr := testAddr()

	result := ebk.GetBalance(context.Background(), addr, "astoc")
	require.Equal(t, "astoc", result.Denom)
	require.True(t, result.Amount.IsZero())
}

// ===================== SendCoins Tests =====================

func TestSendCoins_EvmDenom_ConvertsToCosmosAndSends(t *testing.T) {
	ebk, mock := setup()
	from, to := testAddr(), testAddr2()

	// Send 1 astoc (10^12 wei) = 1 ustoc
	evmAmount := types.ConversionMultiplier
	amt := sdk.NewCoins(sdk.NewCoin("astoc", evmAmount))
	err := ebk.SendCoins(context.Background(), from, to, amt)
	require.NoError(t, err)

	// Verify converted to ustoc
	require.Equal(t, math.NewInt(1), mock.lastSentCoins.AmountOf("ustoc"))
}

func TestSendCoins_CosmosDenom_PassesThrough(t *testing.T) {
	ebk, mock := setup()
	from, to := testAddr(), testAddr2()

	amt := sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(50)))
	err := ebk.SendCoins(context.Background(), from, to, amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(50), mock.lastSentCoins.AmountOf("ustoc"))
}

func TestSendCoins_CustomToken_Blocked(t *testing.T) {
	ebk, _ := setup()
	from, to := testAddr(), testAddr2()

	amt := sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	err := ebk.SendCoins(context.Background(), from, to, amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom token")
	require.Contains(t, err.Error(), "MYTOKEN_0")
}

func TestSendCoins_DustAmount_ReturnsError(t *testing.T) {
	ebk, _ := setup()
	from, to := testAddr(), testAddr2()

	// Send 1 wei (too small to convert to 1 ustoc) — rejected by dust remainder check
	amt := sdk.NewCoins(sdk.NewCoin("astoc", math.NewInt(1)))
	err := ebk.SendCoins(context.Background(), from, to, amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dust remainder")
}

func TestSendCoins_MixedEvmAndCosmos(t *testing.T) {
	ebk, mock := setup()
	from, to := testAddr(), testAddr2()

	amt := sdk.NewCoins(
		sdk.NewCoin("astoc", types.ConversionMultiplier.MulRaw(5)), // 5 ustoc worth
		sdk.NewCoin("ustoc", math.NewInt(3)),
	)
	err := ebk.SendCoins(context.Background(), from, to, amt)
	require.NoError(t, err)
	// Both should be consolidated as ustoc: 5 + 3 = 8
	require.Equal(t, math.NewInt(8), mock.lastSentCoins.AmountOf("ustoc"))
}

// ===================== MintCoins Tests =====================

func TestMintCoins_EvmDenom_ConvertsToCosmosAndMints(t *testing.T) {
	ebk, mock := setup()

	evmAmount := types.ConversionMultiplier.MulRaw(10) // 10 ustoc worth
	amt := sdk.NewCoins(sdk.NewCoin("astoc", evmAmount))
	err := ebk.MintCoins(context.Background(), "evm", amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(10), mock.lastMintCoins.AmountOf("ustoc"))
}

func TestMintCoins_CosmosDenom(t *testing.T) {
	ebk, mock := setup()

	amt := sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(100)))
	err := ebk.MintCoins(context.Background(), "evm", amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(100), mock.lastMintCoins.AmountOf("ustoc"))
}

func TestMintCoins_CustomToken_Blocked(t *testing.T) {
	ebk, _ := setup()

	amt := sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	err := ebk.MintCoins(context.Background(), "evm", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom token")
}

func TestMintCoins_DustAmount_ReturnsError(t *testing.T) {
	ebk, _ := setup()

	amt := sdk.NewCoins(sdk.NewCoin("astoc", math.NewInt(1)))
	err := ebk.MintCoins(context.Background(), "evm", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dust remainder")
}

// ===================== BurnCoins Tests =====================

func TestBurnCoins_EvmDenom_ConvertsAndBurns(t *testing.T) {
	ebk, mock := setup()

	evmAmount := types.ConversionMultiplier.MulRaw(5) // 5 ustoc worth
	amt := sdk.NewCoins(sdk.NewCoin("astoc", evmAmount))
	err := ebk.BurnCoins(context.Background(), "evm", amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(5), mock.lastBurnCoins.AmountOf("ustoc"))
}

func TestBurnCoins_DustAmount_ReturnsError(t *testing.T) {
	ebk, _ := setup()

	// Not divisible by conversion multiplier
	amt := sdk.NewCoins(sdk.NewCoin("astoc", math.NewInt(999)))
	err := ebk.BurnCoins(context.Background(), "evm", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "dust remainder")
}

func TestBurnCoins_CustomToken_Blocked(t *testing.T) {
	ebk, _ := setup()

	amt := sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	err := ebk.BurnCoins(context.Background(), "evm", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom token")
}

func TestBurnCoins_CosmosDenom(t *testing.T) {
	ebk, mock := setup()

	amt := sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(50)))
	err := ebk.BurnCoins(context.Background(), "evm", amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(50), mock.lastBurnCoins.AmountOf("ustoc"))
}

// ===================== SpendableCoins Tests =====================

func TestSpendableCoins_ReturnsOnlyEvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(
		sdk.NewCoin("ustoc", math.NewInt(100)),
		sdk.NewCoin("MYTOKEN_0", math.NewInt(999)),
	))

	result := ebk.SpendableCoins(context.Background(), addr)
	// Should only return astoc, no MYTOKEN_0
	require.Len(t, result, 1)
	require.Equal(t, "astoc", result[0].Denom)
	require.Equal(t, math.NewInt(100).Mul(types.ConversionMultiplier), result[0].Amount)
}

func TestSpendableCoins_ZeroBalance_ReturnsEmpty(t *testing.T) {
	ebk, _ := setup()
	addr := testAddr()

	result := ebk.SpendableCoins(context.Background(), addr)
	require.Empty(t, result)
}

// ===================== SpendableCoin Tests =====================

func TestSpendableCoin_EvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(50))))

	result := ebk.SpendableCoin(context.Background(), addr, "astoc")
	require.Equal(t, "astoc", result.Denom)
	require.Equal(t, math.NewInt(50).Mul(types.ConversionMultiplier), result.Amount)
}

func TestSpendableCoin_CustomToken_ReturnsZero(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(999))))

	result := ebk.SpendableCoin(context.Background(), addr, "MYTOKEN_0")
	require.Equal(t, "MYTOKEN_0", result.Denom)
	require.True(t, result.Amount.IsZero())
}

func TestSpendableCoin_CosmosDenom_PassesThrough(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(200))))

	result := ebk.SpendableCoin(context.Background(), addr, "ustoc")
	require.Equal(t, "ustoc", result.Denom)
	require.Equal(t, math.NewInt(200), result.Amount)
}

// ===================== GetAllBalances Tests =====================

func TestGetAllBalances_ReturnsOnlyEvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(
		sdk.NewCoin("ustoc", math.NewInt(100)),
		sdk.NewCoin("MYTOKEN_0", math.NewInt(500)),
	))

	result := ebk.GetAllBalances(context.Background(), addr)
	require.Len(t, result, 1)
	require.Equal(t, "astoc", result[0].Denom)
	require.Equal(t, math.NewInt(100).Mul(types.ConversionMultiplier), result[0].Amount)
}

func TestGetAllBalances_ZeroBalance(t *testing.T) {
	ebk, _ := setup()
	addr := testAddr()

	result := ebk.GetAllBalances(context.Background(), addr)
	require.Empty(t, result)
}

// ===================== GetSupply Tests =====================

func TestGetSupply_EvmDenom(t *testing.T) {
	ebk, mock := setup()
	mock.supply = sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(1000)))

	result := ebk.GetSupply(context.Background(), "astoc")
	require.Equal(t, "astoc", result.Denom)
	require.Equal(t, math.NewInt(1000).Mul(types.ConversionMultiplier), result.Amount)
}

func TestGetSupply_CosmosDenom(t *testing.T) {
	ebk, mock := setup()
	mock.supply = sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(1000)))

	result := ebk.GetSupply(context.Background(), "ustoc")
	require.Equal(t, "ustoc", result.Denom)
	require.Equal(t, math.NewInt(1000), result.Amount)
}

func TestGetSupply_CustomToken_ReturnsZero(t *testing.T) {
	ebk, mock := setup()
	mock.supply = sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(5000)))

	result := ebk.GetSupply(context.Background(), "MYTOKEN_0")
	require.Equal(t, "MYTOKEN_0", result.Denom)
	require.True(t, result.Amount.IsZero())
}

// ===================== IsSendEnabledCoin Tests =====================

func TestIsSendEnabledCoin_EvmDenom_ChecksCosmosDenom(t *testing.T) {
	ebk, mock := setup()
	mock.sendEnabled["ustoc"] = true

	result := ebk.IsSendEnabledCoin(context.Background(), sdk.NewCoin("astoc", math.NewInt(100)))
	require.True(t, result)
}

func TestIsSendEnabledCoin_EvmDenom_Disabled(t *testing.T) {
	ebk, mock := setup()
	mock.sendEnabled["ustoc"] = false

	result := ebk.IsSendEnabledCoin(context.Background(), sdk.NewCoin("astoc", math.NewInt(100)))
	require.False(t, result)
}

func TestIsSendEnabledCoin_CustomToken_AlwaysFalse(t *testing.T) {
	ebk, _ := setup()

	result := ebk.IsSendEnabledCoin(context.Background(), sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	require.False(t, result)
}

func TestIsSendEnabledCoin_CosmosDenom(t *testing.T) {
	ebk, mock := setup()
	mock.sendEnabled["ustoc"] = true

	result := ebk.IsSendEnabledCoin(context.Background(), sdk.NewCoin("ustoc", math.NewInt(100)))
	require.True(t, result)
}

// ===================== IsSendEnabledCoins Tests =====================

func TestIsSendEnabledCoins_EvmDenom(t *testing.T) {
	ebk, mock := setup()
	mock.sendEnabled["ustoc"] = true

	err := ebk.IsSendEnabledCoins(context.Background(), sdk.NewCoin("astoc", math.NewInt(100)))
	require.NoError(t, err)
}

func TestIsSendEnabledCoins_CustomToken_ReturnsError(t *testing.T) {
	ebk, _ := setup()

	err := ebk.IsSendEnabledCoins(context.Background(), sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom token")
}

func TestIsSendEnabledCoins_Mixed_WithCustomToken_ReturnsError(t *testing.T) {
	ebk, _ := setup()

	err := ebk.IsSendEnabledCoins(context.Background(),
		sdk.NewCoin("astoc", math.NewInt(100)),
		sdk.NewCoin("MYTOKEN_0", math.NewInt(50)),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MYTOKEN_0")
}

// ===================== GetDenomMetaData Tests =====================

func TestGetDenomMetaData_EvmDenom(t *testing.T) {
	ebk, _ := setup()

	md, found := ebk.GetDenomMetaData(context.Background(), "astoc")
	require.True(t, found)
	require.Equal(t, "astoc", md.Base)
	require.Equal(t, "stoc", md.Display)
	require.Equal(t, "STOC", md.Symbol)
	require.Len(t, md.DenomUnits, 2)
	require.Equal(t, uint32(0), md.DenomUnits[0].Exponent)
	require.Equal(t, uint32(18), md.DenomUnits[1].Exponent)
}

func TestGetDenomMetaData_CosmosDenom_DelegatesToBank(t *testing.T) {
	ebk, mock := setup()
	expected := banktypes.Metadata{Base: "ustoc", Display: "stoc"}
	mock.denomMetadata["ustoc"] = expected

	md, found := ebk.GetDenomMetaData(context.Background(), "ustoc")
	require.True(t, found)
	require.Equal(t, expected, md)
}

// ===================== SetDenomMetaData Tests =====================

func TestSetDenomMetaData_EvmDenom_NotStored(t *testing.T) {
	ebk, mock := setup()
	md := banktypes.Metadata{Base: "astoc"}
	ebk.SetDenomMetaData(context.Background(), md)

	_, found := mock.denomMetadata["astoc"]
	require.False(t, found, "astoc metadata should not be stored")
}

func TestSetDenomMetaData_CosmosDenom_Stored(t *testing.T) {
	ebk, mock := setup()
	md := banktypes.Metadata{Base: "ustoc"}
	ebk.SetDenomMetaData(context.Background(), md)

	stored, found := mock.denomMetadata["ustoc"]
	require.True(t, found)
	require.Equal(t, md, stored)
}

// ===================== BlockedAddr Tests =====================

func TestBlockedAddr_DelegatesToBank(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.blockedAddrs[addr.String()] = true

	require.True(t, ebk.BlockedAddr(addr))
	require.False(t, ebk.BlockedAddr(testAddr2()))
}

// ===================== SendCoinsFromModuleToAccount Tests =====================

func TestSendCoinsFromModuleToAccount_EvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()

	evmAmount := types.ConversionMultiplier.MulRaw(7)
	amt := sdk.NewCoins(sdk.NewCoin("astoc", evmAmount))
	err := ebk.SendCoinsFromModuleToAccount(context.Background(), "evm", addr, amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(7), mock.lastSentCoins.AmountOf("ustoc"))
}

func TestSendCoinsFromModuleToAccount_CustomToken_Blocked(t *testing.T) {
	ebk, _ := setup()
	addr := testAddr()

	amt := sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	err := ebk.SendCoinsFromModuleToAccount(context.Background(), "evm", addr, amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom token")
}

// ===================== SendCoinsFromAccountToModule Tests =====================

func TestSendCoinsFromAccountToModule_EvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()

	evmAmount := types.ConversionMultiplier.MulRaw(3)
	amt := sdk.NewCoins(sdk.NewCoin("astoc", evmAmount))
	err := ebk.SendCoinsFromAccountToModule(context.Background(), addr, "evm", amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(3), mock.lastSentCoins.AmountOf("ustoc"))
}

func TestSendCoinsFromAccountToModule_CustomToken_Blocked(t *testing.T) {
	ebk, _ := setup()
	addr := testAddr()

	amt := sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	err := ebk.SendCoinsFromAccountToModule(context.Background(), addr, "evm", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom token")
}

// ===================== SendCoinsFromModuleToModule Tests =====================

func TestSendCoinsFromModuleToModule_EvmDenom(t *testing.T) {
	ebk, mock := setup()

	evmAmount := types.ConversionMultiplier.MulRaw(2)
	amt := sdk.NewCoins(sdk.NewCoin("astoc", evmAmount))
	err := ebk.SendCoinsFromModuleToModule(context.Background(), "evm", "fee_collector", amt)
	require.NoError(t, err)
	require.Equal(t, math.NewInt(2), mock.lastSentCoins.AmountOf("ustoc"))
}

func TestSendCoinsFromModuleToModule_CustomToken_Blocked(t *testing.T) {
	ebk, _ := setup()

	amt := sdk.NewCoins(sdk.NewCoin("MYTOKEN_0", math.NewInt(100)))
	err := ebk.SendCoinsFromModuleToModule(context.Background(), "evm", "fee_collector", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "custom token")
}

// ===================== IterateAccountBalances Tests =====================

func TestIterateAccountBalances_OnlyEmitsEvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(
		sdk.NewCoin("ustoc", math.NewInt(100)),
		sdk.NewCoin("MYTOKEN_0", math.NewInt(500)),
	))

	var collected []sdk.Coin
	ebk.IterateAccountBalances(context.Background(), addr, func(coin sdk.Coin) bool {
		collected = append(collected, coin)
		return false
	})

	require.Len(t, collected, 1)
	require.Equal(t, "astoc", collected[0].Denom)
	require.Equal(t, math.NewInt(100).Mul(types.ConversionMultiplier), collected[0].Amount)
}

// ===================== IterateAllBalances Tests =====================

func TestIterateAllBalances_OnlyEmitsEvmDenom(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()
	mock.setBalance(addr, sdk.NewCoins(
		sdk.NewCoin("ustoc", math.NewInt(50)),
		sdk.NewCoin("MYTOKEN_0", math.NewInt(300)),
	))

	var collected []sdk.Coin
	ebk.IterateAllBalances(context.Background(), func(_ sdk.AccAddress, coin sdk.Coin) bool {
		collected = append(collected, coin)
		return false
	})

	require.Len(t, collected, 1)
	require.Equal(t, "astoc", collected[0].Denom)
}

// ===================== IterateTotalSupply Tests =====================

func TestIterateTotalSupply_OnlyEmitsEvmDenom(t *testing.T) {
	ebk, mock := setup()
	mock.supply = sdk.NewCoins(
		sdk.NewCoin("ustoc", math.NewInt(1000)),
		sdk.NewCoin("MYTOKEN_0", math.NewInt(5000)),
	)

	var collected []sdk.Coin
	ebk.IterateTotalSupply(context.Background(), func(coin sdk.Coin) bool {
		collected = append(collected, coin)
		return false
	})

	require.Len(t, collected, 1)
	require.Equal(t, "astoc", collected[0].Denom)
	require.Equal(t, math.NewInt(1000).Mul(types.ConversionMultiplier), collected[0].Amount)
}

// ===================== ConvertCosmosCoinToEvmCoin Tests =====================

func TestConvertCosmosCoinToEvmCoin(t *testing.T) {
	sdk.DefaultBondDenom = "ustoc"

	coin := sdk.NewCoin("ustoc", math.NewInt(1))
	result, err := keeper.ConvertCosmosCoinToEvmCoin(coin)
	require.NoError(t, err)
	require.Equal(t, "astoc", result.Denom)
	require.Equal(t, types.ConversionMultiplier, result.Amount)
}

func TestConvertCosmosCoinToEvmCoin_WrongDenom_ReturnsError(t *testing.T) {
	sdk.DefaultBondDenom = "ustoc"

	_, err := keeper.ConvertCosmosCoinToEvmCoin(sdk.NewCoin("MYTOKEN_0", math.NewInt(1)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid denom")
}

// ===================== ConvertEvmCoinToCosmosCoin Tests =====================

func TestConvertEvmCoinToCosmosCoin(t *testing.T) {
	sdk.DefaultBondDenom = "ustoc"

	coin := sdk.NewCoin("astoc", types.ConversionMultiplier.MulRaw(5))
	result, err := keeper.ConvertEvmCoinToCosmosCoin(coin)
	require.NoError(t, err)
	require.Equal(t, "ustoc", result.Denom)
	require.Equal(t, math.NewInt(5), result.Amount)
}

func TestConvertEvmCoinToCosmosCoin_Dust_Truncates(t *testing.T) {
	sdk.DefaultBondDenom = "ustoc"

	// 1.5 ustoc worth = 1_500_000_000_000 astoc → should truncate to 1 ustoc
	coin := sdk.NewCoin("astoc", types.ConversionMultiplier.MulRaw(1).Add(math.NewInt(500_000_000_000)))
	result, err := keeper.ConvertEvmCoinToCosmosCoin(coin)
	require.NoError(t, err)
	require.Equal(t, "ustoc", result.Denom)
	require.Equal(t, math.NewInt(1), result.Amount)
}

func TestConvertEvmCoinToCosmosCoin_SubDust_TruncatesToZero(t *testing.T) {
	sdk.DefaultBondDenom = "ustoc"

	// 1 wei → should truncate to 0 ustoc
	coin := sdk.NewCoin("astoc", math.NewInt(1))
	result, err := keeper.ConvertEvmCoinToCosmosCoin(coin)
	require.NoError(t, err)
	require.Equal(t, "ustoc", result.Denom)
	require.True(t, result.Amount.IsZero())
}

func TestConvertEvmCoinToCosmosCoin_WrongDenom_ReturnsError(t *testing.T) {
	sdk.DefaultBondDenom = "ustoc"

	_, err := keeper.ConvertEvmCoinToCosmosCoin(sdk.NewCoin("ustoc", math.NewInt(1)))
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid denom")
}

// ===================== ConvertCosmosAmountToEvmAmount Tests =====================

func TestConvertCosmosAmountToEvmAmount(t *testing.T) {
	result := keeper.ConvertCosmosAmountToEvmAmount(math.NewInt(1))
	require.Equal(t, types.ConversionMultiplier, result)
}

func TestConvertEvmAmountToCosmosAmount(t *testing.T) {
	result := keeper.ConvertEvmAmountToCosmosAmount(types.ConversionMultiplier)
	require.Equal(t, math.NewInt(1), result)
}

// ===================== Edge Cases & Security Tests =====================

func TestSendCoins_LargeAmount_NoOverflow(t *testing.T) {
	ebk, mock := setup()
	from, to := testAddr(), testAddr2()

	// Large amount: 10^18 ustoc worth of astoc = 10^30 astoc
	largeUstoc := math.NewInt(1_000_000_000_000_000_000) // 10^18
	largeAstoc := largeUstoc.Mul(types.ConversionMultiplier)
	amt := sdk.NewCoins(sdk.NewCoin("astoc", largeAstoc))
	err := ebk.SendCoins(context.Background(), from, to, amt)
	require.NoError(t, err)
	require.Equal(t, largeUstoc, mock.lastSentCoins.AmountOf("ustoc"))
}

func TestConvertAndValidateCoins_EmptyCoins(t *testing.T) {
	ebk, _ := setup()

	// Empty coins should work (pass through to bank keeper)
	amt := sdk.NewCoins()
	err := ebk.SendCoinsFromModuleToModule(context.Background(), "evm", "fee_collector", amt)
	require.NoError(t, err)
}

func TestGetBalance_ExactConversion(t *testing.T) {
	ebk, mock := setup()
	addr := testAddr()

	// Set specific amounts and verify exact conversion
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(1))))

	result := ebk.GetBalance(context.Background(), addr, "astoc")
	require.Equal(t, types.ConversionMultiplier, result.Amount)

	// And back
	mock.setBalance(addr, sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(999_999))))
	result = ebk.GetBalance(context.Background(), addr, "astoc")
	require.Equal(t, math.NewInt(999_999).Mul(types.ConversionMultiplier), result.Amount)
}

// ===================== getDisplayDenom Tests =====================

func TestGetDenomMetaData_EvmDenom_TestnetDenom(t *testing.T) {
	// Switch to testnet denom
	sdk.DefaultBondDenom = "utstoc"
	defer func() { sdk.DefaultBondDenom = "ustoc" }()

	ebk, _ := setup()
	// Need to reset the denom after setup since setup sets it to ustoc
	sdk.DefaultBondDenom = "utstoc"

	md, found := ebk.GetDenomMetaData(context.Background(), "atstoc")
	require.True(t, found)
	require.Equal(t, "atstoc", md.Base)
	require.Equal(t, "tstoc", md.Display)
	require.Equal(t, "TSTOC", md.Symbol)
}

// ===================== BankKeeper Error Propagation Tests =====================

func TestSendCoins_BankKeeperError_Propagated(t *testing.T) {
	ebk, mock := setup()
	mock.sendCoinsErr = fmt.Errorf("insufficient funds")

	amt := sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(100)))
	err := ebk.SendCoins(context.Background(), testAddr(), testAddr2(), amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "insufficient funds")
}

func TestMintCoins_BankKeeperError_Propagated(t *testing.T) {
	ebk, mock := setup()
	mock.mintCoinsErr = fmt.Errorf("mint error")

	amt := sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(100)))
	err := ebk.MintCoins(context.Background(), "evm", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mint error")
}

func TestBurnCoins_BankKeeperError_Propagated(t *testing.T) {
	ebk, mock := setup()
	mock.burnCoinsErr = fmt.Errorf("burn error")

	amt := sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(100)))
	err := ebk.BurnCoins(context.Background(), "evm", amt)
	require.Error(t, err)
	require.Contains(t, err.Error(), "burn error")
}
