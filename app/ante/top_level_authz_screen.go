package ante

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// AuthzPreSigScreenMaxDepth is the recursion cap applied by
// AuthzPreSigScreenDecorator when walking nested authz.MsgExec wrappers.
// The cap is intentionally tight — the canonical attack pattern is one or
// two MsgExec wrappers around a disabled type, and the recursive
// cosmosante.NewAuthzLimiterDecorator post-fee already enforces
// maxNestedMsgs=7 as defence-in-depth. A pre-sig cap of 3 catches every
// realistic shallow attack (depth 0, 1, 2) while bounding the worst-case
// per-tx work to a small constant.
const AuthzPreSigScreenMaxDepth = 3

// AuthzPreSigScreenDecorator does a depth-bounded prefilter on tx.GetMsgs()
// looking for authz.MsgExec or authz.MsgGrant entries that target any of the
// listed disabled type URLs. The check inspects Any.TypeUrl directly — no
// codec unpack — at depth 0, and recursively walks up to
// AuthzPreSigScreenMaxDepth levels of nested MsgExec. The decorator is
// placed BEFORE the signature verification decorators so that shallow
// authz-shaped attacks (single or doubly-wrapped MsgExec with a disabled
// inner type, or top-level MsgGrant for a disabled type) do not consume
// ecrecover before being rejected.
//
// Two attack shapes covered:
//
//  1. authz.MsgExec(...) up to depth AuthzPreSigScreenMaxDepth.
//     Recurses into exec.Msgs, peeks each Any.TypeUrl, descends into nested
//     MsgExec without codec.UnpackAny — only the outer Any.TypeUrl string
//     comparison is needed at each level, so cost is O(N) in total Any
//     entries across the bounded tree.
//  2. authz.MsgGrant{Authorization: <Authorization-Any>}. cosmos-sdk
//     authz.Authorization.MsgTypeURL() returns the granted msg type URL for
//     all built-in variants (GenericAuthorization, SendAuthorization,
//     StakeAuthorization, etc.). Reject if the granted type is on the
//     disabled list.
//
// Defence-in-depth: the full recursive cosmosante.NewAuthzLimiterDecorator
// runs LATER in the post-fee message-walking block (depth cap
// maxNestedMsgs=7) and catches anything beyond
// AuthzPreSigScreenMaxDepth. This screen is the cheap first-pass; the
// recursive check post-fee is the deep backstop.
//
// SA-AUDIT-2026-06-07 M1 + LOW-4 (round 2 R2-M1, R2-LOW4): pre-fix13 the
// recursive AuthzLimiter ran at position 2 of the ante chain (before
// NewSigVerificationDecorator). Fix13 moved it post-fee under the
// (inaccurate) assumption that the fee gate would deter nested-Any CPU
// amplification. Round 2 audit established that the fee debit is never
// persisted on ante failure (Cosmos SDK baseapp wraps the ante in
// cacheTxContext and discards on error), so the post-fee placement
// re-introduced an ecrecover-spend regression for shallow attacks.
// fix14 added a depth-0-only TopLevelAuthzMsgExecScreenDecorator that
// closed the MsgExec shape but missed two adjacent gaps round 3 then
// flagged: nested MsgExec(MsgExec(disabled)) at depth 1 (R3-M2) and
// authz.MsgGrant on disabled types (R3-M1). This decorator merges fix14
// + the round 3 coverage: bounded recursion to AuthzPreSigScreenMaxDepth
// AND symmetric MsgGrant coverage. Both round 3 MED findings are now
// closed at the pre-sig layer.
//
// The disabled msg type list MUST be kept in sync with the list passed to
// the post-fee cosmosante.NewAuthzLimiterDecorator — see
// app/ante/cosmos_handler.go authzDisabledMsgTypes.
type AuthzPreSigScreenDecorator struct {
	disabledMsgTypes []string
}

// NewAuthzPreSigScreenDecorator constructs the screen. Pass the same
// disabled msg type URLs handed to the post-fee recursive
// cosmosante.NewAuthzLimiterDecorator so the two layers agree.
func NewAuthzPreSigScreenDecorator(disabledMsgTypes ...string) AuthzPreSigScreenDecorator {
	return AuthzPreSigScreenDecorator{disabledMsgTypes: disabledMsgTypes}
}

// AnteHandle implements sdk.AnteDecorator.
func (d AuthzPreSigScreenDecorator) AnteHandle(
	ctx sdk.Context,
	tx sdk.Tx,
	simulate bool,
	next sdk.AnteHandler,
) (sdk.Context, error) {
	if len(d.disabledMsgTypes) == 0 {
		return next(ctx, tx, simulate)
	}
	for _, msg := range tx.GetMsgs() {
		if err := d.screenMsg(ctx, msg, 0); err != nil {
			return ctx, err
		}
	}
	return next(ctx, tx, simulate)
}

