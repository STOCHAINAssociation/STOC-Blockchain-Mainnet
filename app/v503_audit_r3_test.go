package app

import (
	"fmt"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	connectiontypes "github.com/cosmos/ibc-go/v10/modules/core/03-connection/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	commitmenttypes "github.com/cosmos/ibc-go/v10/modules/core/23-commitment/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibctm "github.com/cosmos/ibc-go/v10/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"

	stoctypes "stoc/x/stoc/types"
)

// Regression tests adapted from the v5.0.3 audit round 3 overlay.

// openGasWithPrefundedEscrow returns the gas of the transfer route's
// OnChanOpenInit for channel-0 after n denoms were pre-funded into its escrow.
func openGasWithPrefundedEscrow(t *testing.T, n int, custom bool) uint64 {
	t.Helper()
	a, _ := v503Setup(t)
	ctx := v503Finalize(a, 20)
	escrow := escrowOf("channel-0")
	for i := 0; i < n; i++ {
		d := fmt.Sprintf("zzz%06d", i)
		if custom {
			d = fmt.Sprintf("Z%06d_%d", i, i+1)
			tk := r2Token(d, sdk.AccAddress([]byte("creator_____________")), math.LegacyZeroDec(), "")
			tk.Symbol = fmt.Sprintf("Z%06d", i)
			require.NoError(t, a.StocKeeper.SetToken(ctx, tk))
		}
		rawSetBalance(t, a, ctx, escrow, d, 2)
	}
	route, ok := a.IBCKeeper.PortKeeper.Route(ibctransfertypes.ModuleName)
	require.True(t, ok)
	branch, _ := ctx.CacheContext()
	branch = branch.WithGasMeter(storetypes.NewGasMeter(200_000_000))
	_, err := route.OnChanOpenInit(branch, channeltypes.UNORDERED, []string{"connection-0"}, ibctransfertypes.PortID, "channel-0",
		channeltypes.NewCounterparty(ibctransfertypes.PortID, ""), ibctransfertypes.V1)
	require.NoError(t, err)
	require.True(t, a.StocKeeper.IsIBCEscrow(branch, escrow))
	return branch.GasMeter().GasConsumed()
}

// Audit round 3 (MED): the channel open must cost the same whatever the
// escrow holds. A cost growing with pre-funded denoms let anyone push the
// open of the next channel id over the block gas cap; the failed open does
// not consume the id, so that channel id would be blocked forever.
func TestV503_ChannelOpenGasIndependentOfPrefundedEscrow(t *testing.T) {
	empty := openGasWithPrefundedEscrow(t, 0, false)
	require.Equal(t, empty, openGasWithPrefundedEscrow(t, 1_000, true), "1,000 pre-funded custom denoms")
	require.Equal(t, empty, openGasWithPrefundedEscrow(t, 1_000, false), "1,000 pre-funded other denoms")
}

// Audit round 3 (LOW): an IBC escrow or an EVM precompile address cannot be a
// tax recipient (every taxed transfer would fail).
func TestV503_CreateTokenRejectsEscrowAndPrecompileTaxRecipient(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 20)
	creator := sdk.AccAddress([]byte("creator_____________"))
	base := authtypes.NewBaseAccountWithAddress(creator)
	require.NoError(t, base.SetPubKey(secp256k1.GenPrivKey().PubKey()))
	base.AccountNumber = a.AccountKeeper.NextAccountNumber(ctx)
	a.AccountKeeper.SetAccount(ctx, base)
	require.NoError(t, a.StocKeeper.SetTokenCounter(ctx, 1))

	// channel-0 exists and its escrow received native STOC once (account exists).
	v503OpenTransferChannel(t, a, ctx, "channel-0", false)
	escrow := escrowOf("channel-0")
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, escrow))

	// An account at an EVM precompile address (e.g. touched by an EVM tx).
	var precompile sdk.AccAddress
	for bech := range a.blockedPrecompileAddrs {
		precompile = sdk.MustAccAddressFromBech32(bech)
		break
	}
	require.NotNil(t, precompile)
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, precompile))

	mk := func(rcpt sdk.AccAddress) *stoctypes.MsgCreateToken {
		return &stoctypes.MsgCreateToken{
			Creator: creator.String(), Name: "Taxed", Symbol: "TXE",
			InitialSupply: math.NewInt(1_000_000), TotalSupply: math.NewInt(1_000_000), Decimals: 0,
			Logo: "https://example.com/logo.png",
			Tax:  stoctypes.TokenTax{Percent: math.LegacyNewDecWithPrec(1, 2), RecipientAddress: rcpt.String()},
		}
	}
	h := a.MsgServiceRouter().Handler(mk(escrow))
	require.NotNil(t, h)
	for rcpt, want := range map[string]string{escrow.String(): "IBC transfer escrow", precompile.String(): "EVM precompile"} {
		c, _ := ctx.CacheContext()
		_, err := h(c, mk(sdk.MustAccAddressFromBech32(rcpt)))
		require.ErrorIs(t, err, stoctypes.ErrInvalidTaxRecipient, rcpt)
		require.ErrorContains(t, err, want, rcpt)
	}
}

