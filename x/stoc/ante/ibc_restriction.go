package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	govv1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	govv1beta1types "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	grouptypes "github.com/cosmos/cosmos-sdk/x/group"
	ibctransfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"

	"stoc/x/stoc/keeper"
	stoctypes "stoc/x/stoc/types"
)

// IBCCustomTokenRestriction is an AnteDecorator that blocks outgoing IBC transfers
// of custom tokens created via x/stoc. Custom tokens are Cosmos-only and
// should not be sent cross-chain because:
//  1. Tax enforcement cannot be applied on other chains
//  2. Token metadata/rules are lost when crossing chains
//  3. Returning tokens via IBC would bypass tax on the outgoing path
//
// Allowed denom categories for outgoing IBC transfers (MsgTransfer):
//   - Native chain denom (Cosmos / EVM / display variants) — pass via IsNativeDenom()
//   - Foreign IBC-wrapped denoms (prefix "ibc/...") — pass through (fall-through path)
//
// Blocked: any denom registered in x/stoc keeper (token storage), which means
// every token created via x/stoc MsgCreateToken (e.g. "MYTOKEN_0").
//
// =============================================================================
// FUTURE WORK — Foreign Asset Integration (e.g. USDC via Noble)
// =============================================================================
// The current decorator only blocks outgoing transfers of x/stoc-created tokens.
// Inbound foreign assets (USDC from Noble, USDT from Kava, etc.) are minted by
// the ibctransfer module as "ibc/<HASH>" denoms and the chain can then forward
// or return them via standard IBC paths. No changes are required at the ante
// layer to support new inbound foreign assets — the existing fall-through path
// permits them automatically.
//
// However, when integrating new foreign assets, the operator MUST also confirm:
//   1. An IBC channel and connection are established with the counterparty
//      chain (governance / relayer coordination).
//   2. x/bank "SendEnabled" parameter does not include a per-denom block for
//      the foreign denom — by default bank allows all denoms; if an explicit
//      block list is added in the future, foreign denoms must be excluded.
//   3. x/erc20 token-pair registration (if EVM exposure of the foreign asset
//      is desired) is performed via governance proposal, mapping the IBC denom
//      to an ERC-20 contract address.
//   4. The custom-token block list above (x/stoc keeper lookup) remains
//      authoritative for chain-native custom tokens. Foreign assets must NOT
//      be registered through x/stoc MsgCreateToken — they are already provided
//      as IBC-wrapped denoms by their origin chain.
//
// Summary of denom flows:
//   Native STOC denom out ........ allowed (used as fee / cross-chain settlement)
//   Foreign IBC denom out ........ allowed (USDC/USDT return path, etc.)
//   x/stoc custom token out ...... BLOCKED (this decorator)
//   Inbound IBC packets .......... not handled here (see OnRecvPacket in ibctransfer)
type IBCCustomTokenRestriction struct {
	k keeper.Keeper
}

func NewIBCCustomTokenRestriction(k keeper.Keeper) IBCCustomTokenRestriction {
	return IBCCustomTokenRestriction{k: k}
}

func (d IBCCustomTokenRestriction) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := d.checkMsgs(ctx, tx.GetMsgs(), 0); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

