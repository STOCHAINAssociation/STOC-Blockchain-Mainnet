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
	if !success || !msgsExecuted(ctx, simulate) {
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

// msgsExecuted reports whether baseapp executed the tx messages before calling
// the post handler. The tax leg moves funds the recipient received from the
// message itself, so it can only be applied where the messages ran.
//
// cosmos-sdk v0.53 baseapp runTx calls the post handler in EVERY exec mode
// with success=true, but runMsgs only executes messages in FinalizeBlock and
// Simulate. In CheckTx, ReCheckTx, PrepareProposal and ProcessProposal the
// recipient has not received the transfer yet, so a fail-closed tax leg there
// would reject every taxed transfer to a recipient that does not already hold
// the tax amount (mempool admission, proposal building and proposal
// verification). Those modes skip the tax; FinalizeBlock enforces it.
//
// Simulate runs the tax (v5.0.3) so gas estimation (--gas auto) includes the
// tax leg and a transfer that would fail its tax is reported before broadcast.
// The simulate context derives from the CheckTx state (IsCheckTx is true
// there), hence the explicit simulate check first.
func msgsExecuted(ctx sdk.Context, simulate bool) bool {
	if simulate {
		return true
	}
	if ctx.IsCheckTx() {
		return false
	}
	switch ctx.ExecMode() {
	case sdk.ExecModePrepareProposal, sdk.ExecModeProcessProposal,
		sdk.ExecModeVoteExtension, sdk.ExecModeVerifyVoteExtension:
		return false
	}
	return true
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
			// SA-2026-06-04 LOW defense-in-depth: explicit single-input guard.
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
			// SA-AUDIT-2026-06-11 fix19 A16-STO-M1: parse the sender ONCE for
			// the whole MultiSend instead of once per output — a 50-output
			// MultiSend re-ran the same bech32 decode 50× (perf griefing
			// surface; MaxMultiSendOutputs bounds it but the work is pure
			// waste). Canonicalization semantics identical to the per-call
			// parse it replaces.
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
// Business rules (locked 2026-06-03 — preserve through future audits):
//
//   - R1: tax recipient is whatever creator set on the token at CreateToken
//     time: the creator wallet, a team multisig or any other regular user
//     wallet. Since v5.0.3 it must be an existing plain wallet that can
//     receive custom tokens (no module, vesting, group policy, contract,
//     precompile or IBC escrow address — see Keeper.validateTaxRecipient),
//     because this tax leg is fail-closed. Within that, the choice belongs to
//     the token issuer.
//   - R2: SKIP tax when sender == tax_recipient. Taxing the tax collector
//     when they themselves are sending is a no-op (the tax flows back to
//     the sender) and the round-trip wastes gas + emits a misleading event.
//   - R3: SKIP tax when recipient == tax_recipient. Recipient would receive
//     amount × (1 - tax_pct) PLUS tax_pct as the tax payment — total still
//     equals amount — so splitting it is also a no-op. Indexers + wallets
//     read cleaner when the single transfer event stands without a paired
//     tax_applied event.
//   - R4: tax is computed FROM the sent amount (not from sender's remaining
//     balance). Single atomic split. If the tax leg cannot be paid (the
//     recipient no longer holds the amount, or the tax recipient cannot
//     receive custom tokens) the whole tx FAILS (v5.0.3) — a taxable
//     transfer never succeeds untaxed.
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
	// SA-AUDIT-2026-06-05-fix11 MED (audit pass A12): canonicalize BOTH sides
	// of the R2/R3 compare. fix8 M1 only normalized tax_recipient; an attacker
	// could still craft an ALL-UPPERCASE FromAddress equal to the tax_recipient
	// to lex-mismatch the lowercase canonical form, dodge the R2 skip, and
	// pump tax to themselves while the victim pays. Verified exploitable via
	// unit test (victim 100→90, tax_recipient 100→110). Parse the sender once
	// here; tax_recipient parsing remains inside the loop because it's per-token.
	senderAddrParsed, senderErr := sdk.AccAddressFromBech32(senderAddress)
	if senderErr != nil {
		return fmt.Errorf("invalid sender address: %v", senderErr)
	}
	return tpd.applyTaxForRecipientCanonical(ctx, senderAddrParsed.String(), recipientAddress, coins)
}

// applyTaxForRecipientCanonical is applyTaxForRecipient with the sender
// already in canonical bech32 form.
//
// SA-AUDIT-2026-06-11 fix19 A16-STO-M1: split out so the MsgMultiSend path
// can parse its single semantic sender ONCE and reuse the canonical form
// across all outputs instead of re-running bech32 decode per output.
// CALLER CONTRACT: senderCanonical MUST be the output of
// sdk.AccAddress.String() (canonical lowercase) — passing a raw
// user-supplied string here would re-open the uppercase R2-dodge that
// fix11 M1 closed.
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

		// SA-2026-06-04 LOW defense-in-depth: validate token.Tax.RecipientAddress
		// as bech32 BEFORE the R2/R3 string compares below. CreateToken already
		// rejects malformed values today, but if a future state-migration ever
		// landed a malformed string in this field the R2/R3 string compare could
		// produce a false-positive skip when sender/recipient happens to
		// lex-equal the malformed value. Fail closed instead.
		// SA-AUDIT-2026-06-05 M1: re-encode via AccAddress.String() to obtain
		// the canonical lowercase form. Bech32 permits ALL-UPPERCASE addresses
		// to round-trip the checksum, so a stored uppercase tax_recipient would
		// fail the case-sensitive R2/R3 string compares below and let tax bypass.
		taxRecipientAddr, addrErr := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if addrErr != nil {
			return fmt.Errorf("token %s has malformed Tax.RecipientAddress %q: %v", coin.Denom, token.Tax.RecipientAddress, addrErr)
		}
		taxRecipientCanonical := taxRecipientAddr.String()

		// R2 / R3 skip conditions (locked 2026-06-03). See function godoc for
		// rationale. SA-AUDIT-2026-06-05-fix11 MED: compare canonical-vs-canonical
		// on BOTH sides — sender/recipient canonicalized above (line ~170 area),
		// tax_recipient canonicalized here. Prevents uppercase-FromAddress dodge.
		if senderCanonical == taxRecipientCanonical {
			// SA-AUDIT-2026-06-11 fix19 Q6=B (owner policy decision):
			// the R2 skip stays, but it now leaves an audit
			// trail. For STO compliance every transfer that moves a taxed
			// security WITHOUT paying tax must be explainable from the event
			// stream alone — auditors should not have to re-derive the R2
			// rule from source to explain a missing tax_applied event. The
			// BE indexer (stoc-backend-sync-chain) indexes this event;
			// schema task tracked separately.
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
		// SA-2026-06-04 LOW-3 (comprehensive audit): nil or negative Percent
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
		// SA-AUDIT-2026-06-10 fix18 A15-STO-L2: defensive — after the
		// half-cap + recipient-retain floor, taxAmount can only collapse
		// to zero if the SA-L1 amount<=1 reject above is bypassed (e.g. a
		// future code change). Replace the silent `continue` with a
		// fail-loud reject so the "every taxable transfer pays tax"
		// invariant stays explicit + testable. The SendCoins attempt below
		// is fail-closed as well (v5.0.3).
		if taxAmount.IsZero() {
			return fmt.Errorf(
				"tax computation collapsed to zero for %s %s at percent %s — refusing to silently skip tax (A15-STO-L2)",
				coin.Amount.String(), coin.Denom, taxPercent.String(),
			)
		}

		// taxRecipientAddr already parsed + validated above (line ~191, M1 canonicalize).
		// Reused at SendCoins below — second AccAddressFromBech32 call removed
		// after re-audit fix9 LOW finding flagged redundant re-parse.

		// SA-C5 audit-2026-05-29 PROPOSED skipping the recipient==taxRecipient
		// skip on the theory that an attacker could promote themselves to
		// tax_recipient. The 2026-06-03 business rule review (locked in this
		// file's applyTaxForRecipient godoc) confirmed that theory is INVALID
		// for STO chains because tax_recipient is creator-controlled at token
		// creation time. The recipient==tax_recipient skip is now ENFORCED by
		// R3 above and the no-op SendCoins below is unreachable.

		// v5.0.3 audit round 1: the tax leg is fail-closed. If SendCoins
		// fails the tx fails — it never completes untaxed. The former SA-C5
		// v3b silent skip existed because the recipient's "pre-send balance"
		// was seen at this point (2026-06 devnet replay). That was CheckTx /
		// ProcessProposal, where baseapp runs the post handler without
		// executing the messages; those modes now skip the tax entirely
		// (msgsExecuted). In FinalizeBlock and Simulate the recipient is
		// credited before this PostHandler runs (re-measured on devnet
		// 2026-07-09), so the skip only ever fired there in abuse cases:
		//   - drain-then-evade: a later message in the same tx moves the
		//     received amount away, so the recipient can no longer pay;
		//   - a tax recipient that cannot receive custom tokens (e.g. a
		//     fresh tax address turned into a permanently locked vesting
		//     account by a third party) silently disabled tax for everyone.
		// MsgCreateToken now requires an existing plain wallet as tax
		// recipient and the v5.0.3 upgrade handler materialises missing
		// recipients of existing taxed tokens, so a legitimate transfer
		// cannot hit this error.
		taxCoin := sdk.NewCoin(coin.Denom, taxAmount)
		if err := tpd.k.GetBankKeeper().SendCoins(ctx, recipientAddr, taxRecipientAddr, sdk.NewCoins(taxCoin)); err != nil {
			return fmt.Errorf("collect tax %s from %s to %s: %w", taxCoin.String(), recipientCanonical, taxRecipientCanonical, err)
		}

		ctx.Logger().Info("Tax transaction processed",
			"token_denom", coin.Denom,
			"tax_amount", taxAmount.String(),
			"from", recipientAddress,
			"to", token.Tax.RecipientAddress,
		)

		// SA-AUDIT-2026-06-10 fix18 A15-STO-I1: emit canonical lowercase
		// bech32 in event attributes to match fix15-3 (ReleaseTokens) +
		// fix16 A14-STOC-L5 (Burn) canonicalization. Bank op already used
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