// The v5.0.3 pre-flight reports an existing taxed token whose recipient is a
// transfer escrow (indexed by the same handler).
func TestV503Upgrade_TaxRecipientIsEscrowReported(t *testing.T) {
	a := v503App(t)
	ctx := v503Finalize(a, 100)
	v503SetTransferChannel(a, ctx, "channel-0", channeltypes.OPEN) // pre-v5.0.3 channel, not indexed yet
	escrow := escrowOf("channel-0")
	a.AccountKeeper.SetAccount(ctx, a.AccountKeeper.NewAccountWithAddress(ctx, escrow))
	require.NoError(t, a.StocKeeper.SetToken(ctx, r2Token("ESC_1", sdk.AccAddress([]byte("creator_____________")),
		math.LegacyNewDecWithPrec(1, 2), escrow.String())))

	require.NoError(t, runV503Handler(t, a, ctx))
	require.Equal(t, map[string]string{"ESC_1": escrow.String()}, notWalletEvents(ctx))
}

// Ports that the port router resolves to the transfer stack but that are not
// the transfer port, and channel opens rejected by the transfer module, leave
// no index entry.
func TestV503_NonTransferPortAndFailedOpenNotIndexed(t *testing.T) {
	a, _ := v503Setup(t)
	ctx := v503Finalize(a, 20)
	for _, port := range []string{"xtransfer", "transfer-2", "evil.transfer"} {
		route, ok := a.IBCKeeper.PortKeeper.Route(port)
		require.True(t, ok, "the port router resolves %q to the transfer stack", port)
		_, err := route.OnChanOpenInit(ctx, channeltypes.UNORDERED, []string{"connection-0"}, port, "channel-5",
			channeltypes.NewCounterparty(ibctransfertypes.PortID, ""), ibctransfertypes.V1)
		require.ErrorContains(t, err, "invalid port", port)
		require.False(t, a.StocKeeper.IsIBCEscrow(ctx, ibctransfertypes.GetEscrowAddress(port, "channel-5")), port)
	}
	route, _ := a.IBCKeeper.PortKeeper.Route(ibctransfertypes.PortID)
	_, err := route.OnChanOpenInit(ctx, channeltypes.ORDERED, []string{"connection-0"}, ibctransfertypes.PortID, "channel-6",
		channeltypes.NewCounterparty(ibctransfertypes.PortID, ""), ibctransfertypes.V1)
	require.Error(t, err, "transfer channels must be unordered")
	require.False(t, a.StocKeeper.IsIBCEscrow(ctx, escrowOf("channel-6")))
}

