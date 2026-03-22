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
// If either condition fails, the chain halts so operators can investigate.
func SupplyInvariant(k Keeper) sdk.Invariant {
	return func(ctx sdk.Context) (string, bool) {
		var msg string
		var broken bool

		moduleAddr := authtypes.NewModuleAddress(types.ModuleName)
		tokens := k.GetAllTokens(ctx)

		for _, token := range tokens {
			denom := token.MinimalDenom

			// Check 1: bank supply == token.TotalSupply
			bankSupply := k.bankKeeper.GetSupply(ctx, denom)
			if !bankSupply.Amount.Equal(token.TotalSupply) {
				msg += fmt.Sprintf(
					"\ttoken %s (%s): bank supply %s != tracked TotalSupply %s\n",
					token.Symbol, denom, bankSupply.Amount, token.TotalSupply,
				)
				broken = true
			}

			// Check 2: module account balance == token.RemainingSupply
			moduleBalance := k.bankKeeper.GetBalance(ctx, moduleAddr, denom)
			if !moduleBalance.Amount.Equal(token.RemainingSupply) {
				msg += fmt.Sprintf(
					"\ttoken %s (%s): module balance %s != tracked RemainingSupply %s\n",
					token.Symbol, denom, moduleBalance.Amount, token.RemainingSupply,
				)
				broken = true
			}

			// Check 3: RemainingSupply must never exceed TotalSupply
			if token.RemainingSupply.GT(token.TotalSupply) {
				msg += fmt.Sprintf(
					"\ttoken %s (%s): RemainingSupply %s > TotalSupply %s\n",
					token.Symbol, denom, token.RemainingSupply, token.TotalSupply,
				)
				broken = true
			}
		}

		return sdk.FormatInvariant(types.ModuleName, supplyInvariantRoute, msg), broken
	}
}
