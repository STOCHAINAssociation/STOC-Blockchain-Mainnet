package keeper

import (
	"cosmossdk.io/store/prefix"
	"github.com/cosmos/cosmos-sdk/runtime"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"

	"stoc/x/stoc/types"
)

// Index of ICS-20 transfer escrow addresses (v5.0.3).
//
// Custom tokens must never be escrowed by IBC transfer nor credited from an
// escrow. The bank SendRestriction checks both addresses of every
// custom-token send against this index: one KV lookup per address, charged
// to the tx gas meter like any other read, independent of how many channels
// exist. The app writes an entry when a transfer channel is created (the
// transfer IBC stack's OnChanOpenInit / OnChanOpenTry), when the v5.0.3
// upgrade runs (existing channels) and at InitChain (channels imported from
// genesis). Entries are never deleted: a closed channel's escrow still holds
// funds. Writes are ordinary state writes and revert with the tx.
//
// IBC v2 is not covered: a v2 ICS-20 escrow is GetEscrowAddress("transfer",
// <client id>) and is never indexed. v2 is disabled (no v2 router; v2
// messages are rejected in the ante and in the proposal walker). If v2 is
// ever enabled, the index must also record client-id escrows.

func (k Keeper) ibcEscrowStore(ctx sdk.Context) prefix.Store {
	return prefix.NewStore(runtime.KVStoreAdapter(k.storeService.OpenKVStore(ctx)), types.KeyPrefix(types.IBCEscrowKey))
}

// SetIBCEscrow records escrow as the escrow address of the given transfer
// channel.
func (k Keeper) SetIBCEscrow(ctx sdk.Context, escrow sdk.AccAddress, channelID string) {
	k.ibcEscrowStore(ctx).Set(address.MustLengthPrefix(escrow), []byte(channelID))
}

// IsIBCEscrow reports whether addr is a recorded transfer escrow address.
func (k Keeper) IsIBCEscrow(ctx sdk.Context, addr sdk.AccAddress) bool {
	if len(addr) == 0 {
		return false
	}
	return k.ibcEscrowStore(ctx).Has(address.MustLengthPrefix(addr))
}

// GetIBCEscrowChannel returns the channel id recorded for an escrow address.
func (k Keeper) GetIBCEscrowChannel(ctx sdk.Context, addr sdk.AccAddress) (string, bool) {
	if len(addr) == 0 {
		return "", false
	}
	bz := k.ibcEscrowStore(ctx).Get(address.MustLengthPrefix(addr))
	if bz == nil {
		return "", false
	}
	return string(bz), true
}
