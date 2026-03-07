package keeper

import (
	"context"
	"fmt"
	"strings"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"stoc/x/evmutil/types"
)

// EvmBankKeeper wraps the BankKeeper to provide EVM-compatible denomination conversion
// between ustoc (6 decimals) and astoc (18 decimals) for mainnet
// or utstoc (6 decimals) and atstoc (18 decimals) for testnet
type EvmBankKeeper struct {
	bankKeeper types.BankKeeper
}

// NewEvmBankKeeper creates a new EvmBankKeeper
func NewEvmBankKeeper(bankKeeper types.BankKeeper) EvmBankKeeper {
	return EvmBankKeeper{
		bankKeeper: bankKeeper,
	}
}

// getEvmDenom returns the current EVM denom (dynamic based on chain config)
func getEvmDenom() string {
	return types.GetEvmDenom()
}

// getCosmosDenom returns the current Cosmos denom (dynamic based on chain config)
func getCosmosDenom() string {
	return types.GetCosmosDenom()
}

// GetBalance returns the balance of the given account for the given denom.
// For EVM denom (astoc/atstoc), it converts from Cosmos denom (ustoc/utstoc) balance.
// Custom tokens (created via STOC module) are NOT accessible from EVM.
func (k EvmBankKeeper) GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	evmDenom := types.GetEvmDenom()
	cosmosDenom := types.GetCosmosDenom()

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
// For EVM denom, it converts and uses Cosmos denom under the hood.
// Custom tokens (created via STOC module) are NOT transferable from EVM.
func (k EvmBankKeeper) SendCoins(ctx context.Context, from, to sdk.AccAddress, amt sdk.Coins) error {
	convertedAmt, err := k.convertAndValidateCoins(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoins(ctx, from, to, convertedAmt)
}

// MintCoins mints coins to the module account.
// For EVM denom, it converts to Cosmos denom.
// Custom tokens cannot be minted from EVM context.
func (k EvmBankKeeper) MintCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	convertedAmt, err := k.convertAndValidateCoins(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.MintCoins(ctx, moduleName, convertedAmt)
}

// BurnCoins burns coins from the module account.
// For EVM denom, it converts to Cosmos denom.
// Custom tokens cannot be burned from EVM context.
func (k EvmBankKeeper) BurnCoins(ctx context.Context, moduleName string, amt sdk.Coins) error {
	convertedAmt, err := k.convertAndValidateCoins(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.BurnCoins(ctx, moduleName, convertedAmt)
}

// SendCoinsFromModuleToAccount transfers coins from module to account.
// Custom tokens cannot be transferred from EVM context.
func (k EvmBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	convertedAmt, err := k.convertAndValidateCoins(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToAccount(ctx, senderModule, recipientAddr, convertedAmt)
}

// SendCoinsFromAccountToModule transfers coins from account to module.
// Custom tokens cannot be transferred from EVM context.
func (k EvmBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	convertedAmt, err := k.convertAndValidateCoins(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, senderAddr, recipientModule, convertedAmt)
}

// SpendableCoins returns the spendable balance for the given account.
// RESTRICTION: Only returns EVM-side representation (astoc) — no custom tokens, no double-counting.
func (k EvmBankKeeper) SpendableCoins(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	// Only return the EVM denom (astoc) — converted from the underlying cosmos denom (ustoc)
	cosmosBalance := k.bankKeeper.SpendableCoin(ctx, addr, getCosmosDenom())
	if cosmosBalance.IsPositive() {
		evmAmount := cosmosBalance.Amount.Mul(types.ConversionMultiplier)
		return sdk.NewCoins(sdk.NewCoin(getEvmDenom(), evmAmount))
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
	return strings.TrimPrefix(getCosmosDenom(), "u")
}

// GetDenomMetaData returns the metadata for a given denom.
// For EVM denom (astoc/atstoc), it returns custom metadata with 18 decimals.
func (k EvmBankKeeper) GetDenomMetaData(ctx context.Context, denom string) (banktypes.Metadata, bool) {
	if denom == getEvmDenom() {
		displayDenom := getDisplayDenom()
		displayUpper := strings.ToUpper(displayDenom)
		metadata := banktypes.Metadata{
			Description: displayUpper + " token in EVM format (18 decimals)",
			DenomUnits: []*banktypes.DenomUnit{
				{
					Denom:    getEvmDenom(),
					Exponent: 0,
					Aliases:  []string{getEvmDenom()},
				},
				{
					Denom:    displayDenom,
					Exponent: 18,
					Aliases:  []string{},
				},
			},
			Base:    getEvmDenom(),
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
	if denom == getEvmDenom() {
		// Get ustoc supply and convert to astoc
		cosmosSupply := k.bankKeeper.GetSupply(ctx, getCosmosDenom())
		evmCoin, err := ConvertCosmosCoinToEvmCoin(cosmosSupply)
		if err != nil {
			return sdk.NewCoin(getEvmDenom(), math.ZeroInt())
		}
		return evmCoin
	}
	if denom != getCosmosDenom() {
		// RESTRICTION: Return zero supply for custom tokens from EVM context
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	return k.bankKeeper.GetSupply(ctx, denom)
}

// IsSendEnabledCoin checks if a coin's denom is enabled for sending.
// For EVM denom (astoc), it checks the Cosmos denom (ustoc) instead.
// RESTRICTION: Custom tokens always return false from EVM context.
func (k EvmBankKeeper) IsSendEnabledCoin(ctx context.Context, coin sdk.Coin) bool {
	if coin.Denom == getEvmDenom() {
		// Check if ustoc is send enabled
		cosmosCoin := sdk.NewCoin(getCosmosDenom(), coin.Amount)
		return k.bankKeeper.IsSendEnabledCoin(ctx, cosmosCoin)
	}
	if coin.Denom != getCosmosDenom() {
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
		if coin.Denom == getEvmDenom() {
			cosmosCoin, err := ConvertEvmCoinToCosmosCoin(coin)
			if err != nil {
				return err
			}
			convertedCoins = append(convertedCoins, cosmosCoin)
		} else if coin.Denom == getCosmosDenom() {
			convertedCoins = append(convertedCoins, coin)
		} else {
			// RESTRICTION: Custom tokens are not send-enabled from EVM context
			return fmt.Errorf("custom token %s is not send-enabled from EVM context", coin.Denom)
		}
	}
	return k.bankKeeper.IsSendEnabledCoins(ctx, convertedCoins...)
}

// GetAllBalances returns all balances for an account.
// RESTRICTION: Only returns EVM-side representation (astoc) — no custom tokens, no double-counting.
func (k EvmBankKeeper) GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins {
	// Only return the EVM denom (astoc) — converted from the underlying cosmos denom (ustoc)
	cosmosBalance := k.bankKeeper.GetBalance(ctx, addr, getCosmosDenom())
	if cosmosBalance.IsPositive() {
		evmAmount := cosmosBalance.Amount.Mul(types.ConversionMultiplier)
		return sdk.NewCoins(sdk.NewCoin(getEvmDenom(), evmAmount))
	}
	return sdk.NewCoins()
}

// SpendableCoin returns the spendable balance for a specific denom.
// RESTRICTION: Custom tokens return zero balance from EVM context.
func (k EvmBankKeeper) SpendableCoin(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin {
	if denom == getEvmDenom() {
		cosmosBalance := k.bankKeeper.SpendableCoin(ctx, addr, getCosmosDenom())
		evmCoin, err := ConvertCosmosCoinToEvmCoin(cosmosBalance)
		if err != nil {
			return sdk.NewCoin(getEvmDenom(), math.ZeroInt())
		}
		return evmCoin
	}
	if denom != getCosmosDenom() {
		// RESTRICTION: Return zero balance for custom tokens from EVM context
		return sdk.NewCoin(denom, math.ZeroInt())
	}
	return k.bankKeeper.SpendableCoin(ctx, addr, denom)
}

// SendCoinsFromModuleToModule transfers coins between modules.
// Custom tokens cannot be transferred from EVM context.
func (k EvmBankKeeper) SendCoinsFromModuleToModule(ctx context.Context, senderModule string, recipientModule string, amt sdk.Coins) error {
	convertedAmt, err := k.convertAndValidateCoins(amt)
	if err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromModuleToModule(ctx, senderModule, recipientModule, convertedAmt)
}

// convertAndValidateCoins converts EVM coins to Cosmos coins with dust remainder and zero-amount validation.
// Rejects custom tokens and ensures converted amounts are non-zero.
func (k EvmBankKeeper) convertAndValidateCoins(amt sdk.Coins) (sdk.Coins, error) {
	convertedAmt := sdk.NewCoins()
	for _, coin := range amt {
		if coin.Denom == getEvmDenom() {
			// Reject amounts with dust remainder to prevent silent value loss
			remainder := coin.Amount.Mod(types.ConversionMultiplier)
			if !remainder.IsZero() {
				return nil, fmt.Errorf(
					"amount %s %s has dust remainder %s wei that would be lost in conversion. "+
						"Use multiples of %s wei (1 %s)",
					coin.Amount.String(), getEvmDenom(), remainder.String(),
					types.ConversionMultiplier.String(), getCosmosDenom(),
				)
			}
			cosmosCoin, err := ConvertEvmCoinToCosmosCoin(coin)
			if err != nil {
				return nil, err
			}
			if cosmosCoin.Amount.IsZero() {
				return nil, fmt.Errorf("amount too small: %s %s converts to 0 %s", coin.Amount.String(), getEvmDenom(), getCosmosDenom())
			}
			convertedAmt = convertedAmt.Add(cosmosCoin)
		} else if coin.Denom == getCosmosDenom() {
			convertedAmt = convertedAmt.Add(coin)
		} else {
			return nil, fmt.Errorf("custom token %s cannot be transferred from EVM context", coin.Denom)
		}
	}
	return convertedAmt, nil
}

// SetDenomMetaData sets the metadata for a denom.
func (k EvmBankKeeper) SetDenomMetaData(ctx context.Context, denomMetaData banktypes.Metadata) {
	// For astoc, we don't actually store it - it's computed from ustoc
	if denomMetaData.Base != getEvmDenom() {
		k.bankKeeper.SetDenomMetaData(ctx, denomMetaData)
	}
}

// IterateAccountBalances iterates over all balances of an account.
// RESTRICTION: Skips custom tokens — only EVM denom (astoc) visible from EVM context.
func (k EvmBankKeeper) IterateAccountBalances(ctx context.Context, account sdk.AccAddress, cb func(coin sdk.Coin) bool) {
	k.bankKeeper.IterateAccountBalances(ctx, account, func(coin sdk.Coin) bool {
		// Skip custom tokens — only allow native cosmos denom
		if coin.Denom != getCosmosDenom() {
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
		if coin.Denom != getCosmosDenom() {
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
		if coin.Denom != getCosmosDenom() {
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
	if coin.Denom != getCosmosDenom() {
		return sdk.Coin{}, fmt.Errorf("invalid denom for conversion to EVM coin: expected %s, got %s", getCosmosDenom(), coin.Denom)
	}

	evmAmount := coin.Amount.Mul(types.ConversionMultiplier)
	return sdk.NewCoin(getEvmDenom(), evmAmount), nil
}

// ConvertEvmCoinToCosmosCoin converts astoc (18 decimals) to ustoc (6 decimals)
// 10^12 astoc = 1 ustoc
// Truncates dust amounts (rounds down). Returns error if denom is invalid.
func ConvertEvmCoinToCosmosCoin(coin sdk.Coin) (sdk.Coin, error) {
	if coin.Denom != getEvmDenom() {
		return sdk.Coin{}, fmt.Errorf("invalid denom for conversion to Cosmos coin: expected %s, got %s", getEvmDenom(), coin.Denom)
	}

	// Truncate by dividing (automatically rounds down)
	cosmosAmount := coin.Amount.Quo(types.ConversionMultiplier)
	return sdk.NewCoin(getCosmosDenom(), cosmosAmount), nil
}

// ConvertCosmosAmountToEvmAmount converts a raw amount from Cosmos decimals to EVM decimals
func ConvertCosmosAmountToEvmAmount(amount math.Int) math.Int {
	return amount.Mul(types.ConversionMultiplier)
}

// ConvertEvmAmountToCosmosAmount converts a raw amount from EVM decimals to Cosmos decimals
func ConvertEvmAmountToCosmosAmount(amount math.Int) math.Int {
	return amount.Quo(types.ConversionMultiplier)
}
