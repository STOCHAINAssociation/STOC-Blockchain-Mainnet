package ante_test

import (
	"testing"

	"cosmossdk.io/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	stocante "stoc/app/ante"
)

// SA-AUDIT-2026-06-07 M1 + LOW-4 regression coverage (R2-M1, R2-LOW4): the
// depth-0 pre-sig screen rejects authz.MsgExec wrapping a disabled msg type
// BEFORE NewSigVerificationDecorator runs ecrecover. This restores the
// pre-fix13 cheap-reject behavior for the common shallow attack without
// re-introducing the deep-recursion CPU amplification surface the fix13
// post-fee reorder was meant to address. Deep nesting is still caught by
// the post-fee cosmosante.NewAuthzLimiterDecorator.

var screenDisabledTypes = []string{
	sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
}

func screenCheckCtx() sdk.Context {
	return sdk.NewContext(nil, cmtproto.Header{Height: 1}, true, log.NewNopLogger())
}

func TestTopLevelAuthzScreen_NoMatchingMsg_PassesThrough(t *testing.T) {
	decorator := stocante.NewTopLevelAuthzMsgExecScreenDecorator(screenDisabledTypes...)

	cosmosMsg := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress([]byte("sender______________")).String(),
		ToAddress:   sdk.AccAddress([]byte("recipient___________")).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("ustoc", 100)),
	}
	tx := mockTxAnte{msgs: []sdk.Msg{cosmosMsg}}

	_, err := decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.NoError(t, err, "non-authz msg must pass through the screen")
}

func TestTopLevelAuthzScreen_AuthzExecWithAllowedInner_PassesThrough(t *testing.T) {
	decorator := stocante.NewTopLevelAuthzMsgExecScreenDecorator(screenDisabledTypes...)

	innerSend := &banktypes.MsgSend{
		FromAddress: sdk.AccAddress([]byte("granter_____________")).String(),
		ToAddress:   sdk.AccAddress([]byte("recipient___________")).String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("ustoc", 100)),
	}
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	exec := authz.NewMsgExec(grantee, []sdk.Msg{innerSend})

	tx := mockTxAnte{msgs: []sdk.Msg{&exec}}
	_, err := decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.NoError(t, err, "authz.MsgExec wrapping an ALLOWED inner msg type must pass through")
}

func TestTopLevelAuthzScreen_AuthzExecWithDisabledInner_Rejected(t *testing.T) {
	decorator := stocante.NewTopLevelAuthzMsgExecScreenDecorator(screenDisabledTypes...)

	// Wrap a disabled type (MsgEthereumTx) inside authz.MsgExec. The screen
	// inspects Any.TypeUrl directly — no codec unpack required.
	disabledInner := &evmtypes.MsgEthereumTx{}
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	exec := authz.NewMsgExec(grantee, []sdk.Msg{disabledInner})

	tx := mockTxAnte{msgs: []sdk.Msg{&exec}}
	_, err := decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.Error(t, err, "M1: authz.MsgExec containing a disabled inner msg type must be rejected pre-sig")
	require.Contains(t, err.Error(), "disabled inner msg type",
		"error must surface the diagnostic that the rejection came from the pre-sig screen")
	require.Contains(t, err.Error(), sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
		"error must name the disabled type URL")
}

func TestTopLevelAuthzScreen_EmptyDisabledList_PassesThrough(t *testing.T) {
	// Defensive: a screen constructed with no disabled types must short-
	// circuit without touching tx.GetMsgs().
	decorator := stocante.NewTopLevelAuthzMsgExecScreenDecorator()

	disabledInner := &evmtypes.MsgEthereumTx{}
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	exec := authz.NewMsgExec(grantee, []sdk.Msg{disabledInner})

	tx := mockTxAnte{msgs: []sdk.Msg{&exec}}
	_, err := decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.NoError(t, err, "empty disabledMsgTypes list must short-circuit (no scan)")
}
