package app

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/group"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	icatypes "github.com/cosmos/ibc-go/v10/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	channelv2types "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	stocante "stoc/x/stoc/ante"
	stoctypes "stoc/x/stoc/types"
)

// Regression tests for the v5.0.3 audit round 1 fixes (runtime rules). The
// round-1 PoCs (escrow cache poisoning, silent tax skip, pre-funded escrow)
// are adapted here to assert the fixed behaviour.

const v503ChainID = "stoc_262144-1"

const v503Denom = "XCC_0"

func v503App(t testing.TB) *App {
	t.Helper()
	opts := simtestutil.AppOptionsMap{flags.FlagHome: t.TempDir(), "evm.evm-chain-id": uint64(262144)}
	a, err := New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, opts, baseapp.SetChainID(v503ChainID))
	require.NoError(t, err)
	// Minimal genesis-equivalent state used by the tests (no InitChain).
	ctx := v503Ctx(a, 1)
	require.NoError(t, a.BankKeeper.SetParams(ctx, banktypes.DefaultParams()))
	a.TransferKeeper.SetPort(ctx, ibctransfertypes.PortID)
	return a
}

// v503Ctx is a non-Finalize context (exec mode CheckTx, like any query or
// trace context) writing straight to the app store.
func v503Ctx(a *App, height int64) sdk.Context {
	return a.BaseApp.NewUncachedContext(false, cmtproto.Header{Height: height, ChainID: v503ChainID})
}

// v503Finalize is the same context in FinalizeBlock exec mode.
func v503Finalize(a *App, height int64) sdk.Context {
	return v503Ctx(a, height).WithExecMode(sdk.ExecModeFinalize)
}

func v503Token(creator sdk.AccAddress, taxPct math.LegacyDec, taxRecipient string) stoctypes.Token {
	return stoctypes.Token{
		Id: v503Denom, MinimalDenom: v503Denom, Symbol: "XCC", Name: "X", Logo: "https://x/logo.png", Decimals: 0,
		InitialSupply: math.NewInt(1_000_000), TotalSupply: math.NewInt(1_000_000), RemainingSupply: math.ZeroInt(),
		Tax:     stoctypes.TokenTax{Percent: taxPct, RecipientAddress: taxRecipient},
		Creator: creator.String(),
	}
}

// v503Fund mints custom tokens through the x/stoc module (the real issuance
// path) to a wallet.
func v503Fund(t testing.TB, a *App, ctx sdk.Context, to sdk.AccAddress, amt int64) {
	t.Helper()
	c := sdk.NewCoins(sdk.NewInt64Coin(v503Denom, amt))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, stoctypes.ModuleName, c))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, stoctypes.ModuleName, to, c))
}

func v503Coins(amt int64) sdk.Coins { return sdk.NewCoins(sdk.NewInt64Coin(v503Denom, amt)) }

// v503Setup returns an app with a custom token and a funded user wallet.
func v503Setup(t testing.TB) (*App, sdk.AccAddress) {
	t.Helper()
	a := v503App(t)
	user := sdk.AccAddress([]byte("user________________"))
	ctx := v503Ctx(a, 10)
	require.NoError(t, a.StocKeeper.SetToken(ctx, v503Token(user, math.LegacyZeroDec(), "")))
	v503Fund(t, a, ctx, user, 1000)
	return a, user
}

func v503SetTransferChannel(a *App, ctx sdk.Context, channelID string, state channeltypes.State) {
	ch := channeltypes.NewChannel(state, channeltypes.UNORDERED,
		channeltypes.NewCounterparty(ibctransfertypes.PortID, "channel-0"), []string{"connection-0"}, ibctransfertypes.V1)
	a.IBCKeeper.ChannelKeeper.SetChannel(ctx, ibctransfertypes.PortID, channelID, ch)
}

