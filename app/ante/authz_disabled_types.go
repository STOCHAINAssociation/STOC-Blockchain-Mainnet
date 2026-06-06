package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// AuthzDisabledMsgTypes returns the single source of truth for msg type URLs
// that BOTH the pre-sig AuthzPreSigScreenDecorator AND the post-fee
// cosmosante.NewAuthzLimiterDecorator must reject when found inside
// authz.MsgExec or authz.MsgGrant.
//
// Hoisted to a package-level function (SA-AUDIT-2026-06-08 fix15-2 R3
// completeness-critic finding) so the two-layer authz screen invariant has
// a single editing point. Adding a new disabled type here propagates to
// both decorators automatically; conversely, the
// TestAuthzDisabledMsgTypes_StableInvariant test pins the contents so that
// a future PR that touches one layer's blocklist but forgets the other
// surfaces in CI rather than as a silent ecrecover-DoS regression.
//
// Returns a fresh slice on each call to avoid accidental shared-state
// mutation by callers.
func AuthzDisabledMsgTypes() []string {
	return []string{
		sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
		sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
		sdk.MsgTypeURL(&sdkvesting.MsgCreatePermanentLockedAccount{}),
		sdk.MsgTypeURL(&sdkvesting.MsgCreatePeriodicVestingAccount{}),
	}
}
