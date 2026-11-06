package app

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/collections"
	"cosmossdk.io/core/header"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	stocante "stoc/x/stoc/ante"
	stoctypes "stoc/x/stoc/types"
)

// Regression tests adapted from the v5.0.3 audit round 2 overlay.

func r2Token(denom string, creator sdk.AccAddress, pct math.LegacyDec, recipient string) stoctypes.Token {
	tk := v503Token(creator, pct, recipient)
	tk.Id, tk.MinimalDenom, tk.Symbol = denom, denom, denom[:3]
	return tk
}

// rawSetBalance writes a balance bypassing send restrictions (pre-upgrade state).
func rawSetBalance(t *testing.T, a *App, ctx sdk.Context, addr sdk.AccAddress, denom string, amt int64) {
	t.Helper()
	bk := a.BankKeeper.(bankkeeper.BaseKeeper)
	require.NoError(t, bk.Balances.Set(ctx, collections.Join(addr, denom), math.NewInt(amt)))
}

func r2Header(ctx sdk.Context, h int64) sdk.Context {
	return ctx.WithBlockHeight(h).WithHeaderInfo(header.Info{Height: h, Time: time.Unix(1_800_000_000, 0), ChainID: v503ChainID})
}

// Mainnet-shaped state, upgrade executed through the real x/upgrade
// PreBlocker (scheduled at H-1, applied at H), then legitimate flows.
func TestV503_MainnetShapeUpgradeViaPreBlocker(t *testing.T) {
	a := v503App(t)
	const H = int64(6_900_000)
	pre := r2Header(v503Finalize(a, H-1), H-1)

	require.NoError(t, a.UpgradeKeeper.SetModuleVersionMap(pre, a.ModuleManager.GetVersionMap()))
	mainnetLikeState(a, pre)
	require.NoError(t, a.Erc20Keeper.SetParams(pre, erc20types.NewParams(true, true))) // mainnet erc20 params today

	for _, n := range []string{authtypes.FeeCollectorName, "distribution", "mint", "bonded_tokens_pool", "not_bonded_tokens_pool", "gov", stoctypes.ModuleName, "evm"} {
		require.NotNil(t, a.AccountKeeper.GetModuleAccount(pre, n))
	}
	// erc20 module address used as an EVM sender before its module account
	// existed: plain BaseAccount with nonce 5.
	erc20Addr := authtypes.NewModuleAddress("erc20")
	erc20Squat := a.AccountKeeper.NewAccountWithAddress(pre, erc20Addr)
	require.NoError(t, erc20Squat.SetSequence(5))
	a.AccountKeeper.SetAccount(pre, erc20Squat)

	// 10 custom tokens, only ASL_8 taxed (0.1%) to an existing keyed wallet.
	creator := sdk.AccAddress([]byte("creator_____________"))
	taxRcpt := sdk.AccAddress([]byte("asl8_tax_recipient__"))
	base := authtypes.NewBaseAccountWithAddress(taxRcpt)
	require.NoError(t, base.SetPubKey(secp256k1.GenPrivKey().PubKey()))
	base.AccountNumber = a.AccountKeeper.NextAccountNumber(pre)
	a.AccountKeeper.SetAccount(pre, base)
	for i := 0; i < 10; i++ {
		if i == 8 {
			require.NoError(t, a.StocKeeper.SetToken(pre, r2Token("ASL_8", creator, math.LegacyNewDecWithPrec(1, 3), taxRcpt.String())))
			continue
		}
		require.NoError(t, a.StocKeeper.SetToken(pre, r2Token(fmt.Sprintf("TK%d_%d", i, i), creator, math.LegacyZeroDec(), "")))
	}
	rawSetBalance(t, a, pre, creator, "ASL_8", 1_000_000)

	// Squatted transfer module address (BaseAccount holding native and custom
	// tokens), absent ICA module account.
	transferAddr := authtypes.NewModuleAddress(ibctransfertypes.ModuleName)
	a.AccountKeeper.SetAccount(pre, a.AccountKeeper.NewAccountWithAddress(pre, transferAddr))
	rawSetBalance(t, a, pre, transferAddr, sdk.DefaultBondDenom, 1)
	rawSetBalance(t, a, pre, transferAddr, "ASL_8", 50)
	require.Nil(t, a.AccountKeeper.GetAccount(pre, authtypes.NewModuleAddress(icatypes.ModuleName)))

	// One transfer channel opened before v5.0.3 (never seen by the guard).
	v503SetTransferChannel(a, pre, "channel-0", channeltypes.OPEN)
	oldEscrow := ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, "channel-0")
	require.False(t, a.StocKeeper.IsIBCEscrow(pre, oldEscrow))

	plan := upgradetypes.Plan{Name: UpgradeNameV503, Height: H, Info: "https://example/v5.0.3"}
	require.NoError(t, a.UpgradeKeeper.ScheduleUpgrade(pre, plan))

	ctx := r2Header(v503Finalize(a, H), H).WithGasMeter(storetypes.NewInfiniteGasMeter())
	_, err := a.ModuleManager.PreBlock(ctx)
	require.NoError(t, err, "v5.0.3 PreBlocker must succeed on mainnet-shaped state")
	done, err := a.UpgradeKeeper.GetDoneHeight(ctx, UpgradeNameV503)
	require.NoError(t, err)
	require.Equal(t, H, done)

	for _, p := range moduleAccPerms {
		_, ok := a.AccountKeeper.GetAccount(ctx, authtypes.NewModuleAddress(p.Account)).(sdk.ModuleAccountI)
		require.True(t, ok, p.Account)
	}
	require.Equal(t, uint64(5), a.AccountKeeper.GetAccount(ctx, erc20Addr).GetSequence())
	require.False(t, a.ICAHostKeeper.GetParams(ctx).HostEnabled)
	require.Equal(t, erc20types.NewParams(true, false), a.Erc20Keeper.GetParams(ctx), "erc20 registration is governance-only")
	require.Empty(t, notWalletEvents(ctx), "ASL_8 recipient passes the pre-flight")
	require.True(t, a.StocKeeper.IsIBCEscrow(ctx, oldEscrow), "pre-existing channel indexed by the handler")
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, creator, oldEscrow, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1))), "IBC transfer of custom token")

	// IBC voucher mint/send/burn through the (now blocked) transfer module account.
	user := sdk.AccAddress([]byte("user_after_upgrade__"))
	voucher := sdk.NewCoins(sdk.NewInt64Coin("ibc/27394FB092D2ECCD56123C74F36E4C1F926001CEADA9CA97EA622B25F41E5EB2", 10))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, ibctransfertypes.ModuleName, voucher))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, ibctransfertypes.ModuleName, user, voucher))
	require.NoError(t, a.BankKeeper.SendCoinsFromAccountToModule(ctx, user, ibctransfertypes.ModuleName, voucher))
	require.NoError(t, a.BankKeeper.BurnCoins(ctx, ibctransfertypes.ModuleName, voucher))

	// ASL_8 taxed transfer to a fresh wallet: 1000 -> 999 + 1 tax.
	fresh := sdk.AccAddress([]byte("fresh_recipient_____"))
	require.NoError(t, v503RunTx(t, a, ctx, banktypes.NewMsgSend(creator, fresh, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1000)))))
	require.Equal(t, int64(999), a.BankKeeper.GetBalance(ctx, fresh, "ASL_8").Amount.Int64())
	require.Equal(t, int64(1), a.BankKeeper.GetBalance(ctx, taxRcpt, "ASL_8").Amount.Int64())

	// Minimum-amount path: 2 units at 0.1% -> 1 tax.
	fresh2 := sdk.AccAddress([]byte("fresh_recipient_2___"))
	require.NoError(t, v503RunTx(t, a, ctx, banktypes.NewMsgSend(creator, fresh2, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 2)))))
	require.Equal(t, int64(1), a.BankKeeper.GetBalance(ctx, fresh2, "ASL_8").Amount.Int64())

	// MultiSend: outputs include the tax recipient (R3 skip) and the sender itself.
	out1 := sdk.AccAddress([]byte("multi_out_1_________"))
	ms := banktypes.NewMsgMultiSend(banktypes.NewInput(creator, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 3000))), []banktypes.Output{
		banktypes.NewOutput(out1, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1000))),
		banktypes.NewOutput(taxRcpt, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1000))),
		banktypes.NewOutput(creator, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1000))),
	})
	require.NoError(t, v503RunTx(t, a, ctx, ms))
	require.Equal(t, int64(999), a.BankKeeper.GetBalance(ctx, out1, "ASL_8").Amount.Int64())

	// Native STOC transfers are unaffected.
	native := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 5))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", native))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", user, native))
	require.NoError(t, v503RunTx(t, a, ctx, banktypes.NewMsgSend(user, fresh, native)))
}