// v503OpenTransferChannel runs what core IBC does for MsgChannelOpenInit
// (try=false) or MsgChannelOpenTry: the transfer route callback (our guard is
// the outermost middleware), then the channel write.
func v503OpenTransferChannel(t *testing.T, a *App, ctx sdk.Context, channelID string, try bool) {
	t.Helper()
	route, ok := a.IBCKeeper.PortKeeper.Route(ibctransfertypes.ModuleName)
	require.True(t, ok)
	state := channeltypes.INIT
	var err error
	if try {
		state = channeltypes.TRYOPEN
		_, err = route.OnChanOpenTry(ctx, channeltypes.UNORDERED, []string{"connection-0"}, ibctransfertypes.PortID, channelID,
			channeltypes.NewCounterparty(ibctransfertypes.PortID, "channel-99"), ibctransfertypes.V1)
	} else {
		_, err = route.OnChanOpenInit(ctx, channeltypes.UNORDERED, []string{"connection-0"}, ibctransfertypes.PortID, channelID,
			channeltypes.NewCounterparty(ibctransfertypes.PortID, ""), ibctransfertypes.V1)
	}
	require.NoError(t, err)
	v503SetTransferChannel(a, ctx, channelID, state)
}

// ---------------------------------------------------------------------------
// FIX-1: module-account squatting
// ---------------------------------------------------------------------------

func TestV503_SendToTransferAndICAModuleAddressRejected(t *testing.T) {
	a, user := v503Setup(t)
	ctx := v503Finalize(a, 11)
	native := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", native))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", user, native))

	msgServer := bankkeeper.NewMsgServerImpl(a.BankKeeper)
	for _, name := range []string{ibctransfertypes.ModuleName, icatypes.ModuleName} {
		addr := authtypes.NewModuleAddress(name)
		require.True(t, a.BankKeeper.BlockedAddr(addr), "%s must be blocked", name)

		_, err := msgServer.Send(ctx, banktypes.NewMsgSend(user, addr, native))
		require.ErrorContains(t, err, "not allowed to receive funds", "native MsgSend to %s", name)

		_, err = msgServer.Send(ctx, banktypes.NewMsgSend(user, addr, v503Coins(5)))
		require.ErrorContains(t, err, "not allowed to receive funds", "custom MsgSend to %s", name)

		// Keeper-level sends (gov/group/ICA execution paths) of custom tokens
		// are rejected by the SendRestriction even though no account exists.
		require.Nil(t, a.AccountKeeper.GetAccount(ctx, addr))
		require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, user, addr, v503Coins(5)), "module account ("+name+")")
		require.Nil(t, a.AccountKeeper.GetAccount(ctx, addr), "no account may be created at %s", name)
	}
}

// ---------------------------------------------------------------------------
// FIX-4: tax fail-closed (adapted PoC TestPOC_TaxSilentlySkipped)
// ---------------------------------------------------------------------------

type v503Tx struct{ msgs []sdk.Msg }

func (t v503Tx) GetMsgs() []sdk.Msg                    { return t.msgs }
func (t v503Tx) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

// v503RunTx mimics baseapp runTx in FinalizeBlock for a bank tx: messages and
// the tax post handler share one cache branch, written only if both succeed.
func v503RunTx(t *testing.T, a *App, ctx sdk.Context, msgs ...sdk.Msg) error {
	t.Helper()
	runCtx, write := ctx.CacheContext()
	srv := bankkeeper.NewMsgServerImpl(a.BankKeeper)
	for _, m := range msgs {
		var err error
		switch mm := m.(type) {
		case *banktypes.MsgSend:
			_, err = srv.Send(runCtx, mm)
		case *banktypes.MsgMultiSend:
			_, err = srv.MultiSend(runCtx, mm)
		default:
			t.Fatalf("unsupported msg %T", m)
		}
		if err != nil {
			return err
		}
	}
	post := stocante.NewTaxPostDecorator(a.StocKeeper, a.AppCodec())
	if _, err := post.PostHandle(runCtx, v503Tx{msgs: msgs}, false, true,
		func(c sdk.Context, _ sdk.Tx, _, _ bool) (sdk.Context, error) { return c, nil }); err != nil {
		return err
	}
	write()
	return nil
}

