package app

import (
	"context"
	"fmt"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	stoctypes "stoc/x/stoc/types"
)

// Event types emitted by the v5.0.4 foundation burn.
const (
	EventTypeV504FoundationBurn        = "upgrade_v504_foundation_burn"
	EventTypeV504FoundationBurnSkipped = "upgrade_v504_foundation_burn_skipped"
	EventTypeV504FoundationBurnSummary = "upgrade_v504_foundation_burn_summary"
)

// v504Burn is one hardcoded burn instruction. Amount is in ustoc.
type v504Burn struct {
	Category string
	Address  string
	Amount   int64
}

// v504FoundationBurns is the fixed burn list of the v5.0.4 upgrade. Amounts
// are whole ustoc (1 STOC = 1,000,000 ustoc). The sum is 3,000,000,000 STOC.
var v504FoundationBurns = []v504Burn{
	{"Seed&Private Sales", "stoc18tfsgxy6hx6jy3dmcq49m7ex398hn926h83jqk", 300_000_000_000_000},
	{"Team&Advisor", "stoc1xevdypv5xmcjv8rw87c87j7ac5ljyzjjwdedql", 550_000_001_000_000},
	{"Public sales", "stoc1adgm0sjpesjafvqgz0xj2na8z0u4fdepza3ddz", 150_000_000_000_000},
	{"Liquidity&Exchange", "stoc1xmgghfl5c7ww2tk52fsd8s65xsffpl67qj4hp9", 150_000_000_000_000},
	{"Staking Rewards", "stoc13qn7rhcwdy42n3ay28a09jfhgesgw8jahy2dgn", 900_000_000_000_000},
	{"Development fund", "stoc1yk6cwrmxfajcj8rgk4v6kzv0fytyv48me66qx7", 649_999_999_000_000},
	{"Community rewards", "stoc103rm28zu9fxndzaxuapuu36y7ejyzxe42kttpc", 300_000_000_000_000},
}

// upgradeHandlerV504 runs the module migrations (none are expected) and then
// burns the fixed foundation amounts.
//
// A returned error or a panic at the upgrade height halts the chain, so the
// burn step never fails: every wallet is processed in its own cache context
// and any wallet-level problem (missing account, insufficient spendable
// balance, bank error, panic) discards that wallet's writes, emits a skipped
// event and moves on. Only RunMigrations errors propagate, as in v5.0.3.
func (app *App) upgradeHandlerV504(ctx context.Context, _ upgradetypes.Plan, fromVM module.VersionMap) (module.VersionMap, error) {
	vm, err := app.ModuleManager.RunMigrations(ctx, app.Configurator(), fromVM)
	if err != nil {
		return vm, err
	}

	// Infinite gas meter: the burn must cost the same on warm and cold nodes
	// so that block gas (and therefore the app hash) cannot diverge.
	sdkCtx := sdk.UnwrapSDKContext(ctx).WithGasMeter(storetypes.NewInfiniteGasMeter())
	app.applyV504FoundationBurn(sdkCtx)
	return vm, nil
}

// applyV504FoundationBurn burns every entry of v504FoundationBurns that has
// enough spendable ustoc. It never returns an error and never panics.
//
// The burn denom is the staking bond denom read from state (ustoc on
// mainnet; all networks use 6 decimals, so the base-unit amounts are the same).
// If it cannot be read, every wallet is skipped.
func (app *App) applyV504FoundationBurn(ctx sdk.Context) {
	denom, denomErr := app.v504BondDenom(ctx)
	if denomErr != "" {
		ctx.Logger().Error("v5.0.4 foundation burn: bond denom unavailable, skipping all wallets", "reason", denomErr)
	}
	supplyBefore, supplyAfter := math.ZeroInt(), math.ZeroInt()
	if denomErr == "" {
		supplyBefore = app.BankKeeper.GetSupply(ctx, denom).Amount
	}
	total := math.ZeroInt()

	for _, b := range v504FoundationBurns {
		amount := math.NewInt(b.Amount)
		reason := denomErr
		if reason == "" {
			reason = app.burnV504Wallet(ctx, denom, b, amount)
		}
		if reason != "" {
			app.emitV504Skipped(ctx, denom, b, amount, reason)
			continue
		}
		total = total.Add(amount)
	}

	if denomErr == "" {
		supplyAfter = app.BankKeeper.GetSupply(ctx, denom).Amount
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		EventTypeV504FoundationBurnSummary,
		sdk.NewAttribute("total_burned", total.String()),
		sdk.NewAttribute("denom", denom),
		sdk.NewAttribute("supply_before", supplyBefore.String()),
		sdk.NewAttribute("supply_after", supplyAfter.String()),
	))
	ctx.Logger().Info("v5.0.4 foundation burn finished",
		"total_burned", total.String(), "supply_before", supplyBefore.String(), "supply_after", supplyAfter.String())
}

