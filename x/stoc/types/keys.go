package types

const (
	// ModuleName defines the module name
	ModuleName = "stoc"
	// StoreKey defines the primary module store key
	StoreKey = ModuleName
	// MemStoreKey defines the in-memory store key
	MemStoreKey = "mem_stoc"

	// TokenKey defines the key for the token store
	TokenKey = "Token-"
)

// Event type and attribute keys
const (
	EventTypeCreateToken      = "create_token"
	EventTypeMintToken        = "mint_token"
	AttributeKeyTokenSymbol   = "token_symbol"
	AttributeKeyTokenName     = "token_name"
	AttributeKeyTokenCreator  = "token_creator"
	AttributeKeyInitialSupply = "initial_supply"
	AttributeKeyMintToken     = "mint_token"
)

var (
	ParamsKey = []byte("p_stoc")
)

func KeyPrefix(p string) []byte {
	return []byte(p)
}