// checkMsgs recursively checks messages, unwrapping any of the following
// wrappers so an inner ibctransfertypes.MsgTransfer of a custom denom is
// caught at submit time:
//
//   - cosmos.authz.v1beta1.MsgExec        (signer-delegated execution)
//   - cosmos.group.v1.MsgSubmitProposal   (group policy proposals, fix17)
//   - cosmos.gov.v1.MsgSubmitProposal     (gov v1 proposals, fix17)
//
// Depth is capped at stoctypes.MaxAuthzUnwrapDepth to bound CheckTx CPU
// (DoS prevention) — mirrors TaxPostDecorator and CustomTokenChainOpsRestriction.
//
// Forward-defense note: grouptypes.MsgExec is NOT walked here — see the
// matching no-op case below (and the longer comment in
// custom_token_restriction.go fix14 R2-LOW1) for the rationale.
//
// SA-AUDIT-2026-06-10 fix18 A15-BOUNDARY-I1: godoc accuracy — fix17 added
// group + gov v1 wrappers; prior godoc only mentioned authz.
//
// SA-AUDIT-2026-06-11 fix19 A16-BOUNDARY-I1: unlike the bank-send walker in
// custom_token_restriction.go, this walker does NOT carry an
// insideProposalWrapper flag — and does not need one. The check here is
// DENOM-ONLY (is the MsgTransfer token a x/stoc-managed denom?), with an
// identical verdict at every nesting depth: a custom-token MsgTransfer is
// blocked whether it arrives bare, inside authz, or inside a gov/group
// proposal. Wrapper context never changes the outcome, so threading a flag
// through the recursion would be dead state. The bank walker needs its flag
// because its verdict differs by context (module-account recipients are
// legal for direct sends but not for proposal-dispatched sends).
func (d IBCCustomTokenRestriction) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *ibctransfertypes.MsgTransfer:
			if err := d.checkTransfer(ctx, m); err != nil {
				return err
			}
		case *authz.MsgExec:
			// SA-AUDIT-2026-06-10 fix18 A15-BOUNDARY-L2: include msg type
			// prefix + cap value so log alerting can distinguish authz vs
			// group vs gov sources.
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("authz MsgExec nesting depth exceeded (%d/%d) in IBC restriction check", depth, stoctypes.MaxAuthzUnwrapDepth)
			}
			innerMsgs, err := m.GetMessages()
			if err != nil {
				return fmt.Errorf("failed to unwrap authz MsgExec for IBC restriction: %w", err)
			}
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}
		case *grouptypes.MsgSubmitProposal:
			// SA-AUDIT-2026-06-10 fix17 A15-BOUNDARY-H2: x/group policy accounts
			// can hold custom-token balances and dispatch inner messages on
			// execution. Recurse into the proposal's Messages so an inner
			// ibctransfertypes.MsgTransfer of a custom denom is blocked at
			// submit time (companion to the bank.MsgSend group recursion
			// already present in custom_token_restriction.go).
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("group.MsgSubmitProposal nesting depth exceeded (%d/%d) in IBC restriction check", depth, stoctypes.MaxAuthzUnwrapDepth)
			}
			innerMsgs, err := m.GetMsgs()
			if err != nil {
				return fmt.Errorf("failed to unwrap group.MsgSubmitProposal for IBC restriction: %w", err)
			}
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}
		case *govv1types.MsgSubmitProposal:
			// SA-AUDIT-2026-06-10 fix17 A15-BOUNDARY-H2: gov v1 proposals can
			// carry arbitrary inner messages too. Recurse same as group above.
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("gov.v1.MsgSubmitProposal nesting depth exceeded (%d/%d) in IBC restriction check", depth, stoctypes.MaxAuthzUnwrapDepth)
			}
			innerMsgs, err := m.GetMsgs()
			if err != nil {
				return fmt.Errorf("failed to unwrap gov v1 MsgSubmitProposal for IBC restriction: %w", err)
			}
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}

		// SA-AUDIT-2026-06-10 fix18 A15-BOUNDARY-L1: grouptypes.MsgExec
		// triggers execution of a PREVIOUSLY-stored group proposal which may
		// carry an inner ibctransfertypes.MsgTransfer. The fix17 recursion on
		// grouptypes.MsgSubmitProposal closes the SUBMIT-time leg, so any
		// proposal entering chain state from fix17 deploy forward cannot carry
		// a custom-token MsgTransfer. The remaining risk window is stale
		// pre-fix17 proposals — empirically empty on STOChain (devnet wiped
		// every redeploy, mainnet has no x/group state). Marker case kept in
		// sync with custom_token_restriction.go forward-defense annotation
		// (see fix14 R2-LOW1 for the longer rationale); mainnet v5.0.0 upgrade
		// handler should add a one-time sweep if x/group usage starts before
		// then.
		case *grouptypes.MsgExec:
			_ = m

		// SA-AUDIT-2026-06-11 fix19 A16-BOUNDARY-L2: forward-defense marker.
		// gov v1beta1 proposals carry a Content interface (not raw sdk.Msg
		// slices), and no registered v1beta1 Content type today can embed an
		// ibctransfertypes.MsgTransfer — so there is nothing to recurse into.
		// This no-op case exists so that IF a future SDK/IBC rev adds a
		// Content type that wraps executable messages, the maintainer who
		// greps for v1beta1 handling lands here and extends the walker
		// instead of assuming v1 coverage suffices. Mirrors the
		// grouptypes.MsgExec marker pattern above.
		case *govv1beta1types.MsgSubmitProposal:
			_ = m
		}
	}
	return nil
}

func (d IBCCustomTokenRestriction) checkTransfer(ctx sdk.Context, transfer *ibctransfertypes.MsgTransfer) error {
	coin := transfer.Token

	// Skip native denoms — always allowed for IBC
	if stoctypes.IsNativeDenom(coin.Denom) {
		return nil
	}

	// Check if this denom is a custom stoc token
	if d.k.HasToken(ctx, coin.Denom) {
		ctx.Logger().Warn("Blocked IBC transfer of custom token",
			"denom", coin.Denom,
			"sender", transfer.Sender,
			"source_port", transfer.SourcePort,
			"source_channel", transfer.SourceChannel,
		)
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(
				"ibc_custom_token_blocked",
				sdk.NewAttribute("denom", coin.Denom),
				sdk.NewAttribute("sender", transfer.Sender),
			),
		)
		return fmt.Errorf(
			"IBC transfer of custom token %q is not allowed: custom tokens created via x/stoc are Cosmos-only and cannot be transferred cross-chain",
			coin.Denom,
		)
	}
	return nil
}
