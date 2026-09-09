package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"

	erc20types "github.com/cosmos/evm/x/erc20/types"

	"stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"
)

// CustomTokenChainOpsRestriction is an AnteDecorator enforcing the rule that
// custom stoc tokens (tokens created via x/stoc, representing company securities
// certificates) can ONLY be moved via:
//
//  1. Direct user-to-user transfer (bank.MsgSend, bank.MsgMultiSend) — taxed
//     by TaxPostDecorator
//  2. Self-burn (stoc.MsgBurnToken) — user destroys own balance
//
// Any other operation that would move a custom stoc token out of a user wallet
// is rejected at the ante handler, preventing tax evasion and chain-level
// entanglement of securities tokens.
//
// SA-AUDIT-2026-06-05-fix12 LOW (audit A13): this decorator is the FIRST layer
// of the two-layer restriction model. The SECOND layer is the bank
// SendRestrictionFn registered in app/app.go (see SetSendRestriction call) +
// the IBC SendRestriction in x/stoc/ante/ibc_restriction.go. The ante here
// blocks the user-signed msg flow; the bank/IBC SendRestriction catches any
// indirect path (precompile callbacks, IBC packets, gov-prop disbursement,
// module-internal accounting) that bypasses ante. Both layers are required —
// removing either re-opens custom-token leakage. See [[stochain/design-decisions]]
// for the full threat model and PR #80 audit rationale.
//
// Design rationale: Custom stoc tokens represent company securities (digital
// stock certificates). They are wallet-level ownership records for individual
// holders. They must NOT:
//   - Enter the community pool (funds controlled by chain governance, not the
//     issuing company)
//   - Back governance proposals on STOChain itself (the chain's gov is
//     about chain params, not company business)
//   - Be locked in vesting accounts (vesting with end_time=now+1 trivially
//     bypasses tax enforcement)
//   - Be delegated/staked (staking is for the chain's bond denom only — custom
//     tokens don't secure the chain)
//   - Be bridged cross-chain via IBC (already blocked by IBCCustomTokenRestriction)
//   - Be wrapped into EVM via ERC20 conversion (custom tokens are Cosmos-only —
//     EvmBankKeeper already blocks, this adds ante-level defense)
//
// Native chain denoms (ustoc/astoc/stoc and test/devnet variants) are ALWAYS
// allowed through all the operations above — vesting native tokens for team
// allocation, funding community pool with STOC, staking STOC, etc. are all
// legitimate use cases.
//
// Blocked message types:
//
//   - cosmos.distribution.v1beta1.MsgFundCommunityPool (Amount)
//   - cosmos.gov.v1.MsgSubmitProposal (InitialDeposit)
//   - cosmos.gov.v1.MsgDeposit (Amount)
//   - cosmos.gov.v1beta1.MsgSubmitProposal (InitialDeposit)   — legacy gov
//   - cosmos.gov.v1beta1.MsgDeposit (Amount)                  — legacy gov
//   - cosmos.group.v1.MsgSubmitProposal (recursive check of inner Messages)
//   - cosmos.vesting.v1beta1.MsgCreateVestingAccount (Amount)
//   - cosmos.vesting.v1beta1.MsgCreatePermanentLockedAccount (Amount)
//   - cosmos.vesting.v1beta1.MsgCreatePeriodicVestingAccount (Period.Amount)
//   - cosmos.evm.erc20.v1.MsgConvertCoin (Coin)
//   - cosmos.authz.v1beta1.MsgExec (recursive unwrap)
//
// Matches the design pattern of IBCCustomTokenRestriction: block custom stoc
// tokens from any path other than wallet-to-wallet transfer + self-burn.
//
// SA-AUDIT-2026-06-11 fix19 A16-BOUNDARY-I2 — forward-defense note on
// feegrant: there is deliberately no feegrant case in the blocklist above
// because the feegrant+custom-token combination is a DEAD PATH today — fee
// denoms are always native (feemarket/ante reject custom denoms as fees),
// so a feegrant allowance can never move custom-token value by itself, and
// the feegrant module account is bank-blocked (A16-APP-H2) so coins cannot
// be parked there either. IF a future change ever allows non-native fee
// denoms, revisit this walker AND the AllowedMsgAllowance msg-type surface
// at the same time.
type CustomTokenChainOpsRestriction struct {
	k keeper.Keeper
}

func NewCustomTokenChainOpsRestriction(k keeper.Keeper) CustomTokenChainOpsRestriction {
	return CustomTokenChainOpsRestriction{k: k}
}

