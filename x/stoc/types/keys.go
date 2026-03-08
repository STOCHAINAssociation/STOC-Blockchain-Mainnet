package types

import "cosmossdk.io/math"

const (
	// ModuleName defines the module name
	ModuleName = "stoc"
	// StoreKey defines the primary module store key
	StoreKey = ModuleName
	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_stoc"

	// TokenKey defines the key for the token store
	TokenKey       = "Token-"
	TokenSymbolKey = "TokenSymbol-"

	// TokenCounterKey is the key for storing the token ID counter
	TokenCounterKey = "TokenCounter-"

	// Query pagination limits
	MaxQueryLimit     = 100
	DefaultQueryLimit = 20
	MaxBalancesResult = 200

	// MaxMultiSendOutputs limits outputs in MsgMultiSend to prevent DoS and tax bypass
	MaxMultiSendOutputs = 50
)

// Event type and attribute keys
const (
	EventTypeCreateToken      = "create_token"
	EventTypeMintToken        = "mint_token"
	EventTypeBurnToken        = "burn_token"
	AttributeKeyTokenSymbol   = "token_symbol"
	AttributeKeyTokenName     = "token_name"
	AttributeKeyTokenCreator  = "token_creator"
	AttributeKeyInitialSupply = "initial_supply"
	AttributeKeyMinimalDenom  = "minimal_denom"
	AttributeKeyMintToken       = "mint_token"
	AttributeKeyBurner          = "burner"
	AttributeKeyBurnAmount      = "burn_amount"
	EventTypeReleaseTokens      = "tokens_released"
	AttributeKeyRecipient       = "recipient"
	AttributeKeyRemainingSupply = "remaining"
	AttributeKeyAmount          = "amount"
)

var (
	ParamsKey = []byte("p_stoc")

	// TokenCreationFee is the fee (in ustoc) burned when creating a new token.
	// 100 STOC = 100_000_000 ustoc
	TokenCreationFee = math.NewInt(100_000_000)
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}
