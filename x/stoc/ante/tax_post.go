package ante

import (
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/bank/types"

	"fmt"
	"stoc/x/stoc/keeper"

	"cosmossdk.io/math"
)

type TaxPostDecorator struct {
	k   keeper.Keeper
	cdc codec.BinaryCodec
}

func NewTaxPostDecorator(k keeper.Keeper, cdc codec.BinaryCodec) TaxPostDecorator {
	return TaxPostDecorator{
		k:   k,
		cdc: cdc,
	}
}

func (tpd TaxPostDecorator) PostHandle(ctx sdk.Context, tx sdk.Tx, simulate, success bool, next sdk.PostHandler) (newCtx sdk.Context, err error) {

	if !success || simulate {
		return next(ctx, tx, simulate, success)
	}

	// handle with MsgSend transactions
	for _, msg := range tx.GetMsgs() {
		sendMsg, ok := msg.(*types.MsgSend)
		if !ok {
			continue
		}

		// handle tax for each coin being sent
		for _, coin := range sendMsg.Amount {
			// check if token has tax
			token, found := tpd.k.GetToken(ctx, coin.Denom)
			if !found || token.Tax.Percent.IsZero() || token.Tax.RecipientAddress == "" {

				continue
			}

			// calculate tax
			taxAmount := coin.Amount.ToLegacyDec().Mul(token.Tax.Percent).RoundInt()

			if taxAmount.IsZero() {
				//Default min max tax
				taxAmount = math.NewInt(1)

			}

			// get recipient address and tax address
			recipientAddr, err := sdk.AccAddressFromBech32(sendMsg.ToAddress)
			if err != nil {

				continue
			}

			taxRecipientAddr, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
			if err != nil {

				continue
			}

			// check recipient balance
			recipientBalance := tpd.k.BankKeeper().GetBalance(ctx, recipientAddr, coin.Denom)
			if recipientBalance.Amount.LT(taxAmount) {

				continue
			}

			// send tax from recipient to tax address
			taxCoin := sdk.NewCoin(coin.Denom, taxAmount)
			err = tpd.k.BankKeeper().SendCoins(ctx, recipientAddr, taxRecipientAddr, sdk.NewCoins(taxCoin))
			if err != nil {

				continue
			}

			// write log and emit event
			fmt.Sprintf("Tax subtracted from recipient: %s\n %s \n %s", coin.Denom, taxAmount.String(), token.Tax.RecipientAddress)

			ctx.EventManager().EmitEvent(
				sdk.NewEvent(
					"token_tax_applied",
					sdk.NewAttribute("token_denom", coin.Denom),
					sdk.NewAttribute("token_symbol", token.Symbol),
					sdk.NewAttribute("tax_amount", taxAmount.String()),
					sdk.NewAttribute("recipient", sendMsg.ToAddress),
					sdk.NewAttribute("tax_recipient", token.Tax.RecipientAddress),
				),
			)
		}
	}

	// call next decorator in chain
	return next(ctx, tx, simulate, success)
}
