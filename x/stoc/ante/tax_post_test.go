package ante_test

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
	protov2 "google.golang.org/protobuf/proto"

	"stoc/x/stoc/ante"
	"stoc/x/stoc/keeper"
	"stoc/x/stoc/types"
)

// mockTx implements sdk.Tx for testing ante/post handlers.
type mockTx struct {
	msgs []sdk.Msg
}

func (t mockTx) GetMsgs() []sdk.Msg                        { return t.msgs }
func (t mockTx) GetMsgsV2() ([]protov2.Message, error)     { return nil, nil }

// mockBankKeeper implements types.BankKeeper with in-memory balance tracking.
type mockBankKeeper struct {
	balances  map[string]sdk.Coins
	sendError error // if set, SendCoins always returns this error
}

func newMockBankKeeper() *mockBankKeeper {
	return &mockBankKeeper{
		balances: make(map[string]sdk.Coins),
	}
}

func (m *mockBankKeeper) SpendableCoins(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[addr.String()]
}

func (m *mockBankKeeper) SendCoins(_ context.Context, from sdk.AccAddress, to sdk.AccAddress, amt sdk.Coins) error {
	if m.sendError != nil {
		return m.sendError
	}
	fromKey := from.String()
	toKey := to.String()

	fromBal := m.balances[fromKey]
	newFrom, hasNeg := fromBal.SafeSub(amt...)
	if hasNeg {
		return fmt.Errorf("insufficient funds")
	}
	m.balances[fromKey] = newFrom
	m.balances[toKey] = m.balances[toKey].Add(amt...)
	return nil
}

func (m *mockBankKeeper) SendCoinsFromModuleToAccount(_ context.Context, _ string, _ sdk.AccAddress, _ sdk.Coins) error {
	return nil
}
func (m *mockBankKeeper) SendCoinsFromAccountToModule(_ context.Context, sender sdk.AccAddress, _ string, amt sdk.Coins) error {
	senderBal := m.balances[sender.String()]
	newBal, hasNeg := senderBal.SafeSub(amt...)
	if hasNeg {
		return fmt.Errorf("insufficient funds")
	}
	m.balances[sender.String()] = newBal
	return nil
}
func (m *mockBankKeeper) MintCoins(_ context.Context, _ string, _ sdk.Coins) error  { return nil }
func (m *mockBankKeeper) BurnCoins(_ context.Context, _ string, _ sdk.Coins) error  { return nil }
func (m *mockBankKeeper) GetAllBalances(_ context.Context, addr sdk.AccAddress) sdk.Coins {
	return m.balances[addr.String()]
}
func (m *mockBankKeeper) GetBalance(_ context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	amt := m.balances[addr.String()].AmountOf(denom)
	return sdk.NewCoin(denom, amt)
}
func (m *mockBankKeeper) GetSupply(_ context.Context, denom string) sdk.Coin {
	total := math.ZeroInt()
	for _, coins := range m.balances {
		total = total.Add(coins.AmountOf(denom))
	}
	return sdk.NewCoin(denom, total)
}
func (m *mockBankKeeper) SetDenomMetaData(_ context.Context, _ banktypes.Metadata) {}
func (m *mockBankKeeper) BlockedAddr(_ sdk.AccAddress) bool                         { return false }

// mockAccountKeeper satisfies SA-L8 nil-keeper guard in keeper.NewKeeper.
type mockAccountKeeper struct{}

func (m *mockAccountKeeper) GetAccount(_ context.Context, _ sdk.AccAddress) sdk.AccountI {
	return nil
}
func (m *mockBankKeeper) IterateAccountBalances(_ context.Context, addr sdk.AccAddress, cb func(coin sdk.Coin) bool) {
	for _, coin := range m.balances[addr.String()] {
		if cb(coin) {
			break
		}
	}
}

// setupTaxTest creates a keeper, context, and mock bank for tax post decorator tests.
func setupTaxTest(t testing.TB) (keeper.Keeper, sdk.Context, *mockBankKeeper, codec.BinaryCodec) {
	t.Helper()

	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	authority := authtypes.NewModuleAddress(govtypes.ModuleName)

	mockBank := newMockBankKeeper()

	k := keeper.NewKeeper(
		cdc,
		runtime.NewKVStoreService(storeKey),
		log.NewNopLogger(),
		authority.String(),
		mockBank,
		&mockAccountKeeper{},
	)

	ctx := sdk.NewContext(stateStore, cmtproto.Header{}, false, log.NewNopLogger())
	require.NoError(t, k.SetParams(ctx, types.DefaultParams()))

	return k, ctx, mockBank, cdc
}

// noopPostHandler is a terminal PostHandler that does nothing.
func noopPostHandler(ctx sdk.Context, _ sdk.Tx, _, _ bool) (sdk.Context, error) {
	return ctx, nil
}