// End to end through the real core IBC msg server: MsgChannelOpenInit on the
// transfer port (core handshake checks, channel id allocation, our guard,
// channel write), then a custom-token send to the new escrow is rejected and
// native STOC to it is unaffected.
func TestV503_CoreMsgChannelOpenInitIndexesEscrow(t *testing.T) {
	a, user := v503Setup(t)
	now := time.Unix(1_800_000_000, 0).UTC()
	ctx := v503Finalize(a, 20).WithBlockTime(now)

	// Minimal IBC state: an active 07-tendermint client and an OPEN
	// connection on it.
	a.IBCKeeper.ClientKeeper.SetParams(ctx, clienttypes.NewParams(ibcexported.Tendermint))
	a.IBCKeeper.ChannelKeeper.SetNextChannelSequence(ctx, 0)
	const clientID = "07-tendermint-0"
	height := clienttypes.NewHeight(1, 10)
	cs := ibctm.NewClientState("counterparty-1", ibctm.DefaultTrustLevel, 14*24*time.Hour, 21*24*time.Hour, 10*time.Second,
		height, commitmenttypes.GetSDKSpecs(), []string{"upgrade", "upgradedIBCState"})
	a.IBCKeeper.ClientKeeper.SetClientState(ctx, clientID, cs)
	a.IBCKeeper.ClientKeeper.SetClientConsensusState(ctx, clientID, height,
		ibctm.NewConsensusState(now, commitmenttypes.NewMerkleRoot([]byte("root")), []byte("0123456789abcdef0123456789abcdef")))
	require.Equal(t, ibcexported.Active, a.IBCKeeper.ClientKeeper.GetClientStatus(ctx, clientID))
	a.IBCKeeper.ConnectionKeeper.SetConnection(ctx, "connection-0", connectiontypes.NewConnectionEnd(connectiontypes.OPEN, clientID,
		connectiontypes.NewCounterparty("07-tendermint-0", "connection-0", commitmenttypes.NewMerklePrefix([]byte("ibc"))),
		connectiontypes.GetCompatibleVersions(), 0))

	relayer := sdk.AccAddress([]byte("relayer_____________"))
	msg := channeltypes.NewMsgChannelOpenInit(ibctransfertypes.PortID, ibctransfertypes.V1, channeltypes.UNORDERED,
		[]string{"connection-0"}, ibctransfertypes.PortID, relayer.String())
	require.NoError(t, msg.ValidateBasic())
	handler := a.MsgServiceRouter().Handler(msg)
	require.NotNil(t, handler)
	res, err := handler(ctx, msg)
	require.NoError(t, err)
	var resp channeltypes.MsgChannelOpenInitResponse
	require.NoError(t, resp.Unmarshal(res.MsgResponses[0].Value))
	require.Equal(t, "channel-0", resp.ChannelId)
	ch, found := a.IBCKeeper.ChannelKeeper.GetChannel(ctx, ibctransfertypes.PortID, resp.ChannelId)
	require.True(t, found)
	require.Equal(t, channeltypes.INIT, ch.State)

	escrow := escrowOf(resp.ChannelId)
	require.True(t, a.StocKeeper.IsIBCEscrow(ctx, escrow))

	// Custom token to the new escrow: rejected, both via MsgSend and at the keeper.
	bankSrv := bankkeeper.NewMsgServerImpl(a.BankKeeper)
	_, err = bankSrv.Send(ctx, banktypes.NewMsgSend(user, escrow, v503Coins(5)))
	require.ErrorContains(t, err, "IBC transfer of custom token")
	require.ErrorContains(t, a.BankKeeper.SendCoins(ctx, user, escrow, v503Coins(5)), "IBC transfer of custom token")

	// Native STOC to the escrow is unaffected.
	native := sdk.NewCoins(sdk.NewInt64Coin(sdk.DefaultBondDenom, 7))
	require.NoError(t, a.BankKeeper.MintCoins(ctx, "mint", native))
	require.NoError(t, a.BankKeeper.SendCoinsFromModuleToAccount(ctx, "mint", user, native))
	_, err = bankSrv.Send(ctx, banktypes.NewMsgSend(user, escrow, native))
	require.NoError(t, err)
	require.Equal(t, int64(7), a.BankKeeper.GetBalance(ctx, escrow, sdk.DefaultBondDenom).Amount.Int64())
}
