package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	stoctypes "stoc/x/stoc/types"
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
		// SA-AUDIT-2026-06-10 fix18 A15-STO-L3: block authz delegation of
		// every x/stoc lifecycle msg. The creator hot wallet is the SINGLE
		// authority over the token's supply reserve (RemainingSupply) and
		// metadata. A grantee key compromise (or a phishing-induced grant)
		// previously let an attacker submit MsgExec wrapping
		// MsgMintTokens / MsgReleaseTokens / MsgBurnToken under the
		// creator's authz grant — draining the entire reserve into the
		// grantee's wallet WITHOUT the creator signing the actual lifecycle
		// tx. The STO compliance model requires every supply-changing event
		// to be directly signed by the issuer; routing supply through authz
		// breaks the audit trail + SOC2/KYC chain-of-custody.
		// Two-layer enforcement (pre-sig screen + post-fee AuthzLimiter)
		// already reuses this list, so adding the type URLs closes both
		// MsgGrant (depth 0) and shallow MsgExec(MsgMintTokens) at the same
		// time as a single source-of-truth edit.
		sdk.MsgTypeURL(&stoctypes.MsgCreateToken{}),
		sdk.MsgTypeURL(&stoctypes.MsgMintTokens{}),
		sdk.MsgTypeURL(&stoctypes.MsgReleaseTokens{}),
		sdk.MsgTypeURL(&stoctypes.MsgBurnToken{}),
		// SA-AUDIT-2026-06-11 fix19 A16-BOUNDARY-L4 (ACCEPTED RESIDUAL —
		// deliberately NOT adding ibctransfertypes.MsgTransfer here): an
		// authz-wrapped MsgTransfer of a CUSTOM token is still rejected by
		// IBCCustomTokenRestriction's MsgExec recursion, but only AFTER
		// signature verification — so an attacker can burn proposer time on
		// ecrecover before the reject lands (bounded by per-tx gas; same
		// cost profile as any other failing tx). Blocking MsgTransfer here
		// pre-sig would also block NATIVE-denom authz transfers, which
		// legitimate IBC relayer/exchange automation uses. Mirrors the
		// fix14 trade-off for bank.MsgSend. Revisit only with evidence of
		// actual ecrecover-grief volume on mainnet.
	}
}
