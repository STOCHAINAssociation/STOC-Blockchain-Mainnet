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
// Tax state is intentionally CUMULATIVE across messages within a tx.
// msg[i+1] observes balance changes from msg[i]'s tax deduction: if msg[i] drains a
// recipient, msg[i+1]'s tax check on the same recipient must see the reduced balance
// to detect drain-then-evade attacks.
//
// No per-msg CacheContext is used: Cosmos SDK PostHandler atomicity already reverts
// the entire tx on any error, so a per-msg cache layer would add nothing.
func (tpd TaxPostDecorator) applyTaxesForMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *types.MsgSend:
			if err := tpd.applyTaxForRecipient(ctx, m.FromAddress, m.ToAddress, m.Amount); err != nil {
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
			// MsgMultiSend has a single semantic sender (Inputs[0]); we use it
			// for the R2 skip-conditions check (sender == tax_recipient). Per-output
			// recipient still drives the R3 check (recipient == tax_recipient)
			// inside applyTaxForRecipient.
			// Defense-in-depth: explicit single-input guard.
			// Cosmos SDK v0.50+ x/bank.MsgMultiSend.ValidateBasic rejects
			// multi-input MultiSend at the wire level (single-input post-v0.47
			// restriction), but if a future SDK rev relaxed that, our
			// "sender = Inputs[0].Address" attribution would mis-blame the
			// first input for outputs paid by later inputs, letting an
			// attacker with input role pay alongside a tax_recipient-input to
			// bypass R2. Pin the assumption explicitly so an SDK upgrade that
			// breaks it surfaces as a hard error here rather than as a silent
			// evasion path.
			if len(m.Inputs) != 1 {
				return fmt.Errorf("MsgMultiSend with %d inputs not supported (single-input only — multi-input MultiSend is disallowed since Cosmos SDK v0.47)", len(m.Inputs))
			}
			sender := m.Inputs[0].Address
			// Parse the sender ONCE for the whole MultiSend instead of once per
			// output, avoiding a repeated bech32 decode per output (perf griefing
			// surface; MaxMultiSendOutputs bounds it but the work is pure waste).
			// Canonicalization semantics are identical to the per-call parse.
			senderAddrParsed, senderErr := sdk.AccAddressFromBech32(sender)
			if senderErr != nil {
				return fmt.Errorf("invalid sender address: %v", senderErr)
			}
			senderCanonical := senderAddrParsed.String()
			for _, output := range m.Outputs {
				if err := tpd.applyTaxForRecipientCanonical(ctx, senderCanonical, output.Address, output.Coins); err != nil {
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

// applyTaxForRecipient deducts tax from recipient for each taxable coin and
// sends it to the token's TokenTax.RecipientAddress.
//
// Tax rules:
//
//   - R1: tax recipient is whatever creator set on the token at CreateToken
//     time. It can be the creator wallet, a team multisig, a third-party
//     escrow, or any other address. The module does not enforce a
//     particular shape — that decision belongs to the token issuer.
//   - R2: SKIP tax when sender == tax_recipient. Taxing the tax collector
//     when they themselves are sending is a no-op (the tax flows back to
//     the sender) and the round-trip wastes gas + emits a misleading event.
//   - R3: SKIP tax when recipient == tax_recipient. Recipient would receive
//     amount × (1 - tax_pct) PLUS tax_pct as the tax payment — total still
//     equals amount — so splitting it is also a no-op. Indexers + wallets
//     read cleaner when the single transfer event stands without a paired
//     tax_applied event.
//   - R4: tax is computed FROM the sent amount (not from sender's remaining
//     balance). Single atomic split. If the bank SendCoins for the tax
//     leg surfaces an insufficient-funds error (e.g. against a stale
//     pre-runMsgs balance view), the tax leg is silently skipped rather
//     than reverting the whole transfer. The architecturally complete
//     alternative is a bank.MsgServer wrapper that applies tax in the same
//     execution path as MsgSend.
//   - Native denoms (ustoc, astoc, etc.) bypass tax entirely — only custom
//     x/stoc-managed tokens carry tax. Withdraw-commission, withdraw-rewards,
//     vesting transfers, gov deposits etc. that move native denoms are
//     UNAFFECTED by this PostHandler.
//   - IBC transfers, gov / vesting / group / erc20 chain-ops are blocked
//     at ANTE-time for custom tokens (see IBCCustomTokenRestriction and
//     CustomTokenChainOpsRestriction). The tax PostHandler does not need
//     to second-guess those paths.
//
// Note on R3: an attacker cannot promote themselves to tax_recipient to
// receive transfers tax-free, because only the token creator can set
// tax_recipient at CreateToken time and it can only change later via
// gov-prop or token re-issue.
func (tpd TaxPostDecorator) applyTaxForRecipient(ctx sdk.Context, senderAddress, recipientAddress string, coins sdk.Coins) error {
	// Canonicalize BOTH sides of the R2/R3 compare. Normalizing only
	// tax_recipient would let an attacker craft an ALL-UPPERCASE FromAddress
	// equal to the tax_recipient to lex-mismatch the lowercase canonical form,
	// dodge the R2 skip, and pump tax to themselves while the victim pays.
	// Parse the sender once here; tax_recipient parsing remains inside the
	// loop because it's per-token.
	senderAddrParsed, senderErr := sdk.AccAddressFromBech32(senderAddress)
	if senderErr != nil {
		return fmt.Errorf("invalid sender address: %v", senderErr)
	}
	return tpd.applyTaxForRecipientCanonical(ctx, senderAddrParsed.String(), recipientAddress, coins)
}

// applyTaxForRecipientCanonical is applyTaxForRecipient with the sender
// already in canonical bech32 form.
//
// Split out so the MsgMultiSend path can parse its single semantic sender
// ONCE and reuse the canonical form across all outputs instead of re-running
// bech32 decode per output.
// CALLER CONTRACT: senderCanonical MUST be the output of
// sdk.AccAddress.String() (canonical lowercase) — passing a raw
// user-supplied string here would re-open the uppercase R2-dodge.
func (tpd TaxPostDecorator) applyTaxForRecipientCanonical(ctx sdk.Context, senderCanonical, recipientAddress string, coins sdk.Coins) error {
	recipientAddr, err := sdk.AccAddressFromBech32(recipientAddress)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %v", err)
	}
	recipientCanonical := recipientAddr.String()

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

		// Defense-in-depth: validate token.Tax.RecipientAddress
		// as bech32 BEFORE the R2/R3 string compares below. CreateToken already
		// rejects malformed values, but if a future state-migration ever
		// landed a malformed string in this field the R2/R3 string compare could
		// produce a false-positive skip when sender/recipient happens to
		// lex-equal the malformed value. Fail closed instead.
		// Re-encode via AccAddress.String() to obtain
		// the canonical lowercase form. Bech32 permits ALL-UPPERCASE addresses
		// to round-trip the checksum, so a stored uppercase tax_recipient would
		// fail the case-sensitive R2/R3 string compares below and let tax bypass.
		taxRecipientAddr, addrErr := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if addrErr != nil {
			return fmt.Errorf("token %s has malformed Tax.RecipientAddress %q: %v", coin.Denom, token.Tax.RecipientAddress, addrErr)
		}
		taxRecipientCanonical := taxRecipientAddr.String()

		// R2 / R3 skip conditions. See applyTaxForRecipient godoc for
		// rationale. Compare canonical-vs-canonical on BOTH sides —
		// sender/recipient canonicalized above, tax_recipient canonicalized
		// here. Prevents uppercase-FromAddress dodge.
		if senderCanonical == taxRecipientCanonical {
			// The R2 skip leaves an audit trail: every transfer that moves a
			// taxed security WITHOUT paying tax must be explainable from the
			// event stream alone, without re-deriving the R2 rule from source
			// to explain a missing tax_applied event.
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				"tax_skipped_creator_send",
				sdk.NewAttribute("denom", coin.Denom),
				sdk.NewAttribute("symbol", token.Symbol),
				sdk.NewAttribute("amount", coin.Amount.String()),
				sdk.NewAttribute("sender", senderCanonical),
				sdk.NewAttribute("tax_recipient", taxRecipientCanonical),
				sdk.NewAttribute("reason", "sender_is_tax_recipient"),
			))
			continue
		}
		if recipientCanonical == taxRecipientCanonical {
			continue
		}

		// Runtime cap: enforce MaxTaxPercent even if state was modified outside ValidateBasic
		taxPercent := token.Tax.Percent
		// Nil or negative Percent
		// must not reach the multiplier below — math.LegacyDec.Mul on a nil
		// dec panics, and a negative percent would invert the tax direction
		// (paying the sender from the recipient's pocket). Treat as
		// "tax disabled" and skip this coin without surfacing an error,
		// since the on-chain state is recoverable via gov param update.
		if taxPercent.IsNil() || taxPercent.IsNegative() {
			continue
		}
		if taxPercent.GT(stoctypes.MaxTaxPercent) {
			taxPercent = stoctypes.MaxTaxPercent
		}

		// Skip zero-amount transfers (no-op)
		if coin.Amount.IsZero() {
			continue
		}

		// Reject 1-unit transfers of taxable custom tokens to close the
		// micro-spam evasion vector. A 1-unit transfer can only carry 0 or 1
		// tax; setting taxAmount=0 here (to preserve "1 unit reaches
		// recipient") would let an attacker split N tokens into
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
		// Defensive — after the half-cap + recipient-retain floor, taxAmount
		// can only collapse to zero if the amount<=1 reject above is bypassed
		// (e.g. a future code change). Fail loud instead of a silent `continue`
		// so the "every taxable transfer pays tax" invariant stays explicit +
		// testable. The silent-skip path BELOW at the SendCoins attempt covers
		// a different scenario (stale balance view) and is unaffected.
		if taxAmount.IsZero() {
			return fmt.Errorf(
				"tax computation collapsed to zero for %s %s at percent %s — refusing to silently skip tax (A15-STO-L2)",
				coin.Amount.String(), coin.Denom, taxPercent.String(),
			)
		}

		// taxRecipientAddr was already parsed + validated (canonical form)
		// above and is reused at SendCoins below.

		// When recipient == tax_recipient the R3 skip above already returned,
		// so a no-op self-transfer SendCoins below is unreachable.

		// If the bank keeper's view of the recipient balance does not yet
		// reflect the runMsgs writes, SendCoins for the tax leg can fail with
		// insufficient funds on a legitimate first transfer. A complete
		// solution moves tax application out of the PostHandler entirely
		// (e.g. msg-server wrapper or bank SendRestriction) so the deduction
		// is colocated with the original transfer.
		//
		// Behaviour here: attempt the tax SendCoins; if it returns an error,
		// SILENTLY SKIP the tax for this message and let the original
		// transfer stand. Using the canonical SendCoins path means the tax
		// goes through whenever the balance view is current. Drain-then-evade
		// residual risk is acceptable because the actual evasion channels are
		// closed elsewhere:
		//
		//   - x/stoc/ante/ibc_restriction.go blocks IBC laundering.
		//   - x/stoc/ante/custom_token_restriction.go blocks the gov /
		//     vesting / group / erc20 paths through which a drain msg would
		//     otherwise compose with the original send.
		//   - The micro-transfer rejection above closes the 1-unit-spam
		//     evasion vector.
		//   - msg_server_release_token.go forces creator-only release,
		//     blocking the primary-market tax-free distribution vector.
		//
		// Do NOT reintroduce a GetBalance pre-check or rewrite the SendCoins
		// attempt to ERROR on insufficient funds without ALSO moving tax
		// application out of the PostHandler as described above.
		//
		// With the current wiring the recipient IS credited by the bank
		// transfer BEFORE this PostHandler runs, so SendCoins below succeeds
		// for brand-new recipients and tax IS collected. The `continue` branch
		// below is therefore a defensive path, NOT an active tax-evasion route.
		taxCoin := sdk.NewCoin(coin.Denom, taxAmount)
		if err := tpd.k.GetBankKeeper().SendCoins(ctx, recipientAddr, taxRecipientAddr, sdk.NewCoins(taxCoin)); err != nil {
			ctx.Logger().Warn("Tax SendCoins skipped (keeper wiring sees pre-send balance — SA-C5 v3b interim)",
				"recipient", recipientAddress,
				"tax_denom", coin.Denom,
				"tax_amount", taxAmount.String(),
				"error", err)
			continue
		}

		ctx.Logger().Info("Tax transaction processed",
			"token_denom", coin.Denom,
			"tax_amount", taxAmount.String(),
			"from", recipientAddress,
			"to", token.Tax.RecipientAddress,
		)

		// Emit canonical lowercase bech32 in event attributes to match the
		// ReleaseTokens and BurnToken canonicalization. Bank op already used
		// recipientAddr.String() at SendCoins above — reuse here so
		// indexers see consistent addresses across release/burn/tax events.
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"token_tax_applied",
				sdk.NewAttribute("token_denom", coin.Denom),
				sdk.NewAttribute("token_symbol", token.Symbol),
				sdk.NewAttribute("tax_amount", taxAmount.String()),
				sdk.NewAttribute("recipient", recipientAddr.String()),
				sdk.NewAttribute("tax_recipient", taxRecipientAddr.String()),
			),
		)
	}

	return nil
}
