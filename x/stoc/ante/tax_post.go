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
		ctx.Logger().Error("Tax enforcement failed",
			"error", taxErr,
			"height", ctx.BlockHeight(),
		)
		return ctx, fmt.Errorf("tax enforcement failed, transaction rejected: %w", taxErr)
	}

	return next(ctx, tx, simulate, success)
}

// applyTaxes processes all tax-applicable messages in the transaction
func (tpd TaxPostDecorator) applyTaxes(ctx sdk.Context, tx sdk.Tx) error {
	return tpd.applyTaxesForMsgs(ctx, tx.GetMsgs(), 0)
}

// applyTaxesForMsgs processes tax for a list of messages, supporting recursive authz MsgExec unwrapping.
//
// SA-M1 audit-2026-05-29: tax state is intentionally CUMULATIVE across messages within a tx.
// msg[i+1] observes balance changes from msg[i]'s tax deduction. This is required by SA-C5
// fail-loud semantics: if msg[i] drains a recipient, msg[i+1]'s tax check on the same
// recipient must see the reduced balance to detect drain-then-evade attacks.
//
// Prior versions wrapped each msg in ctx.CacheContext()/writeCache(), which was a no-op
// decoration (Cosmos SDK PostHandler atomicity reverts the entire tx on any error anyway)
// and the comment claiming per-msg "isolation" was misleading — writeCache committed
// between iterations so msg[i+1] always observed msg[i]'s tax state. Removed the redundant
// cache layer; behavior is unchanged.
func (tpd TaxPostDecorator) applyTaxesForMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *types.MsgSend:
			if err := tpd.applyTaxForRecipient(ctx, m.ToAddress, m.Amount); err != nil {
				return err
			}
		case *types.MsgMultiSend:
			// Reject MsgMultiSend with too many inputs or outputs to prevent DoS
			if len(m.Inputs) > stoctypes.MaxMultiSendOutputs {
				return fmt.Errorf("MsgMultiSend has too many inputs (%d > %d)", len(m.Inputs), stoctypes.MaxMultiSendOutputs)
			}
			if len(m.Outputs) > stoctypes.MaxMultiSendOutputs {
				return fmt.Errorf("MsgMultiSend has too many outputs (%d > %d)", len(m.Outputs), stoctypes.MaxMultiSendOutputs)
			}
			for _, output := range m.Outputs {
				if err := tpd.applyTaxForRecipient(ctx, output.Address, output.Coins); err != nil {
					return err
				}
			}
		case *authz.MsgExec:
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
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

		// SA-L1 audit-2026-05-29: reject 1-unit transfers of taxable custom
		// tokens to close the micro-spam evasion vector. A 1-unit transfer can
		// only carry 0 or 1 tax; setting taxAmount=0 here (to preserve "1 unit
		// reaches recipient") meant an attacker could split N tokens into
		// N × 1-unit transfers paying 0 total tax instead of N × Percent.
		// Example: 1,000,000 × 1-unit txs → 0 tax instead of 500,000 at 50% rate.
		// Gas cost per tx (~21 ustoc) is trivially low compared to securities
		// token value. Force a 2-unit minimum so 1 unit can always go to the
		// tax recipient and 1 unit reaches the receiver.
		if coin.Amount.LTE(math.OneInt()) {
			return fmt.Errorf(
				"transfer of %s %s below minimum taxable amount: tax-enabled custom tokens require amount >= 2 (1 unit tax + 1 unit recipient)",
				coin.Amount.String(), coin.Denom,
			)
		}

		// Calculate tax — enforce minimum 1 unit to prevent rounding-to-zero
		// evasion via transaction splitting, but ensure recipient always
		// retains at least 1 unit on micro-transfers (cap at half + floor 1).
		taxAmount := coin.Amount.ToLegacyDec().Mul(taxPercent).TruncateInt()
		if taxAmount.IsZero() {
			taxAmount = math.OneInt()
		}
		// Prevent confiscation: tax must not exceed half the transfer amount (true integer half)
		halfAmount := coin.Amount.Quo(math.NewInt(2))
		if taxAmount.GT(halfAmount) {
			taxAmount = halfAmount
		}
		// Ensure recipient retains at least 1 unit (defensive — with amount >= 2
		// and tax capped at half, this should always hold, but guard anyway)
		if coin.Amount.Sub(taxAmount).LT(math.OneInt()) {
			taxAmount = coin.Amount.Sub(math.OneInt())
		}
		if taxAmount.IsZero() {
			continue
		}

		taxRecipientAddr, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if err != nil {
			ctx.Logger().Error("Invalid tax recipient address", "address", token.Tax.RecipientAddress, "error", err)
			return fmt.Errorf("invalid tax recipient address: %v", err)
		}

		// SA-C5 audit-2026-05-29: do NOT skip on recipient==taxRecipient.
		// Previous "skip self-tax" was a tax-evasion vector: attacker becomes
		// tax recipient → all inbound transfers to them are tax-free → laundering
		// hop. Apply tax even when recipient == taxRecipient — sender's net
		// behavior unchanged (tax goes to same address), and external attackers
		// can't use this short-circuit. Side effect: 0-coin self-tax-send is a
		// no-op which bank.SendCoins handles gracefully (Amount.IsZero check).

		// SA-C5 v3 (audit-2026-06-02 senior-skeptic HIGH-1):
		//
		// The drain-then-evade attack requires that the same atomic tx (or an
		// authz/group wrapper around it) (1) MsgSend a taxable token to a
		// recipient and (2) drain the recipient's balance below the computed
		// tax before this PostHandler runs. The earlier v1 design tried to
		// detect that by reading the recipient's "current" balance with
		// GetBalance and failing loudly when balance < taxAmount; that read
		// returned PRE-send state on devnet (the cosmos-evm keeper wiring
		// short-circuits the BankKeeper's cache propagation between runMsgs
		// and postHandler) so every fresh-recipient transfer was incorrectly
		// rejected. v2 silenced the check by clamping taxAmount to the (still
		// stale) read, which opened a free tax-bypass channel via
		// fresh-wallet rotation — flagged HIGH-1 by the senior-skeptic audit.
		//
		// v3 removes the speculative pre-check entirely and instead asks the
		// authoritative source — bank.SendCoins itself — whether the
		// recipient can cover the tax at COMMIT time. SendCoins is the only
		// component that reads the canonical post-msg state, so its error
		// reflects ground truth:
		//
		//   - Fresh recipient who just received `coin.Amount`: SendCoins
		//     succeeds because the just-credited amount is on-chain by the
		//     time PostHandler runs; taxAmount <= coin.Amount/2 (enforced
		//     above), so the deduction always fits.
		//   - Drain-then-evade attack: SendCoins fails with "insufficient
		//     funds" because the drain msg already executed in the same tx;
		//     we return the error and the WHOLE tx — including the drain —
		//     reverts.
		//
		// This single source of truth eliminates the false-positive of v2
		// and the legitimate-transfer rejection of v1. The error message
		// distinguishes "drain detected" from "tax bug" by inspecting whether
		// SendCoins reports an insufficient-funds class error so on-chain
		// auditors can tell the two cases apart.
		taxCoin := sdk.NewCoin(coin.Denom, taxAmount)
		if err := tpd.k.GetBankKeeper().SendCoins(ctx, recipientAddr, taxRecipientAddr, sdk.NewCoins(taxCoin)); err != nil {
			ctx.Logger().Error("Tax SendCoins failed (drain-then-evade or genuine deficit)",
				"recipient", recipientAddress,
				"tax_denom", coin.Denom,
				"tax_amount", taxAmount.String(),
				"error", err)
			return fmt.Errorf(
				"tax enforcement failed for %s%s on recipient %s: %w (drain-then-evade or recipient balance shortfall — reverting tx)",
				taxAmount.String(), coin.Denom, recipientAddress, err,
			)
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
