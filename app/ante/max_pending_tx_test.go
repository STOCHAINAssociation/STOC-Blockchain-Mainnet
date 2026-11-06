package ante_test

import (
	"math/big"
	"testing"

	"cosmossdk.io/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	stocante "stoc/app/ante"
)

// SA-AUDIT-2026-06-06 HIGH-1 regression coverage: per-wallet EVM mempool quota
// must be bound to the SIGNATURE-verified sender, not the unverified
// MsgEthereumTx.From wire field. Without the inline VerifySender, an attacker
// can rotate a fake From per tx so each tx's quota slot is counted against a
// fresh, unused address, defeating the rate limit.

// stocChainID is the STOChain EVM chain ID used for signer construction in tests.
const stocChainID = uint64(13061999)

// initChainConfig initializes the evm types chainConfig package var. SetChainConfig
// errors if already set, so we ignore that — idempotent across tests.
func initChainConfig(t testing.TB) {
	t.Helper()
	_ = evmtypes.SetChainConfig(evmtypes.DefaultChainConfig(stocChainID))
}

// mockTxAnte minimally satisfies sdk.Tx with arbitrary msgs (separate name from
// the mockTx in tax_post_test.go to avoid redefinition).
type mockTxAnte struct {
	msgs []sdk.Msg
}

func (t mockTxAnte) GetMsgs() []sdk.Msg                    { return t.msgs }
func (t mockTxAnte) GetMsgsV2() ([]protov2.Message, error) { return nil, nil }

// noopAnteHandlerAnte is a terminal AnteHandler that returns success. Named to
// avoid collision with the existing noopAnteHandler in the package_test scope
// when this file ships into x/stoc/ante/ — but here we're in app/ante so the
// scope is independent.
func noopAnteHandlerAnte(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

// buildSignedEthMsg constructs a real signed Ethereum tx and the corresponding
// MsgEthereumTx with msg.From populated from the recovered signer.
func buildSignedEthMsg(t testing.TB) (*evmtypes.MsgEthereumTx, ethcommon.Address) {
	t.Helper()

	privKey, err := ethcrypto.GenerateKey()
	require.NoError(t, err)
	realFrom := ethcrypto.PubkeyToAddress(privKey.PublicKey)

	chainID := big.NewInt(int64(stocChainID))
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     0,
		GasTipCap: big.NewInt(1),
		GasFeeCap: big.NewInt(1_000_000_000),
		Gas:       21000,
		To:        &ethcommon.Address{1, 2, 3},
		Value:     big.NewInt(0),
		Data:      nil,
	})

	signer := ethtypes.LatestSignerForChainID(chainID)
	signedTx, err := ethtypes.SignTx(tx, signer, privKey)
	require.NoError(t, err)

	msg := &evmtypes.MsgEthereumTx{}
	require.NoError(t, msg.FromSignedEthereumTx(signedTx, signer))
	require.Equal(t, realFrom.Bytes(), msg.From, "msg.From must equal recovered signer")

	return msg, realFrom
}

func TestMaxPendingTx_SimulateSkipped(t *testing.T) {
	initChainConfig(t)

	decorator := stocante.NewMaxPendingTxPerWalletDecorator(nil, 0)
	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 1}, false, log.NewNopLogger()) // simulate=true via flag below
	msg, _ := buildSignedEthMsg(t)
	tx := mockTxAnte{msgs: []sdk.Msg{msg}}

	// Simulate=true → decorator should bypass entirely without invoking
	// VerifySender (chainConfig already set, but the simulate guard short-
	// circuits first).
	newCtx, err := decorator.AnteHandle(ctx, tx, true, noopAnteHandlerAnte)
	require.NoError(t, err)
	require.NotNil(t, newCtx)
}