// Simulate applies the tax, so simulated gas equals FinalizeBlock gas.
func TestV503_SimulateGasIncludesTaxLeg(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 10)
	creator := sdk.AccAddress([]byte("creator_____________"))
	taxRcpt := sdk.AccAddress([]byte("taxrecipient________"))
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, taxRcpt))
	require.NoError(t, a.StocKeeper.SetToken(ctx, r2Token("ASL_8", creator, math.LegacyNewDecWithPrec(1, 3), taxRcpt.String())))
	c := sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 10_000))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, stoctypes.ModuleName, c))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, stoctypes.ModuleName, creator, c))
	to := sdk.AccAddress([]byte("fresh_______________"))
	msg := banktypes.NewMsgSend(creator, to, sdk.NewCoins(sdk.NewInt64Coin("ASL_8", 1000)))

	measure := func(mode sdk.ExecMode, simulate bool) uint64 {
		branch, _ := ctx.CacheContext()
		branch = branch.WithExecMode(mode).WithGasMeter(storetypes.NewGasMeter(10_000_000))
		if simulate {
			branch = branch.WithIsCheckTx(true).WithExecMode(sdk.ExecModeSimulate)
		}
		_, err := bankkeeper.NewMsgServerImpl(a.BankKeeper).Send(branch, msg)
		require.NoError(t, err)
		post := stocante.NewTaxPostDecorator(a.StocKeeper, a.AppCodec())
		_, err = post.PostHandle(branch, v503Tx{msgs: []sdk.Msg{msg}}, simulate, true,
			func(c sdk.Context, _ sdk.Tx, _, _ bool) (sdk.Context, error) { return c, nil })
		require.NoError(t, err)
		return branch.GasMeter().GasConsumed()
	}
	sim := measure(sdk.ExecModeSimulate, true)
	deliver := measure(sdk.ExecModeFinalize, false)
	require.Equal(t, deliver, sim)
}

