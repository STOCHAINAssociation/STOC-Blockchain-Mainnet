package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	clientv2types "github.com/cosmos/ibc-go/v10/modules/core/02-client/v2/types"
	channelv2types "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	"github.com/stretchr/testify/require"

	stocante "stoc/app/ante"
)

// v5.0.3 audit round 1 (FIX-8): IBC v2 messages are rejected in the ante so
// they fail cleanly instead of reaching the unset v2 router.
func TestIBCV2DisabledDecorator(t *testing.T) {
	decorator := stocante.NewIBCV2DisabledDecorator()
	signer := sdk.AccAddress([]byte("relayer_____________")).String()

	for _, msg := range []sdk.Msg{
		&clientv2types.MsgRegisterCounterparty{ClientId: "07-tendermint-0", CounterpartyClientId: "07-tendermint-0", Signer: signer},
		&channelv2types.MsgSendPacket{SourceClient: "07-tendermint-0", Signer: signer},
		&channelv2types.MsgRecvPacket{Signer: signer},
		&channelv2types.MsgAcknowledgement{Signer: signer},
		&channelv2types.MsgTimeout{Signer: signer},
	} {
		_, err := decorator.AnteHandle(screenCheckCtx(), mockTxAnte{msgs: []sdk.Msg{msg}}, false, noopAnteHandlerAnte)
		require.ErrorContains(t, err, "is disabled on STOChain", sdk.MsgTypeURL(msg))
	}

	send := &banktypes.MsgSend{
		FromAddress: signer,
		ToAddress:   sdk.AccAddress([]byte("recipient___________")).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("ustoc", 1)),
	}
	_, err := decorator.AnteHandle(screenCheckCtx(), mockTxAnte{msgs: []sdk.Msg{send}}, false, noopAnteHandlerAnte)
	require.NoError(t, err, "non-v2 msgs pass through")
}

// authz-wrapped IBC v2 messages are rejected by the pre-sig authz screen,
// which shares AuthzDisabledMsgTypes.
func TestIBCV2Disabled_InsideAuthzRejected(t *testing.T) {
	disabled := stocante.AuthzDisabledMsgTypes()
	for _, url := range stocante.IBCV2DisabledMsgTypes() {
		require.Contains(t, disabled, url)
	}

	grantee := sdk.AccAddress([]byte("grantee_____________"))
	inner := &channelv2types.MsgSendPacket{SourceClient: "07-tendermint-0", Signer: sdk.AccAddress([]byte("granter_____________")).String()}
	exec := authz.NewMsgExec(grantee, []sdk.Msg{inner})
	screen := stocante.NewTopLevelAuthzMsgExecScreenDecorator(disabled...)
	_, err := screen.AnteHandle(screenCheckCtx(), mockTxAnte{msgs: []sdk.Msg{&exec}}, false, noopAnteHandlerAnte)
	require.Error(t, err)
}