// setDefaultBondDenom sets sdk.DefaultBondDenom to "ustoc" for test and restores on cleanup.
func setDefaultBondDenom(t testing.TB) {
	t.Helper()
	original := sdk.DefaultBondDenom
	sdk.DefaultBondDenom = "ustoc"
	t.Cleanup(func() { sdk.DefaultBondDenom = original })
}

func TestTaxPostDecorator_NoTax(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, cdc := setupTaxTest(t)

	decorator := ante.NewTaxPostDecorator(k, cdc)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))

	// MsgSend with native denom "ustoc" — should skip tax entirely
	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(1000))),
	}

	tx := mockTx{msgs: []sdk.Msg{msg}}
	newCtx, err := decorator.PostHandle(ctx, tx, false, true, noopPostHandler)
	require.NoError(t, err)
	require.NotNil(t, newCtx)
}

func TestTaxPostDecorator_CustomTokenWithTax(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, mockBank, cdc := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	taxCollector := sdk.AccAddress([]byte("tax_collector_______"))

	denom := "MYTOKEN_0"

	// Create custom token with 10% tax
	token := types.Token{
		Id:              denom,
		Name:            "My Token",
		Symbol:          "MYTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
		Tax: types.TokenTax{
			Percent:          math.LegacyNewDecWithPrec(1, 1), // 0.1 = 10%
			RecipientAddress: taxCollector.String(),
		},
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Recipient has 100 tokens after receiving transfer
	mockBank.balances[recipient.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100)))

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}

	decorator := ante.NewTaxPostDecorator(k, cdc)
	tx := mockTx{msgs: []sdk.Msg{msg}}

	_, err := decorator.PostHandle(ctx, tx, false, true, noopPostHandler)
	require.NoError(t, err)

	// Tax = 10% of 100 = 10 tokens deducted from recipient → tax collector
	recipientBal := mockBank.balances[recipient.String()].AmountOf(denom)
	taxCollectorBal := mockBank.balances[taxCollector.String()].AmountOf(denom)

	require.Equal(t, math.NewInt(90), recipientBal)
	require.Equal(t, math.NewInt(10), taxCollectorBal)
}

func TestTaxPostDecorator_ZeroTaxPercent(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, mockBank, cdc := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	taxCollector := sdk.AccAddress([]byte("tax_collector_______"))

	denom := "ZEROTAX_0"

	// Token with 0% tax
	token := types.Token{
		Id:              denom,
		Name:            "Zero Tax Token",
		Symbol:          "ZEROTAX",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
		Tax: types.TokenTax{
			Percent:          math.LegacyZeroDec(),
			RecipientAddress: taxCollector.String(),
		},
	}
	require.NoError(t, k.SetToken(ctx, token))

	mockBank.balances[recipient.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100)))

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}

	decorator := ante.NewTaxPostDecorator(k, cdc)
	tx := mockTx{msgs: []sdk.Msg{msg}}

	_, err := decorator.PostHandle(ctx, tx, false, true, noopPostHandler)
	require.NoError(t, err)

	// No tax deducted — recipient keeps full amount
	recipientBal := mockBank.balances[recipient.String()].AmountOf(denom)
	require.Equal(t, math.NewInt(100), recipientBal)
}

func TestTaxPostDecorator_SkipOnSimulate(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, mockBank, cdc := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	taxCollector := sdk.AccAddress([]byte("tax_collector_______"))

	denom := "SIMTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "Sim Token",
		Symbol:          "SIMTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
		Tax: types.TokenTax{
			Percent:          math.LegacyNewDecWithPrec(1, 1), // 10%
			RecipientAddress: taxCollector.String(),
		},
	}
	require.NoError(t, k.SetToken(ctx, token))

	mockBank.balances[recipient.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100)))

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}

	decorator := ante.NewTaxPostDecorator(k, cdc)
	tx := mockTx{msgs: []sdk.Msg{msg}}

	// simulate=true → no tax
	_, err := decorator.PostHandle(ctx, tx, true, true, noopPostHandler)
	require.NoError(t, err)

	// Recipient balance unchanged
	recipientBal := mockBank.balances[recipient.String()].AmountOf(denom)
	require.Equal(t, math.NewInt(100), recipientBal)
}

