package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"

	"stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"
)

// VestingCustomTokenRestriction is an AnteDecorator that blocks ALL vesting-related
// messages from carrying custom tokens created via x/stoc.
//
// Rationale — custom tokens are securities certificates for individual companies:
//   - They should only circulate via direct user-to-user transfers (MsgSend/MsgMultiSend)
//     where the tax system can enforce compliance deductions per transfer.
//   - Vesting would let a sender trivially bypass the tax: MsgCreateVestingAccount
//     with end_time = now + 1s effectively transfers full amount to the recipient
//     without going through MsgSend → TaxPostDecorator never applies.
//   - Custom tokens are NOT designed to participate in chain-level features
//     (staking, governance, community pool, vesting schedules). Only the native
//     STOC denom participates in those.
//
// Native chain denoms (ustoc/astoc/stoc and test/devnet variants) are allowed
// through — vesting of native tokens is a legitimate use case (team tokens,
// genesis allocation, payroll, etc.).
//
// Blocked messages:
//   - cosmos.vesting.v1beta1.MsgCreateVestingAccount
//   - cosmos.vesting.v1beta1.MsgCreatePermanentLockedAccount
//   - cosmos.vesting.v1beta1.MsgCreatePeriodicVestingAccount
//
// Matches the design pattern of IBCCustomTokenRestriction: block custom stoc
// tokens from leaving the MsgSend/MsgMultiSend taxation path.
type VestingCustomTokenRestriction struct {
	k keeper.Keeper
}

func NewVestingCustomTokenRestriction(k keeper.Keeper) VestingCustomTokenRestriction {
	return VestingCustomTokenRestriction{k: k}
}

func (d VestingCustomTokenRestriction) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.checkMsgs(ctx, tx.GetMsgs(), 0); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// checkMsgs recursively checks messages including authz-wrapped MsgExec.
// The recursion depth limit mirrors the cap used by TaxPostDecorator and
// IBCCustomTokenRestriction to prevent DoS via deeply nested authz messages.
func (d VestingCustomTokenRestriction) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *vestingtypes.MsgCreateVestingAccount:
			if err := d.rejectCustomTokens(ctx, m.Amount, "MsgCreateVestingAccount", m.FromAddress, m.ToAddress); err != nil {
				return err
			}
		case *vestingtypes.MsgCreatePermanentLockedAccount:
			if err := d.rejectCustomTokens(ctx, m.Amount, "MsgCreatePermanentLockedAccount", m.FromAddress, m.ToAddress); err != nil {
				return err
			}
		case *vestingtypes.MsgCreatePeriodicVestingAccount:
			// Periodic vesting splits the amount across multiple periods.
			// Check each period's amount for custom tokens.
			for i, period := range m.VestingPeriods {
				if err := d.rejectCustomTokens(ctx, period.Amount, fmt.Sprintf("MsgCreatePeriodicVestingAccount period[%d]", i), m.FromAddress, m.ToAddress); err != nil {
					return err
				}
			}
		case *authz.MsgExec:
			if depth >= maxAuthzUnwrapDepth {
				return fmt.Errorf("authz MsgExec nesting depth exceeded (%d) in vesting restriction check", depth)
			}
			innerMsgs, err := m.GetMessages()
			if err != nil {
				return fmt.Errorf("failed to unwrap authz MsgExec for vesting restriction: %w", err)
			}
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

// rejectCustomTokens iterates the coin set and returns an error if any coin is
// a custom stoc-managed token. Native chain denoms pass through.
func (d VestingCustomTokenRestriction) rejectCustomTokens(ctx sdk.Context, coins sdk.Coins, msgLabel, fromAddr, toAddr string) error {
	for _, coin := range coins {
		// Native denoms (ustoc/astoc/stoc + variants) are always allowed.
		if stoctypes.IsNativeDenom(coin.Denom) {
			continue
		}

		// Any denom tracked by x/stoc is a custom token — reject.
		if d.k.HasToken(ctx, coin.Denom) {
			ctx.Logger().Warn("Blocked vesting of custom token",
				"msg", msgLabel,
				"denom", coin.Denom,
				"from", fromAddr,
				"to", toAddr,
			)
			ctx.EventManager().EmitEvent(
				sdk.NewEvent(
					"vesting_custom_token_blocked",
					sdk.NewAttribute("msg", msgLabel),
					sdk.NewAttribute("denom", coin.Denom),
					sdk.NewAttribute("from", fromAddr),
					sdk.NewAttribute("to", toAddr),
				),
			)
			return fmt.Errorf(
				"vesting of custom token %q is not allowed via %s: custom tokens created via x/stoc are restricted to direct transfers (MsgSend/MsgMultiSend) to preserve tax enforcement",
				coin.Denom, msgLabel,
			)
		}
	}
	return nil
}