func v503TaxedTransfer(t *testing.T, lockRecipient bool) (sdk.AccAddress, sdk.AccAddress, *App, sdk.Context, error) {
	t.Helper()
	a := v503App(t)
	sender := sdk.AccAddress([]byte("A___________________"))
	recipient := sdk.AccAddress([]byte("B___________________"))
	taxRecipient := sdk.AccAddress([]byte("taxrecipient________"))
	ctx := v503Finalize(a, 10)
	require.NoError(t, a.StocKeeper.SetToken(ctx, v503Token(sender, math.LegacyNewDecWithPrec(1, 1), taxRecipient.String())))
	if lockRecipient {
		base := authtypes.NewBaseAccountWithAddress(taxRecipient)
		base.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
		acc, err := vestingtypes.NewPermanentLockedAccount(base, sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1)))
		require.NoError(t, err)
		a.AccountKeeper.SetAccount(ctx, acc)
	} else {
		a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, taxRecipient))
	}
	v503Fund(t, a, ctx, sender, 100)

	amt := v503Coins(100)
	require.NoError(t, a.BankKeeper.SendCoins(ctx, sender, recipient, amt))
	post := stocante.NewTaxPostDecorator(a.StocKeeper, a.AppCodec())
	_, err := post.PostHandle(ctx, v503Tx{msgs: []sdk.Msg{banktypes.NewMsgSend(sender, recipient, amt)}}, false, true,
		func(c sdk.Context, _ sdk.Tx, _, _ bool) (sdk.Context, error) { return c, nil })
	return recipient, taxRecipient, a, ctx, err
}

func TestV503_TaxedTransferUnchanged(t *testing.T) {
	recipient, taxRecipient, a, ctx, err := v503TaxedTransfer(t, false)
	require.NoError(t, err)
	require.Equal(t, int64(90), a.BankKeeper.GetBalance(ctx, recipient, v503Denom).Amount.Int64())
	require.Equal(t, int64(10), a.BankKeeper.GetBalance(ctx, taxRecipient, v503Denom).Amount.Int64())
}

func TestV503_TaxRecipientLockedTransferFails(t *testing.T) {
	_, _, _, _, err := v503TaxedTransfer(t, true)
	require.Error(t, err, "a transfer whose tax cannot be collected must fail, not complete untaxed")
	require.ErrorContains(t, err, "tax enforcement failed")
}

// ---------------------------------------------------------------------------
// FIX-5: ICS-20 escrow index (adapted PoC TestPOC_EscrowCachePoison)
// ---------------------------------------------------------------------------

func escrowOf(channelID string) sdk.AccAddress {
	return ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, channelID)
}

// A transfer channel created in a block (Init or Try) is blocked for custom
// tokens from the next message on, in the same block, in both directions.
func TestV503_EscrowIndex_SameBlockChannelBlocked(t *testing.T) {
	a, user := v503Setup(t)
	ctx := v503Finalize(a, 30)

	v503OpenTransferChannel(t, a, ctx, "channel-7", false)
	require.True(t, a.StocKeeper.IsIBCEscrow(ctx, escrowOf("channel-7")))
	ch, ok := a.StocKeeper.GetIBCEscrowChannel(ctx, escrowOf("channel-7"))
	require.True(t, ok)
	require.Equal(t, "channel-7", ch)
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, user, escrowOf("channel-7"), v503Coins(5)), "IBC transfer of custom token")

	v503OpenTransferChannel(t, a, ctx, "channel-8", true)
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, user, escrowOf("channel-8"), v503Coins(5)), "IBC transfer of custom token")

	// Inbound credit from an indexed escrow is rejected too.
	rawSetBalance(t, a, ctx, escrowOf("channel-7"), v503Denom, 3)
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, escrowOf("channel-7"), user, v503Coins(1)), "IBC inbound credit")

	// Native denoms are unaffected.
	native := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", native))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", user, native))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, user, escrowOf("channel-7"), native))
}

