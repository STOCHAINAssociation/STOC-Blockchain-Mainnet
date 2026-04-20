package types

const (
	// ModuleName defines the module name
	ModuleName = "stoc"
	// StoreKey defines the primary module store key
	StoreKey = ModuleName
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

	// MaxAuthzUnwrapDepth caps the number of nested authz.MsgExec or group
	// proposal layers the custom-token ante decorators will unwrap when
	// looking for blocked message types. A single user authz grant is the
	// common case; deeper nesting is rejected to bound worst-case CPU cost
	// per transaction and to prevent an attacker from hiding a tax-bypassing
	// bank.MsgSend behind arbitrarily many wrappers.
	MaxAuthzUnwrapDepth = 3
)

// Event type and attribute keys
const (
	EventTypeCreateToken   = "create_token"
	EventTypeMintToken     = "mint_token"
	EventTypeBurnToken     = "burn_token"
	EventTypeReleaseTokens = "release_tokens"

	AttributeKeyTokenSymbol     = "token_symbol"
	AttributeKeyTokenName       = "token_name"
	AttributeKeyTokenCreator    = "token_creator"
	AttributeKeyInitialSupply   = "initial_supply"
	AttributeKeyMinimalDenom    = "minimal_denom"
	AttributeKeyMintAmount      = "mint_amount"
	AttributeKeyBurner          = "burner"
	AttributeKeyBurnAmount      = "burn_amount"
	AttributeKeyRecipient       = "recipient"
	AttributeKeyRemainingSupply = "remaining"
	AttributeKeyAmount          = "amount"
)

var (
	ParamsKey = []byte("p_stoc")
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}
