package ante_test

import (
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	"github.com/stretchr/testify/require"

	"stoc/x/stoc/ante"
	"stoc/x/stoc/types"
)

// noopAnteHandler is a terminal AnteHandler that does nothing.
func noopAnteHandler(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}

func TestIBCRestriction_NativeDenomAllowed(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	decorator := ante.NewIBCCustomTokenRestriction(k)

	sender := sdk.AccAddress([]byte("sender______________"))

	// IBC transfer of native denom "ustoc" — should be allowed
	msg := &ibctransfertypes.MsgTransfer{
		SourcePort:    "transfer",
		SourceChannel: "channel-0",
		Token:         sdk.NewCoin("ustoc", math.NewInt(1000)),
		Sender:        sender.String(),
		Receiver:      "cosmos1abcdefghijklmnopqrstuvwxyz",
	}

	tx := mockTx{msgs: []sdk.Msg{msg}}
	newCtx, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err)
	require.NotNil(t, newCtx)
}

func TestIBCRestriction_CustomTokenBlocked(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	sender := sdk.AccAddress([]byte("sender______________"))
	denom := "BLOCKED_0"

	// Create custom token in the store so HasToken returns true
	token := types.Token{
		Id:              denom,
		Name:            "Blocked Token",
		Symbol:          "BLOCKED",
		Decimals:        6,
		Logo:            "https://example.com/logo.png",
		InitialSupply:   math.NewInt(10000),
		TotalSupply:     math.NewInt(10000),
		RemainingSupply: math.ZeroInt(),
		MinimalDenom:    denom,
		Creator:         sender.String(),
		Unlimited:       false,
	}
	require.NoError(t, k.SetToken(ctx, token))

	decorator := ante.NewIBCCustomTokenRestriction(k)

	// IBC transfer of custom stoc token — should be blocked
	msg := &ibctransfertypes.MsgTransfer{
		SourcePort:    "transfer",
		SourceChannel: "channel-0",
		Token:         sdk.NewCoin(denom, math.NewInt(100)),
		Sender:        sender.String(),
		Receiver:      "cosmos1abcdefghijklmnopqrstuvwxyz",
	}

	tx := mockTx{msgs: []sdk.Msg{msg}}
	_, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.Error(t, err)
	require.Contains(t, err.Error(), "IBC transfer of custom token")
	require.Contains(t, err.Error(), denom)
}

func TestIBCRestriction_NonTransferMsgAllowed(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	decorator := ante.NewIBCCustomTokenRestriction(k)

	sender := sdk.AccAddress([]byte("sender______________"))
	recipient := sdk.AccAddress([]byte("recipient___________"))

	// Non-MsgTransfer message (a regular bank MsgSend) — should pass through
	msg := &banktypes.MsgSend{
		FromAddress: sender.String(),
		ToAddress:   recipient.String(),
		Amount:      sdk.NewCoins(sdk.NewCoin("ustoc", math.NewInt(100))),
	}

	tx := mockTx{msgs: []sdk.Msg{msg}}
	newCtx, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err)
	require.NotNil(t, newCtx)
}

func TestIBCRestriction_UnknownDenomAllowed(t *testing.T) {
	setDefaultBondDenom(t)
	k, ctx, _, _ := setupTaxTest(t)

	decorator := ante.NewIBCCustomTokenRestriction(k)

	sender := sdk.AccAddress([]byte("sender______________"))

	// IBC transfer of unknown denom (not a stoc custom token, not native) — should be allowed
	msg := &ibctransfertypes.MsgTransfer{
		SourcePort:    "transfer",
		SourceChannel: "channel-0",
		Token:         sdk.NewCoin("uatom", math.NewInt(500)),
		Sender:        sender.String(),
		Receiver:      "cosmos1abcdefghijklmnopqrstuvwxyz",
	}

	tx := mockTx{msgs: []sdk.Msg{msg}}
	newCtx, err := decorator.AnteHandle(ctx, tx, false, noopAnteHandler)
	require.NoError(t, err)
	require.NotNil(t, newCtx)
}
