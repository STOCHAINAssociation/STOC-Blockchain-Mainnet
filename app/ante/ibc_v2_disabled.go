package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	clientv2types "github.com/cosmos/ibc-go/v10/modules/core/02-client/v2/types"
	channelv2types "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
)

// IBCV2DisabledMsgTypes returns the IBC v2 (Eureka) message type URLs that
// STOChain rejects (v5.0.3 audit round 1).
//
// STOChain never registers an IBC v2 application router (SetRouterV2), so a
// v2 packet that reaches its application callback dereferences a nil router
// and panics (recovered by baseapp, but not a clean failure). Every v2 packet
// path (MsgSendPacket, MsgRecvPacket, MsgAcknowledgement, MsgTimeout, and an
// ICS-20 MsgTransfer whose source channel is a v2 client id) first requires a
// counterparty registered with MsgRegisterCounterparty, and only the client
// creator may register it. Rejecting these messages at the top level and
// inside authz (this list is also part of AuthzDisabledMsgTypes) makes v2
// fail cleanly with "counterparty not found" or an ante error instead.
//
// Returns a fresh slice on each call.
func IBCV2DisabledMsgTypes() []string {
	return []string{
		sdk.MsgTypeURL(&clientv2types.MsgRegisterCounterparty{}),
		sdk.MsgTypeURL(&channelv2types.MsgSendPacket{}),
		sdk.MsgTypeURL(&channelv2types.MsgRecvPacket{}),
		sdk.MsgTypeURL(&channelv2types.MsgAcknowledgement{}),
		sdk.MsgTypeURL(&channelv2types.MsgTimeout{}),
	}
}

// IBCV2DisabledDecorator rejects any top-level IBC v2 message (see
// IBCV2DisabledMsgTypes). Constant cost per message, no unpacking; placed
// before signature verification. authz-wrapped v2 messages are rejected by
// the authz screens through AuthzDisabledMsgTypes.
type IBCV2DisabledDecorator struct {
	disabled map[string]struct{}
}

// NewIBCV2DisabledDecorator returns the decorator.
func NewIBCV2DisabledDecorator() IBCV2DisabledDecorator {
	disabled := make(map[string]struct{})
	for _, url := range IBCV2DisabledMsgTypes() {
		disabled[url] = struct{}{}
	}
	return IBCV2DisabledDecorator{disabled: disabled}
}

// AnteHandle implements sdk.AnteDecorator.
func (d IBCV2DisabledDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		url := sdk.MsgTypeURL(msg)
		if _, ok := d.disabled[url]; ok {
			return ctx, errorsmod.Wrapf(errortypes.ErrUnauthorized, "IBC v2 message %s is disabled on STOChain", url)
		}
	}
	return next(ctx, tx, simulate)
}