func TestTaxPostDecorator_SkipOnFailure(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, mockBank, cdc := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	taxCollector := sdk.AccAddress([]byte("tax_collector_______"))

	denom := "FAILTOKEN_0"

	token := types.Token{
		Id:              denom,
		Name:            "Fail Token",
		Symbol:          "FAILTOKEN",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
		Tax: types.TokenTax{
			Percent:          math.LegacyNewDecWithPrec(1, 1), // 10%
			RecipientAddress: taxCollector.String(),
		},
	}
	require.NoError(t, k.SetToken(ctx, token))

	mockBank.balances[recipient.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100)))

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}

	decorator := ante.NewTaxPostDecorator(k, cdc)
	tx := mockTx{msgs: []sdk.Msg{msg}}

	// success=false → no tax
	_, err := decorator.PostHandle(ctx, tx, false, false, noopPostHandler)
	require.NoError(t, err)

	// Recipient balance unchanged
	recipientBal := mockBank.balances[recipient.String()].AmountOf(denom)
	require.Equal(t, math.NewInt(100), recipientBal)
}

func TestTaxPostDecorator_SelfTransferSkipped(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, mockBank, cdc := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	// Recipient IS the tax collector — tax should be skipped to prevent loop
	taxCollector := sdk.AccAddress([]byte("tax_collector_______"))

	denom := "SELFTAX_0"

	token := types.Token{
		Id:              denom,
		Name:            "Self Tax Token",
		Symbol:          "SELFTAX",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
		Tax: types.TokenTax{
			Percent:          math.LegacyNewDecWithPrec(1, 1), // 10%
			RecipientAddress: taxCollector.String(),
		},
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Recipient IS the tax collector
	mockBank.balances[taxCollector.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100)))

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   taxCollector.String(), // sending TO tax collector
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}

	decorator := ante.NewTaxPostDecorator(k, cdc)
	tx := mockTx{msgs: []sdk.Msg{msg}}

	_, err := decorator.PostHandle(ctx, tx, false, true, noopPostHandler)
	require.NoError(t, err)

	// Tax collector balance unchanged — no self-tax
	taxCollectorBal := mockBank.balances[taxCollector.String()].AmountOf(denom)
	require.Equal(t, math.NewInt(100), taxCollectorBal)
}

func TestTaxPostDecorator_MicroTransferCap(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, mockBank, cdc := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	taxCollector := sdk.AccAddress([]byte("tax_collector_______"))

	denom := "MICRO_0"

	// Token with 0.1% tax
	token := types.Token{
		Id:              denom,
		Name:            "Micro Token",
		Symbol:          "MICRO",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
		Tax: types.TokenTax{
			Percent:          math.LegacyNewDecWithPrec(1, 3), // 0.001 = 0.1%
			RecipientAddress: taxCollector.String(),
		},
	}
	require.NoError(t, k.SetToken(ctx, token))

	// Taxable tokens reject transfers of 1 unit or less. A 1-unit transfer
	// cannot be split into a non-zero tax share plus a non-zero recipient
	// share, so allowing it would let a caller split a larger payment into
	// N × 1-unit transfers to bypass the tax entirely. The post-handler
	// must surface this as an error that reverts the transaction.
	mockBank.balances[recipient.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1)))

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(1))),
	}

	decorator := ante.NewTaxPostDecorator(k, cdc)
	tx := mockTx{msgs: []sdk.Msg{msg}}

	_, err := decorator.PostHandle(ctx, tx, false, true, noopPostHandler)
	require.Error(t, err)
	require.Contains(t, err.Error(), "below minimum taxable amount")

	// Recipient balance unchanged; tax collector received nothing.
	recipientBal := mockBank.balances[recipient.String()].AmountOf(denom)
	taxCollectorBal := mockBank.balances[taxCollector.String()].AmountOf(denom)

	require.Equal(t, math.NewInt(1), recipientBal)
	require.Equal(t, math.ZeroInt(), taxCollectorBal)
}

func TestTaxPostDecorator_TaxFailRevertsTx(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, mockBank, cdc := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))
	taxCollector := sdk.AccAddress([]byte("tax_collector_______"))

	denom := "FAILTAX_0"

	token := types.Token{
		Id:              denom,
		Name:            "Fail Tax Token",
		Symbol:          "FAILTAX",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
		Tax: types.TokenTax{
			Percent:          math.LegacyNewDecWithPrec(1, 1), // 10%
			RecipientAddress: taxCollector.String(),
		},
	}
	require.NoError(t, k.SetToken(ctx, token))

	mockBank.balances[recipient.String()] = sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100)))

	// Force SendCoins to fail
	mockBank.sendError = fmt.Errorf("bank module error: insufficient funds")

	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin(denom, math.NewInt(100))),
	}

	decorator := ante.NewTaxPostDecorator(k, cdc)
	tx := mockTx{msgs: []sdk.Msg{msg}}

	_, err := decorator.PostHandle(ctx, tx, false, true, noopPostHandler)
	// Tax failure must revert the entire transaction
	require.Error(t, err)
	require.Contains(t, err.Error(), "tax enforcement failed")
}
