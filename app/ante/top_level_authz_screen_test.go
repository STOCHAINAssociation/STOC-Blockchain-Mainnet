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

// SA-AUDIT-2026-06-08 R3-M1 regression coverage: pre-sig screen must cover
// top-level authz.MsgGrant whose Authorization targets a disabled msg type.
// Pre-fix15, only *authz.MsgExec was matched, leaving the symmetric MsgGrant
// shape exposed to the same ecrecover-spend regression.
func TestAuthzPreSigScreen_MsgGrant_DisabledType_Rejected(t *testing.T) {
	decorator := stocante.NewAuthzPreSigScreenDecorator(screenDisabledTypes...)

	granter := sdk.AccAddress([]byte("granter_____________"))
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	// GenericAuthorization for a disabled type (MsgEthereumTx).
	genericAuth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}))
	grant, err := authz.NewMsgGrant(granter, grantee, genericAuth, nil)
	require.NoError(t, err)

	tx := mockTxAnte{msgs: []sdk.Msg{grant}}
	_, err = decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.Error(t, err, "R3-M1: authz.MsgGrant for a disabled type must be rejected pre-sig")
	require.Contains(t, err.Error(), "authz.MsgGrant grants disabled msg type")
	require.Contains(t, err.Error(), sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}))
}

func TestAuthzPreSigScreen_MsgGrant_AllowedType_Allowed(t *testing.T) {
	decorator := stocante.NewAuthzPreSigScreenDecorator(screenDisabledTypes...)

	granter := sdk.AccAddress([]byte("granter_____________"))
	grantee := sdk.AccAddress([]byte("grantee_____________"))
	genericAuth := authz.NewGenericAuthorization(sdk.MsgTypeURL(&banktypes.MsgSend{}))
	grant, err := authz.NewMsgGrant(granter, grantee, genericAuth, nil)
	require.NoError(t, err)

	tx := mockTxAnte{msgs: []sdk.Msg{grant}}
	_, err = decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.NoError(t, err, "MsgGrant for ALLOWED type must pass through")
}

// SA-AUDIT-2026-06-08 R3-M2 regression coverage: pre-sig screen must walk
// at least AuthzPreSigScreenMaxDepth (=3) levels of nested authz.MsgExec so
// that a single-wrapper bypass (MsgExec(MsgExec(disabled))) is caught
// before ecrecover.
func TestAuthzPreSigScreen_NestedMsgExec_DisabledInner_Rejected(t *testing.T) {
	decorator := stocante.NewAuthzPreSigScreenDecorator(screenDisabledTypes...)

	grantee := sdk.AccAddress([]byte("grantee_____________"))
	// Outer wraps an inner MsgExec wrapping a disabled MsgEthereumTx.
	innerExec := authz.NewMsgExec(grantee, []sdk.Msg{&evmtypes.MsgEthereumTx{}})
	outerExec := authz.NewMsgExec(grantee, []sdk.Msg{&innerExec})

	tx := mockTxAnte{msgs: []sdk.Msg{&outerExec}}
	_, err := decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.Error(t, err, "R3-M2: nested MsgExec(MsgExec(disabled)) must be caught at depth 1")
	require.Contains(t, err.Error(), "disabled inner msg type")
	require.Contains(t, err.Error(), "depth 1",
		"error must surface the depth at which the rejection fired (defence-in-depth diagnostic)")
}

func TestAuthzPreSigScreen_DeeplyNestedMsgExec_BeyondCap_DefersToPostFee(t *testing.T) {
	// Defence-in-depth boundary: an attack nested deeper than
	// AuthzPreSigScreenMaxDepth (=3) is intentionally NOT rejected at the
	// pre-sig screen; the post-fee recursive AuthzLimiter handles it.
	// Build MsgExec(MsgExec(MsgExec(MsgExec(disabled)))) — 4 wrappers, so
	// the disabled type sits at depth 4 which is >= cap.
	decorator := stocante.NewAuthzPreSigScreenDecorator(screenDisabledTypes...)

	grantee := sdk.AccAddress([]byte("grantee_____________"))
	disabledInner := &evmtypes.MsgEthereumTx{}
	level3 := authz.NewMsgExec(grantee, []sdk.Msg{disabledInner})
	level2 := authz.NewMsgExec(grantee, []sdk.Msg{&level3})
	level1 := authz.NewMsgExec(grantee, []sdk.Msg{&level2})
	level0 := authz.NewMsgExec(grantee, []sdk.Msg{&level1})

	tx := mockTxAnte{msgs: []sdk.Msg{&level0}}
	_, err := decorator.AnteHandle(screenCheckCtx(), tx, false, noopAnteHandlerAnte)
	require.NoError(t, err,
		"defence-in-depth: attacks deeper than AuthzPreSigScreenMaxDepth are defered to the post-fee recursive AuthzLimiter; pre-sig screen must NOT reject")
}