// Query, simulate and trace contexts run on branches that are never
// committed: whatever they execute (a forged block height, a channel open)
// cannot change what FinalizeBlock sees. A channel open in a tx that fails
// leaves no entry either.
func TestV503_EscrowIndex_UncommittedBranchesHaveNoEffect(t *testing.T) {
	a, user := v503Setup(t)
	user2 := sdk.AccAddress([]byte("user2_______________"))

	for _, mode := range []sdk.ExecMode{sdk.ExecModeCheck, sdk.ExecModeSimulate, sdk.ExecModeFinalize} {
		branch, _ := v503Ctx(a, 1_000_000_000_000).WithExecMode(mode).CacheContext() // never written
		v503OpenTransferChannel(t, a, branch, "channel-3", false)
		require.NoError(t, a.BankKeeper.SendCoins(branch, user, user2, v503Coins(1)))
	}

	ctx := v503Finalize(a, 31)
	require.False(t, a.StocKeeper.IsIBCEscrow(ctx, escrowOf("channel-3")))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, user, escrowOf("channel-3"), v503Coins(1)),
		"no channel exists: the address is not an escrow")
}

// Pre-funded escrow (adapted PoC TestPOC_PrefundFutureEscrow, audit F4). The
// channel open is NOT rejected (that would let anyone block every future
// transfer channel id with one unit of a custom token); the pre-funded tokens
// stay frozen instead.
func TestV503_PrefundedEscrowFrozen(t *testing.T) {
	a, user := v503Setup(t)
	escrow := escrowOf("channel-9")

	// Before the channel exists the escrow is an ordinary-looking address.
	require.NoError(t, a.BankKeeper.SendCoins(v503Finalize(a, 11), user, escrow, v503Coins(5)))

	ctx := v503Finalize(a, 12)
	v503OpenTransferChannel(t, a, ctx, "channel-9", false)

	// Frozen: nothing more can go in, nothing can come out.
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, user, escrow, v503Coins(1)), "IBC transfer of custom token")
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, escrow, user, v503Coins(1)), "IBC inbound credit")
	require.Equal(t, int64(5), a.BankKeeper.GetBalance(ctx, escrow, v503Denom).Amount.Int64())
}

// customSendGas returns the gas a keeper-level custom-token send consumes in
// a fresh branch of ctx.
func customSendGas(t testing.TB, a *App, ctx sdk.Context, from, to sdk.AccAddress) uint64 {
	branch, _ := ctx.CacheContext()
	branch = branch.WithGasMeter(storetypes.NewGasMeter(10_000_000))
	require.NoError(t, a.BankKeeper.SendCoins(branch, from, to, v503Coins(1)))
	return branch.GasMeter().GasConsumed()
}

// indexChannels creates n transfer channels with their escrow index entries
// (what n channel handshakes leave in state).
func indexChannels(a *App, ctx sdk.Context, n int) {
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("channel-%d", i)
		v503SetTransferChannel(a, ctx, id, channeltypes.OPEN)
		a.StocKeeper.SetIBCEscrow(ctx, escrowOf(id), id)
	}
}

// The escrow check is O(1): gas of a custom-token send does not depend on
// the number of transfer channels, nor on the exec mode.
func TestV503_EscrowIndex_SendGasIndependentOfChannelCount(t *testing.T) {
	user2 := sdk.AccAddress([]byte("user2_______________"))
	measure := func(channels int) map[sdk.ExecMode]uint64 {
		a, user := v503Setup(t)
		indexChannels(a, v503Ctx(a, 10), channels)
		require.NoError(t, a.BankKeeper.SendCoins(v503Finalize(a, 11), user, user2, v503Coins(10))) // create user2
		out := map[sdk.ExecMode]uint64{}
		for _, mode := range []sdk.ExecMode{sdk.ExecModeFinalize, sdk.ExecModeSimulate, sdk.ExecModeCheck} {
			out[mode] = customSendGas(t, a, v503Ctx(a, 12).WithExecMode(mode), user, user2)
		}
		return out
	}
	none, many := measure(0), measure(5_000)
	require.Equal(t, none, many, "gas with 0 vs 5,000 transfer channels")
	require.Equal(t, none[sdk.ExecModeFinalize], none[sdk.ExecModeSimulate])
	require.Equal(t, none[sdk.ExecModeFinalize], none[sdk.ExecModeCheck])
}