// screenMsg matches msg against the two covered authz shapes.
//
// Depth semantics: depth=0 is the top-level tx msg. Recursing INTO an
// authz.MsgExec increments depth. When depth >= AuthzPreSigScreenMaxDepth
// the recursion stops without rejecting — anything deeper is left for the
// post-fee recursive AuthzLimiter (which has its own
// maxNestedMsgs=7 cap).
func (d AuthzPreSigScreenDecorator) screenMsg(ctx sdk.Context, msg sdk.Msg, depth int) error {
	if depth >= AuthzPreSigScreenMaxDepth {
		return nil
	}
	switch m := msg.(type) {
	case *authz.MsgExec:
		for _, anyMsg := range m.Msgs {
			if anyMsg == nil {
				continue
			}
			for _, disabled := range d.disabledMsgTypes {
				if anyMsg.TypeUrl == disabled {
					return errorsmod.Wrapf(
						errortypes.ErrUnauthorized,
						"authz.MsgExec contains disabled inner msg type %q at depth %d (pre-sig screen — see SA-AUDIT-2026-06-08 R3-M1/M2)",
						anyMsg.TypeUrl, depth,
					)
				}
			}
			// Cheap nested recursion: if this Any is another MsgExec, descend.
			// Detect via the URL string — no codec unpack needed at this
			// layer either. The Any.TypeUrl scheme is the proto type URL
			// like "/cosmos.authz.v1beta1.MsgExec".
			if anyMsg.TypeUrl == authzMsgExecTypeURL {
				// We need the actual MsgExec struct to recurse. The Any
				// may carry a cached value populated by SetMsgs upstream
				// (typical for tx-encoded paths); fall back to unmarshal
				// only when needed and only against the bounded depth.
				if cached, ok := anyMsg.GetCachedValue().(*authz.MsgExec); ok && cached != nil {
					if err := d.screenMsg(ctx, cached, depth+1); err != nil {
						return err
					}
					continue
				}
				// No cached value — at the pre-sig layer we conservatively
				// reject txs that nest MsgExec without a cached struct.
				// The post-fee recursive AuthzLimiter will unmarshal and
				// either accept or reject; doing the unmarshal here would
				// open the CPU surface fix14 intentionally avoids.
				// In practice cosmos-sdk's MsgExec.SetMsgs (used by all
				// canonical tx-builders) populates the cache, so a
				// missing cache marks a hand-crafted or partially-decoded
				// tx and is itself a yellow flag. Conservative reject.
				return errorsmod.Wrapf(
					errortypes.ErrUnauthorized,
					"authz.MsgExec nests another MsgExec without a cached struct at depth %d (pre-sig screen)",
					depth,
				)
			}
		}
	case *authz.MsgGrant:
		authzAuth, err := m.GetAuthorization()
		if err != nil || authzAuth == nil {
			// Malformed Authorization — defer to the post-fee recursive
			// AuthzLimiter which surfaces the canonical error for the
			// caller. We intentionally do not reject here so legitimate
			// edge cases (e.g. future authorization types whose
			// GetAuthorization signature changes) flow through.
			return nil
		}
		grantedURL := authzAuth.MsgTypeURL()
		for _, disabled := range d.disabledMsgTypes {
			if grantedURL == disabled {
				return errorsmod.Wrapf(
					errortypes.ErrUnauthorized,
					"authz.MsgGrant grants disabled msg type %q (pre-sig screen — see SA-AUDIT-2026-06-08 R3-M1)",
					grantedURL,
				)
			}
		}
	}
	return nil
}

// authzMsgExecTypeURL is the canonical proto URL for cosmos-sdk
// authz.MsgExec. Hard-coded to avoid pulling sdk.MsgTypeURL() at recursion
// hot paths.
const authzMsgExecTypeURL = "/cosmos.authz.v1beta1.MsgExec"

// --- backwards-compatibility shims for the fix14 type name -----------------
//
// TopLevelAuthzMsgExecScreenDecorator is preserved as a type alias and
// constructor wrapper so any external caller built against the fix14 names
// keeps compiling. New code should use AuthzPreSigScreenDecorator /
// NewAuthzPreSigScreenDecorator.

// TopLevelAuthzMsgExecScreenDecorator is the fix14 name for what is now
// AuthzPreSigScreenDecorator. Kept as a type alias for compatibility.
type TopLevelAuthzMsgExecScreenDecorator = AuthzPreSigScreenDecorator

// NewTopLevelAuthzMsgExecScreenDecorator is the fix14 constructor name for
// NewAuthzPreSigScreenDecorator. Kept for compatibility.
func NewTopLevelAuthzMsgExecScreenDecorator(disabledMsgTypes ...string) AuthzPreSigScreenDecorator {
	return NewAuthzPreSigScreenDecorator(disabledMsgTypes...)
}
