package keeper

import (
	"fmt"

	"stoc/x/stoc/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

const supplyInvariantRoute = "token-supply"

// RegisterInvariants registers all stoc module invariants.
func RegisterInvariants(ir sdk.InvariantRegistry, k Keeper) {
	ir.RegisterRoute(types.ModuleName, supplyInvariantRoute, SupplyInvariant(k))
}

// SupplyInvariant checks that for every stoc-managed token:
//   - bank total supply == token.TotalSupply
//   - module account balance == token.RemainingSupply
//
// SA-H14 audit-2026-05-29: returns broken=false (non-halting) and instead
// logs + emits event when divergence detected. Previous halting behavior
// = chain-halt-as-a-weapon: any user could send `MsgVerifyInvariant` to
// trigger halt if state drift exists (and SA-C2 module-lock vulnerability
// guaranteed drift would exist). Operators monitor `stoc_supply_drift`
// events instead. To restore hard-halt behavior, set `broken = true` again.
func SupplyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		var drift bool

		moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
		tokens := k.GetAllTokens(ctx)

		for _, token := range tokens {
			denom := token.MinimalDenom

			bankSupply := k.bankKeeper.GetSupply(ctx, denom)
			if !bankSupply.Amount.Equal(token.TotalSupply) {
				msg += fmt.Sprintf(
					"\ttoken %s (%s): bank supply %s != tracked TotalSupply %s\n",
					token.Symbol, denom, bankSupply.Amount, token.TotalSupply,
				)
				drift = true
			}

			moduleBalance := k.bankKeeper.GetBalance(ctx, moduleAddr, denom)
			if !moduleBalance.Amount.Equal(token.RemainingSupply) {
				msg += fmt.Sprintf(
					"\ttoken %s (%s): module balance %s != tracked RemainingSupply %s\n",
					token.Symbol, denom, moduleBalance.Amount, token.RemainingSupply,
				)
				drift = true
			}

			if token.RemainingSupply.GT(token.TotalSupply) {
				msg += fmt.Sprintf(
					"\ttoken %s (%s): RemainingSupply %s > TotalSupply %s\n",
					token.Symbol, denom, token.RemainingSupply, token.TotalSupply,
				)
				drift = true
			}
		}

		// SA-H14: log + emit event for operator observability; return broken=false
		// so MsgVerifyInvariant cannot weaponize the check to halt the chain.
		if drift {
			k.Logger().Error("x/stoc supply drift detected (non-halting)", "details", msg)
			ctx.EventManager().EmitEvent(sdk.NewEvent(
				"stoc_supply_drift",
				sdk.NewAttribute("details", msg),
			))
		}
		return sdk.FormatInvariant(types.ModuleName, supplyInvariantRoute, msg), false
	}
}