// Wall time of a custom-token send is flat in the number of channels:
//
//	go test ./app/ -run '^$' -bench BenchmarkV503_CustomTokenSend -benchtime 2000x
func BenchmarkV503_CustomTokenSend(b *testing.B) {
	for _, channels := range []int{0, 5_000} {
		b.Run(fmt.Sprintf("channels=%d", channels), func(b *testing.B) {
			a, user := v503Setup(b)
			indexChannels(a, v503Ctx(a, 10), channels)
			user2 := sdk.AccAddress([]byte("user2_______________"))
			ctx := v503Finalize(a, 11)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				branch, _ := ctx.CacheContext()
				if err := a.BankKeeper.SendCoins(branch, user, user2, v503Coins(1)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// The index has no in-memory state: concurrent FinalizeBlock-, CheckTx- and
// simulate-mode readers and writers on their own branches (run with -race).
func TestV503_EscrowIndex_Concurrent(t *testing.T) {
	a, user := v503Setup(t)
	indexChannels(a, v503Ctx(a, 10), 3)
	a.CommitMultiStore().Commit()
	version := a.CommitMultiStore().LastCommitID().Version
	user2 := sdk.AccAddress([]byte("user2_______________"))
	route, ok := a.IBCKeeper.PortKeeper.Route(ibctransfertypes.ModuleName)
	require.True(t, ok)

	var wg sync.WaitGroup
	for g, mode := range []sdk.ExecMode{sdk.ExecModeFinalize, sdk.ExecModeCheck, sdk.ExecModeSimulate, sdk.ExecModeSimulate} {
		cms, err := a.CommitMultiStore().CacheMultiStoreWithVersion(version)
		require.NoError(t, err)
		ctx := sdk.NewContext(cms, cmtproto.Header{Height: 11, ChainID: v503ChainID}, false, log.NewNopLogger()).WithExecMode(mode)
		wg.Add(1)
		go func(g int, ctx sdk.Context) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if err := a.BankKeeper.SendCoins(ctx, user, escrowOf("channel-1"), v503Coins(1)); err == nil {
					t.Errorf("goroutine %d: send to an indexed escrow must fail", g)
				}
				if err := a.BankKeeper.SendCoins(ctx, user, user2, v503Coins(1)); err != nil {
					t.Errorf("goroutine %d: %v", g, err)
				}
				id := fmt.Sprintf("channel-%d", 100+g*100+i)
				if _, err := route.OnChanOpenInit(ctx, channeltypes.UNORDERED, []string{"connection-0"}, ibctransfertypes.PortID, id,
					channeltypes.NewCounterparty(ibctransfertypes.PortID, ""), ibctransfertypes.V1); err != nil {
					t.Errorf("goroutine %d: %v", g, err)
				}
				if !a.StocKeeper.IsIBCEscrow(ctx, escrowOf(id)) {
					t.Errorf("goroutine %d: %s not indexed in its own branch", g, id)
				}
			}
		}(g, ctx)
	}
	wg.Wait()
}

func TestV503_TraceBlockHeightBounded(t *testing.T) {
	a := v503App(t)
	ctx := v503Ctx(a, 10)
	_, err := a.EVMKeeper.TraceBlock(ctx, &evmtypes.QueryTraceBlockRequest{BlockNumber: 1_000_000_000_000})
	require.ErrorContains(t, err, "requested height [1000000000000] must be less than or equal to current height [10]")
}

// ---------------------------------------------------------------------------
// FIX-6: group policies and EVM contracts cannot hold custom tokens
// ---------------------------------------------------------------------------

func TestV503_CustomTokenToGroupPolicyRejected(t *testing.T) {
	a, user := v503Setup(t)
	ctx := v503Finalize(a, 11).WithBlockTime(time.Unix(1_700_000_000, 0))

	msg, err := group.NewMsgCreateGroupWithPolicy(user.String(),
		[]group.MemberRequest{{Address: user.String(), Weight: "1"}}, "", "", false,
		group.NewThresholdDecisionPolicy("1", time.Hour, 0))
	require.NoError(t, err)
	res, err := a.GroupKeeper.CreateGroupWithPolicy(ctx, msg)
	require.NoError(t, err)
	policy := sdk.MustAccAddressFromBech32(res.GroupPolicyAddress)

	require.True(t, a.customTokenWallets.isGroupPolicy(ctx, policy))
	require.Error(t, a.BankKeeper.SendCoins(ctx, user, policy, v503Coins(5)))

	// Native STOC to a group policy is unaffected.
	native := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 1))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", native))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", user, native))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, user, policy, native))
}

