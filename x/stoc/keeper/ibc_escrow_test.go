package keeper_test

import (
	"bytes"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	keepertest "stoc/testutil/keeper"
	"stoc/x/stoc/types"
)

func TestIBCEscrowIndex(t *testing.T) {
	k, ctx := keepertest.StocKeeper(t)

	escrow := sdk.AccAddress(bytes.Repeat([]byte{0xab}, 20))
	longer := sdk.AccAddress(append(bytes.Repeat([]byte{0xab}, 20), bytes.Repeat([]byte{0x01}, 12)...)) // same 20-byte prefix
	require.False(t, k.IsIBCEscrow(ctx, escrow))
	require.False(t, k.IsIBCEscrow(ctx, nil))

	k.SetIBCEscrow(ctx, escrow, "channel-4")
	require.True(t, k.IsIBCEscrow(ctx, escrow))
	ch, ok := k.GetIBCEscrowChannel(ctx, escrow)
	require.True(t, ok)
	require.Equal(t, "channel-4", ch)

	// Keys are length-prefixed: a longer address sharing the prefix is not an escrow.
	require.False(t, k.IsIBCEscrow(ctx, longer))
	_, ok = k.GetIBCEscrowChannel(ctx, longer)
	require.False(t, ok)

	// Idempotent overwrite.
	k.SetIBCEscrow(ctx, escrow, "channel-4")
	require.True(t, k.IsIBCEscrow(ctx, escrow))

	// The index does not leak into token iteration.
	require.NoError(t, k.SetToken(ctx, types.Token{
		Id: "T_0", MinimalDenom: "T_0", Symbol: "T", Name: "T", Logo: "https://x/l.png",
		InitialSupply: math.NewInt(1), TotalSupply: math.NewInt(1), RemainingSupply: math.ZeroInt(),
		Creator: sdk.AccAddress([]byte("creator_____________")).String(),
		Tax:     types.TokenTax{Percent: math.LegacyZeroDec()},
	}))
	require.Len(t, k.GetAllTokens(ctx), 1)
}
