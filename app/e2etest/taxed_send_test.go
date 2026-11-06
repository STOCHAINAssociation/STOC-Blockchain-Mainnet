// Package e2etest runs whole-app flows through BaseApp (InitChain, CheckTx,
// Simulate, ProcessProposal, FinalizeBlock). It is a separate package so it
// gets its own test binary: x/vm sets process-global EVM configuration at
// InitGenesis / PreBlock, which only works once per process.
package e2etest

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/evm/contracts"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	feemarkettypes "github.com/cosmos/evm/x/feemarket/types"
	"github.com/cosmos/evm/x/vm/statedb"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibccoretypes "github.com/cosmos/ibc-go/v10/modules/core/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"stoc/app"
	stoctypes "stoc/x/stoc/types"
)

const chainID = "stoc_262144-1"

// End-to-end through BaseApp: a taxed custom-token MsgSend to a FRESH
// recipient must pass CheckTx (where baseapp runs the post handler without
// executing the messages), Simulate (tax included) and FinalizeBlock (tax
// collected). Guards the v5.0.3 round-2 regression where the fail-closed tax
// leg rejected such transfers at mempool admission.
func TestV503_E2E_TaxedSendToFreshRecipient(t *testing.T) {
	opts := simtestutil.AppOptionsMap{flags.FlagHome: t.TempDir(), "evm.evm-chain-id": uint64(262144)}
	a, err := app.New(log.NewNopLogger(), dbm.NewMemDB(), nil, true, opts, baseapp.SetChainID(chainID))
	require.NoError(t, err)

	senderKey := secp256k1.GenPrivKey()
	sender := sdk.AccAddress(senderKey.PubKey().Address())
	taxRcpt := sdk.AccAddress([]byte("taxrecipient________"))
	fresh := sdk.AccAddress([]byte("fresh_recipient_____"))
	const denom = "ASL_8"

	valSet, err := simtestutil.CreateRandomValidatorSet()
	require.NoError(t, err)
	genAccs := []authtypes.GenesisAccount{
		authtypes.NewBaseAccount(sender, nil, 0, 0),
		authtypes.NewBaseAccount(taxRcpt, nil, 1, 0),
	}
	balances := []banktypes.Balance{{
		Address: sender.String(),
		Coins: sdk.NewCoins(
			sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(1_000_000_000_000)),
			sdk.NewInt64Coin(denom, 1_000_000),
		),
	}}
	gs, err := simtestutil.GenesisStateWithValSet(a.AppCodec(), a.DefaultGenesis(), valSet, genAccs, balances...)
	require.NoError(t, err)

	// EVM InitGenesis needs the bond denom metadata (real genesis files have it).
	var bankGS banktypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(gs[banktypes.ModuleName], &bankGS)
	display := sdk.DefaultBondDenom[1:]
	bankGS.DenomMetadata = append(bankGS.DenomMetadata, banktypes.Metadata{
		Base: sdk.DefaultBondDenom, Display: display, Name: display, Symbol: display,
		DenomUnits: []*banktypes.DenomUnit{{Denom: sdk.DefaultBondDenom, Exponent: 0}, {Denom: display, Exponent: 6}},
	})
	gs[banktypes.ModuleName] = a.AppCodec().MustMarshalJSON(&bankGS)

	// Mainnet feemarket values (v5.0.0 handler): 0.001 ustoc/gas.
	var fmGS feemarkettypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(gs[feemarkettypes.ModuleName], &fmGS)
	fmGS.Params.NoBaseFee = false
	fmGS.Params.BaseFee = math.LegacyNewDecWithPrec(1, 3)
	fmGS.Params.MinGasPrice = math.LegacyNewDecWithPrec(1, 3)
	gs[feemarkettypes.ModuleName] = a.AppCodec().MustMarshalJSON(&fmGS)

	// A transfer channel imported from genesis: InitChain must index its
	// escrow (the index is not part of the exported x/stoc genesis).
	var ibcGS ibccoretypes.GenesisState
	a.AppCodec().MustUnmarshalJSON(gs[ibcexported.ModuleName], &ibcGS)
	ibcGS.ChannelGenesis.Channels = []channeltypes.IdentifiedChannel{channeltypes.NewIdentifiedChannel(
		ibctransfertypes.PortID, "channel-0",
		channeltypes.NewChannel(channeltypes.OPEN, channeltypes.UNORDERED,
			channeltypes.NewCounterparty(ibctransfertypes.PortID, "channel-5"), []string{"connection-0"}, ibctransfertypes.V1),
	)}
	ibcGS.ChannelGenesis.NextChannelSequence = 1
	gs[ibcexported.ModuleName] = a.AppCodec().MustMarshalJSON(&ibcGS)

	stocGS := stoctypes.DefaultGenesis()
	stocGS.Tokens = []stoctypes.Token{{
		Id: denom, MinimalDenom: denom, Symbol: "ASL", Name: "ASL", Logo: "https://x/logo.png",
		InitialSupply: math.NewInt(1_000_000), TotalSupply: math.NewInt(1_000_000), RemainingSupply: math.ZeroInt(),
		Tax:     stoctypes.TokenTax{Percent: math.LegacyNewDecWithPrec(1, 1), RecipientAddress: taxRcpt.String()},
		Creator: sender.String(),
	}}
	gs[stoctypes.ModuleName] = a.AppCodec().MustMarshalJSON(stocGS)

	stateBytes, err := json.Marshal(gs)
	require.NoError(t, err)
	_, err = a.InitChain(&abci.RequestInitChain{
		ChainId: chainID, Validators: []abci.ValidatorUpdate{},
		ConsensusParams: simtestutil.DefaultConsensusParams, AppStateBytes: stateBytes, InitialHeight: 1,
	})
	require.NoError(t, err)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	_, err = a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: blockTime, NextValidatorsHash: valSet.Hash()})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)

	qctx, err := a.CreateQueryContext(1, false)
	require.NoError(t, err)
	accNum := a.AccountKeeper.GetAccount(qctx, sender).GetAccountNumber()
	genesisEscrow := ibctransfertypes.GetEscrowAddress(ibctransfertypes.PortID, "channel-0")
	ch, ok := a.StocKeeper.GetIBCEscrowChannel(qctx, genesisEscrow)
	require.True(t, ok, "InitChain indexes transfer channels imported from genesis")
	require.Equal(t, "channel-0", ch)

	msg := banktypes.NewMsgSend(sender, fresh, sdk.NewCoins(sdk.NewInt64Coin(denom, 1000)))
	fee := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, math.NewInt(10_000_000)))
	tx, err := simtestutil.GenSignedMockTx(rand.New(rand.NewSource(1)), a.TxConfig(), []sdk.Msg{msg}, fee, 1_000_000,
		chainID, []uint64{accNum}, []uint64{0}, senderKey)
	require.NoError(t, err)
	bz, err := a.TxConfig().TxEncoder()(tx)
	require.NoError(t, err)

	// Simulate before CheckTx: CheckTx bumps the sender sequence in the check
	// state that simulate runs on.
	_, simRes, err := a.Simulate(bz)
	require.NoError(t, err, "simulate runs the tax leg")
	taxEvent := false
	for _, ev := range simRes.Events {
		if ev.Type == "token_tax_applied" {
			taxEvent = true
		}
	}
	require.True(t, taxEvent, "simulate must include the tax leg")

	check, err := a.CheckTx(&abci.RequestCheckTx{Tx: bz, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, uint32(0), check.Code, "CheckTx must admit a taxed transfer to a fresh recipient: %s", check.Log)

	// Validators verifying a proposal that contains the tx must accept it
	// (baseapp also runs the post handler there without executing messages).
	pp, err := a.ProcessProposal(&abci.RequestProcessProposal{
		Height: 2, Time: blockTime.Add(time.Second), Txs: [][]byte{bz},
		NextValidatorsHash: valSet.Hash(), ProposerAddress: valSet.Proposer.Address,
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, pp.Status)

	res, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 2, Time: blockTime.Add(time.Second), Txs: [][]byte{bz}, NextValidatorsHash: valSet.Hash(), ProposerAddress: valSet.Proposer.Address})
	require.NoError(t, err)
	require.Equal(t, uint32(0), res.TxResults[0].Code, res.TxResults[0].Log)
	_, err = a.Commit()
	require.NoError(t, err)

	qctx, err = a.CreateQueryContext(2, false)
	require.NoError(t, err)
	require.Equal(t, int64(900), a.BankKeeper.GetBalance(qctx, fresh, denom).Amount.Int64())
	require.Equal(t, int64(100), a.BankKeeper.GetBalance(qctx, taxRcpt, denom).Amount.Int64())

	// ---- x/erc20 registration is governance-only (v5.0.3 genesis default) ----
	// Same app instance: x/vm process-global EVM configuration allows a single
	// InitChain per test binary.
	ectx := a.BaseApp.NewUncachedContext(false, cmtproto.Header{
		Height: 3, ChainID: chainID, Time: blockTime.Add(2 * time.Second), ProposerAddress: valSet.Proposer.Address,
	}).WithExecMode(sdk.ExecModeFinalize)
	require.Equal(t, erc20types.NewParams(true, false), a.Erc20Keeper.GetParams(ectx))

	// Deploy a real ERC-20 contract from the sender.
	ctorArgs, err := contracts.ERC20MinterBurnerDecimalsContract.ABI.Pack("", "Test Token", "TST", uint8(6))
	require.NoError(t, err)
	deployer := common.BytesToAddress(sender.Bytes())
	nonce, err := a.AccountKeeper.GetSequence(ectx, sender)
	require.NoError(t, err)
	_, err = a.EVMKeeper.CallEVMWithData(ectx, statedb.New(ectx, a.EVMKeeper, statedb.NewEmptyTxConfig()), deployer, nil,
		append(append([]byte{}, contracts.ERC20MinterBurnerDecimalsContract.Bin...), ctorArgs...), true, false, nil)
	require.NoError(t, err)
	token := crypto.CreateAddress(deployer, nonce)
	require.True(t, a.EVMKeeper.IsContract(ectx, token))

	register := func(signer string) error {
		msg := &erc20types.MsgRegisterERC20{Signer: signer, Erc20Addresses: []string{token.Hex()}}
		h := a.MsgServiceRouter().Handler(msg)
		require.NotNil(t, h)
		_, err := h(ectx, msg)
		return err
	}
	err = register(sender.String())
	require.ErrorIs(t, err, govtypes.ErrInvalidSigner, "a non-gov signer cannot register an ERC-20")
	require.False(t, a.Erc20Keeper.IsERC20Registered(ectx, token))

	require.NoError(t, register(authtypes.NewModuleAddress(govtypes.ModuleName).String()), "gov registers the ERC-20")
	require.True(t, a.Erc20Keeper.IsERC20Registered(ectx, token))
}
