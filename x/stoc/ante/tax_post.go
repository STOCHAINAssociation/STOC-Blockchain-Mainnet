package ante

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/bank/types"

	"stoc/x/stoc/keeper"
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

	for _, msg := range tx.GetMsgs() {
		switch m := msg.(type) {
		case *types.MsgSend:
			if err := tpd.applyTaxForRecipient(ctx, m.ToAddress, m.Amount); err != nil {
				return ctx, err
			}
		case *types.MsgMultiSend:
			for _, output := range m.Outputs {
				if err := tpd.applyTaxForRecipient(ctx, output.Address, output.Coins); err != nil {
					return ctx, err
				}
			}
		}
	}

	return next(ctx, tx, simulate, success)
}

// applyTaxForRecipient deducts tax from recipient for each taxable coin and sends it to the tax recipient.
func (tpd TaxPostDecorator) applyTaxForRecipient(ctx sdk.Context, recipientAddress string, coins sdk.Coins) error {
	recipientAddr, err := sdk.AccAddressFromBech32(recipientAddress)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %v", err)
	}

	for _, coin := range coins {
		token, found := tpd.k.GetToken(ctx, coin.Denom)
		if !found || token.Tax.Percent.IsZero() || token.Tax.RecipientAddress == "" {
			continue
		}

		// calculate tax — skip if rounds to zero
		taxAmount := coin.Amount.ToLegacyDec().Mul(token.Tax.Percent).TruncateInt()
		if taxAmount.IsZero() {
			continue
		}

		taxRecipientAddr, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if err != nil {
			ctx.Logger().Error("Invalid tax recipient address", "address", token.Tax.RecipientAddress, "error", err)
			return fmt.Errorf("invalid tax recipient address: %v", err)
		}

		// cap tax at recipient's available balance to avoid reverting the entire tx
		recipientBalance := tpd.k.BankKeeper.GetBalance(ctx, recipientAddr, coin.Denom)
		if recipientBalance.Amount.LT(taxAmount) {
			taxAmount = recipientBalance.Amount
		}
		if taxAmount.IsZero() {
			ctx.Logger().Info("Tax skipped, recipient has zero balance",
				"token_denom", coin.Denom,
				"recipient", recipientAddress,
			)
			continue
		}

		taxCoin := sdk.NewCoin(coin.Denom, taxAmount)
		if err := tpd.k.BankKeeper.SendCoins(ctx, recipientAddr, taxRecipientAddr, sdk.NewCoins(taxCoin)); err != nil {
			ctx.Logger().Error("Failed to send tax", "error", err)
			return fmt.Errorf("failed to send tax: %v", err)
		}

		ctx.Logger().Info("Tax transaction processed",
			"token_denom", coin.Denom,
			"tax_amount", taxAmount.String(),
			"from", recipientAddress,
			"to", token.Tax.RecipientAddress,
		)

		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"token_tax_applied",
				sdk.NewAttribute("token_denom", coin.Denom),
				sdk.NewAttribute("token_symbol", token.Symbol),
				sdk.NewAttribute("tax_amount", taxAmount.String()),
				sdk.NewAttribute("recipient", recipientAddress),
				sdk.NewAttribute("tax_recipient", token.Tax.RecipientAddress),
			),
		)
	}

	return nil
}
