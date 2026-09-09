package keeper_test

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	"stoc/x/evmutil/types"
)

// SA-AUDIT-2026-06-06 MED-2 regression coverage: convertCoinsForOutflow
// silently drops dust legs that round to zero ustoc (SA-2026-06-02 MED-3
// business rule kept intact). The MED-2 fix layers an observability event
// on top of the silent drop so off-chain indexers and EVM contracts with
// independent ledgers can reconcile dropped dust.

// dustTestCtx returns an sdk.Context with a fresh EventManager so tests can
// inspect emitted events. Production paths always wrap an sdk.Context via
// sdk.WrapSDKContext before invoking keeper methods, so the
// trySDKContext helper in bank_keeper.go always finds an EventManager and
// emits — tests that pass context.Background() (e.g. the existing
// TestSendCoins_DustAmount_SilentlyDropped) silently skip emission, which
// is intentional best-effort behavior documented in the trySDKContext
// godoc.
func dustTestCtx() sdk.Context {
	return sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger()).WithEventManager(sdk.NewEventManager())
}

func TestConvertOutflow_DustDrop_EmitsObservabilityEvent(t *testing.T) {
	ebk, _ := setup(t)
	from, to := testAddr(), testAddr2()
	ctx := dustTestCtx()

	// 999 wei astoc rounds DOWN to 0 ustoc → silent drop + event.
	amt := sdk.NewCoins(sdk.NewCoin("astoc", math.NewInt(999)))
	require.NoError(t, ebk.SendCoins(ctx, from, to, amt))

	events := ctx.EventManager().Events()
	dustEvents := filterEventsByType(events, "evm_dust_dropped")
	require.Len(t, dustEvents, 1, "MED-2: expected exactly one evm_dust_dropped event for the single dust leg")

	attrs := attrMap(dustEvents[0])
	require.Equal(t, "astoc", attrs["evm_denom"])
	require.Equal(t, "999", attrs["evm_amount"])
	require.Equal(t, "ustoc", attrs["cosmos_denom"])
	require.Contains(t, attrs["reason"], "amount below 1 ustoc resolution",
		"MED-2: event reason must surface the dust-drop diagnostic so contract authors and indexers can identify the cause")
}

func TestConvertOutflow_NonDust_NoDustEvent(t *testing.T) {
	// Non-dust amount (exactly 1 ustoc worth = 1e12 wei) must NOT emit
	// evm_dust_dropped. Pin the negative case so a future refactor that
	// over-emits surfaces here.
	ebk, _ := setup(t)
	from, to := testAddr(), testAddr2()
	ctx := dustTestCtx()

	amt := sdk.NewCoins(sdk.NewCoin("astoc", types.ConversionMultiplier))
	require.NoError(t, ebk.SendCoins(ctx, from, to, amt))

	dustEvents := filterEventsByType(ctx.EventManager().Events(), "evm_dust_dropped")
	require.Empty(t, dustEvents, "MED-2: non-dust amount must not emit evm_dust_dropped")
}

func TestConvertOutflow_MultipleDustLegs_EmitsOnePerLeg(t *testing.T) {
	// SendCoins's sdk.Coins consolidates by denom, so two astoc entries
	// fold to one. Exercise the multi-emission path via two distinct
	// outflow ops on the same context — both must surface as events.
	ebk, _ := setup(t)
	from, to := testAddr(), testAddr2()
	ctx := dustTestCtx()

	dust := sdk.NewCoins(sdk.NewCoin("astoc", math.NewInt(1)))
	require.NoError(t, ebk.SendCoins(ctx, from, to, dust))
	require.NoError(t, ebk.MintCoins(ctx, "evm", dust))
	require.NoError(t, ebk.BurnCoins(ctx, "evm", dust))

	dustEvents := filterEventsByType(ctx.EventManager().Events(), "evm_dust_dropped")
	require.Len(t, dustEvents, 3, "MED-2: each outflow op that drops dust must emit one event")
}

func TestConvertOutflow_MixedDustAndNonDust_EmitsOnlyForDust(t *testing.T) {
	// Mixed input: dust astoc + non-dust ustoc passes through together.
	// Event must fire only for the dust leg; non-dust ustoc leg is
	// non-observable (no conversion needed).
	ebk, _ := setup(t)
	from, to := testAddr(), testAddr2()
	ctx := dustTestCtx()

	amt := sdk.NewCoins(
		sdk.NewCoin("astoc", math.NewInt(999)), // dust
		sdk.NewCoin("ustoc", math.NewInt(5)),   // non-dust, passes through
	)
	require.NoError(t, ebk.SendCoins(ctx, from, to, amt))

	dustEvents := filterEventsByType(ctx.EventManager().Events(), "evm_dust_dropped")
	require.Len(t, dustEvents, 1, "MED-2: only the astoc dust leg must emit; ustoc passthrough is non-observable")
	require.Equal(t, "astoc", attrMap(dustEvents[0])["evm_denom"])
}

func TestConvertOutflow_ContextBackground_SilentSkip(t *testing.T) {
	// Defense-in-depth: callers that pass context.Background() (existing
	// unit tests, future non-baseapp callers) must NOT panic at the
	// EventManager call. trySDKContext returns false; emission is skipped
	// silently. This pins the safety net.
	ebk, _ := setup(t)
	from, to := testAddr(), testAddr2()

	amt := sdk.NewCoins(sdk.NewCoin("astoc", math.NewInt(1)))
	require.NotPanics(t, func() {
		require.NoError(t, ebk.SendCoins(context.Background(), from, to, amt))
	}, "MED-2: context.Background() callers must not panic at EventManager emission")
}

// ---------- helpers ----------

func filterEventsByType(events []sdk.Event, eventType string) []sdk.Event {
	out := make([]sdk.Event, 0, len(events))
	for _, e := range events {
		if e.Type == eventType {
			out = append(out, e)
		}
	}
	return out
}

func attrMap(e sdk.Event) map[string]string {
	m := make(map[string]string, len(e.Attributes))
	for _, a := range e.Attributes {
		m[a.Key] = a.Value
	}
	return m
}

