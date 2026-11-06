package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	clientv2types "github.com/cosmos/ibc-go/v10/modules/core/02-client/v2/types"
	channelv2types "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/ante"
)

// v5.0.3 audit round 2: IBC v2 messages are rejected inside group and gov
// proposals too.

func groupProposal(t *testing.T, msgs ...sdk.Msg) *grouptypes.MsgSubmitProposal {
	t.Helper()
	prop := &grouptypes.MsgSubmitProposal{
		GroupPolicyAddress: sdk.AccAddress([]byte("group_policy________")).String(),
		Proposers:          []string{sdk.AccAddress([]byte("proposer____________")).String()},
		Title:              "t",
		Summary:            "s",
	}
	require.NoError(t, prop.SetMsgs(msgs))
	return prop
}

func govProposal(t *testing.T, msgs ...sdk.Msg) *govv1types.MsgSubmitProposal {
	t.Helper()
	prop, err := govv1types.NewMsgSubmitProposal(msgs, sdk.NewCoins(), sdk.AccAddress([]byte("proposer____________")).String(), "", "t", "s", false)
	require.NoError(t, err)
	return prop
}

func TestIBCRestriction_IBCV2RejectedInsideProposals(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)
	decorator := ante.NewIBCCustomTokenRestriction(k)
	policy := sdk.AccAddress([]byte("group_policy________")).String()

	for _, msg := range []sdk.Msg{
		&clientv2types.MsgRegisterCounterparty{ClientId: "07-tendermint-0", CounterpartyClientId: "07-tendermint-0", Signer: policy},
		&channelv2types.MsgSendPacket{SourceClient: "07-tendermint-0", Signer: policy},
		&channelv2types.MsgRecvPacket{Signer: policy},
		&channelv2types.MsgAcknowledgement{Signer: policy},
		&channelv2types.MsgTimeout{Signer: policy},
	} {
		_, err := decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{groupProposal(t, msg)}}, false, noopAnteHandler)
		require.ErrorContains(t, err, "is disabled on STOChain", "group: %s", sdk.MsgTypeURL(msg))
		_, err = decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{govProposal(t, msg)}}, false, noopAnteHandler)
		require.ErrorContains(t, err, "is disabled on STOChain", "gov: %s", sdk.MsgTypeURL(msg))
	}
}