// burnV504Wallet burns amount ustoc from one wallet inside a cache context.
// It returns "" on success (state written, burn event emitted) or a reason
// string when the wallet was skipped (nothing written).
func (app *App) burnV504Wallet(ctx sdk.Context, denom string, b v504Burn, amount math.Int) (skipReason string) {
	defer func() {
		if r := recover(); r != nil {
			skipReason = fmt.Sprintf("panic: %v", r)
		}
	}()

	addr, err := sdk.AccAddressFromBech32(b.Address)
	if err != nil {
		return "invalid address: " + err.Error()
	}

	cacheCtx, writeCache := ctx.CacheContext()
	spendable := app.BankKeeper.SpendableCoin(cacheCtx, addr, denom).Amount
	if spendable.LT(amount) {
		return "insufficient spendable balance: " + spendable.String()
	}

	coins := sdk.NewCoins(sdk.NewCoin(denom, amount))
	if err := app.BankKeeper.SendCoinsFromAccountToModule(cacheCtx, addr, stoctypes.ModuleName, coins); err != nil {
		return "send to module failed: " + err.Error()
	}
	if err := app.BankKeeper.BurnCoins(cacheCtx, stoctypes.ModuleName, coins); err != nil {
		return "burn failed: " + err.Error()
	}

	// Events emitted on the cache context are forwarded to ctx on write.
	cacheCtx.EventManager().EmitEvent(sdk.NewEvent(
		EventTypeV504FoundationBurn,
		sdk.NewAttribute("address", b.Address),
		sdk.NewAttribute("category", b.Category),
		sdk.NewAttribute("amount", amount.String()),
		sdk.NewAttribute("denom", denom),
	))
	writeCache()
	ctx.Logger().Info("v5.0.4 foundation burn", "address", b.Address, "category", b.Category, "amount", amount.String())
	return ""
}

func (app *App) emitV504Skipped(ctx sdk.Context, denom string, b v504Burn, wanted math.Int, reason string) {
	spendable := "unknown"
	func() {
		defer func() { _ = recover() }()
		if addr, err := sdk.AccAddressFromBech32(b.Address); err == nil && denom != "" {
			spendable = app.BankKeeper.SpendableCoin(ctx, addr, denom).Amount.String()
		}
	}()
	ctx.EventManager().EmitEvent(sdk.NewEvent(
		EventTypeV504FoundationBurnSkipped,
		sdk.NewAttribute("address", b.Address),
		sdk.NewAttribute("category", b.Category),
		sdk.NewAttribute("wanted", wanted.String()),
		sdk.NewAttribute("spendable", spendable),
		sdk.NewAttribute("denom", denom),
		sdk.NewAttribute("reason", reason),
	))
	ctx.Logger().Error("v5.0.4 foundation burn skipped",
		"address", b.Address, "category", b.Category, "wanted", wanted.String(), "spendable", spendable, "reason", reason)
}

// v504BondDenom reads the staking bond denom from state. It returns a
// non-empty reason when the denom cannot be determined.
func (app *App) v504BondDenom(ctx sdk.Context) (denom, reason string) {
	defer func() {
		if r := recover(); r != nil {
			denom, reason = "", fmt.Sprintf("panic reading staking params: %v", r)
		}
	}()
	params, err := app.StakingKeeper.GetParams(ctx)
	if err != nil {
		return "", "staking params unavailable: " + err.Error()
	}
	if params.BondDenom == "" {
		return "", "empty bond denom"
	}
	return params.BondDenom, ""
}