func (d CustomTokenChainOpsRestriction) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// SA-AUDIT-2026-06-06 CRIT-1: outer entry — insideProposalWrapper=false.
	// Flipped to true only when recursing INTO a group proposal whose inner
	// messages will execute later via MsgServiceRouter, bypassing both the
	// ante chain AND the tax PostDecorator. See checkMsgs godoc.
	if err := d.checkMsgs(ctx, tx.GetMsgs(), 0, false); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// checkMsgs recursively checks messages including authz-wrapped MsgExec and
// group proposal inner messages. Depth cap mirrors TaxPostDecorator and
// IBCCustomTokenRestriction (DoS prevention).
//
// SA-AUDIT-2026-06-06 CRIT-1 (deep re-audit C1, audit batch B):
// The insideProposalWrapper flag tracks whether we are recursing INSIDE a
// proposal that will execute its inner messages later via MsgServiceRouter,
// bypassing the ante chain AND the tax PostDecorator. When inside such a
// wrapper, bank.MsgSend / bank.MsgMultiSend on a custom token MUST be
// rejected at submission time because:
//
//  1. TaxPostDecorator only unwraps authz.MsgExec (tax_post.go applyTaxesForMsgs
//     switch). group.MsgSubmitProposal / group.MsgExec are NOT unwrapped, so any
//     inner bank.MsgSend on a custom token bypasses tax entirely.
//  2. The bank SendRestriction (blockCustomTokenNonWalletAccounts in app/app.go)
//     treats group policy addresses as regular wallets — group policies are
//     authtypes.BaseAccount-typed (cosmos-sdk x/group/keeper/msg_server.go uses
//     authtypes.NewBaseAccountWithPubKey), NOT sdk.ModuleAccountI, so
//     rejectIfNonWallet returns nil for them and the group → user transfer leg
//     is not caught at the SendRestriction layer.
//  3. Therefore the only place to close the bypass is here, at ante submit
//     time, by rejecting any custom-token bank.MsgSend wrapped in a group
//     proposal BEFORE the proposal is accepted into chain state. Once the
//     proposal is accepted, group.MsgExec (or EXEC_TRY auto-execution) runs
//     inner messages via MsgServiceRouter and the bypass succeeds.
//
// authz.MsgExec is NOT a tax-bypass wrapper (TaxPostDecorator unwraps it via
// the *authz.MsgExec case in applyTaxesForMsgs), so the flag is preserved
// across authz recursion — only flipped to true when entering a group
// proposal. authz inside group still bypasses tax because the tax check
// sees the outer group msg type (which it does not unwrap), not authz.
func (d CustomTokenChainOpsRestriction) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int, insideProposalWrapper bool) error {
	for _, msg := range msgs {
		switch m := msg.(type) {

		// -------- Bank (only enforced when inside a proposal-wrapper that bypasses tax) --------
		// SA-AUDIT-2026-06-06 CRIT-1: bank.MsgSend at OUTER depth, or wrapped via
		// authz.MsgExec (which TaxPostDecorator unwraps), flows through the tax
		// PostDecorator and is permitted. bank.MsgSend wrapped in a group
		// proposal (which the tax PostDecorator does NOT unwrap) would bypass
		// tax — reject at submission time so the proposal is never accepted.
		case *banktypes.MsgSend:
			if insideProposalWrapper {
				if err := d.rejectCustomTokens(ctx, m.Amount, "bank.MsgSend wrapped in proposal (tax bypass channel)", m.FromAddress, m.ToAddress); err != nil {
					return err
				}
			}
		case *banktypes.MsgMultiSend:
			if insideProposalWrapper {
				for _, output := range m.Outputs {
					if err := d.rejectCustomTokens(ctx, output.Coins, "bank.MsgMultiSend wrapped in proposal (tax bypass channel)", "", output.Address); err != nil {
						return err
					}
				}
			}

		// -------- Distribution --------
		case *distributiontypes.MsgFundCommunityPool:
			if err := d.rejectCustomTokens(ctx, m.Amount, "MsgFundCommunityPool", m.Depositor, "community_pool"); err != nil {
				return err
			}

		// -------- Governance v1 (current) --------
		case *govv1types.MsgSubmitProposal:
			if err := d.rejectCustomTokens(ctx, m.InitialDeposit, "gov.v1.MsgSubmitProposal.InitialDeposit", m.Proposer, "gov_module"); err != nil {
				return err
			}
			// SA-AUDIT-2026-06-10 fix18 A15-BOUNDARY-M2: gov v1 proposals
			// dispatch arbitrary inner messages from the gov module account on
			// execution. Bank SendRestriction catches inner bank.MsgSend
			// (gov is ModuleAccountI), but MsgFundCommunityPool /
			// MsgCreateVestingAccount / erc20.MsgConvertCoin embedded in a
			// gov v1 proposal are NOT cleanly validated at runtime (they
			// either panic in their respective keepers or trip the EvmBank
			// gate with a generic error). Recurse here so the proposal is
			// rejected at submit time with a clear, per-msg error — matches
			// the group.MsgSubmitProposal recursion above. insideProposalWrapper
			// flag flipped because gov inner messages execute from the gov
			// module account, bypassing both ante and tax PostDecorator.
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("gov.v1 MsgSubmitProposal nesting depth exceeded (%d)", depth)
			}
			govInnerMsgs, err := m.GetMsgs()
			if err != nil {
				return fmt.Errorf("failed to unwrap gov.v1 MsgSubmitProposal inner messages: %w", err)
			}
			if err := d.checkMsgs(ctx, govInnerMsgs, depth+1, true); err != nil {
				return err
			}
		case *govv1types.MsgDeposit:
			if err := d.rejectCustomTokens(ctx, m.Amount, "gov.v1.MsgDeposit", m.Depositor, "gov_module"); err != nil {
				return err
			}

		// -------- Governance v1beta1 (legacy) --------
		case *govv1beta1types.MsgSubmitProposal:
			if err := d.rejectCustomTokens(ctx, m.InitialDeposit, "gov.v1beta1.MsgSubmitProposal.InitialDeposit", m.Proposer, "gov_module"); err != nil {
				return err
			}
		case *govv1beta1types.MsgDeposit:
			if err := d.rejectCustomTokens(ctx, m.Amount, "gov.v1beta1.MsgDeposit", m.Depositor, "gov_module"); err != nil {
				return err
			}

		// -------- Group proposals (recursive inner msg check) --------
		case *grouptypes.MsgSubmitProposal:
			// Group proposals wrap arbitrary inner sdk.Msg that execute later
			// (bypassing ante chain at execution time). We must check inner
			// messages at submission time to prevent custom token leakage via
			// group.MsgExec → bank.MsgSend → tax bypass.
			//
			// SA-2026-06-02 LOW-6 (senior-skeptic audit): the prior version
			// unmarshalled inner messages BEFORE checking depth. A nested
			// proposal with 100 inner msgs at depth N would force the
			// validator to unmarshal 100 Any{} entries before the depth
			// check refused, amplifying CheckTx CPU. Cheap depth check
			// first now.
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("group MsgSubmitProposal nesting depth exceeded (%d)", depth)
			}
			innerMsgs, err := m.GetMsgs()
			if err != nil {
				return fmt.Errorf("failed to unwrap group MsgSubmitProposal inner messages: %w", err)
			}
			// SA-AUDIT-2026-06-06 CRIT-1: recurse with insideProposalWrapper=true
			// so inner bank.MsgSend / bank.MsgMultiSend on custom tokens is
			// rejected. The tax PostDecorator does NOT unwrap group, so without
			// this submit-time block an attacker can drain custom tokens through
			// a group policy account tax-free.
			if err := d.checkMsgs(ctx, innerMsgs, depth+1, true); err != nil {
				return err
			}

		// -------- Group exec (forward-defense for stale custom-token proposals) --------
		// SA-AUDIT-2026-06-07 LOW-1 (round 2 re-audit R2-LOW1): grouptypes.MsgExec
		// triggers execution of a PREVIOUSLY-stored group proposal via x/group's
		// MsgServiceRouter dispatch path, bypassing the ante chain AND the tax
		// PostDecorator. The CRIT-1 fix above closes the SUBMIT-time leg, so any
		// proposal entering chain state from this decorator's deploy height forward
		// is guaranteed not to carry custom-token inner messages. The remaining
		// risk window is purely stale state — a proposal that was submitted under
		// an older binary (pre-CRIT-1) and is still in the x/group store at the
		// moment the new binary swaps in.
		//
		// At fix14 deployment time we have empirical evidence that this window is
		// empty:
		//   - Devnet has been wiped on every redeploy cycle (mainnet-replay flow),
		//     so no proposals predating CRIT-1 survive.
		//   - Mainnet has not yet been upgraded to v5.0.0; this decorator is not
		//     in production. The mainnet v5.0.0 upgrade prop will deploy the
		//     CRIT-1 fix and the *grouptypes.MsgExec walker simultaneously, and
		//     no x/group proposals exist in mainnet state today (x/group has not
		//     been used on STOChain). The mainnet v5.0.0 upgrade handler can add
		//     a one-time sweep step to invalidate any pending proposals carrying
		//     custom-token inner msgs if the assumption ever changes.
		//
		// Therefore we do NOT need to thread x/group keeper into this decorator
		// to query proposal contents at MsgExec time. Document the assumption
		// here and leave the case as an explicit no-op marker. If a future
		// chain ever boots with x/group state predating CRIT-1, this is the
		// place to add a keeper-backed proposal-content lookup + reject path.
		case *grouptypes.MsgExec:
			// no-op — forward-defense marker; see comment above.
			_ = m

		// -------- Vesting --------
		case *vestingtypes.MsgCreateVestingAccount:
			if err := d.rejectCustomTokens(ctx, m.Amount, "MsgCreateVestingAccount", m.FromAddress, m.ToAddress); err != nil {
				return err
			}
		case *vestingtypes.MsgCreatePermanentLockedAccount:
			if err := d.rejectCustomTokens(ctx, m.Amount, "MsgCreatePermanentLockedAccount", m.FromAddress, m.ToAddress); err != nil {
				return err
			}
		case *vestingtypes.MsgCreatePeriodicVestingAccount:
			for i, period := range m.VestingPeriods {
				if err := d.rejectCustomTokens(ctx, period.Amount, fmt.Sprintf("MsgCreatePeriodicVestingAccount period[%d]", i), m.FromAddress, m.ToAddress); err != nil {
					return err
				}
			}

		// -------- ERC20 conversion (Cosmos → EVM) --------
		case *erc20types.MsgConvertCoin:
			// EvmBankKeeper already blocks custom tokens from EVM paths, but
			// defense-in-depth: reject at ante too, with a clearer error.
			if err := d.rejectCustomTokens(ctx, sdk.NewCoins(m.Coin), "erc20.MsgConvertCoin", m.Sender, m.Receiver); err != nil {
				return err
			}

		// -------- Authz wrapper (recursive) --------
		case *authz.MsgExec:
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("authz MsgExec nesting depth exceeded (%d) in custom token restriction", depth)
			}
			innerMsgs, err := m.GetMessages()
			if err != nil {
				return fmt.Errorf("failed to unwrap authz MsgExec for custom token restriction: %w", err)
			}
			// SA-AUDIT-2026-06-06 CRIT-1: authz.MsgExec is NOT a tax-bypass
			// wrapper (TaxPostDecorator unwraps it), so preserve the parent's
			// insideProposalWrapper flag. authz inside a group proposal still
			// bypasses tax (the tax check sees the outer group msg type),
			// so the inherited true value continues to gate inner bank msgs.
			if err := d.checkMsgs(ctx, innerMsgs, depth+1, insideProposalWrapper); err != nil {
				return err
			}
		}
	}
	return nil
}

