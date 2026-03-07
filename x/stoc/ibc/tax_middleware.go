package ibc

import (
	"encoding/json"

	stockeeper "stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	porttypes "github.com/cosmos/ibc-go/v10/modules/core/05-port/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
)

var _ porttypes.IBCModule = &TaxMiddleware{}

// TaxMiddleware is an IBC middleware that applies token tax on incoming IBC transfers.
// Tax is only applied when stoc-managed tokens return to this chain (receiver is source).
// Foreign tokens arriving for the first time are not taxed (they have ibc/ denoms with no tax config).
type TaxMiddleware struct {
	app    porttypes.IBCModule
	keeper stockeeper.Keeper
}

// NewTaxMiddleware creates a new IBC TaxMiddleware.
func NewTaxMiddleware(app porttypes.IBCModule, keeper stockeeper.Keeper) TaxMiddleware {
	return TaxMiddleware{
		app:    app,
		keeper: keeper,
	}
}

func (im TaxMiddleware) OnChanOpenInit(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	counterparty channeltypes.Counterparty,
	version string,
) (string, error) {
	return im.app.OnChanOpenInit(ctx, order, connectionHops, portID, channelID, counterparty, version)
}

func (im TaxMiddleware) OnChanOpenTry(
	ctx sdk.Context,
	order channeltypes.Order,
	connectionHops []string,
	portID string,
	channelID string,
	counterparty channeltypes.Counterparty,
	counterpartyVersion string,
) (string, error) {
	return im.app.OnChanOpenTry(ctx, order, connectionHops, portID, channelID, counterparty, counterpartyVersion)
}

func (im TaxMiddleware) OnChanOpenAck(
	ctx sdk.Context,
	portID, channelID string,
	counterpartyChannelID string,
	counterpartyVersion string,
) error {
	return im.app.OnChanOpenAck(ctx, portID, channelID, counterpartyChannelID, counterpartyVersion)
}

func (im TaxMiddleware) OnChanOpenConfirm(ctx sdk.Context, portID, channelID string) error {
	return im.app.OnChanOpenConfirm(ctx, portID, channelID)
}

func (im TaxMiddleware) OnChanCloseInit(ctx sdk.Context, portID, channelID string) error {
	return im.app.OnChanCloseInit(ctx, portID, channelID)
}

func (im TaxMiddleware) OnChanCloseConfirm(ctx sdk.Context, portID, channelID string) error {
	return im.app.OnChanCloseConfirm(ctx, portID, channelID)
}

func (im TaxMiddleware) OnRecvPacket(
	ctx sdk.Context,
	channelVersion string,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) ibcexported.Acknowledgement {
	// Let the underlying transfer module process the packet first
	ack := im.app.OnRecvPacket(ctx, channelVersion, packet, relayer)

	// Only apply tax on successful transfers
	if ack == nil || !ack.Success() {
		return ack
	}

	// Parse packet data to get denom and receiver
	var data ibctransfertypes.FungibleTokenPacketData
	if err := json.Unmarshal(packet.GetData(), &data); err != nil {
		// Can't parse packet data — skip tax, don't fail the transfer
		ctx.Logger().Error("IBC tax: failed to unmarshal packet data", "error", err)
		return ack
	}

	// Only apply tax when tokens are returning to their source chain.
	// Foreign tokens get ibc/ denoms and won't have tax config in the stoc module.
	if !ibctransfertypes.ReceiverChainIsSource(packet.GetSourcePort(), packet.GetSourceChannel(), data.Denom) {
		return ack
	}

	// Determine the local denom by stripping the source port/channel prefix
	denomPrefix := ibctransfertypes.GetDenomPrefix(packet.GetSourcePort(), packet.GetSourceChannel())
	if len(data.Denom) <= len(denomPrefix) || data.Denom[:len(denomPrefix)] != denomPrefix {
		// Denom doesn't match expected prefix — skip tax (defensive check)
		return ack
	}
	localDenom := data.Denom[len(denomPrefix):]

	// Look up token tax config
	token, found := im.keeper.GetToken(ctx, localDenom)
	if !found || token.Tax.Percent.IsNil() || token.Tax.Percent.IsZero() || token.Tax.RecipientAddress == "" {
		return ack
	}

	receiverAddr, err := sdk.AccAddressFromBech32(data.Receiver)
	if err != nil {
		ctx.Logger().Error("IBC tax: invalid receiver address", "receiver", data.Receiver, "error", err)
		return ack
	}

	taxRecipientAddr, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
	if err != nil {
		ctx.Logger().Error("IBC tax: invalid tax recipient address", "address", token.Tax.RecipientAddress, "error", err)
		return ack
	}

	// Skip tax when receiver IS the tax recipient
	if receiverAddr.Equals(taxRecipientAddr) {
		return ack
	}

	// Parse the transferred amount from the packet (tax only the incoming amount, not entire balance)
	transferredAmount, ok := math.NewIntFromString(data.Amount)
	if !ok || !transferredAmount.IsPositive() {
		return ack
	}

	// Runtime cap: enforce MaxTaxPercent even if state was modified outside ValidateBasic
	taxPercent := token.Tax.Percent
	if taxPercent.GT(stoctypes.MaxTaxPercent) {
		taxPercent = stoctypes.MaxTaxPercent
	}

	// Calculate tax on the transferred amount only
	taxAmount := transferredAmount.ToLegacyDec().Mul(taxPercent).TruncateInt()
	if taxAmount.IsZero() {
		return ack
	}

	// Cap at receiver's available balance
	receiverBalance := im.keeper.GetBankKeeper().GetBalance(ctx, receiverAddr, localDenom)
	if taxAmount.GT(receiverBalance.Amount) {
		taxAmount = receiverBalance.Amount
	}

	taxCoin := sdk.NewCoin(localDenom, taxAmount)
	if err := im.keeper.GetBankKeeper().SendCoins(ctx, receiverAddr, taxRecipientAddr, sdk.NewCoins(taxCoin)); err != nil {
		// Log but don't fail the IBC transfer — tax is best-effort
		ctx.Logger().Error("IBC tax: failed to send tax", "error", err)
		return ack
	}

	ctx.Logger().Info("IBC tax applied",
		"token_denom", localDenom,
		"tax_amount", taxAmount.String(),
		"receiver", data.Receiver,
		"tax_recipient", token.Tax.RecipientAddress,
	)

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			"token_tax_applied",
			sdk.NewAttribute("token_denom", localDenom),
			sdk.NewAttribute("token_symbol", token.Symbol),
			sdk.NewAttribute("tax_amount", taxAmount.String()),
			sdk.NewAttribute("recipient", data.Receiver),
			sdk.NewAttribute("tax_recipient", token.Tax.RecipientAddress),
			sdk.NewAttribute("source", "ibc_transfer"),
		),
	)

	return ack
}

func (im TaxMiddleware) OnAcknowledgementPacket(
	ctx sdk.Context,
	channelVersion string,
	packet channeltypes.Packet,
	acknowledgement []byte,
	relayer sdk.AccAddress,
) error {
	return im.app.OnAcknowledgementPacket(ctx, channelVersion, packet, acknowledgement, relayer)
}

func (im TaxMiddleware) OnTimeoutPacket(
	ctx sdk.Context,
	channelVersion string,
	packet channeltypes.Packet,
	relayer sdk.AccAddress,
) error {
	return im.app.OnTimeoutPacket(ctx, channelVersion, packet, relayer)
}
