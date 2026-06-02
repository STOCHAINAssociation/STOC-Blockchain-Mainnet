package keeper

import (
	"context"
	"fmt"
	"strings"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"stoc/x/evmutil/types"
)

// EvmBankKeeper wraps the BankKeeper to provide EVM-compatible denomination conversion
// between ustoc (6 decimals) and astoc (18 decimals) for mainnet
// or utstoc (6 decimals) and atstoc (18 decimals) for testnet
type EvmBankKeeper struct {
	bankKeeper  types.BankKeeper
	evmDenom    string // cached at construction, avoids runtime panic from GetEvmDenom()
	cosmosDenom string // cached at construction
}

// NewEvmBankKeeper creates a new EvmBankKeeper with cached denom pair.
// Panics at init if DefaultBondDenom is misconfigured (fail-fast at startup, not at runtime).
func NewEvmBankKeeper(bankKeeper types.BankKeeper) EvmBankKeeper {
	evmDenom, err := types.SafeGetEvmDenom()
	if err != nil {
		panic(fmt.Sprintf("evmutil: failed to resolve EVM denom at init: %v", err))
	}
	return EvmBankKeeper{
		bankKeeper:  bankKeeper,
		evmDenom:    evmDenom,
		cosmosDenom: types.GetCosmosDenom(),
	}
}

// getEvmDenom returns the cached EVM denom
func (k EvmBankKeeper) getEvmDenom() string {
	return k.evmDenom
}

// getCosmosDenom returns the cached Cosmos denom
func (k EvmBankKeeper) getCosmosDenom() string {
	return k.cosmosDenom
}

// GetBalance returns the balance of the given account for the given denom.
// For EVM denom (astoc/atstoc), it converts from Cosmos denom (ustoc/utstoc) balance.
// Custom tokens (created via STOC module) are NOT accessible from EVM.
func (k EvmBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	evmDenom := k.getEvmDenom()
	cosmosDenom := k.getCosmosDenom()

	if denom == evmDenom {
		// Get ustoc/utstoc balance and convert to astoc/atstoc
		cosmosBalance := k.bankKeeper.GetBalance(ctx, addr, cosmosDenom)
		evmCoin, err := ConvertCosmosCoinToEvmCoin(cosmosBalance)
		if err != nil {
			// Should not happen since we use the correct cosmos denom
			return sdk.NewCoin(evmDenom, math.ZeroInt())
		}
		return evmCoin
	}

	// RESTRICTION: Only allow native tokens (ustoc/utstoc) from EVM
	// Custom tokens (MYTOKEN_0, etc.) are Cosmos-only
	if denom != cosmosDenom {
		// Return zero balance for custom tokens from EVM context
		return sdk.NewCoin(denom, math.ZeroInt())
	}

	return k.bankKeeper.GetBalance(ctx, addr, denom)
}

