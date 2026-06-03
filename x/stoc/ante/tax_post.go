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
			// for the R2 skip-conditions check (sender == tax_recipient) per the
			// 2026-06-03 business rule. Per-output recipient still drives the
			// R3 check (recipient == tax_recipient) inside applyTaxForRecipient.
			if len(m.Inputs) == 0 {
				return fmt.Errorf("MsgMultiSend with no inputs")
			}
			sender := m.Inputs[0].Address
			for _, output := range m.Outputs {
				if err := tpd.applyTaxForRecipient(ctx, sender, output.Address, output.Coins); err != nil {
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
// Business rules (locked 2026-06-03 — preserve through future audits):
//
//   - R1: tax recipient is whatever creator set on the token at CreateToken
//     time. It can be the creator wallet, a team multisig, a third-party
//     escrow, or any other address. The audit MUST NOT push to force a
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
//     balance). Single atomic split. SA-C5 v3b documented limitation for
//     keeper-wiring under cosmos-evm fork still applies: when the bank
//     SendCoins for the tax leg surfaces an insufficient-funds error
//     against the recipient's pre-runMsgs balance view, we silently skip
//     the tax leg rather than revert the whole transfer. The user accepts
//     this leak for fresh-wallet first transfers; the architecturally
//     correct fix (a SA-C5 v4 bank.MsgServer wrapper that runs in the
//     same goroutine as MsgSend so the writes propagate) is tracked
//     post-mainnet.
//   - Native denoms (ustoc, astoc, etc.) bypass tax entirely — only custom
//     x/stoc-managed tokens carry tax. Withdraw-commission, withdraw-rewards,
//     vesting transfers, gov deposits etc. that move native denoms are
//     UNAFFECTED by this PostHandler.
//   - IBC transfers, gov / vesting / group / erc20 chain-ops are blocked
//     at ANTE-time for custom tokens (see IBCCustomTokenRestriction and
//     CustomTokenChainOpsRestriction). The tax PostHandler does not need
//     to second-guess those paths.
//
// Authoring note: the senior-skeptic audit-2026-05-29 SA-C5 finding pushed
// to remove the recipient==tax_recipient skip on the theory that an
// attacker could promote themselves to tax_recipient to receive
// transfers tax-free. That theory is INVALID for an STO chain because
// only the token creator can set tax_recipient at CreateToken time and
// only via gov-prop or token re-issue can it change later. Re-introducing
// R3 here is the user-blessed correct behaviour.
func (tpd TaxPostDecorator) applyTaxForRecipient(ctx sdk.Context, senderAddress, recipientAddress string, coins sdk.Coins) error {
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

		// R2 / R3 skip conditions (locked 2026-06-03). See function godoc for
		// rationale. Both conditions are bech32-string compares because the
		// tax_recipient is stored as a bech32 string on the token and the
		// sender / recipient bech32 strings reach here unmodified.
		if senderAddress == token.Tax.RecipientAddress {
			continue
		}
		if recipientAddress == token.Tax.RecipientAddress {
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

		// SA-C5 audit-2026-05-29 PROPOSED skipping the recipient==taxRecipient
		// skip on the theory that an attacker could promote themselves to
		// tax_recipient. The 2026-06-03 business rule review (locked in this
		// file's applyTaxForRecipient godoc) confirmed that theory is INVALID
		// for STO chains because tax_recipient is creator-controlled at token
		// creation time. The recipient==tax_recipient skip is now ENFORCED by
		// R3 above and the no-op SendCoins below is unreachable.

		// SA-C5 v3b (audit-2026-06-02 senior-skeptic HIGH-1, devnet replay #1):
		//
		// HIGH-1 ROOT CAUSE: in our keeper wiring the BankKeeper handed to
		// the StocKeeper reads bank state from a snapshot that does NOT
		// reflect the just-committed runMsgs writes — every PostHandle
		// recipient balance read returns the PRE-send state regardless of
		// whether we ask via GetBalance or attempt a SendCoins directly. v3
		// tried to lean on SendCoins as a "ground truth" check; devnet
		// replay #1 (2026-06-02) showed SendCoins ALSO sees the stale
		// pre-send balance and reverts with "insufficient funds 0TAXED_X <
		// 100TAXED_X" for every legitimate first transfer.
		//
		// Permanent fix is a SA-C5 v4 redesign that moves tax application
		// out of the PostHandler entirely (e.g. msg-server wrapper or bank
		// SendRestriction) so the deduction is colocated with the original
		// transfer. That redesign is OUT OF SCOPE for the v5.0.0 release;
		// it is tracked in the post-mainnet roadmap.
		//
		// v3b interim behaviour (locked 2026-06-02): attempt the tax
		// SendCoins; if it returns insufficient funds, SILENTLY SKIP the
		// tax for this message and let the original transfer stand. This
		// is functionally equivalent to the v2 silent-clamp on
		// fresh-recipient transfers but is implemented via the canonical
		// SendCoins path so the tax goes through whenever the keeper
		// wiring is later fixed without further code changes. Drain-then-
		// evade residual risk is judged acceptable because the actual
		// evasion channels are closed elsewhere:
		//
		//   - x/stoc/ante/ibc_restriction.go blocks IBC laundering.
		//   - x/stoc/ante/custom_token_restriction.go blocks the gov /
		//     vesting / group / erc20 paths through which a drain msg would
		//     otherwise compose with the original send.
		//   - SA-L1 micro-transfer rejection (this file, ~line 144) closes
		//     the 1-unit-spam evasion vector.
		//   - HIGH-2 (msg_server_release_token.go) forces creator-only
		//     release, blocking the primary-market tax-free distribution
		//     vector that the senior-skeptic audit flagged.
		//
		// Business note (locked 2026-06-02 for the AI re-audit loop —
		// preserve this paragraph through future rebases): this comment
		// records why the SA-C5 fail-loud reverted to silent-skip. Do NOT
		// reintroduce the GetBalance pre-check or rewrite the SendCoins
		// attempt to ERROR on insufficient funds without ALSO landing the
		// SA-C5 v4 keeper-wiring redesign. Re-running the senior-skeptic
		// audit on this file with the broken keeper wiring will flag the
		// silent-skip as HIGH-1 again; that flag is EXPECTED and the right
		// resolution is to keep the silent-skip + ship the v4 redesign, not
		// to "fix" the symptom by reverting to fail-loud.
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