func TestV503_CustomTokenToEVMContractRejected(t *testing.T) {
	a, user := v503Setup(t)
	ctx := v503Finalize(a, 11)

	setCode := func(addr sdk.AccAddress, code []byte) {
		a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, addr))
		hash := crypto.Keccak256Hash(code)
		a.EVMKeeper.SetCode(ctx, hash.Bytes(), code)
		a.EVMKeeper.SetCodeHash(ctx, addr.Bytes(), hash.Bytes())
	}

	contract := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000c0ffee01").Bytes())
	setCode(contract, []byte{0x60, 0x00, 0x60, 0x00, 0xf3}) // PUSH1 0 PUSH1 0 RETURN
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, user, contract, v503Coins(5)), "EVM contract")

	// Plain 0x EOA (account exists, no code): allowed.
	eoa := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000000e0a01").Bytes())
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, eoa))
	require.NoError(t, a.BankKeeper.SendCoins(ctx, user, eoa, v503Coins(5)))

	// EIP-7702 delegated EOA still has a key: allowed.
	delegated := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000000e0a02").Bytes())
	designator := append([]byte{0xef, 0x01, 0x00}, common.HexToAddress("0x00000000000000000000000000000000c0ffee01").Bytes()...)
	setCode(delegated, designator)
	require.NoError(t, a.BankKeeper.SendCoins(ctx, user, delegated, v503Coins(5)))

	// A key-controlled wallet is never treated as a contract.
	keyed := sdk.AccAddress([]byte("keyed_______________"))
	base := authtypes.NewBaseAccountWithAddress(keyed)
	require.NoError(t, base.SetPubKey(secp256k1.GenPrivKey().PubKey()))
	base.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
	a.AccountKeeper.SetAccount(ctx, base)
	require.NoError(t, a.BankKeeper.SendCoins(ctx, user, keyed, v503Coins(5)))
}

// The app-level tax recipient check is wired into MsgCreateToken.
func TestV503_CreateTokenTaxRecipientAppCheckWired(t *testing.T) {
	a, _ := v503Setup(t)
	ctx := v503Finalize(a, 11)
	for _, name := range []string{ibctransfertypes.ModuleName, icatypes.ModuleName, "gov"} {
		require.Error(t, a.customTokenWallets.checkTaxRecipient(ctx, authtypes.NewModuleAddress(name)), name)
	}
	contract := sdk.AccAddress(common.HexToAddress("0x00000000000000000000000000000000c0ffee02").Bytes())
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, contract))
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	hash := crypto.Keccak256Hash(code)
	a.EVMKeeper.SetCode(ctx, hash.Bytes(), code)
	a.EVMKeeper.SetCodeHash(ctx, contract.Bytes(), hash.Bytes())
	require.ErrorContains(t, a.customTokenWallets.checkTaxRecipient(ctx, contract), "EVM contract")
}

// ---------------------------------------------------------------------------
// FIX-8: IBC v2 fails cleanly
// ---------------------------------------------------------------------------

// A v2 MsgSendPacket that bypasses the ante (gov / group execution) fails
// with an error, not a nil-router panic, because no counterparty can be
// registered (MsgRegisterCounterparty is rejected in the ante).
func TestV503_IBCV2SendPacketFailsCleanly(t *testing.T) {
	a, user := v503Setup(t)
	ctx := v503Finalize(a, 11)
	msg := channelv2types.NewMsgSendPacket("07-tendermint-0", uint64(time.Now().Add(time.Hour).Unix()), user.String(),
		channelv2types.NewPayload(ibctransfertypes.PortID, ibctransfertypes.PortID, ibctransfertypes.V1, ibctransfertypes.EncodingJSON, []byte("{}")))
	handler := a.MsgServiceRouter().Handler(msg)
	require.NotNil(t, handler)
	require.NotPanics(t, func() {
		_, err := handler(ctx, msg)
		require.Error(t, err)
	})
}