// The app-level tax recipient check is active in the msg server copy of the
// x/stoc keeper that the MsgServiceRouter dispatches to.
func TestV503_CreateTokenViaRouterRejectsContractTaxRecipient(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 20)
	creator := sdk.AccAddress([]byte("creator_____________"))
	base := authtypes.NewBaseAccountWithAddress(creator)
	require.NoError(t, base.SetPubKey(secp256k1.GenPrivKey().PubKey()))
	base.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
	a.AccountKeeper.SetAccount(ctx, base)
	require.NoError(t, a.StocKeeper.SetTokenCounter(ctx, 1))

	contract := sdk.AccAddress(common.HexToAddress("0x5afe00000000000000000000000000000000cafe").Bytes())
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, contract))
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	hash := crypto.Keccak256Hash(code)
	a.EVMKeeper.SetCode(ctx, hash.Bytes(), code)
	a.EVMKeeper.SetCodeHash(ctx, contract.Bytes(), hash.Bytes())

	mk := func(rcpt sdk.AccAddress) *stoctypes.MsgCreateToken {
		return &stoctypes.MsgCreateToken{
			Creator: creator.String(), Name: "Taxed", Symbol: "TXR",
			InitialSupply: math.NewInt(1_000_000), TotalSupply: math.NewInt(1_000_000), Decimals: 0,
			Logo: "https://example.com/logo.png",
			Tax:  stoctypes.TokenTax{Percent: math.LegacyNewDecWithPrec(1, 3), RecipientAddress: rcpt.String()},
		}
	}
	h := a.MsgServiceRouter().Handler(mk(contract))
	require.NotNil(t, h)
	c1, _ := ctx.CacheContext()
	_, err := h(c1, mk(contract))
	require.ErrorContains(t, err, "EVM contract")

	c2, _ := ctx.CacheContext()
	_, err = h(c2, mk(creator))
	require.NoError(t, err, "an existing keyed wallet is accepted")
}
