package keeper

import (
	"context"
	"fmt"

	"stoc/x/stoc/types"

	sdkerrors "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func (k msgServer) CreateToken(goCtx context.Context, msg *types.MsgCreateToken) (*types.MsgCreateTokenResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// cosmos-sdk bech32 Normalize (btcutil/bech32 Decode) accepts
	// ALL-UPPERCASE bech32 addresses — only mixed case is rejected. Storing
	// msg.Creator raw means an uppercase-submitted CreateToken persists an
	// uppercase token.Creator, and the subsequent MintToken comparison
	// `token.Creator != owner.String()` (where owner.String() canonicalizes
	// to lowercase via AccAddress -> bech32) fails permanently with
	// ErrUnauthorized. The creator self-DoSes the token's mint and release
	// paths — no fund theft and no chain-wide impact, but a permanent
	// footgun for issuers whose wallet uppercases the address before
	// signing. Parse + re-encode here once, at the only persistence site
	// for token.Creator, so the stored field is always canonical lowercase
	// bech32 regardless of caller input case.
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidCreatorAddress, "invalid creator address (%s)", err)
	}
	canonicalCreator := creatorAddr.String()

	k.Logger().Info("Starting token creation",
		"symbol", msg.Symbol,
		"name", msg.Name,
		"creator", canonicalCreator)
	// Create a token object

	// The same uppercase-Normalize trick applies to two adjacent bech32
	// fields besides token.Creator:
	//   (a) token.Distributions[i].Address — if persisted with the caller's
	//       literal case, the same wallet could appear under two entries
	//       that both canonicalize to the same AccAddress. Not a theft
	//       vector (the handler resolves both to the same canonical
	//       AccAddress and sends the combined balance there), but the
	//       stored Distributions slice would carry a
	//       duplicate-canonical-address footprint that downstream indexers
	//       / explorers render as two separate holders.
	//   (b) token.Tax.RecipientAddress — the tax PostHandler bech32-validates
	//       the field on every send and canonicalizes at compare time, so
	//       the runtime is safe, but a persisted RAW form would leak the
	//       input case to indexers + event attributes and be inconsistent
	//       with the canonicalized token.Creator.
	//
	// Canonicalize both on the way in so token.Distributions and
	// token.Tax.RecipientAddress mirror the invariant for token.Creator:
	// every bech32 stored under this handler is the AccAddress.String()
	// canonical lowercase form, regardless of caller input case.
	// Pre-compute skipDistribution intent here so
	// the persisted token.Distributions matches actual on-chain behaviour.
	// For unlimited tokens with InitialSupply=0 there is nothing to mint, so
	// we must NOT inject the default [{canonicalCreator, 100}] entry — that
	// entry would persist on-chain with no matching bank balance, drifting
	// from explorer / indexer assumptions. Leave token.Distributions empty
	// in that case. The contradictory pair (Unlimited+InitialSupply=0 AND
	// non-empty msg.Distributions) is rejected by the explicit check
	// further down before the mint loop runs.
	// authz MsgExec dispatches inner msgs without running ValidateBasic, so a
	// nil InitialSupply (proto field omitted on the wire) can reach here and
	// nil-deref on IsZero(). Reject explicitly, mirroring the ValidateBasic
	// IsNil guard the authz path bypasses. (A panic would be recovered by
	// runTx, so this hardens robustness, not liveness.)
	if msg.InitialSupply.IsNil() {
		return nil, sdkerrors.Wrap(types.ErrInvalidAmount, "initial supply cannot be nil")
	}
	skipDistributionForToken := msg.Unlimited && msg.InitialSupply.IsZero()

	distributions := msg.Distributions
	if skipDistributionForToken {
		distributions = nil
	} else if len(distributions) == 0 {
		distributions = []types.WalletDistribution{
			{
				Address: canonicalCreator,
				Percent: 100,
			},
		}
	} else {
		canonicalDistributions := make([]types.WalletDistribution, 0, len(distributions))
		for i, d := range distributions {
			distAddr, distErr := sdk.AccAddressFromBech32(d.Address)
			if distErr != nil {
				return nil, sdkerrors.Wrapf(types.ErrInvalidCreatorAddress,
					"invalid distributions[%d] address %q (%s)", i, d.Address, distErr)
			}
			canonicalDistributions = append(canonicalDistributions, types.WalletDistribution{
				Address: distAddr.String(),
				Percent: d.Percent,
			})
		}
		distributions = canonicalDistributions
	}
	taxToUse := msg.Tax
	if taxToUse.Percent.IsNil() {
		taxToUse = types.TokenTax{
			Percent:          math.LegacyZeroDec(),
			RecipientAddress: "",
		}
	} else if taxToUse.RecipientAddress != "" {
		taxRecipAddr, taxRecipErr := sdk.AccAddressFromBech32(taxToUse.RecipientAddress)
		if taxRecipErr != nil {
			return nil, sdkerrors.Wrapf(types.ErrInvalidTokenAmount,
				"invalid tax.recipient_address %q (%s)", taxToUse.RecipientAddress, taxRecipErr)
		}
		taxToUse = types.TokenTax{
			Percent:          taxToUse.Percent,
			RecipientAddress: taxRecipAddr.String(),
		}
	}
	// Build token for validation BEFORE incrementing counter
	counter, err := k.GetTokenCounter(ctx)
	if err != nil {
		return nil, sdkerrors.Wrap(err, "failed to get token counter")
	}
	// Fail-fast: check counter overflow BEFORE any bank operations to prevent orphan coins
	if counter == ^uint64(0) {
		return nil, sdkerrors.Wrap(types.ErrInvalidTokenAmount, "token counter overflow — maximum number of tokens reached")
	}
	minimalDenom := fmt.Sprintf("%s_%d", msg.Symbol, counter)
	tokenId := minimalDenom
	token := types.Token{
		Id:            tokenId,
		Name:          msg.Name,
		Symbol:        msg.Symbol,
		InitialSupply: msg.InitialSupply,
		TotalSupply:   msg.TotalSupply,
		Decimals:      msg.Decimals,
		Logo:          msg.Logo,
		Distributions: distributions,
		Tax:           taxToUse,
		Creator:       canonicalCreator, // store canonical lowercase bech32
		Unlimited:     msg.Unlimited,
		MinimalDenom:  minimalDenom,
	}

	// Validate token BEFORE incrementing counter (prevents counter pollution on invalid tokens)
	if err := types.Validate(token); err != nil {
		k.Logger().Error("Token validation failed", "error", err)
		return nil, sdkerrors.Wrap(err, "invalid token")
	}

	// Reject Tax.RecipientAddress that is bank-blocked.
	// Bank-blocked recipient (module account, etc.) makes every taxable transfer
	// fail at the PostHandler's SendCoins call → token is soft-rugged: all
	// holders permanently unable to transfer it. Creator cannot self-remediate
	// because Tax fields require gov via MsgUpdateParams.
	if token.Tax.RecipientAddress != "" {
		taxRecipient, err := sdk.AccAddressFromBech32(token.Tax.RecipientAddress)
		if err == nil && k.bankKeeper.BlockedAddr(taxRecipient) {
			return nil, sdkerrors.Wrapf(types.ErrInvalidTokenAmount,
				"tax recipient %s is a blocked address; choose a non-module/non-precompile address",
				token.Tax.RecipientAddress)
		}
	}

	// Name / Symbol / Display CAN collide on-chain. Multiple creators with same
	// Symbol "FOO" → each gets unique MinimalDenom via global counter
	// ("FOO_3", "FOO_47", etc).
	//
	// Trust verification (verified / unverified badges) is an off-chain
	// indexer concern, like a block explorer's verified-contract registry.
	// The on-chain layer stays permissionless.
	//
	// MinimalDenom uniqueness is preserved via the counter.
	// Native-denom symbol collision is ALSO allowed:
	// Symbol "ustoc"/"stoc" still mints a unique "ustoc_1" denom — no denom collision.
	// The tax recipient blocklist above still blocks soft-rug.

	// Validate generated denom against SDK rules
	if err := sdk.ValidateDenom(minimalDenom); err != nil {
		return nil, sdkerrors.Wrapf(types.ErrInvalidTokenSymbol, "generated denom %s is invalid: %v", minimalDenom, err)
	}

	// Check if token already exists
	if k.HasToken(ctx, token.MinimalDenom) {
		k.Logger().Error("Token symbol already exists", "symbol", token.MinimalDenom)
		return nil, sdkerrors.Wrapf(types.ErrTokenExists, "token with symbol %s already exists", token.MinimalDenom)
	}

	// Mint initial supply and distribute according to distribution list.
	// msg.Creator was already parsed at the top of this handler into
	// creatorAddr (canonical string in canonicalCreator); reuse it instead
	// of re-parsing.
	creator := creatorAddr

	// InitialSupply and TotalSupply are in raw minimal units (decimals field is metadata only)
	initialSupply := token.InitialSupply

	// Determine if we should mint remaining tokens to module account
	remainingSupply := math.NewInt(0)
	if token.TotalSupply.GT(token.InitialSupply) {
		remainingSupply = token.TotalSupply.Sub(token.InitialSupply)
	}

	token.RemainingSupply = remainingSupply

	// NOTE: SetToken + SetTokenCounter are called AFTER all minting succeeds (see below).
	// NOTE — NOT A CEI VIOLATION:
	// In Cosmos SDK, the entire msg handler runs inside BaseApp.cacheTxContext.
	// If any MintCoins/SendCoins call fails mid-loop, the handler returns error,
	// BaseApp discards the cache, and ALL bank mutations revert atomically.
	// No orphan coins, no counter pollution, no partial token state.
	// See MintToken in token.go for detailed rationale on why Solidity CEI patterns
	// do not apply to Cosmos SDK (no re-entrancy, no external calls).

	// Unlimited tokens whose creator picks the canonical "InitialSupply=0,
	// TotalSupply=0, Unlimited=true, mint later via MsgMintTokens" pattern
	// would otherwise fail the distribution loop below: the default
	// [{creator, 100%}] entry computes amount=0 and hits the zero-rounded
	// reject with a misleading
	// "increase InitialSupply or merge low-percent recipients" error.
	// ValidateBasic (types/msg_create_token.go) explicitly allows this
	// input shape, so the handler should honor the contract.
	// Skip the distribution loop entirely when there is nothing to
	// distribute. token.RemainingSupply was already set to ZeroInt() above
	// (TotalSupply.GT(InitialSupply) is false when both are zero), so
	// persistence below records a token with zero circulating + zero
	// reserve, which MsgMintTokens can then grow on demand.
	skipDistribution := token.Unlimited && initialSupply.IsZero()

	// When the caller submits a NON-EMPTY Distributions slice but their
	// (Unlimited, InitialSupply) pair also satisfies skipDistribution, the
	// skip path would silently drop the user-provided distribution list
	// because the loop body short-circuits on `if skipDistribution
	// { break }`. No bank.MintCoins or SendCoinsFromModuleToAccount would
	// fire, so on-chain balances would diverge from the requested
	// distribution (e.g. "holder A: 60%, holder B: 40%" while
	// bank.GetBalance(holder A) returns zero).
	//
	// Reject loudly so the issuer notices the contradiction. Either:
	//   - the issuer wants InitialSupply=0 with no upfront distribution
	//     (correct shape: omit Distributions entirely, the handler will
	//     skip), or
	//   - the issuer wants an upfront distribution (correct shape: set
	//     InitialSupply > 0 to fund the distribution list).
	// Both intents are valid; what's invalid is the contradictory pair.
	if skipDistribution && len(msg.Distributions) > 0 {
		return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
			"unlimited token with InitialSupply=0 cannot also carry %d Distributions entries — "+
				"either drop Distributions (skip upfront distribution, mint later via MsgMintTokens) "+
				"or set InitialSupply > 0 to fund the entries",
			len(msg.Distributions))
	}

	// Distribute initial supply according to distribution list
	// (distributions always has >= 1 entry: defaults to [{Creator, 100%}] when msg.Distributions is empty)
	totalMinted := math.ZeroInt()
	if skipDistribution {
		k.Logger().Info("Skipping initial distribution loop (unlimited token with InitialSupply=0)",
			"symbol", token.Symbol, "minimal_denom", token.MinimalDenom)
	}
	for i, dist := range token.Distributions {
		if skipDistribution {
			break
		}
		recipient, err := sdk.AccAddressFromBech32(dist.Address)
		if err != nil {
			return nil, sdkerrors.Wrap(err, "invalid distribution address")
		}

		var amount math.Int
		if i == len(token.Distributions)-1 {
			// Last recipient gets the remainder to avoid rounding loss
			amount = initialSupply.Sub(totalMinted)
		} else {
			// Calculate: amount = initialSupply * percent / 100
			amount = initialSupply.MulRaw(int64(dist.Percent)).QuoRaw(100)
		}

		if amount.IsNegative() {
			// Should not happen with valid percent values, but guard against
			// chain halt from sdk.NewCoin panic.
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distribution entry %d (%s) computed negative amount %s — totalMinted exceeded initialSupply",
				i, dist.Address, amount.String())
		}
		if amount.IsZero() {
			// Silently `continue`-ing on zero-rounded entries WITHOUT
			// incrementing totalMinted would let the last-entry remainder
			// branch above push ALL unallocated supply to the last
			// recipient — silently concentrating the entire initial supply
			// in one address even though the creator listed many. For a
			// security-token primary distribution this is a material
			// misallocation that the indexer cannot detect after the fact.
			// Reject loudly so the creator notices the InitialSupply is too
			// small (or the percent slices too thin) for the requested
			// recipient set, and can fix the input before submitting.
			return nil, sdkerrors.Wrapf(types.ErrInvalidAmount,
				"distribution entry %d (address %s, percent %d) rounds to 0 tokens at initial supply %s — increase InitialSupply or merge low-percent recipients",
				i, dist.Address, dist.Percent, initialSupply.String())
		}
		totalMinted = totalMinted.Add(amount)

		// Mint tokens to the recipient
		coin := sdk.NewCoin(token.MinimalDenom, amount)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}

		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, recipient, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}
	}

	// If there are remaining tokens (totals > initial), mint them to module account

	if remainingSupply.GT(math.ZeroInt()) {
		coin := sdk.NewCoin(token.MinimalDenom, remainingSupply)
		if err := k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(coin)); err != nil {
			return nil, err
		}

		k.Logger().Info("Minting remaining tokens to module account", "symbol", token.Symbol, "amount", remainingSupply.String())

	}

	// Persist state AFTER all bank operations succeeded (CEI pattern)
	if err := k.SetTokenCounter(ctx, counter+1); err != nil {
		return nil, sdkerrors.Wrap(err, "failed to set token counter")
	}
	if err := k.SetToken(ctx, token); err != nil {
		return nil, err
	}

	// register metadata for token so that the wallet can display it correctly

	// Use minimalDenom as display to avoid metadata collision when multiple tokens share the same symbol
	denomMetadata := banktypes.Metadata{
		Description: fmt.Sprintf("Token %s (%s) created on STOChain", token.Name, token.Symbol),
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    minimalDenom,
				Exponent: 0,
				Aliases:  []string{token.Symbol},
			},
		},
		Base:    minimalDenom,
		Display: minimalDenom,
		Name:    token.Name,
		Symbol:  token.Symbol,
		URI:     token.Logo,
		URIHash: "",
	}

	// if token has decimals, add DenomUnit with exponent = decimals
	if token.Decimals > 0 {
		denomMetadata.DenomUnits = []*banktypes.DenomUnit{
			{
				Denom:    minimalDenom,
				Exponent: 0,
			},
			{
				Denom:    fmt.Sprintf("%s_display", minimalDenom),
				Exponent: uint32(token.Decimals),
				Aliases:  []string{token.Symbol},
			},
		}
	}

	k.bankKeeper.SetDenomMetaData(ctx, denomMetadata)

	// Emit token creation event
	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeCreateToken,
			sdk.NewAttribute(types.AttributeKeyTokenSymbol, token.Symbol),
			sdk.NewAttribute(types.AttributeKeyTokenName, token.Name),
			sdk.NewAttribute(types.AttributeKeyTokenCreator, token.Creator),
			sdk.NewAttribute(types.AttributeKeyInitialSupply, token.InitialSupply.String()),
			sdk.NewAttribute(types.AttributeKeyMinimalDenom, token.MinimalDenom),
		),
	)

	// After minting:
	k.Logger().Info("Token minting complete",
		"symbol", token.Symbol,
		"amount", initialSupply.String(),
		"recipient", creator.String())

	// Final success log
	k.Logger().Info("Token creation successful", "symbol", token.Symbol)

	return &types.MsgCreateTokenResponse{
		Symbol:  token.Symbol,
		Creator: token.Creator,
		Success: true,
		Message: token.MinimalDenom,
	}, nil

}