// SendCoins transfers coins from one account to another.
// SA-C8 audit-2026-05-29: outflow → round DOWN (caller-bears-dust).
// Prevents contract-internal Transfer(N) corruption where round-UP would
// overcharge sender and skew downstream contract accounting.
func (k EvmBankKeeper) SendCoins(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	convertedAmt, err := k.convertCoinsForOutflow(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoins(ctx, from, to, convertedAmt)
}

// MintCoins mints coins to the module account.
// SA-C7 audit-2026-05-29: outflow (creates supply) → round DOWN.
// Round-UP would over-mint vs caller intent, breaking supply accounting.
func (k EvmBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	convertedAmt, err := k.convertCoinsForOutflow(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.MintCoins(ctx, moduleName, convertedAmt)
}

// BurnCoins burns coins from the module account.
// SA-C7 audit-2026-05-29: outflow (destroys supply) → round DOWN.
// Round-UP would over-burn vs caller intent.
func (k EvmBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	convertedAmt, err := k.convertCoinsForOutflow(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.BurnCoins(ctx, moduleName, convertedAmt)
}

// SendCoinsFromModuleToAccount transfers coins from module to account.
// SA-C7 audit-2026-05-29: outflow (e.g., RefundGas from FeeCollector) → round DOWN.
// Round-UP here gifted 1 ustoc per non-aligned-leftover-gas tx from FeeCollector to user.
// At zero-gas spam rates (post-SA-C1) this was a quantifiable validator-reward drain.
func (k EvmBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	convertedAmt, err := k.convertCoinsForOutflow(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, senderModule, recipientAddr, convertedAmt)
}

// SendCoinsFromAccountToModule transfers coins from account to module.
// SA-C7 audit-2026-05-29: inflow (e.g., fee collection, deposit) → round UP.
// Caller overpays at most 1 udstoc; protects module-account accounting from
// sub-ustoc dust loss.
func (k EvmBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	convertedAmt, err := k.convertCoinsForInflow(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, senderAddr, recipientModule, convertedAmt)
}

// SpendableCoins returns the spendable balance for the given account.
// RESTRICTION: Only returns EVM-side representation (astoc) — no custom tokens, no double-counting.
func (k EvmBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	// Only return the EVM denom (astoc) — converted from the underlying cosmos denom (ustoc)
	cosmosBalance := k.bankKeeper.SpendableCoin(ctx, addr, k.getCosmosDenom())
	if cosmosBalance.IsPositive() {
		evmAmount := cosmosBalance.Amount.Mul(types.ConversionMultiplier)
		return sdk.NewCoins(sdk.NewCoin(k.getEvmDenom(), evmAmount))
	}
	return sdk.NewCoins()
}

// BlockedAddr checks if a given address is blocked from receiving funds.
func (k EvmBankKeeper) BlockedAddr(addr sdk.AccAddress) bool {
	return k.bankKeeper.BlockedAddr(addr)
}

// getDisplayDenom returns the human-readable display denom derived from the cosmos denom.
// "ustoc" -> "stoc", "utstoc" -> "tstoc"
func getDisplayDenom() string {
	return strings.TrimPrefix(types.GetCosmosDenom(), "u")
}

// GetDenomMetaData returns the metadata for a given denom.
// For EVM denom (astoc/atstoc), it returns custom metadata with 18 decimals.
func (k EvmBankKeeper) GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool) {
	if denom == k.getEvmDenom() {
		displayDenom := getDisplayDenom()
		displayUpper := strings.ToUpper(displayDenom)
		metadata := banktypes.Metadata{
			Description: displayUpper + " token in EVM format (18 decimals)",
			DenomUnits: []*banktypes.DenomUnit{
				{
					Denom:    k.getEvmDenom(),
					Exponent: 0,
					Aliases:  []string{k.getEvmDenom()},
				},
				{
					Denom:    displayDenom,
					Exponent: 18,
					Aliases:  []string{},
				},
			},
			Base:    k.getEvmDenom(),
			Display: displayDenom,
			Name:    displayUpper,
			Symbol:  displayUpper,
		}
		return metadata, true
	}
	return k.bankKeeper.GetDenomMetaData(ctx, denom)
}

// GetSupply returns the total supply of a given denom.
// For EVM denom (astoc), it converts from Cosmos denom (ustoc) supply.
// RESTRICTION: Custom tokens return zero supply from EVM context.
func (k EvmBankKeeper) GetSupply(ctx context.Context, denom string) sdk.Coin {
	if denom == k.getEvmDenom() {
		// Get ustoc supply and convert to astoc
		cosmosSupply := k.bankKeeper.GetSupply(ctx, k.getCosmosDenom())
		evmCoin, err := ConvertCosmosCoinToEvmCoin(cosmosSupply)
		if err != nil {
			return sdk.NewCoin(k.getEvmDenom(), math.ZeroInt())
		}
		return evmCoin
	}
	if denom != k.getCosmosDenom() {
		// RESTRICTION: Return zero supply for custom tokens from EVM context
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	return k.bankKeeper.GetSupply(ctx, denom)
}

// IsSendEnabledCoin checks if a coin's denom is enabled for sending.
// For EVM denom (astoc), it checks the Cosmos denom (ustoc) instead.
// RESTRICTION: Custom tokens always return false from EVM context.
func (k EvmBankKeeper) IsSendEnabledCoin(ctx context.Context, coin sdk.Coin) bool {
	if coin.Denom == k.getEvmDenom() {
		// Check if the cosmos denom is send enabled (denom-only check)
		cosmosCoin := sdk.NewCoin(k.getCosmosDenom(), math.OneInt())
		return k.bankKeeper.IsSendEnabledCoin(ctx, cosmosCoin)
	}
	if coin.Denom != k.getCosmosDenom() {
		// RESTRICTION: Custom tokens are not send-enabled from EVM context
		return false
	}
	return k.bankKeeper.IsSendEnabledCoin(ctx, coin)
}

// IsSendEnabledCoins checks if all coins are enabled for sending.
// For EVM denoms, it converts to Cosmos denoms first.
// RESTRICTION: Custom tokens return error from EVM context.
func (k EvmBankKeeper) IsSendEnabledCoins(ctx context.Context, coins ...sdk.Coin) error {
	convertedCoins := make([]sdk.Coin, 0, len(coins))
	for _, coin := range coins {
		if coin.Denom == k.getEvmDenom() {
			cosmosCoin, err := ConvertEvmCoinToCosmosCoin(coin)
			if err != nil {
				return err
			}
			convertedCoins = append(convertedCoins, cosmosCoin)
		} else if coin.Denom == k.getCosmosDenom() {
			convertedCoins = append(convertedCoins, coin)
		} else {
			// RESTRICTION: Custom tokens are not send-enabled from EVM context
			return errorsmod.Wrapf(types.ErrInvalidDenom, "custom token %s is not send-enabled from EVM context", coin.Denom)
		}
	}
	return k.bankKeeper.IsSendEnabledCoins(ctx, convertedCoins...)
}

// GetAllBalances returns all balances for an account.
// RESTRICTION: Only returns EVM-side representation (astoc) — no custom tokens, no double-counting.
func (k EvmBankKeeper) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	// Only return the EVM denom (astoc) — converted from the underlying cosmos denom (ustoc)
	cosmosBalance := k.bankKeeper.GetBalance(ctx, addr, k.getCosmosDenom())
	if cosmosBalance.IsPositive() {
		evmAmount := cosmosBalance.Amount.Mul(types.ConversionMultiplier)
		return sdk.NewCoins(sdk.NewCoin(k.getEvmDenom(), evmAmount))
	}
	return sdk.NewCoins()
}

// SpendableCoin returns the spendable balance for a specific denom.
// RESTRICTION: Custom tokens return zero balance from EVM context.
func (k EvmBankKeeper) SpendableCoin(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	if denom == k.getEvmDenom() {
		cosmosBalance := k.bankKeeper.SpendableCoin(ctx, addr, k.getCosmosDenom())
		evmCoin, err := ConvertCosmosCoinToEvmCoin(cosmosBalance)
		if err != nil {
			return sdk.NewCoin(k.getEvmDenom(), math.ZeroInt())
		}
		return evmCoin
	}
	if denom != k.getCosmosDenom() {
		// RESTRICTION: Return zero balance for custom tokens from EVM context
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	return k.bankKeeper.SpendableCoin(ctx, addr, denom)
}

// SendCoinsFromModuleToModule transfers coins between modules.
// SA-C7 audit-2026-05-29: outflow → round DOWN.
func (k EvmBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule string, recipientModule string, amt sdk.Coins) error {
	convertedAmt, err := k.convertCoinsForOutflow(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToModule(ctx, senderModule, recipientModule, convertedAmt)
}

// convertCoinsForInflow converts EVM coins to Cosmos coins with round-UP (ceiling).
// Use only for inflow paths (account → module fee deposit). Caller overpays at
// most 1 udstoc (1e-6 STOC) per coin entry — negligible cost protecting module
// accounting from sub-ustoc dust loss. Justified UX for fee-collection only.
func (k EvmBankKeeper) convertCoinsForInflow(amt sdk.Coins) (sdk.Coins, error) {
	convertedAmt := sdk.NewCoins()
	for _, coin := range amt {
		if coin.Amount.IsNegative() {
			return nil, errorsmod.Wrapf(types.ErrInvalidDenom, "negative amount %s for denom %s", coin.Amount.String(), coin.Denom)
		}
		if coin.Amount.IsZero() {
			continue // SA-C7: skip zero-amount to prevent phantom 1-udstoc charge
		}
		if coin.Denom == k.getEvmDenom() {
			amount := coin.Amount
			remainder := amount.Mod(types.ConversionMultiplier)
			if !remainder.IsZero() {
				amount = amount.Sub(remainder).Add(types.ConversionMultiplier)
			}
			cosmosAmount := amount.Quo(types.ConversionMultiplier)
			if cosmosAmount.IsZero() {
				return nil, fmt.Errorf("amount too small: %s %s converts to 0 %s", coin.Amount.String(), k.getEvmDenom(), k.getCosmosDenom())
			}
			convertedAmt = convertedAmt.Add(sdk.NewCoin(k.getCosmosDenom(), cosmosAmount))
		} else if coin.Denom == k.getCosmosDenom() {
			convertedAmt = convertedAmt.Add(coin)
		} else {
			return nil, errorsmod.Wrapf(types.ErrInvalidDenom, "custom token %s cannot be transferred from EVM context", coin.Denom)
		}
	}
	return convertedAmt, nil
}

// convertCoinsForOutflow converts EVM coins to Cosmos coins with round-DOWN
// (truncation) and silently drops dust legs that round to zero ustoc.
//
// Use for all outflow paths: SendCoins, MintCoins, BurnCoins,
// SendCoinsFromModuleToAccount (incl. RefundGas), SendCoinsFromModuleToModule.
// Round-DOWN prevents:
//   - SA-C7 FeeCollector drain via RefundGas rounding-up sub-ustoc leftover gas.
//   - SA-C8 contract-internal Transfer(N) corruption where caller intent diverges
//     from on-chain debit.
//
// Caller's astoc balance shows the truncated amount; dust below 1 ustoc is
// burned by the conversion (not leaked to anyone) and cannot be recovered.
//
// SA-2026-06-02 MED-3 (senior-skeptic audit): the prior implementation
// REVERTED the calling EVM frame with "amount too small ... (outflow
// truncates dust)" whenever any leg of an outflow rounded to zero ustoc
// (i.e. amount < 1e12 wei). This unilateral revert broke otherwise-valid
// EVM contract calls and gas refunds that emitted sub-ustoc legs. The
// godoc already documents the "dust burned by conversion" contract; the
// revert was inconsistent with that contract. Business rule (locked
// 2026-06-02): silently drop dust legs and continue processing the
// remaining Coins. If EVERY leg rounds to zero we return an empty
// sdk.Coins which downstream bank calls treat as a no-op. SA-C7 / SA-C8
// protections still hold because non-dust amounts continue to truncate
// (not round up), so neither FeeCollector drain nor Transfer(N)
// corruption is reintroduced.
func (k EvmBankKeeper) convertCoinsForOutflow(amt sdk.Coins) (sdk.Coins, error) {
	convertedAmt := sdk.NewCoins()
	for _, coin := range amt {
		if coin.Amount.IsNegative() {
			return nil, errorsmod.Wrapf(types.ErrInvalidDenom, "negative amount %s for denom %s", coin.Amount.String(), coin.Denom)
		}
		if coin.Amount.IsZero() {
			continue
		}
		if coin.Denom == k.getEvmDenom() {
			// Round DOWN: truncate sub-ustoc remainder.
			cosmosAmount := coin.Amount.Quo(types.ConversionMultiplier)
			if cosmosAmount.IsZero() {
				// SA-2026-06-02 MED-3: silent drop (matches godoc dust-burn
				// contract; replaces prior over-restrictive revert).
				continue
			}
			convertedAmt = convertedAmt.Add(sdk.NewCoin(k.getCosmosDenom(), cosmosAmount))
		} else if coin.Denom == k.getCosmosDenom() {
			convertedAmt = convertedAmt.Add(coin)
		} else {
			return nil, errorsmod.Wrapf(types.ErrInvalidDenom, "custom token %s cannot be transferred from EVM context", coin.Denom)
		}
	}
	return convertedAmt, nil
}

// convertAndValidateCoins is kept as a deprecated alias to the inflow variant.
// All keeper methods have been migrated to the directional helpers above.
// Deprecated: use convertCoinsForInflow or convertCoinsForOutflow per direction.
func (k EvmBankKeeper) convertAndValidateCoins(amt sdk.Coins) (sdk.Coins, error) {
	return k.convertCoinsForInflow(amt)
}

// SetDenomMetaData sets the metadata for a denom.
// RESTRICTION: Only allows writes for the native cosmos denom. EVM denom is computed, custom tokens are blocked.
func (k EvmBankKeeper) SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata) {
	if denomMetaData.Base == k.getEvmDenom() {
		return // EVM denom metadata is computed, not stored
	}
	if denomMetaData.Base != k.getCosmosDenom() {
		return // Block custom token metadata writes from EVM context
	}
	k.bankKeeper.SetDenomMetaData(ctx, denomMetaData)
}

// IterateAccountBalances iterates over all balances of an account.
// RESTRICTION: Skips custom tokens — only EVM denom (astoc) visible from EVM context.
func (k EvmBankKeeper) IterateAccountBalances(ctx context.Context, account sdk.AccAddress, cb func(coin sdk.Coin) bool) {
	k.bankKeeper.IterateAccountBalances(ctx, account, func(coin sdk.Coin) bool {
		// Skip custom tokens — only allow native cosmos denom
		if coin.Denom != k.getCosmosDenom() {
			return false
		}
		// Only emit the EVM representation (astoc) to avoid double-counting
		evmCoin, err := ConvertCosmosCoinToEvmCoin(coin)
		if err != nil {
			return false
		}
		return cb(evmCoin)
	})
}

// IterateAllBalances iterates over all balances of all accounts.
// RESTRICTION: Skips custom tokens — only EVM denom (astoc) visible from EVM context.
func (k EvmBankKeeper) IterateAllBalances(ctx context.Context, cb func(address sdk.AccAddress, coin sdk.Coin) (stop bool)) {
	k.bankKeeper.IterateAllBalances(ctx, func(address sdk.AccAddress, coin sdk.Coin) (stop bool) {
		// Skip custom tokens — only allow native cosmos denom
		if coin.Denom != k.getCosmosDenom() {
			return false
		}
		// Only emit the EVM representation (astoc) to avoid double-counting
		evmCoin, err := ConvertCosmosCoinToEvmCoin(coin)
		if err != nil {
			return false
		}
		return cb(address, evmCoin)
	})
}

// IterateTotalSupply iterates over the total supply of all denoms.
// RESTRICTION: Skips custom tokens — only EVM denom (astoc) visible from EVM context.
func (k EvmBankKeeper) IterateTotalSupply(ctx context.Context, cb func(coin sdk.Coin) bool) {
	k.bankKeeper.IterateTotalSupply(ctx, func(coin sdk.Coin) bool {
		// Skip custom tokens — only allow native cosmos denom
		if coin.Denom != k.getCosmosDenom() {
			return false
		}
		// Only emit the EVM representation (astoc) to avoid double-counting
		evmCoin, err := ConvertCosmosCoinToEvmCoin(coin)
		if err != nil {
			return false
		}
		return cb(evmCoin)
	})
}

// ConvertCosmosCoinToEvmCoin converts ustoc (6 decimals) to astoc (18 decimals)
// 1 ustoc = 10^12 astoc
// Returns error if denom is not the expected cosmos denom.
func ConvertCosmosCoinToEvmCoin(coin sdk.Coin) (sdk.Coin, error) {
	cosmosDenom := types.GetCosmosDenom()
	if coin.Denom != cosmosDenom {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrInvalidDenom, "expected %s, got %s", cosmosDenom, coin.Denom)
	}

	evmAmount := coin.Amount.Mul(types.ConversionMultiplier)
	return sdk.NewCoin(types.GetEvmDenom(), evmAmount), nil
}

// ConvertEvmCoinToCosmosCoin converts astoc (18 decimals) to ustoc (6 decimals)
// 10^12 astoc = 1 ustoc
// Truncates dust amounts (rounds down). Returns error if denom is invalid.
func ConvertEvmCoinToCosmosCoin(coin sdk.Coin) (sdk.Coin, error) {
	evmDenom := types.GetEvmDenom()
	if coin.Denom != evmDenom {
		return sdk.Coin{}, errorsmod.Wrapf(types.ErrInvalidDenom, "expected %s, got %s", evmDenom, coin.Denom)
	}

	// Truncate by dividing (automatically rounds down)
	cosmosAmount := coin.Amount.Quo(types.ConversionMultiplier)
	return sdk.NewCoin(types.GetCosmosDenom(), cosmosAmount), nil
}

// ConvertCosmosAmountToEvmAmount converts a raw amount from Cosmos decimals to EVM decimals
func ConvertCosmosAmountToEvmAmount(amount math.Int) math.Int {
	return amount.Mul(types.ConversionMultiplier)
}

// ConvertEvmAmountToCosmosAmount converts a raw amount from EVM decimals to Cosmos decimals
func ConvertEvmAmountToCosmosAmount(amount math.Int) math.Int {
	return amount.Quo(types.ConversionMultiplier)
}
