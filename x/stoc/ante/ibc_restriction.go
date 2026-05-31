package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
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

// checkMsgs recursively checks messages including authz-wrapped MsgExec.
func (d IBCCustomTokenRestriction) checkMsgs(ctx sdk.Context, msgs []sdk.Msg, depth int) error {
	for _, msg := range msgs {
		switch m := msg.(type) {
		case *ibctransfertypes.MsgTransfer:
			if err := d.checkTransfer(ctx, m); err != nil {
				return err
			}
		case *authz.MsgExec:
			if depth >= stoctypes.MaxAuthzUnwrapDepth {
				return fmt.Errorf("authz MsgExec nesting depth exceeded (%d) in IBC restriction check", depth)
			}
			innerMsgs, err := m.GetMessages()
			if err != nil {
				return fmt.Errorf("failed to unwrap authz MsgExec for IBC restriction: %w", err)
			}
			if err := d.checkMsgs(ctx, innerMsgs, depth+1); err != nil {
				return err
			}
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