func TestMaxPendingTx_NonEvmTxSkipped(t *testing.T) {
	initChainConfig(t)

	decorator := stocante.NewMaxPendingTxPerWalletDecorator(nil, 0)
	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))

	cosmosMsg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("ustoc", 1000)),
	}
	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 1}, true, log.NewNopLogger())
	tx := mockTxAnte{msgs: []sdk.Msg{cosmosMsg}}

	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandlerAnte)
	require.NoError(t, err, "non-EVM tx must skip the decorator without sig verify")
}

func TestMaxPendingTx_MultiMsgSkipped(t *testing.T) {
	initChainConfig(t)

	decorator := stocante.NewMaxPendingTxPerWalletDecorator(nil, 0)
	msg, _ := buildSignedEthMsg(t)
	otherSender := sdk.AccAddress([]byte("sender______________"))
	otherRecipient := sdk.AccAddress([]byte("recipient___________"))
	cosmosMsg := &banktypes.MsgSend{
		FromAddress: otherSender.String(),
		ToAddress:   otherRecipient.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("ustoc", 1)),
	}

	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 1}, true, log.NewNopLogger())
	tx := mockTxAnte{msgs: []sdk.Msg{msg, cosmosMsg}}

	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandlerAnte)
	require.NoError(t, err, "multi-msg tx must skip the decorator without sig verify")
}

func TestMaxPendingTx_LegitSignedTxAllowed(t *testing.T) {
	// SA-AUDIT-2026-06-06 HIGH-1: a legit, untampered signed tx must pass the
	// quota decorator when no mempool getter is wired. Validates the verify
	// path does not over-reject.
	initChainConfig(t)

	decorator := stocante.NewMaxPendingTxPerWalletDecorator(nil, 0)
	msg, _ := buildSignedEthMsg(t)

	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 1}, true, log.NewNopLogger())
	tx := mockTxAnte{msgs: []sdk.Msg{msg}}

	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandlerAnte)
	require.NoError(t, err, "legit signed EVM tx with no mempool wired must pass through")
}

func TestMaxPendingTx_SpoofedFromRejected(t *testing.T) {
	// SA-AUDIT-2026-06-06 HIGH-1 MAIN regression: attacker rotates msg.From to
	// an address that did NOT sign the tx. Decorator must reject at step 0
	// (before mempool quota work) instead of counting the spoofed From toward
	// a fresh quota slot.
	initChainConfig(t)

	decorator := stocante.NewMaxPendingTxPerWalletDecorator(nil, 0)
	msg, realFrom := buildSignedEthMsg(t)

	// Spoof: overwrite msg.From with an address that does NOT correspond to
	// the signing key. VerifySender (called inline by the decorator) must
	// reject this mismatch.
	spoofedFrom := ethcommon.HexToAddress("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	require.NotEqual(t, realFrom, spoofedFrom)
	msg.From = spoofedFrom.Bytes()

	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 1}, true, log.NewNopLogger())
	tx := mockTxAnte{msgs: []sdk.Msg{msg}}

	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandlerAnte)
	require.Error(t, err, "spoofed-From EVM tx must be rejected at quota decorator (HIGH-1)")
	require.Contains(t, err.Error(), "signature verification failed at quota decorator")
}

func TestMaxPendingTx_EmptyFromRejected(t *testing.T) {
	// Defense-in-depth: a tx with empty From (msg.From=nil) bypasses normal
	// signed-tx construction. VerifySender should fail because bytes.Equal
	// between nil and recovered address is false.
	initChainConfig(t)

	decorator := stocante.NewMaxPendingTxPerWalletDecorator(nil, 0)
	msg, _ := buildSignedEthMsg(t)
	msg.From = nil

	ctx := sdk.NewContext(nil, cmtproto.Header{Height: 1}, true, log.NewNopLogger())
	tx := mockTxAnte{msgs: []sdk.Msg{msg}}

	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandlerAnte)
	require.Error(t, err, "empty-From EVM tx must be rejected at quota decorator")
}

// Compile-time check that mockTxAnte satisfies sdk.Tx.
var _ sdk.Tx = mockTxAnte{}
