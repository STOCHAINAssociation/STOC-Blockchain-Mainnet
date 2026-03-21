package ante

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/cosmos/cosmos-sdk/x/bank/types"

	"stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"
)

// maxAuthzUnwrapDepth limits recursive MsgExec unwrapping to prevent DoS via deeply nested authz messages.
const maxAuthzUnwrapDepth = 3

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

	// Apply taxes — if tax collection fails, the transaction MUST fail.
	// Allowing token transfers without tax would violate securities compliance.
	// Each message is processed independently to prevent cross-message interference.
	taxErr := tpd.applyTaxes(ctx, tx)
	if taxErr != nil {
		return ctx, fmt.Errorf("tax enforcement failed, transaction rejected: %w", taxErr)
	}

	return next(ctx, tx, simulate, success)
}

// applyTaxes processes all tax-applicable messages in the transaction
func (tpd TaxPostDecorator) applyTaxes(ctx sdk.Context, tx sdk.Tx) error {
	return tpd.applyTaxesForMsgs(ctx, tx.GetMsgs(), 0)
}

// applyTaxesForMsgs processes tax for a list of messages, supporting recursive authz MsgExec unwrapping.
// Each top-level message uses its own cache context to prevent cross-message tax interference.
func (tpd TaxPostDecorator) applyTaxesForMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *types.MsgSend:
			// Use cache context per message so one message's tax state
			// does not leak into another message's balance checks
			cacheCtx, writeCache := ctx.CacheContext()
			if err := tpd.applyTaxForRecipient(cacheCtx, m.ToAddress, m.Amount); err != nil {
				return err
			}
			writeCache()
		case *types.MsgMultiSend:
			// Reject MsgMultiSend with too many inputs or outputs to prevent DoS
			if len(m.Inputs) > stoctypes.MaxMultiSendOutputs {
				return fmt.Errorf("MsgMultiSend has too many inputs (%d > %d)", len(m.Inputs), stoctypes.MaxMultiSendOutputs)
			}
			if len(m.Outputs) > stoctypes.MaxMultiSendOutputs {
				return fmt.Errorf("MsgMultiSend has too many outputs (%d > %d)", len(m.Outputs), stoctypes.MaxMultiSendOutputs)
			}
			cacheCtx, writeCache := ctx.CacheContext()
			for _, output := range m.Outputs {
				if err := tpd.applyTaxForRecipient(cacheCtx, output.Address, output.Coins); err != nil {
					return err
				}
			}
			writeCache()
		case *authz.MsgExec:
			if depth >= maxAuthzUnwrapDepth {
				return fmt.Errorf("authz MsgExec nesting depth exceeded (%d), rejecting to prevent tax evasion", depth)
			}
			innerMsgs, err := m.GetMessages()
			if err != nil {
				// Return error instead of skipping — prevents tax evasion via corrupted authz messages
				return fmt.Errorf("failed to unwrap authz MsgExec for tax: %w", err)
			}
			if err := tpd.applyTaxesForMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyTaxForRecipient deducts tax from recipient for each taxable coin and sends it to the tax recipient.
// NOTE: Tax applies to MsgSend/MsgMultiSend including those wrapped in authz MsgExec.
// IBC transfers bypass this tax by design — custom tokens are blocked from IBC entirely
// (see IBCCustomTokenRestriction), so tax evasion via IBC is not possible.
func (tpd TaxPostDecorator) applyTaxForRecipient(ctx sdk.Context, recipientAddress string, coins sdk.Coins) error {
	recipientAddr, err := sdk.AccAddressFromBech32(recipientAddress)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %v", err)
	}

	for _, coin := range coins {
		// Fast-path: skip store lookup for native denoms (ustoc, astoc, etc.)
		// which can never be custom tokens — avoids unnecessary store reads per tx
		if stoctypes.IsNativeDenom(coin.Denom) {
			continue
		}

		token, found := tpd.k.GetToken(ctx, coin.Denom)
		if !found || token.Tax.Percent.IsNil() || token.Tax.Percent.IsZero() || token.Tax.RecipientAddress == "" {
			continue
		}

		// Runtime cap: enforce MaxTaxPercent even if state was modified outside ValidateBasic
		taxPercent := token.Tax.Percent
		if taxPercent.GT(stoctypes.MaxTaxPercent) {
			taxPercent = stoctypes.MaxTaxPercent
		}

		// Skip zero-amount transfers (no-op)
		if coin.Amount.IsZero() {
			continue
		}

		// Calculate tax — enforce minimum 1 unit to prevent tax evasion via transaction splitting,
		// but ensure recipient always retains at least 1 unit on micro-transfers.
		taxAmount := coin.Amount.ToLegacyDec().Mul(taxPercent).TruncateInt()
		if taxAmount.IsZero() {
			taxAmount = math.OneInt()
		}
		// Prevent confiscation: tax must not exceed half the transfer amount (true integer half)
		halfAmount := coin.Amount.Quo(math.NewInt(2))
		if taxAmount.GT(halfAmount) {
			taxAmount = halfAmount
		}
		// Ensure recipient retains at least 1 unit — skip tax on 1-unit transfers
		if coin.Amount.Sub(taxAmount).LT(math.OneInt()) {
			if coin.Amount.LTE(math.OneInt()) {
				taxAmount = math.ZeroInt()
			} else {
				taxAmount = coin.Amount.Sub(math.OneInt())
			}
		}

		taxRecipientAddr, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if err != nil {
			ctx.Logger().Error("Invalid tax recipient address", "address", token.Tax.RecipientAddress, "error", err)
			return fmt.Errorf("invalid tax recipient address: %v", err)
		}

		// Skip tax when recipient IS the tax recipient (no-op self-transfer)
		if recipientAddr.Equals(taxRecipientAddr) {
			continue
		}

		// Cap tax at recipient's available balance to avoid reverting the entire tx
		recipientBalance := tpd.k.GetBankKeeper().GetBalance(ctx, recipientAddr, coin.Denom)
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
		if err := tpd.k.GetBankKeeper().SendCoins(ctx, recipientAddr, taxRecipientAddr, sdk.NewCoins(taxCoin)); err != nil {
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
