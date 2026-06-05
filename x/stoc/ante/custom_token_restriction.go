package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
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
//   - Back governance proposals on the STOC chain itself (the chain's gov is
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
type CustomTokenChainOpsRestriction struct {
	k keeper.Keeper
}

func NewCustomTokenChainOpsRestriction(k keeper.Keeper) CustomTokenChainOpsRestriction {
	return CustomTokenChainOpsRestriction{k: k}
}

func (d CustomTokenChainOpsRestriction) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.checkMsgs(ctx, tx.GetMsgs(), 0); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// checkMsgs recursively checks messages including authz-wrapped MsgExec and
// group proposal inner messages. Depth cap mirrors TaxPostDecorator and
// IBCCustomTokenRestriction (DoS prevention).
func (d CustomTokenChainOpsRestriction) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {

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
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}

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
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
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