// rejectCustomTokens iterates the coin set and returns an error if any coin is
// a custom stoc-managed token. Native chain denoms pass through unchanged.
func (d CustomTokenChainOpsRestriction) rejectCustomTokens(ctx sdk.Context, coins sdk.Coins, msgLabel, fromAddr, toAddr string) error {
	for _, coin := range coins {
		// Native denoms (ustoc/astoc/stoc + test/devnet variants) always allowed.
		if stoctypes.IsNativeDenom(coin.Denom) {
			continue
		}

		// Any denom tracked by x/stoc is a custom token — reject.
		if d.k.HasToken(ctx, coin.Denom) {
			ctx.Logger().Warn("Blocked custom token in chain operation",
				"msg", msgLabel,
				"denom", coin.Denom,
				"from", fromAddr,
				"to", toAddr,
			)
			ctx.EventManager().EmitEvent(
				sdk.NewEvent(
					"custom_token_chain_op_blocked",
					sdk.NewAttribute("msg", msgLabel),
					sdk.NewAttribute("denom", coin.Denom),
					sdk.NewAttribute("from", fromAddr),
					sdk.NewAttribute("to", toAddr),
				),
			)
			return fmt.Errorf(
				"custom token %q cannot be used in %s: stoc-created tokens represent company securities and may only be transferred wallet-to-wallet (bank.MsgSend/MsgMultiSend) or self-burned (stoc.MsgBurnToken). All chain-level operations (gov, community pool, vesting, staking, IBC, ERC20 bridge) are reserved for the native denom",
				coin.Denom, msgLabel,
			)
		}
	}
	return nil
}
